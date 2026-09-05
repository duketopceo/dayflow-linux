package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

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
`

// migrations adds columns to existing databases; each is ignored if already applied.
var migrations = []string{
	`ALTER TABLE frames ADD COLUMN app TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE blocks ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE blocks ADD COLUMN app TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE blocks ADD COLUMN activities TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE blocks ADD COLUMN productive INTEGER DEFAULT NULL`,
}

func migrate(db *sql.DB) {
	for _, m := range migrations {
		db.Exec(m) // duplicate-column errors are expected and ignored
	}
}

func openDB() (*sql.DB, error) {
	if err := os.MkdirAll(dataDir(), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath()+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	migrate(db)
	return db, nil
}

func insertFrame(db *sql.DB, ts time.Time, path string) error {
	_, err := db.Exec(`INSERT INTO frames(ts, path) VALUES(?, ?)`, ts.Unix(), path)
	return err
}

func insertFrameApp(db *sql.DB, ts time.Time, path, app string) error {
	_, err := db.Exec(`INSERT INTO frames(ts, path, app) VALUES(?, ?, ?)`, ts.Unix(), path, app)
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

func blockExists(db *sql.DB, start time.Time) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(1) FROM blocks WHERE start_ts = ? AND status='done'`, start.Unix()).Scan(&n)
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
	q := `SELECT start_ts,end_ts,title,summary,category,frame_count,app,activities,productive FROM blocks
	  WHERE start_ts >= ? AND start_ts < ? AND status='done' ORDER BY start_ts ` + order
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
		if err := rows.Scan(&s, &e, &b.Title, &b.Summary, &b.Category, &b.FrameCount, &b.App, &acts, &prod); err != nil {
			return nil, err
		}
		if prod.Valid {
			b.Productive = &prod.Bool
		}
		if acts != "" {
			json.Unmarshal([]byte(acts), &b.Activities)
		}
		b.Start = time.Unix(s, 0).Local()
		b.End = time.Unix(e, 0).Local()
		b.StartTs = s
		b.EndTs = e
		b.StartStr = b.Start.Format("3:04 PM")
		b.EndStr = b.End.Format("3:04 PM")
		b.AppName = appDisplayName(b.App)
		out = append(out, b)
	}
	return out, rows.Err()
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
