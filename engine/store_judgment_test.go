package main

import (
	"testing"
	"time"
)

func TestSetBlockJudgmentRoundTrip(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start := time.Date(2026, 9, 18, 10, 0, 0, 0, time.Local)
	end := start.Add(15 * time.Minute)
	tru := true
	if err := upsertBlockFull(db, start, end, "Coding", "Work", "coding", "neovim", "", 3, 0, "done", "", &tru); err != nil {
		t.Fatal(err)
	}
	conf := 0.83
	if err := setBlockJudgment(db, start, &conf, nil, &tru); err != nil {
		t.Fatal(err)
	}
	blocks, err := blocksForDay(db, start, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	b := blocks[0]
	if b.CategoryConfidence == nil || *b.CategoryConfidence != conf {
		t.Fatalf("confidence = %v", b.CategoryConfidence)
	}
	if b.SameAsPrev == nil || !*b.SameAsPrev {
		t.Fatalf("same_as_prev = %v", b.SameAsPrev)
	}
}

func TestSetBlockJudgmentNilPreservesColumn(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start := time.Date(2026, 9, 18, 11, 0, 0, 0, time.Local)
	end := start.Add(15 * time.Minute)
	if err := upsertBlockFull(db, start, end, "t", "s", "coding", "", "", 1, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}
	conf := 0.9
	tru := true
	if err := setBlockJudgment(db, start, &conf, nil, &tru); err != nil {
		t.Fatal(err)
	}
	// A later judgment with only confidence must not NULL same_as_prev.
	conf2 := 0.5
	if err := setBlockJudgment(db, start, &conf2, nil, nil); err != nil {
		t.Fatal(err)
	}
	var same int64
	if err := db.QueryRow(`SELECT same_as_prev FROM blocks WHERE start_ts = ?`, start.Unix()).Scan(&same); err != nil {
		t.Fatal(err)
	}
	if same != 1 {
		t.Fatalf("same_as_prev overwritten: %d", same)
	}
}

func TestMergeCardsSameAsPrev(t *testing.T) {
	base := time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)
	mk := func(i int, title, app, cat string, same *bool) Block {
		s := base.Add(time.Duration(i*15) * time.Minute)
		e := s.Add(15 * time.Minute)
		return Block{Start: s, End: e, Title: title, App: app, Category: cat,
			SameAsPrev: same, StartStr: s.Format("15:04"), EndStr: e.Format("15:04")}
	}
	tru, fal := true, false

	// jev continuity merges despite differing titles/apps
	cards := mergeCards([]Block{
		mk(0, "Refactor engine", "neovim", "coding", nil),
		mk(1, "Read engine docs", "firefox", "browsing", &tru),
	})
	if len(cards) != 1 || cards[0].Blocks != 2 {
		t.Fatalf("cards = %+v", cards)
	}

	// nil judgment → heuristic unchanged: differing everything stays split
	cards = mergeCards([]Block{
		mk(0, "Refactor engine", "neovim", "coding", nil),
		mk(1, "Read engine docs", "firefox", "browsing", nil),
	})
	if len(cards) != 2 {
		t.Fatalf("cards = %d", len(cards))
	}

	// jev "no" does not veto the heuristic title match
	cards = mergeCards([]Block{
		mk(0, "Same title", "neovim", "coding", nil),
		mk(1, "Same title", "neovim", "coding", &fal),
	})
	if len(cards) != 1 {
		t.Fatalf("cards = %d", len(cards))
	}
}
