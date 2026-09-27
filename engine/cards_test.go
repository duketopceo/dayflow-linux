package main

import (
	"encoding/json"
	"testing"
	"time"
)

// timelineCards is the exact call path `timeline --json` / `today --json` /
// `day --json` use to build their "cards" array (printTimeline).
func timelineCards(t *testing.T, day time.Time) []Card {
	t.Helper()
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	blocks, err := blocksForDay(db, day, true)
	if err != nil {
		t.Fatal(err)
	}
	return mergeCards(blocks)
}

// TestInsightsCardsMatchTimeline is the U6 contract: `insights --json` emits
// a `cards` array identical to `timeline --json` for the same day — both
// surfaces run the single shared mergeCards implementation.
func TestInsightsCardsMatchTimeline(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)
	if err := upsertBlockFull(db, base, base.Add(15*time.Minute),
		"Coding", "s", "coding", "neovim", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := upsertBlockFull(db, base.Add(15*time.Minute), base.Add(30*time.Minute),
		"Coding", "s", "coding", "neovim", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := upsertBlockFull(db, base.Add(30*time.Minute), base.Add(45*time.Minute),
		"Email", "s", "communication", "mailspring", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}

	start, end := dayBounds(base)
	in, err := generateInsights(db, cfg, start, end)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := in.JSON()["cards"].([]Card)
	if !ok {
		t.Fatalf("insights JSON has no cards array: %v", in.JSON()["cards"])
	}
	want := timelineCards(t, base)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("insights cards != timeline cards:\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
	if len(got) != 2 {
		t.Fatalf("cards=%d, want 2 (merged coding pair + email): %s", len(got), gotJSON)
	}
}

// TestMCPGetTimelineEmitsCards: get_timeline returns the same cards array
// as `timeline --json` for the same day.
func TestMCPGetTimelineEmitsCards(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)
	if err := upsertBlockFull(db, base, base.Add(15*time.Minute),
		"Coding", "s", "coding", "neovim", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := upsertBlockFull(db, base.Add(15*time.Minute), base.Add(30*time.Minute),
		"Email", "s", "communication", "mailspring", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}

	res, err := mcpCall(db, cfg, false, "get_timeline", map[string]any{"date": "2026-09-07"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("get_timeline result is %T, want map", res)
	}
	got, _ := json.Marshal(m["cards"])
	want, _ := json.Marshal(timelineCards(t, base))
	if string(got) != string(want) {
		t.Fatalf("get_timeline cards != timeline cards:\n got: %s\nwant: %s", got, want)
	}
	if len(timelineCards(t, base)) != 2 {
		t.Fatalf("expected 2 cards, got %s", want)
	}
}

// TestTimelineCardsSameAsPrev is the merge regression through the real
// `day --json`/`timeline --json` call path: three blocks
// [A(same_as_prev) -> A -> B] fold into two cards. The A blocks differ in
// title/app/category so only Jev's judged continuity can merge them.
func TestTimelineCardsSameAsPrev(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)
	if err := upsertBlockFull(db, base, base.Add(15*time.Minute),
		"Block A1", "s", "coding", "neovim", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := upsertBlockFull(db, base.Add(15*time.Minute), base.Add(30*time.Minute),
		"Block A2", "s", "writing", "obsidian", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}
	same := true
	if err := setBlockJudgment(db, base.Add(15*time.Minute), nil, nil, &same); err != nil {
		t.Fatal(err)
	}
	if err := upsertBlockFull(db, base.Add(30*time.Minute), base.Add(45*time.Minute),
		"Block B", "s", "media", "youtube", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}

	cards := timelineCards(t, base)
	if len(cards) != 2 {
		b, _ := json.Marshal(cards)
		t.Fatalf("cards=%d, want 2: %s", len(cards), b)
	}
	if cards[0].Blocks != 2 || cards[1].Blocks != 1 {
		t.Fatalf("card block counts = %d/%d, want 2/1", cards[0].Blocks, cards[1].Blocks)
	}
	if len(cards[0].Children) != 2 {
		t.Fatalf("first card children=%d, want 2", len(cards[0].Children))
	}
}

// TestMergeCardsNullAppSplits is the drift regression: the deleted JS
// mergeSpans folded adjacent null-app blocks on category alone; mergeCards
// requires a non-empty app for the app+category merge, so app-less blocks
// with different titles stay split.
func TestMergeCardsNullAppSplits(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)
	if err := upsertBlockFull(db, base, base.Add(15*time.Minute),
		"Reading docs", "s", "browsing", "", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := upsertBlockFull(db, base.Add(15*time.Minute), base.Add(30*time.Minute),
		"Still reading", "s", "browsing", "", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}

	cards := timelineCards(t, base)
	if len(cards) != 2 {
		b, _ := json.Marshal(cards)
		t.Fatalf("null-app blocks merged — cards=%d, want 2: %s", len(cards), b)
	}
}
