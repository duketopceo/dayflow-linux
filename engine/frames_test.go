package main

import (
	"testing"
	"time"
)

func TestFramesForDayBoundsAndExists(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	day := time.Date(2026, 9, 15, 12, 0, 0, 0, time.Local)
	p := addFrameFile(t, db, day, 1)
	// a row whose file is missing -> exists=false
	if _, err := db.Exec(`INSERT INTO frames(ts, path) VALUES(?, ?)`,
		day.Add(time.Hour).Unix(), "/nonexistent/frame.jpg"); err != nil {
		t.Fatal(err)
	}
	// a frame from the next day must not leak in
	addFrameFile(t, db, day.AddDate(0, 0, 1), 1)

	frames, err := framesForDay(db, day)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames for the day, got %d", len(frames))
	}
	if frames[0].Path != p || !frames[0].Exists {
		t.Fatalf("first frame wrong: %+v", frames[0])
	}
	if frames[1].Exists {
		t.Fatal("missing file should report exists=false")
	}
}

func TestSetPlayback(t *testing.T) {
	// enabling applies the standard cap only when frames are unlimited
	cfg, changed := setPlayback(Config{MaxFramesMB: 0}, true)
	if !changed || !cfg.KeepFrames || cfg.MaxFramesMB != playbackCapMB {
		t.Fatalf("enable on unlimited cfg: %+v changed=%v", cfg, changed)
	}
	// an explicit cap survives
	cfg, _ = setPlayback(Config{MaxFramesMB: 2048}, true)
	if cfg.MaxFramesMB != 2048 {
		t.Fatalf("explicit cap clobbered: %d", cfg.MaxFramesMB)
	}
	// idempotent
	if _, changed := setPlayback(Config{KeepFrames: true, MaxFramesMB: 10240}, true); changed {
		t.Fatal("re-enabling should be a no-op")
	}
	// disabling keeps the cap, drops retention
	cfg, changed = setPlayback(Config{KeepFrames: true, MaxStorageMB: 10240}, false)
	if !changed || cfg.KeepFrames || cfg.MaxStorageMB != 10240 {
		t.Fatalf("disable: %+v changed=%v", cfg, changed)
	}
}
