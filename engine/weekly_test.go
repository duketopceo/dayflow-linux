package main

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestGenerateWeeklyPayload(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start, end := weekBounds(time.Now())
	trueVal := true
	falseVal := false

	a := start
	b := a.Add(15 * time.Minute)
	upsertBlockFull(db, a, b, "Auth refactor", "Token logic", "coding", "neovim", "", 3, 0, "done", "", &trueVal)

	c := b
	d := c.Add(15 * time.Minute)
	upsertBlockFull(db, c, d, "Docs reading", "Browsed docs", "browsing", "firefox", "", 3, 0, "done", "", &falseVal)

	e := d
	f := e.Add(15 * time.Minute)
	upsertBlockFull(db, e, f, "Fix bug", "Patched issue", "coding", "neovim", "", 3, 0, "done", "", &trueVal)

	p, err := generateWeeklyPayload(db, cfg, start, end)
	if err != nil {
		t.Fatal(err)
	}

	if p.TotalMinutes != 45.0 {
		t.Fatalf("total_minutes=%.1f, want 45.0", p.TotalMinutes)
	}
	if p.FocusMinutes != 30.0 {
		t.Fatalf("focus_minutes=%.1f, want 30.0", p.FocusMinutes)
	}
	if p.DistractionMinutes != 15.0 {
		t.Fatalf("distraction_minutes=%.1f, want 15.0", p.DistractionMinutes)
	}
	if len(p.CategoryDonut) != 2 {
		t.Fatalf("category_donut len=%d, want 2", len(p.CategoryDonut))
	}
	if p.CategoryDonut[0].Name != "coding" {
		t.Fatalf("top category=%s", p.CategoryDonut[0].Name)
	}
	if len(p.ContextShifts) != 2 {
		t.Fatalf("context_shifts len=%d, want 2", len(p.ContextShifts))
	}
	if p.ContextShiftCount != 2 {
		t.Fatalf("context_shift_count=%d, want 2", p.ContextShiftCount)
	}
	if len(p.AppTreemap) != 2 {
		t.Fatalf("app_treemap len=%d, want 2", len(p.AppTreemap))
	}
	if p.AppTreemap[0].Name != "neovim" {
		t.Fatalf("top app=%s", p.AppTreemap[0].Name)
	}
	if len(p.Highlights) < 2 {
		t.Fatalf("highlights too short: %v", p.Highlights)
	}
	if len(p.Heatmap) != 7 {
		t.Fatalf("heatmap days=%d, want 7", len(p.Heatmap))
	}
}

func TestCategoryDonutPercentages(t *testing.T) {
	cats := []insightDist{
		{Name: "coding", Mins: 300, Count: 4},
		{Name: "browsing", Mins: 150, Count: 2},
	}
	donut := buildCategoryDonut(cats, 450, defaultConfig())
	sum := 0.0
	for _, d := range donut {
		sum += d.Percentage
	}
	if math.Abs(sum-100.0) > 1.0 {
		t.Fatalf("donut percentages sum=%.1f, want ~100", sum)
	}
	if donut[0].Name != "coding" {
		t.Fatalf("first donut item=%s", donut[0].Name)
	}
	if donut[0].Percentage != 66.7 {
		t.Fatalf("coding percentage=%.1f", donut[0].Percentage)
	}
}

func TestContextShifts(t *testing.T) {
	base := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	blocks := []Block{
		{Start: base, End: base.Add(15 * time.Minute), Category: "coding"},
		{Start: base.Add(15 * time.Minute), End: base.Add(30 * time.Minute), Category: "browsing"},
		{Start: base.Add(30 * time.Minute), End: base.Add(45 * time.Minute), Category: "coding"},
		{Start: base.Add(45 * time.Minute), End: base.Add(60 * time.Minute), Category: "coding"},
	}
	shifts, count := buildContextShifts(blocks)
	if count != 2 {
		t.Fatalf("shift count=%d, want 2", count)
	}
	m := map[string]int{}
	for _, s := range shifts {
		key := fmt.Sprintf("%s->%s", s.Source, s.Target)
		m[key] += s.Count
	}
	if m["coding->browsing"] != 1 {
		t.Fatalf("coding->browsing count=%d", m["coding->browsing"])
	}
	if m["browsing->coding"] != 1 {
		t.Fatalf("browsing->coding count=%d", m["browsing->coding"])
	}
}

func TestWeekTrendsNoPrev(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start, end := weekBounds(time.Now())
	a := start
	b := a.Add(30 * time.Minute)
	trueVal := true
	upsertBlockFull(db, a, b, "Coding", "work", "coding", "neovim", "", 1, 0, "done", "", &trueVal)

	p, err := generateWeeklyPayload(db, cfg, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if p.Trends.HasPrev {
		t.Fatal("HasPrev true with no prior-week data")
	}
	if p.Trends.Categories == nil {
		t.Fatal("categories nil — must marshal as []")
	}
}

func TestWeekTrendsDeltas(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start, end := weekBounds(time.Now())
	prevStart, _ := weekBounds(start.Add(-24 * time.Hour))
	trueVal := true
	falseVal := false

	// Previous week: 60m coding, 60m browsing.
	upsertBlockFull(db, prevStart, prevStart.Add(60*time.Minute), "c", "", "coding", "nvim", "", 1, 0, "done", "", &trueVal)
	upsertBlockFull(db, prevStart.Add(60*time.Minute), prevStart.Add(120*time.Minute), "b", "", "browsing", "ff", "", 1, 0, "done", "", &falseVal)

	// This week: 90m coding, 30m meetings (browsing dropped).
	upsertBlockFull(db, start, start.Add(90*time.Minute), "c2", "", "coding", "nvim", "", 1, 0, "done", "", &trueVal)
	upsertBlockFull(db, start.Add(90*time.Minute), start.Add(120*time.Minute), "m", "", "meetings", "meet", "", 1, 0, "done", "", &falseVal)

	p, err := generateWeeklyPayload(db, cfg, start, end)
	if err != nil {
		t.Fatal(err)
	}
	tr := p.Trends
	if !tr.HasPrev {
		t.Fatal("HasPrev false with prior-week data")
	}
	if tr.PrevTotal != 120 {
		t.Fatalf("prev_total=%v, want 120", tr.PrevTotal)
	}
	if tr.TotalDelta != 0 {
		t.Fatalf("total_delta=%v, want 0 (both 120m)", tr.TotalDelta)
	}
	if tr.FocusDelta != 30 {
		t.Fatalf("focus_delta=%v, want +30", tr.FocusDelta)
	}
	// Category deltas: coding +30, meetings +30 (new), browsing -60 (dropped).
	byCat := map[string]TrendDelta{}
	for _, c := range tr.Categories {
		byCat[c.Name] = c
	}
	if byCat["coding"].DeltaMinutes != 30 {
		t.Fatalf("coding delta=%v", byCat["coding"].DeltaMinutes)
	}
	if byCat["meetings"].DeltaMinutes != 30 || byCat["meetings"].PrevMinutes != 0 {
		t.Fatalf("meetings delta=%+v (new category)", byCat["meetings"])
	}
	if byCat["browsing"].DeltaMinutes != -60 || byCat["browsing"].CurrMinutes != 0 {
		t.Fatalf("browsing delta=%+v (dropped category)", byCat["browsing"])
	}
	// Sorted by |delta| — browsing (60) first.
	if tr.Categories[0].Name != "browsing" {
		t.Fatalf("first delta=%s, want browsing (largest |Δ|)", tr.Categories[0].Name)
	}
	// Share deltas: coding 50%→75% = +25pts.
	if byCat["coding"].DeltaShare != 25 {
		t.Fatalf("coding share delta=%v, want +25", byCat["coding"].DeltaShare)
	}
	// Shift delta: prev week had 1 shift (coding→browsing), this week 1 → 0.
	if tr.ShiftDelta != 0 {
		t.Fatalf("shift_delta=%d, want 0", tr.ShiftDelta)
	}
}

func TestBuildWeekTrendsEmpty(t *testing.T) {
	tr := buildWeekTrends(insights{}, insights{})
	if tr.HasPrev {
		t.Fatal("empty prev should not set HasPrev")
	}
}
