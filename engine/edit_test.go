package main

import (
	"strings"
	"testing"
	"time"
)

func TestSaveBlockEditApplies(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 9, 0, 0, 0, time.Local)
	end := start.Add(15 * time.Minute)
	prod := false
	if err := upsertBlockFull(db, start, end, "Orig title", "Orig summary", "browsing", "firefox", "", 3, 0, "done", "", &prod); err != nil {
		t.Fatal(err)
	}

	if err := saveBlockEdit(db, cfg, start.Unix(), "title", "Deep work on API"); err != nil {
		t.Fatal(err)
	}
	if err := saveBlockEdit(db, cfg, start.Unix(), "category", "coding"); err != nil {
		t.Fatal(err)
	}
	if err := saveBlockEdit(db, cfg, start.Unix(), "productive", "true"); err != nil {
		t.Fatal(err)
	}

	blocks, err := blocksForDay(db, start, false)
	if err != nil || len(blocks) != 1 {
		t.Fatalf("blocks=%v err=%v", blocks, err)
	}
	b := blocks[0]
	if b.Title != "Deep work on API" || b.Category != "coding" {
		t.Fatalf("edits not applied: %+v", b)
	}
	if b.Productive == nil || !*b.Productive {
		t.Fatalf("productive edit not applied: %+v", b.Productive)
	}
	// raw row must be untouched — edits are an overlay
	var raw string
	if err := db.QueryRow(`SELECT title FROM blocks WHERE start_ts=?`, start.Unix()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "Orig title" {
		t.Fatalf("raw title changed to %q", raw)
	}
}

func TestSaveBlockEditInvalidField(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	start := time.Now().Truncate(time.Minute)
	upsertBlockFull(db, start, start.Add(15*time.Minute), "t", "s", "coding", "", "", 1, 0, "done", "", nil)

	if err := saveBlockEdit(db, cfg, start.Unix(), "app", "firefox"); err == nil {
		t.Fatal("expected error for non-editable field")
	}
	if err := saveBlockEdit(db, cfg, start.Unix(), "productive", "maybe"); err == nil {
		t.Fatal("expected error for non-boolean productive")
	}
}

func TestSaveBlockEditMissingBlock(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	if err := saveBlockEdit(db, cfg, 1700000000, "title", "x"); err == nil {
		t.Fatal("expected error editing a nonexistent block")
	}
}

func TestSaveBlockEditSensitiveFilter(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	start := time.Now().Truncate(time.Minute)
	upsertBlockFull(db, start, start.Add(15*time.Minute), "t", "s", "coding", "", "", 1, 0, "done", "", nil)

	cfg.FilterInappropriate = true
	err := saveBlockEdit(db, cfg, start.Unix(), "title", "checking credit card number")
	if err == nil || !strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("expected sensitive-content rejection, got %v", err)
	}

	cfg.FilterInappropriate = false
	if err := saveBlockEdit(db, cfg, start.Unix(), "title", "checking credit card number"); err != nil {
		t.Fatalf("filter off should allow edit: %v", err)
	}
}

func TestSaveBlockEditLatestWins(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	start := time.Now().Truncate(time.Minute)
	upsertBlockFull(db, start, start.Add(15*time.Minute), "t", "s", "coding", "", "", 1, 0, "done", "", nil)

	saveBlockEdit(db, cfg, start.Unix(), "title", "first")
	time.Sleep(1100 * time.Millisecond) // edited_at has second resolution
	saveBlockEdit(db, cfg, start.Unix(), "title", "second")

	b, err := loadBlockWithEdits(db, start.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "second" {
		t.Fatalf("latest edit should win, got %q", b.Title)
	}
}

func TestEditsForBlock(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	start := time.Now().Truncate(time.Minute)
	upsertBlockFull(db, start, start.Add(15*time.Minute), "t", "s", "coding", "", "", 1, 0, "done", "", nil)

	saveBlockEdit(db, cfg, start.Unix(), "title", "new title")
	saveBlockEdit(db, cfg, start.Unix(), "category", "writing")

	edits, err := editsForBlock(db, start.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 2 {
		t.Fatalf("edits=%v", edits)
	}
	// newest first; second save was category
	if edits[0].Field != "category" || edits[1].Field != "title" {
		t.Fatalf("order=%v %v", edits[0].Field, edits[1].Field)
	}
	if edits[1].OldValue != "t" || edits[1].NewValue != "new title" {
		t.Fatalf("edit=%+v", edits[1])
	}
	// unrelated block has no edits
	other, err := editsForBlock(db, start.Unix()+900)
	if err != nil || len(other) != 0 {
		t.Fatalf("other=%v err=%v", other, err)
	}
}

func TestParseBlockStart(t *testing.T) {
	ts, rest, err := parseBlockStart([]string{"1757000000", "title", "x"})
	if err != nil || ts != 1757000000 || len(rest) != 2 {
		t.Fatalf("ts=%d rest=%v err=%v", ts, rest, err)
	}
	ts, rest, err = parseBlockStart([]string{"2026-09-05", "14:30", "title", "x"})
	if err != nil || len(rest) != 2 {
		t.Fatalf("rest=%v err=%v", rest, err)
	}
	want := time.Date(2026, 9, 5, 14, 30, 0, 0, time.Local).Unix()
	if ts != want {
		t.Fatalf("ts=%d want %d", ts, want)
	}
	if _, _, err := parseBlockStart([]string{"bogus"}); err == nil {
		t.Fatal("expected parse error")
	}
}
