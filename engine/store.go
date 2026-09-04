package main

import (
	"database/sql"
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
  created_at  INTEGER NOT NULL
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
	return db, nil
}

func insertFrame(db *sql.DB, ts time.Time, path string) error {
	_, err := db.Exec(`INSERT INTO frames(ts, path) VALUES(?, ?)`, ts.Unix(), path)
	return err
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

func blockExists(db *sql.DB, start time.Time) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(1) FROM blocks WHERE start_ts = ? AND status='done'`, start.Unix()).Scan(&n)
	return n > 0, err
}

type Block struct {
	Start      time.Time `json:"-"`
	End        time.Time `json:"-"`
	StartStr   string    `json:"start"`
	EndStr     string    `json:"end"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
	Category   string    `json:"category"`
	FrameCount int       `json:"frame_count"`
}

func blocksForDay(db *sql.DB, day time.Time) ([]Block, error) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	end := start.Add(24 * time.Hour)
	rows, err := db.Query(`SELECT start_ts,end_ts,title,summary,category,frame_count FROM blocks
	  WHERE start_ts >= ? AND start_ts < ? AND status='done' ORDER BY start_ts`,
		start.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Block
	for rows.Next() {
		var b Block
		var s, e int64
		if err := rows.Scan(&s, &e, &b.Title, &b.Summary, &b.Category, &b.FrameCount); err != nil {
			return nil, err
		}
		b.Start = time.Unix(s, 0).Local()
		b.End = time.Unix(e, 0).Local()
		b.StartStr = b.Start.Format("15:04")
		b.EndStr = b.End.Format("15:04")
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
		rows.Scan(&p)
		paths = append(paths, p)
	}
	rows.Close()
	_, err = db.Exec(`DELETE FROM frames WHERE ts < ?`, cutoff.Unix())
	return paths, err
}

func pruneOldEvents(db *sql.DB, cutoff time.Time) {
	db.Exec(`DELETE FROM events WHERE ts < ?`, cutoff.Unix())
	db.Exec(`DELETE FROM api_calls WHERE ts < ?`, cutoff.Unix())
}
