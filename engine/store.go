package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// schemaVersion is the highest migration this binary knows how to apply.
// Bump it and add an applyMigration case when the schema changes.
const schemaVersion = 3

// schema is the base (v1) schema: capture and journal tables only.
const schema = `
CREATE TABLE IF NOT EXISTS frames (
  id   INTEGER PRIMARY KEY,
  ts   INTEGER NOT NULL,
  path TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS frames_ts ON frames(ts);

CREATE TABLE IF NOT EXISTS blocks (
  start_ts    INTEGER PRIMARY KEY,
  end_ts      INTEGER NOT NULL,
  title       TEXT NOT NULL DEFAULT '',
  summary     TEXT NOT NULL DEFAULT '',
  category    TEXT NOT NULL DEFAULT '',
  frame_count INTEGER NOT NULL DEFAULT 0,
  status      TEXT NOT NULL DEFAULT 'done',
  error       TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  productive  INTEGER DEFAULT NULL
);
CREATE INDEX IF NOT EXISTS blocks_status ON blocks(status);

-- Full audit log: every capture decision, pause change, summarizer run, error.
CREATE TABLE IF NOT EXISTS events (
  id     INTEGER PRIMARY KEY,
  ts     INTEGER NOT NULL,
  type   TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS events_ts ON events(ts);

-- One row per OpenRouter call: cost accounting + failure forensics.
CREATE TABLE IF NOT EXISTS api_calls (
  id                INTEGER PRIMARY KEY,
  ts                INTEGER NOT NULL,
  block_start       INTEGER NOT NULL,
  model             TEXT NOT NULL,
  frames_sent       INTEGER NOT NULL,
  prompt_tokens     INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  latency_ms        INTEGER NOT NULL DEFAULT 0,
  status            TEXT NOT NULL DEFAULT 'ok',
  error             TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS api_calls_ts ON api_calls(ts);

-- Small key/value scratch table for engine bookkeeping (e.g. one-time
-- backfill flags). Created in the base schema so every open converges.
CREATE TABLE IF NOT EXISTS meta (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL DEFAULT ''
);
`

// schemaV2 is migration version 2: chat, standup, journal, goals, LLM-call
// logging, and block-edit overlay tables.
const schemaV2 = `
-- Conversations for the chat-with-your-journal feature.
CREATE TABLE IF NOT EXISTS chat_conversations (
  id         INTEGER PRIMARY KEY,
  title      TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS chat_conversations_updated ON chat_conversations(updated_at);

CREATE TABLE IF NOT EXISTS chat_messages (
  id              INTEGER PRIMARY KEY,
  conversation_id INTEGER NOT NULL,
  role            TEXT NOT NULL,
  content         TEXT NOT NULL,
  tool_calls      TEXT NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  FOREIGN KEY (conversation_id) REFERENCES chat_conversations(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS chat_messages_conversation ON chat_messages(conversation_id);

-- Editable standup drafts keyed by date (YYYY-MM-DD).
CREATE TABLE IF NOT EXISTS standup_drafts (
  date       TEXT PRIMARY KEY,
  highlights TEXT NOT NULL DEFAULT '',
  tasks      TEXT NOT NULL DEFAULT '',
  blockers   TEXT NOT NULL DEFAULT '',
  priorities TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL
);

-- Journal beta: morning/evening notes and AI summary.
CREATE TABLE IF NOT EXISTS journal_entries (
  date       TEXT PRIMARY KEY,
  morning    TEXT NOT NULL DEFAULT '',
  evening    TEXT NOT NULL DEFAULT '',
  summary    TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS day_goals (
  id         INTEGER PRIMARY KEY,
  date       TEXT NOT NULL,
  goal       TEXT NOT NULL,
  completed  INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS day_goals_date ON day_goals(date);

-- Generic log for all LLM calls (chat, review, standup, etc.).
CREATE TABLE IF NOT EXISTS llm_calls (
  id                INTEGER PRIMARY KEY,
  ts                INTEGER NOT NULL,
  task              TEXT NOT NULL DEFAULT '',
  provider          TEXT NOT NULL DEFAULT '',
  model             TEXT NOT NULL DEFAULT '',
  prompt_tokens     INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  latency_ms        INTEGER NOT NULL DEFAULT 0,
  status            TEXT NOT NULL DEFAULT 'ok',
  error             TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS llm_calls_ts ON llm_calls(ts);

-- User edits to timeline blocks.
CREATE TABLE IF NOT EXISTS block_edits (
  id         INTEGER PRIMARY KEY,
  start_ts   INTEGER NOT NULL,
  field      TEXT NOT NULL,
  old_value  TEXT NOT NULL DEFAULT '',
  new_value  TEXT NOT NULL DEFAULT '',
  edited_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS block_edits_start_ts ON block_edits(start_ts);
`

// schemaV3 enforces one goal per day: drop duplicate rows (keeping the most
// recently inserted) and replace the plain date index with a unique one so
// concurrent upserts can't create dupes.
const schemaV3 = `
DELETE FROM day_goals WHERE id NOT IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY date ORDER BY completed DESC, id DESC
    ) rn FROM day_goals
  ) WHERE rn = 1
);
DROP INDEX IF EXISTS day_goals_date;
CREATE UNIQUE INDEX IF NOT EXISTS day_goals_date ON day_goals(date);
`

// columnPatches adds columns to databases created before the columns existed.
// Each is applied only when the column is actually missing.
var columnPatches = []struct {
	table, column, ddl string
}{
	{"frames", "app", `ALTER TABLE frames ADD COLUMN app TEXT NOT NULL DEFAULT ''`},
	{"frames", "bytes", `ALTER TABLE frames ADD COLUMN bytes INTEGER NOT NULL DEFAULT 0`},
	{"blocks", "attempts", `ALTER TABLE blocks ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0`},
	{"blocks", "app", `ALTER TABLE blocks ADD COLUMN app TEXT NOT NULL DEFAULT ''`},
	{"blocks", "activities", `ALTER TABLE blocks ADD COLUMN activities TEXT NOT NULL DEFAULT ''`},
	{"blocks", "productive", `ALTER TABLE blocks ADD COLUMN productive INTEGER DEFAULT NULL`},
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func applyColumnPatches(db *sql.DB) error {
	for _, p := range columnPatches {
		has, err := hasColumn(db, p.table, p.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := db.Exec(p.ddl); err != nil {
			return fmt.Errorf("add column %s.%s: %w", p.table, p.column, err)
		}
	}
	return nil
}

// applyMigration runs one schema version's statements plus its version stamp
// in a single transaction.
func applyMigration(db *sql.DB, v int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	switch v {
	case 2:
		if _, err := tx.Exec(schemaV2); err != nil {
			return err
		}
	case 3:
		if _, err := tx.Exec(schemaV3); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no migration defined for schema version %d", v)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, v, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// migrate brings the database to schemaVersion. It never drops data and
// returns the first real error rather than continuing on a partial schema.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	var cur int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&cur); err != nil {
		return err
	}
	if cur > schemaVersion {
		return fmt.Errorf("database schema v%d is newer than this binary supports (v%d)", cur, schemaVersion)
	}
	// Column patches are idempotent (guarded by hasColumn) and cheap; run them
	// on every open so any pre-versioning install converges.
	if err := applyColumnPatches(db); err != nil {
		return err
	}
	if cur == 0 {
		// Unversioned install (fresh or pre-1.0.1): the base schema plus
		// column patches above constitute v1.
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(1, ?)`, time.Now().Unix()); err != nil {
			return err
		}
		cur = 1
	}
	for v := cur + 1; v <= schemaVersion; v++ {
		if err := applyMigration(db, v); err != nil {
			return fmt.Errorf("migration to schema v%d: %w", v, err)
		}
	}
	return nil
}

// dbSchemaVersion reports the recorded schema version, or 0 when the database
// has never been versioned (unmigrated or absent).
func dbSchemaVersion(db *sql.DB) int {
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v); err != nil {
		return 0
	}
	return v
}

func openDB() (*sql.DB, error) {
	if err := os.MkdirAll(dataDir(), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath()+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// openDBReadOnly opens the database without creating dirs or running
// migrations — used by `mcp --read-only` so a read-only agent session can
// never write, even via schema changes. Errors if the database is missing.
func openDBReadOnly() (*sql.DB, error) {
	if _, err := os.Stat(dbPath()); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", "file:"+dbPath()+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
}

func insertFrame(db *sql.DB, ts time.Time, path string) error {
	_, err := db.Exec(`INSERT INTO frames(ts, path) VALUES(?, ?)`, ts.Unix(), path)
	return err
}

func insertFrameApp(db *sql.DB, ts time.Time, path, app string, bytes int64) error {
	_, err := db.Exec(`INSERT INTO frames(ts, path, app, bytes) VALUES(?, ?, ?, ?)`, ts.Unix(), path, app, bytes)
	return err
}

// dominantApp returns the most common non-empty app among frames in [start,end).
func dominantApp(db *sql.DB, start, end time.Time) string {
	var app string
	db.QueryRow(`SELECT app FROM frames WHERE ts >= ? AND ts < ? AND app != ''
	  GROUP BY app ORDER BY COUNT(1) DESC LIMIT 1`, start.Unix(), end.Unix()).Scan(&app)
	return app
}

// framesBetween returns frame rows in [start, end).
func framesBetween(db *sql.DB, start, end time.Time) ([]struct {
	TS   int64
	Path string
}, error) {
	rows, err := db.Query(`SELECT ts, path FROM frames WHERE ts >= ? AND ts < ? ORDER BY ts`,
		start.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		TS   int64
		Path string
	}
	for rows.Next() {
		var r struct {
			TS   int64
			Path string
		}
		if err := rows.Scan(&r.TS, &r.Path); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func deleteFrames(db *sql.DB, start, end time.Time) error {
	_, err := db.Exec(`DELETE FROM frames WHERE ts >= ? AND ts < ?`, start.Unix(), end.Unix())
	return err
}

func upsertBlock(db *sql.DB, start, end time.Time, title, summary, category string, frameCount int, status, errStr string) error {
	_, err := db.Exec(`INSERT INTO blocks(start_ts,end_ts,title,summary,category,frame_count,status,error,created_at)
	  VALUES(?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(start_ts) DO UPDATE SET end_ts=excluded.end_ts, title=excluded.title,
	    summary=excluded.summary, category=excluded.category, frame_count=excluded.frame_count,
	    status=excluded.status, error=excluded.error`,
		start.Unix(), end.Unix(), title, summary, category, frameCount, status, errStr, time.Now().Unix())
	return err
}

// upsertBlockFull additionally stores the dominant app and per-app activities JSON.
func upsertBlockFull(db *sql.DB, start, end time.Time, title, summary, category, app, activities string, frameCount, attempts int, status, errStr string, productive *bool) error {
	prodArg := sql.NullBool{Valid: productive != nil}
	if productive != nil {
		prodArg.Bool = *productive
	}
	_, err := db.Exec(`INSERT INTO blocks(start_ts,end_ts,title,summary,category,app,activities,frame_count,attempts,status,error,created_at,productive)
	  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(start_ts) DO UPDATE SET end_ts=excluded.end_ts, title=excluded.title,
	    summary=excluded.summary, category=excluded.category, app=excluded.app,
	    activities=excluded.activities, frame_count=excluded.frame_count, attempts=excluded.attempts,
	    status=excluded.status, error=excluded.error, productive=excluded.productive`,
		start.Unix(), end.Unix(), title, summary, category, app, activities, frameCount, attempts, status, errStr, time.Now().Unix(), prodArg)
	return err
}

// flagFailedBlock gives dead/failed blocks a visible identity at read time
// (DB rows stay untouched): they render as "Recording failed" entries so
// gaps in timelines and exports are explainable instead of invisible.
func flagFailedBlock(b *Block) {
	if b.Status == "done" {
		return
	}
	b.Title = "Recording failed"
	b.Category = "failed"
	if b.Summary == "" && b.Error != "" {
		b.Summary = strings.SplitN(b.Error, "\n", 2)[0]
	}
}

func blockExists(db *sql.DB, start time.Time) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(1) FROM blocks WHERE start_ts = ? AND status IN ('done','dead')`, start.Unix()).Scan(&n)
	return n > 0, err
}

// blockAttempts returns the retry count for a (usually failed) block.
func blockAttempts(db *sql.DB, start time.Time) int {
	var n int
	db.QueryRow(`SELECT attempts FROM blocks WHERE start_ts = ?`, start.Unix()).Scan(&n)
	return n
}

func resetFailedBlocks(db *sql.DB) (int64, error) {
	res, err := db.Exec(`DELETE FROM blocks WHERE status IN ('failed','dead')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type Activity struct {
	App        string `json:"app"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Category   string `json:"category"`
	Productive *bool  `json:"productive,omitempty"`
}

type Block struct {
	Start      time.Time  `json:"-"`
	End        time.Time  `json:"-"`
	StartTs    int64      `json:"start_ts"`
	EndTs      int64      `json:"end_ts"`
	StartStr   string     `json:"start"`
	EndStr     string     `json:"end"`
	Title      string     `json:"title"`
	Summary    string     `json:"summary"`
	Category   string     `json:"category"`
	App        string     `json:"app"`
	AppName    string     `json:"app_name"`
	Productive *bool      `json:"productive,omitempty"`
	Activities []Activity `json:"activities,omitempty"`
	FrameCount int        `json:"frame_count"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
}

// IsProductive returns true for blocks the LLM flagged as productive, or
// falls back to the legacy category/app heuristic for older blocks.
func (b Block) IsProductive() bool {
	if b.Productive != nil {
		return *b.Productive
	}
	return !isDistractionCategory(b.Category) && !isDistractionApp(b.App)
}

func blocksForDay(db *sql.DB, day time.Time, desc bool) ([]Block, error) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	end := start.Add(24 * time.Hour)
	order := "ASC"
	if desc {
		order = "DESC"
	}
	q := `SELECT start_ts,end_ts,title,summary,category,frame_count,app,activities,productive,status,COALESCE(error,'') FROM blocks
	  WHERE start_ts >= ? AND start_ts < ? AND status IN ('done','dead','failed') ORDER BY start_ts ` + order
	rows, err := db.Query(q, start.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Block
	for rows.Next() {
		var b Block
		var s, e int64
		var acts string
		var prod sql.NullBool
		if err := rows.Scan(&s, &e, &b.Title, &b.Summary, &b.Category, &b.FrameCount, &b.App, &acts, &prod, &b.Status, &b.Error); err != nil {
			return nil, err
		}
		if prod.Valid {
			b.Productive = &prod.Bool
		}
		if acts != "" {
			json.Unmarshal([]byte(acts), &b.Activities)
		}
		flagFailedBlock(&b)
		b.Start = time.Unix(s, 0).Local()
		b.End = time.Unix(e, 0).Local()
		b.StartTs = s
		b.EndTs = e
		b.StartStr = b.Start.Format("3:04 PM")
		b.EndStr = b.End.Format("3:04 PM")
		b.AppName = appDisplayName(b.App)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applyBlockEdits(db, out, start.Unix(), end.Unix())
}

func countFramesToday(db *sql.DB, now time.Time) (int, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var n int
	err := db.QueryRow(`SELECT COUNT(1) FROM frames WHERE ts >= ?`, start.Unix()).Scan(&n)
	return n, err
}

func countBlocksToday(db *sql.DB, now time.Time) (int, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var n int
	err := db.QueryRow(`SELECT COUNT(1) FROM blocks WHERE start_ts >= ? AND status='done'`, start.Unix()).Scan(&n)
	return n, err
}

func lastFrameTS(db *sql.DB) (int64, error) {
	var ts int64
	err := db.QueryRow(`SELECT COALESCE(MAX(ts),0) FROM frames`).Scan(&ts)
	return ts, err
}

func logEvent(db *sql.DB, typ, detail string) {
	if db == nil {
		return
	}
	db.Exec(`INSERT INTO events(ts, type, detail) VALUES(?,?,?)`, time.Now().Unix(), typ, detail)
}

func logAPICall(db *sql.DB, blockStart time.Time, model string, framesSent, promptTok, completionTok, latencyMs int, status, errStr string) {
	if db == nil {
		return
	}
	db.Exec(`INSERT INTO api_calls(ts, block_start, model, frames_sent, prompt_tokens, completion_tokens, latency_ms, status, error)
	  VALUES(?,?,?,?,?,?,?,?,?)`,
		time.Now().Unix(), blockStart.Unix(), model, framesSent, promptTok, completionTok, latencyMs, status, errStr)
}

type usageRow struct {
	Calls      int `json:"calls"`
	OK         int `json:"ok"`
	Failed     int `json:"failed"`
	PromptTok  int `json:"prompt_tokens"`
	ComplTok   int `json:"completion_tokens"`
}

// usageGroup aggregates one ledger grouped by a column. The select must
// return (key, calls, ok, failed, prompt_tokens, completion_tokens).
func usageGroup(db *sql.DB, query string) (map[string]usageRow, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]usageRow{}
	for rows.Next() {
		var k string
		var r usageRow
		if err := rows.Scan(&k, &r.Calls, &r.OK, &r.Failed, &r.PromptTok, &r.ComplTok); err != nil {
			return nil, err
		}
		if k == "" {
			k = "unknown"
		}
		out[k] = r
	}
	return out, rows.Err()
}

const usageGroupSelect = `SELECT %s, COUNT(1),
  COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN status!='ok' THEN 1 ELSE 0 END),0),
  COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0)
  FROM %s GROUP BY %s`

// usageSummary aggregates both LLM ledgers: api_calls (block summarization,
// always OpenRouter) and llm_calls (chat/review/standup, any provider).
func usageSummary(db *sql.DB) (map[string]any, error) {
	var calls, pt, ct, okn, failed int
	if err := db.QueryRow(`SELECT COUNT(1), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
	  COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status!='ok' THEN 1 ELSE 0 END),0)
	  FROM api_calls`).Scan(&calls, &pt, &ct, &okn, &failed); err != nil {
		return nil, err
	}
	var lcalls, lpt, lct, lok, lfailed int
	db.QueryRow(`SELECT COUNT(1), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
	  COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status!='ok' THEN 1 ELSE 0 END),0)
	  FROM llm_calls`).Scan(&lcalls, &lpt, &lct, &lok, &lfailed)

	byTask, err := usageGroup(db, fmt.Sprintf(usageGroupSelect, "task", "llm_calls", "task"))
	if err != nil {
		return nil, err
	}
	if calls > 0 {
		r := byTask["summarize"]
		r.Calls += calls
		r.OK += okn
		r.Failed += failed
		r.PromptTok += pt
		r.ComplTok += ct
		byTask["summarize"] = r
	}
	byProvider, err := usageGroup(db, fmt.Sprintf(usageGroupSelect, "provider", "llm_calls", "provider"))
	if err != nil {
		return nil, err
	}
	if calls > 0 {
		r := byProvider["openrouter"]
		r.Calls += calls
		r.OK += okn
		r.Failed += failed
		r.PromptTok += pt
		r.ComplTok += ct
		byProvider["openrouter"] = r
	}
	byModel := map[string]usageRow{}
	for _, tbl := range []string{"api_calls", "llm_calls"} {
		g, err := usageGroup(db, fmt.Sprintf(usageGroupSelect, "model", tbl, "model"))
		if err != nil {
			return nil, err
		}
		for k, r := range g {
			m := byModel[k]
			m.Calls += r.Calls
			m.OK += r.OK
			m.Failed += r.Failed
			m.PromptTok += r.PromptTok
			m.ComplTok += r.ComplTok
			byModel[k] = m
		}
	}
	return map[string]any{
		"api_calls": calls, "ok": okn, "failed": failed,
		"prompt_tokens": pt, "completion_tokens": ct,
		"other_llm_calls": lcalls, "other_ok": lok, "other_failed": lfailed,
		"other_prompt_tokens": lpt, "other_completion_tokens": lct,
		"total_prompt_tokens": pt + lpt, "total_completion_tokens": ct + lct,
		"breakdown": map[string]any{
			"by_task": byTask, "by_provider": byProvider, "by_model": byModel,
		},
	}, nil
}

// framesBefore deletes frame rows (and optionally files) older than cutoff.
func framesBefore(db *sql.DB, cutoff time.Time) ([]string, error) {
	rows, err := db.Query(`SELECT path FROM frames WHERE ts < ?`, cutoff.Unix())
	if err != nil {
		return nil, err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	_, err = db.Exec(`DELETE FROM frames WHERE ts < ?`, cutoff.Unix())
	return paths, err
}

// deleteBlocksLike removes done/failed blocks whose title or summary matches
// the case-insensitive LIKE pattern. It returns the number of rows deleted.
func deleteBlocksLike(db *sql.DB, pattern string) (int64, error) {
	like := "%" + pattern + "%"
	r, err := db.Exec(`DELETE FROM blocks
	  WHERE status IN ('done','failed') AND
	        (LOWER(title) LIKE LOWER(?) OR LOWER(summary) LIKE LOWER(?))`, like, like)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func pruneOldEvents(db *sql.DB, cutoff time.Time) {
	db.Exec(`DELETE FROM events WHERE ts < ?`, cutoff.Unix())
	db.Exec(`DELETE FROM api_calls WHERE ts < ?`, cutoff.Unix())
}
