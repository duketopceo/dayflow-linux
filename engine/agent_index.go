package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Agent chat indexing: every harness's conversation text lands in a local
// FTS5 index so "what did Codex do about X" is one query — for the chat
// tool, `dayflow ask`, and MCP alike. Design:
//
//   - agent_msgs_fts is a CONTENT-stored FTS5 table (unlike blocks_fts,
//     which is external-content over blocks — agent messages have no other
//     home, so the index is the store). text is indexed; source, session,
//     project, role, ts ride along as UNINDEXED columns for citation.
//   - Text is scrubbed at ingest (same scrubText the egress path uses),
//     so the index carries no secrets either.
//   - agent_ingest is the dedup tracker — sha(source|session|turn|ts) —
//     so re-running a day costs a scan but zero writes.
//   - meta agent_ingest_day is the watermark: the newest scanned day.
//     Each pass covers watermark..today (first run backfills ~30 days);
//     the watermark day always re-scans since sessions grow in place.
//   - ensureAgentIndex runs lazily like the derived FTS migrations —
//     a failure degrades search to "no index" rather than wedging.

const agentMsgsFTSDDL = `CREATE VIRTUAL TABLE IF NOT EXISTS agent_msgs_fts
USING fts5(text, source UNINDEXED, session UNINDEXED, project UNINDEXED,
           role UNINDEXED, ts UNINDEXED)`

const agentIngestDDL = `CREATE TABLE IF NOT EXISTS agent_ingest (
  k TEXT PRIMARY KEY, sess TEXT NOT NULL
)`

// agent_sess_fp remembers each indexed session's adapter fingerprint so a
// re-scan can skip unchanged sessions before decoding their turns — the
// decode (full transcript JSON unmarshal) is the expensive half of ingest.
const agentSessFPDDL = `CREATE TABLE IF NOT EXISTS agent_sess_fp (
  k TEXT PRIMARY KEY, fp TEXT NOT NULL
)`

// agentIngestBackfillDays bounds the first-run scan window — same order as
// the adapters' own discovery depth.
const agentIngestBackfillDays = 30

// metaAgentIngestDay records the newest day fully scanned; the next pass
// resumes from it (re-scanning catches sessions still in flight).
const metaAgentIngestDay = "agent_ingest_day"

// ingestMu serializes lazy ingest passes — a second search while one runs
// serves stale results instead of doubling the decode work.
var ingestMu sync.Mutex

func ensureAgentIndex(db *sql.DB) error {
	if _, err := db.Exec(agentMsgsFTSDDL); err != nil {
		return err
	}
	if _, err := db.Exec(agentIngestDDL); err != nil {
		return err
	}
	// The dedup table gained a sess column (per-session delete on
	// fingerprint change); a pre-column table is derived state — drop and
	// rebuild rather than migrate.
	if !tableHasColumn(db, "agent_ingest", "sess") {
		if _, err := db.Exec(`DROP TABLE agent_ingest`); err != nil {
			return err
		}
		if _, err := db.Exec(agentIngestDDL); err != nil {
			return err
		}
		db.Exec(`DELETE FROM agent_msgs_fts`)
		db.Exec(`DELETE FROM agent_sess_fp`)
		metaSet(db, metaAgentIngestDay, "")
		return nil
	}
	_, err := db.Exec(agentSessFPDDL)
	return err
}

// agentIndexPresent reports whether the FTS table exists — on a read-only
// handle this lets search degrade to empty hits instead of failing the
// CREATE DDL.
func agentIndexPresent(db *sql.DB) bool {
	var name string
	return db.QueryRow(`SELECT name FROM sqlite_master
	  WHERE type='table' AND name='agent_msgs_fts'`).Scan(&name) == nil
}

// dbReadOnly reports whether the connection is a query_only (mode=ro)
// handle — ingest and DDL are skipped rather than noisily failing.
func dbReadOnly(db *sql.DB) bool {
	var v int
	return db.QueryRow(`PRAGMA query_only`).Scan(&v) == nil && v == 1
}

func tableHasColumn(db *sql.DB, table, col string) bool {
	var name string
	return db.QueryRow(`SELECT name FROM pragma_table_info(?)
	  WHERE name=?`, table, col).Scan(&name) == nil
}

// resetAgentIndex wipes the derived agent index so `ingest --reindex`
// rebuilds from scratch — repair path for corruption or bulk removal.
func resetAgentIndex(db *sql.DB) error {
	if err := ensureAgentIndex(db); err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM agent_msgs_fts`,
		`DELETE FROM agent_ingest`,
		`DELETE FROM agent_sess_fp`,
	} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	metaSet(db, metaAgentIngestDay, "")
	return nil
}

// ingestKey is one turn's stable dedup identity.
func ingestKey(source, session string, idx int, ts int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d", source, session, idx, ts)))
	return hex.EncodeToString(h[:16])
}

// ingestAgentChats scans the watermark window and indexes any new turns.
// Idempotent; returns (indexedTurns, error).
func ingestAgentChats(db *sql.DB) (int, error) {
	if db == nil {
		return 0, nil
	}
	if err := ensureAgentIndex(db); err != nil {
		return 0, err
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	watermark := today.AddDate(0, 0, -agentIngestBackfillDays)
	if v := metaGet(db, metaAgentIngestDay); v != "" {
		// Resume from the last fully-scanned day (re-scanning it catches
		// sessions that were still in flight); never scan the future.
		if d, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil && !d.After(today) {
			watermark = d
		}
	}
	inserted := 0
	for d := watermark; !d.After(today); d = d.AddDate(0, 0, 1) {
		n, err := ingestAgentDay(db, d)
		if err != nil {
			return inserted, err
		}
		inserted += n
	}
	metaSet(db, metaAgentIngestDay, today.Format("2006-01-02"))
	return inserted, nil
}

// ingestAgentDay indexes one day's turns for all sources. Transcript
// decode (Turns) happens outside any transaction — it can take seconds
// per session — so the write tx is only held for the inserts themselves.
// A long-lived tx would hit SQLITE_BUSY_SNAPSHOT whenever the capture
// daemon commits mid-ingest; per-session txs stay short enough that the
// occasional collision is retried cheaply.
func ingestAgentDay(db *sql.DB, d time.Time) (int, error) {
	srcs, sessions, _ := scanAgentDay(db, d)
	defer closeAgentSources(srcs)

	inserted := 0
	for _, sess := range sessions {
		src := agentSourceNamed(srcs, sess.Source)
		if src == nil {
			continue
		}
		// Session-level skip: an unchanged fingerprint means every turn
		// was already indexed — skip the Turns decode entirely. The fp
		// row is only written after the session's turns are ingested.
		sessKey := ingestKey(sess.Source, sess.File, -1, 0)
		var sessFP string
		fpOK := false
		fpChanged := false
		if fp, ok := src.Fingerprint(sess); ok {
			sessFP = fmt.Sprintf("%d:%d", fp.Mtime, fp.Size)
			fpOK = true
			var prev string
			prevErr := db.QueryRow(`SELECT fp FROM agent_sess_fp WHERE k=?`, sessKey).Scan(&prev)
			if prevErr == nil && prev == sessFP {
				continue
			}
			// Fingerprint moved (or is new): an edited session's old
			// turns are deleted before re-insert so stale text can't
			// linger in the index (fp change on a previously-ingested
			// session only — a new session has nothing to delete).
			fpChanged = prevErr == nil
		}
		// Decode + normalize first, write after — keeps the tx short.
		var pending []agentIndexRow
		for i, t := range src.Turns(sess) {
			role := turnRole(t)
			if role == "" {
				continue
			}
			// Bound before scrubbing: transcripts carry multi-MB single
			// lines (minified blobs, data URIs) that send the secret
			// regexes' backtracker spinning — cap first so per-turn cost
			// stays constant. Scrub still runs before storage so the
			// index retains no pasted secrets.
			text := truncate(strings.Join(strings.Fields(
				stripCtl(scrubText(boundForScrub(t.text, 8000)))), " "), 4000)
			if text == "" {
				continue
			}
			pending = append(pending, agentIndexRow{ingestKey(sess.Source, sess.File, i, t.unixTs), text, role, t.unixTs})
		}
		// No usable turns and no fingerprint to stamp: nothing to record.
		// An empty decode deliberately gets NO fp row — Turns returns nil
		// on transient read failure as well as genuine emptiness, and
		// stamping would seal a failed decode until the file next grows.
		if len(pending) == 0 {
			continue
		}
		n, err := writeAgentSessionRows(db, pending, sessKey, sessFP, fpOK, fpChanged, sess)
		if err != nil {
			return inserted, err
		}
		inserted += n
	}
	return inserted, nil
}

// boundForScrub caps text before the scrub regexes run. When the cap cuts
// mid-token the trailing fragment is dropped — a severed credential prefix
// would neither match the patterns nor be useful content.
func boundForScrub(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := truncate(s, n)
	if i := strings.LastIndexAny(strings.TrimRight(cut, "."), " \t\n"); i > 0 {
		cut = cut[:i]
	}
	return cut
}

// agentIndexRow is one normalized turn pending insertion.
type agentIndexRow struct {
	key, text, role string
	ts              int64
}

// writeAgentSessionRows commits one session's normalized turns plus its
// fingerprint watermark in a single short transaction, retried once on a
// busy snapshot from a concurrent daemon write.
func writeAgentSessionRows(db *sql.DB, pending []agentIndexRow, sessKey, sessFP string, fpOK, fpChanged bool, sess AgentSession) (int, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		inserted, err := func() (int, error) {
			tx, err := db.Begin()
			if err != nil {
				return 0, err
			}
			defer tx.Rollback()
			if fpChanged {
				// Session was edited in place: drop its previously
				// indexed turns and dedup keys so new text fully
				// replaces old.
				if _, err := tx.Exec(`DELETE FROM agent_msgs_fts WHERE session=?`, sess.File); err != nil {
					return 0, err
				}
				if _, err := tx.Exec(`DELETE FROM agent_ingest WHERE sess=?`, sessKey); err != nil {
					return 0, err
				}
			}
			inserted := 0
			for _, r := range pending {
				res, err := tx.Exec(`INSERT OR IGNORE INTO agent_ingest(k, sess) VALUES(?,?)`, r.key, sessKey)
				if err != nil {
					return inserted, err
				}
				if n, _ := res.RowsAffected(); n == 0 {
					continue // already indexed
				}
				if _, err := tx.Exec(`INSERT INTO agent_msgs_fts
				  (text, source, session, project, role, ts) VALUES(?,?,?,?,?,?)`,
					r.text, sess.Source, sess.File, sess.Project, r.role, r.ts); err != nil {
					return inserted, err
				}
				inserted++
			}
			if fpOK {
				if _, err := tx.Exec(`INSERT OR REPLACE INTO agent_sess_fp(k, fp) VALUES(?,?)`, sessKey, sessFP); err != nil {
					return inserted, err
				}
			}
			return inserted, tx.Commit()
		}()
		if err == nil {
			return inserted, nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "busy") {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return 0, lastErr
}

// agentSearchHit is one FTS match with its citation metadata.
type agentSearchHit struct {
	Session string `json:"session"`
	Source  string `json:"source"`
	Project string `json:"project,omitempty"`
	Role    string `json:"role"`
	Ts      int64  `json:"ts"`
	Snippet string `json:"snippet"`
}

const (
	agentSearchHitLimit     = 20 // raw matches before grouping
	agentSearchSessionLimit = 8  // distinct sessions in the result
)

// ftsQuery turns free text into a safe FTS5 expression: each word becomes
// a double-quoted term (so MATCH syntax in user input can't break the
// parse), joined with explicit AND — adjacent quoted strings would parse
// as a phrase, requiring word adjacency the user never asked for.
func ftsQuery(q string) string {
	var terms []string
	for _, w := range strings.Fields(q) {
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			terms = append(terms, `"`+w+`"`)
		}
	}
	return strings.Join(terms, ` AND `)
}

// searchAgentSessions answers "what did my agents do/say" queries: FTS
// keyword match → grouped per session, bounded for chat-context budgets.
// Local-only — no egress; the model sees the returned snippets, not raw
// transcripts.
func searchAgentSessions(db *sql.DB, cfg Config, query string) ([]agentSearchHit, error) {
	if db == nil {
		return nil, fmt.Errorf("no database")
	}
	q := ftsQuery(query)
	if q == "" {
		return nil, fmt.Errorf("searchAgentSessions requires a query")
	}
	// Read-only handles (MCP --read-only) never create the index or
	// ingest — absent tables degrade to empty hits, matching the
	// derived-index "no index" posture rather than erroring.
	if dbReadOnly(db) {
		if !agentIndexPresent(db) {
			return nil, nil
		}
	} else if err := ensureAgentIndex(db); err != nil {
		return nil, err
	}
	// Lazy freshness: a stale watermark kicks off a background ingest and
	// search proceeds on what exists — a synchronous refresh would block
	// a chat turn for minutes while transcripts decode. TryLock keeps
	// concurrent searchers from piling up ingest passes.
	if v := metaGet(db, metaAgentIngestDay); v != todayStr(time.Now()) && !dbReadOnly(db) && ingestMu.TryLock() {
		go func() {
			defer ingestMu.Unlock()
			if _, err := ingestAgentChats(db); err != nil {
				debugf(cfg, "agent index refresh failed: %v", err)
			}
		}()
	}
	rows, err := db.Query(`SELECT session, source, project, role, ts,
	  snippet(agent_msgs_fts, 0, '«', '»', '…', 24)
	  FROM agent_msgs_fts WHERE agent_msgs_fts MATCH ?
	  ORDER BY bm25(agent_msgs_fts) LIMIT ?`, q, agentSearchHitLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []agentSearchHit
	seen := map[string]bool{}
	for rows.Next() {
		var h agentSearchHit
		if err := rows.Scan(&h.Session, &h.Source, &h.Project, &h.Role, &h.Ts, &h.Snippet); err != nil {
			return hits, err
		}
		// Citation fields are raw session paths/project names — scrub
		// before they can reach CLI output or a provider request.
		if seen[h.Session] {
			h.Session, h.Project = scrubText(h.Session), scrubText(h.Project)
			hits = append(hits, h)
			continue
		}
		if len(seen) >= agentSearchSessionLimit {
			break
		}
		seen[h.Session] = true
		h.Session, h.Project = scrubText(h.Session), scrubText(h.Project)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func todayStr(t time.Time) string {
	return t.Local().Format("2006-01-02")
}

// printAgentSearch renders search hits for the CLI.
func printAgentSearch(db *sql.DB, cfg Config, query string, jsonOut bool) {
	hits, err := searchAgentSessions(db, cfg, query)
	if err != nil {
		fatal(err)
	}
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"query": query, "hits": hits})
		return
	}
	if len(hits) == 0 {
		fmt.Println("no agent session matches")
		return
	}
	for _, h := range hits {
		fmt.Printf("%s [%s] %s\n  %s\n",
			time.Unix(h.Ts, 0).Format("2006-01-02 15:04"), h.Source, h.Project,
			strings.ReplaceAll(h.Snippet, "\n", " "))
	}
}
