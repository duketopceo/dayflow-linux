// Hidden `dev seed-day` command — writes a deterministic anonymized day
// (cards across categories, one gap, one failed block) into
// DAYFLOW_DATA_DIR for the preview harness, onboarding preview, and
// marketing shots. Refuses to run against the real default data dir.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// devSeedDay writes the fixture day. date is YYYY-MM-DD (default: today).
func devSeedDay(date string) error {
	dir := os.Getenv("DAYFLOW_DATA_DIR")
	if dir == "" {
		return fmt.Errorf("DAYFLOW_DATA_DIR must point at a scratch dir — refusing to run without it")
	}
	def := defaultDataDir()
	abs, _ := filepath.Abs(dir)
	absDef, _ := filepath.Abs(def)
	if r, err := filepath.EvalSymlinks(abs); err == nil { abs = r }
	if r, err := filepath.EvalSymlinks(absDef); err == nil { absDef = r }
	if abs == absDef {
		return fmt.Errorf("DAYFLOW_DATA_DIR resolves to the default data dir (%s) — refusing", def)
	}
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return fmt.Errorf("bad date %q (want YYYY-MM-DD)", date)
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	base := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	at := func(h, m int) int64 { return base.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute).Unix() }
	type b struct {
		s, e                       int64
		title, summary, cat        string
		frames                     int
		status, err                string
		prod                       *int
	}
	p1, p0 := 1, 0
	blocks := []b{
		{at(9, 0), at(9, 30), "Dayflow popup polish", "tightening the classic popup chrome", "coding", 60, "done", "", &p1},
		{at(9, 30), at(10, 15), "Weekly review standup", "team sync and planning", "communication", 90, "done", "", &p1},
		// gap 10:15-10:45 (no block) — the "one gap" fixture
		{at(10, 45), at(11, 30), "Hacker News rabbit hole", "distraction", "browsing", 90, "done", "", &p0},
		{at(11, 30), at(12, 0), "vision summarizer failure", "summarize failed: provider unreachable", "idle", 10, "failed", "provider error", nil},
		{at(13, 0), at(14, 30), "engine capture backend work", "capture seam + x11 adapter", "coding", 180, "done", "", &p1},
		{at(14, 30), at(15, 0), "email triage", "inbox zero run", "communication", 45, "done", "", &p1},
		{at(15, 0), at(16, 30), "docs and roadmap writing", "program roadmap doc", "writing", 100, "done", "", &p1},
	}
	for _, blk := range blocks {
		if _, err := db.Exec(`INSERT OR REPLACE INTO blocks
			(start_ts, end_ts, title, summary, category, frame_count, status, error, created_at, productive, triaged)
			VALUES(?,?,?,?,?,?,?,?,?,?,1)`,
			blk.s, blk.e, blk.title, blk.summary, blk.cat, blk.frames, blk.status, blk.err, blk.s, blk.prod); err != nil {
			return err
		}
	}
	fmt.Printf("seeded %d blocks into %s\n", len(blocks), dir)
	return nil
}

func defaultDataDir() string {
	d, err := os.UserHomeDir()
	if err != nil {
		d = os.Getenv("HOME")
	}
	return filepath.Join(d, ".local", "share", "dayflow")
}
