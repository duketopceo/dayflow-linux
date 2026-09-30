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
//   - meta agent_ingest_day is the watermark: the oldest unscanned day.
//     Each pass covers watermark..today (first run backfills the discovery
//     window); today and yesterday always re-scan since sessions grow.
//   - ensureAgentIndex runs lazily like the derived FTS migrations —
//     a failure degrades search to "no index" rather than wedging.

const agentMsgsFTSDDL = `CREATE VIRTUAL TABLE IF NOT EXISTS agent_msgs_fts
USING fts5(text, source UNINDEXED, session UNINDEXED, project UNINDEXED,
           role UNINDEXED, ts UNINDEXED)`

const agentIngestDDL = `CREATE TABLE IF NOT EXISTS agent_ingest (
  k TEXT PRIMARY KEY
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

// metaAgentIngestDay marks the oldest day not yet fully scanned.
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
	_, err := db.Exec(agentSessFPDDL)
	return err
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
		if d, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil && d.Before(today) {
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
		if fp, ok := src.Fingerprint(sess); ok {
			sessFP = fmt.Sprintf("%d:%d", fp.Mtime, fp.Size)
			fpOK = true
			var prev string
			if db.QueryRow(`SELECT fp FROM agent_sess_fp WHERE k=?`, sessKey).Scan(&prev) == nil && prev == sessFP {
				continue
			}
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
				stripCtl(scrubText(truncate(t.text, 8000)))), " "), 4000)
			if text == "" {
				continue
			}
			pending = append(pending, agentIndexRow{ingestKey(sess.Source, sess.File, i, t.unixTs), text, role, t.unixTs})
		}
		if len(pending) == 0 && !fpOK {
			continue
		}
		n, err := writeAgentSessionRows(db, pending, sessKey, sessFP, fpOK, sess)
		if err != nil {
			return inserted, err
		}
		inserted += n
	}
	return inserted, nil
}

// agentIndexRow is one normalized turn pending insertion.
type agentIndexRow struct {
	key, text, role string
	ts              int64
}

// writeAgentSessionRows commits one session's normalized turns plus its
// fingerprint watermark in a single short transaction, retried once on a
// busy snapshot from a concurrent daemon write.
func writeAgentSessionRows(db *sql.DB, pending []agentIndexRow, sessKey, sessFP string, fpOK bool, sess AgentSession) (int, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		inserted, err := func() (int, error) {
			tx, err := db.Begin()
			if err != nil {
				return 0, err
			}
			defer tx.Rollback()
			inserted := 0
			for _, r := range pending {
				res, err := tx.Exec(`INSERT OR IGNORE INTO agent_ingest(k) VALUES(?)`, r.key)
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
// a double-quoted term so MATCH syntax in user input can't break the parse.
func ftsQuery(q string) string {
	var terms []string
	for _, w := range strings.Fields(q) {
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			terms = append(terms, `"`+w+`"`)
		}
	}
	return strings.Join(terms, " ")
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
	if err := ensureAgentIndex(db); err != nil {
		return nil, err
	}
	// Lazy freshness: a stale watermark kicks off a background ingest and
	// search proceeds on what exists — a synchronous refresh would block
	// a chat turn for minutes while transcripts decode. TryLock keeps
	// concurrent searchers from piling up ingest passes.
	if v := metaGet(db, metaAgentIngestDay); v != todayStr(time.Now()) && ingestMu.TryLock() {
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
		if seen[h.Session] {
			hits = append(hits, h)
			continue
		}
		if len(seen) >= agentSearchSessionLimit {
			break
		}
		seen[h.Session] = true
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
