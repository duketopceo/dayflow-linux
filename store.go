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
