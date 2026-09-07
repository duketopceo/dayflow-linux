package main

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateDailyWorkflow(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.Local)
	at := func(h, m int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.Local)
	}
	tru := true
	// 3 hours of coding (one 2h block + one 1h block) and 1 hour of media.
	upsertBlockFull(db, at(9, 0), at(11, 0), "Refactor engine", "Split store.go", "coding", "neovim", "", 8, 0, "done", "", &tru)
	upsertBlockFull(db, at(11, 0), at(12, 0), "Write tests", "Grid tests", "coding", "neovim", "", 4, 0, "done", "", &tru)
	fal := false
	upsertBlockFull(db, at(13, 0), at(14, 0), "YouTube break", "Watched videos", "media", "firefox", "", 4, 0, "done", "", &fal)

	wf, err := generateDailyWorkflow(db, cfg, day)
	if err != nil {
		t.Fatal(err)
	}
	if wf.SlotMinutes != 15 {
		t.Fatalf("slot_minutes=%d", wf.SlotMinutes)
	}
	// 09:00–14:00 => 20 slots, all filled.
	if len(wf.Slots) != 20 {
		t.Fatalf("slots=%d, want 20", len(wf.Slots))
	}
	if wf.Slots[0].Time != "09:00" || wf.Slots[0].Category != "coding" {
		t.Fatalf("first slot=%+v", wf.Slots[0])
	}
	if wf.Slots[19].Time != "13:45" || wf.Slots[19].Category != "media" {
		t.Fatalf("last slot=%+v", wf.Slots[19])
	}
	if wf.TotalMinutes != 240 {
		t.Fatalf("total=%d, want 240 (16 filled slots)", wf.TotalMinutes)
	}
	// Category totals: coding 180, media 60.
	if len(wf.Categories) != 2 {
		t.Fatalf("categories=%v", wf.Categories)
	}
	if wf.Categories[0].Name != "coding" || wf.Categories[0].Minutes != 180 || !wf.Categories[0].Productive {
		t.Fatalf("coding row=%+v", wf.Categories[0])
	}
	if wf.Categories[1].Name != "media" || wf.Categories[1].Minutes != 60 || wf.Categories[1].Productive {
		t.Fatalf("media row=%+v", wf.Categories[1])
	}
	// A gap between 12:00 and 13:00 stays unfilled.
	if wf.Slots[12].Category != "" {
		t.Fatalf("gap slot filled: %+v", wf.Slots[12])
	}
}

func TestGenerateDailyWorkflowEmpty(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	wf, err := generateDailyWorkflow(db, cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(wf.Slots) != 0 || len(wf.Categories) != 0 || wf.TotalMinutes != 0 {
		t.Fatalf("empty day produced %+v", wf)
	}
}

func TestStandupDraftRoundtrip(t *testing.T) {
	testEnv(t)
	db, _ := openDB()
	defer db.Close()

	// Saving without a valid date is a validation error.
	if err := saveStandupDraft(db, "", "", "", "b", ""); err == nil {
		t.Fatal("expected validation error for empty date")
	}
	if err := saveStandupDraft(db, "not-a-date", "", "", "", ""); err == nil {
		t.Fatal("expected validation error for bad date")
	}

	err := saveStandupDraft(db, "2026-09-05", "Shipped U3", "- tests", "waiting on CI", "write docs")
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadStandupDraft(db, "2026-09-05")
	if err != nil {
		t.Fatal(err)
	}
	if d.Highlights != "Shipped U3" || d.Tasks != "- tests" ||
		d.Blockers != "waiting on CI" || d.Priorities != "write docs" {
		t.Fatalf("draft=%+v", d)
	}
	if d.UpdatedAt == 0 {
		t.Fatal("updated_at not set")
	}
	// Upsert overwrites.
	if err := saveStandupDraft(db, "2026-09-05", "h2", "", "b2", ""); err != nil {
		t.Fatal(err)
	}
	d, _ = loadStandupDraft(db, "2026-09-05")
	if d.Highlights != "h2" || d.Blockers != "b2" || d.Tasks != "" {
		t.Fatalf("after upsert draft=%+v", d)
	}
	// Missing date returns an empty draft, not an error.
	d, err = loadStandupDraft(db, "2026-01-01")
	if err != nil || d.Date != "2026-01-01" || d.Blockers != "" {
		t.Fatalf("missing draft=%+v err=%v", d, err)
	}
}

func TestGenerateStandupIncludesDraft(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()

	today := time.Now().Format("2006-01-02")
	err := saveStandupDraft(db, today, "Demo day", "- finish U3",
		"blocked on panel review", "ship the grid")
	if err != nil {
		t.Fatal(err)
	}

	md, j, err := generateStandup(db, cfg, true)
	_ = md
	if err != nil {
		t.Fatal(err)
	}
	draft, ok := j["draft"].(StandupDraft)
	if !ok || draft.Blockers != "blocked on panel review" || draft.Priorities != "ship the grid" {
		t.Fatalf("json draft=%v", j["draft"])
	}

	md, _, err = generateStandup(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"blocked on panel review", "ship the grid", "Demo day", "finish U3"} {
		if !strings.Contains(md, want) {
			t.Fatalf("standup markdown missing %q:\n%s", want, md)
		}
	}
}
