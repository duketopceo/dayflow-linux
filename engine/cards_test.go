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

// TestCardEmitsUnixTimestamps pins the machine-readable card fields: alongside
// the local-time start/end strings, cards carry start_ts/end_ts unix seconds
// so consumers never re-parse strings. The merged card's end_ts tracks the
// last folded block.
func TestCardEmitsUnixTimestamps(t *testing.T) {
	s := time.Date(2026, 9, 7, 21, 0, 0, 0, time.Local).Unix()
	e1 := s + 900
	e2 := s + 1800
	blocks := []Block{
		{Start: time.Unix(s, 0).Local(), End: time.Unix(e1, 0).Local(),
			StartTs: s, EndTs: e1, StartStr: "9:00 PM", EndStr: "9:15 PM",
			Title: "Coding", Category: "coding", App: "neovim", Status: "done"},
		{Start: time.Unix(e1, 0).Local(), End: time.Unix(e2, 0).Local(),
			StartTs: e1, EndTs: e2, StartStr: "9:15 PM", EndStr: "9:30 PM",
			Title: "Coding", Category: "coding", App: "neovim", Status: "done"},
	}
	cards := mergeCards(blocks)
	if len(cards) != 1 {
		t.Fatalf("cards=%d, want 1", len(cards))
	}
	var m map[string]any
	raw, err := json.Marshal(cards[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if got, ok := m["start_ts"]; !ok || got != float64(s) {
		t.Fatalf("start_ts = %v (present=%v), want %d", got, ok, s)
	}
	if got, ok := m["end_ts"]; !ok || got != float64(e2) {
		t.Fatalf("end_ts = %v (present=%v), want %d (merged end)", got, ok, e2)
	}
}

// TestTimelineJSONContract pins the payload shape the QML panel parses:
// "cards" must be a present key — the panel distinguishes "no data" from
// "engine too old to emit cards" by key presence, so an empty day still
// marshals the key (as null/[]), never omits it.
func TestTimelineJSONContract(t *testing.T) {
	p := timelineJSON(nil)
	for _, k := range []string{"blocks", "cards"} {
		if _, ok := p[k]; !ok {
			t.Fatalf("timelineJSON(nil) missing key %q", k)
		}
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if _, ok := back["cards"]; !ok {
		t.Fatalf("marshaled payload lost the cards key: %s", raw)
	}

	// Non-empty input carries the merged cards through.
	s := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local).Unix()
	blocks := []Block{
		{Start: time.Unix(s, 0).Local(), End: time.Unix(s+900, 0).Local(),
			StartTs: s, EndTs: s + 900, Title: "Coding", Category: "coding",
			App: "neovim", Status: "done"},
	}
	p = timelineJSON(blocks)
	cards, ok := p["cards"].([]Card)
	if !ok || len(cards) != 1 {
		t.Fatalf("timelineJSON cards = %v, want 1 card", p["cards"])
	}
}

// TestMCPGetTimelineWeekEmitsCards covers the range path: a "week" request
// goes through the same timelineJSON payload, so blocks+cards+start/end all
// appear in the result.
func TestMCPGetTimelineWeekEmitsCards(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ws, _ := weekBounds(time.Now())
	b := ws.Add(10 * time.Hour) // Monday 10:00 — inside this week
	if err := upsertBlockFull(db, b, b.Add(15*time.Minute),
		"Week block", "s", "coding", "neovim", "", 3, 0, "done", "", nil); err != nil {
		t.Fatal(err)
	}

	res, err := mcpCall(db, cfg, false, "get_timeline", map[string]any{"date": "week"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("get_timeline week result is %T, want map", res)
	}
	for _, k := range []string{"blocks", "cards", "start", "end"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("get_timeline week missing key %q", k)
		}
	}
	cards, ok := m["cards"].([]Card)
	if !ok || len(cards) != 1 {
		t.Fatalf("week cards = %v, want 1 card", m["cards"])
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
