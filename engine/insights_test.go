package main

import (
	"testing"
	"time"
)

func TestInsightsExcludesIdleAndScreensaver(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)
	// 60 min focus block
	upsertBlockFull(db, base, base.Add(60*time.Minute), "Coding", "Work", "coding", "neovim", "", 0, 0, "done", "", nil)
	// 30 min idle block — should be excluded from analytics
	upsertBlockFull(db, base.Add(60*time.Minute), base.Add(90*time.Minute), "No activity", "Screen locked", "idle", "", "", 0, 0, "done", "", nil)
	// 30 min distraction block
	upsertBlockFull(db, base.Add(90*time.Minute), base.Add(120*time.Minute), "Browsing", "Social media", "browsing", "brave", "", 0, 0, "done", "", nil)
	// 30 min screensaver block — should be excluded
	upsertBlockFull(db, base.Add(120*time.Minute), base.Add(150*time.Minute), "Screensaver", "", "system", "xscreensaver", "", 0, 0, "done", "", nil)

	start, end := dayBounds(base)
	in, err := generateInsights(db, cfg, start, end.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	if in.TotalMins != 90 {
		t.Fatalf("total=%f, want 90", in.TotalMins)
	}
	if in.FocusMins != 60 {
		t.Fatalf("focus=%f, want 60", in.FocusMins)
	}
	if in.IdleMins != 0 {
		t.Fatalf("idle=%f, want 0", in.IdleMins)
	}
	if in.DistractionMins != 30 {
		t.Fatalf("distraction=%f, want 30", in.DistractionMins)
	}

	for _, c := range in.Categories {
		if c.Name == "idle" {
			t.Fatalf("idle should not be a category: %v", in.Categories)
		}
	}

	for _, a := range in.Apps {
		if isExcludedApp(a.Name) {
			t.Fatalf("excluded app should not appear: %v", in.Apps)
		}
	}
}
