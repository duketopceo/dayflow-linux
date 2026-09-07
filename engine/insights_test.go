package main

import (
	"testing"
	"time"
)

func TestInsightsIdleTracking(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)
	// 60 min focus block
	upsertBlockFull(db, base, base.Add(60*time.Minute), "Coding", "Work", "coding", "neovim", "", 0, 0, "done", "", nil)
	// 30 min idle block (nil productive falls back to not productive because category is idle)
	upsertBlockFull(db, base.Add(60*time.Minute), base.Add(90*time.Minute), "No activity", "Screen locked", "idle", "", "", 0, 0, "done", "", nil)
	// 30 min distraction block
	upsertBlockFull(db, base.Add(90*time.Minute), base.Add(120*time.Minute), "Browsing", "Social media", "browsing", "brave", "", 0, 0, "done", "", nil)

	start, end := dayBounds(base)
	in, err := generateInsights(db, cfg, start, end.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	if in.TotalMins != 120 {
		t.Fatalf("total=%f, want 120", in.TotalMins)
	}
	if in.FocusMins != 60 {
		t.Fatalf("focus=%f, want 60", in.FocusMins)
	}
	if in.IdleMins != 30 {
		t.Fatalf("idle=%f, want 30", in.IdleMins)
	}
	if in.DistractionMins != 30 {
		t.Fatalf("distraction=%f, want 30", in.DistractionMins)
	}

	// Idle should not appear in top distractions.
	for _, d := range in.TopDistractions {
		if d.Name == "idle" {
			t.Fatalf("idle should not be listed as a top distraction: %v", in.TopDistractions)
		}
	}

	// Idle should still be reported as a category.
	foundIdle := false
	for _, c := range in.Categories {
		if c.Name == "idle" && c.Mins == 30 {
			foundIdle = true
		}
	}
	if !foundIdle {
		t.Fatalf("idle category missing or wrong minutes: %v", in.Categories)
	}
}
