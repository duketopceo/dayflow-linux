package main

import (
	"database/sql"
	"math"
	"testing"
	"time"
)

func addBlock(t *testing.T, db *sql.DB, start time.Time, mins int, cat string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO blocks(start_ts,end_ts,title,category,frame_count,status,created_at)
		VALUES(?,?,?,?,0,'done',?)`,
		start.Unix(), start.Add(time.Duration(mins)*time.Minute).Unix(), cat+" block", cat, start.Unix()); err != nil {
		t.Fatal(err)
	}
}

func TestForecastBlendsWeekdayHistory(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Target: a Monday. Seed 4 prior Mondays heavy on coding, plus a
	// mid-week day heavy on meetings to confirm weekday weighting.
	target := time.Date(2026, 9, 21, 9, 0, 0, 0, time.Local) // Monday
	for i := 7; i <= 28; i += 7 {
		d := target.AddDate(0, 0, -i)
		addBlock(t, db, d, 300, "coding")
		addBlock(t, db, d.Add(6*time.Hour), 60, "communication")
	}
	addBlock(t, db, target.AddDate(0, 0, -5), 400, "meetings") // Wednesday

	fc, err := forecast(db, target)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Samples != 4 {
		t.Fatalf("expected 4 same-weekday samples, got %d", fc.Samples)
	}
	if fc.Confidence != "high" {
		t.Fatalf("expected high confidence, got %s", fc.Confidence)
	}
	if len(fc.Items) == 0 || fc.Items[0].Category != "coding" {
		t.Fatalf("top prediction wrong: %+v", fc.Items)
	}
	sum := 0.0
	for _, it := range fc.Items {
		sum += it.Pct
	}
	if math.Abs(sum-100) > 1.5 {
		t.Fatalf("percentages don't sum to ~100: %v", sum)
	}
	// meetings-only Wednesday shouldn't dominate a Monday forecast
	for _, it := range fc.Items {
		if it.Category == "meetings" && it.Pct > 40 {
			t.Fatalf("weekday blending failed: meetings=%.1f%%", it.Pct)
		}
	}
}

func TestForecastNoHistory(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fc, err := forecast(db, time.Now().AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Items) != 0 || fc.Confidence != "low" {
		t.Fatalf("empty history should give low/no items: %+v", fc)
	}
}
