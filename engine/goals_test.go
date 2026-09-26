package main

import (
	"database/sql"
	"testing"
	"time"
)

func goalsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGoalSetAndGet(t *testing.T) {
	db := goalsTestDB(t)
	today := time.Now().Format("2006-01-02")

	if err := setGoal(db, today, "Ship the feature"); err != nil {
		t.Fatalf("setGoal: %v", err)
	}
	g, err := getGoal(db, today)
	if err != nil {
		t.Fatalf("getGoal: %v", err)
	}
	if g.Goal != "Ship the feature" || g.Completed {
		t.Fatalf("unexpected goal: %+v", g)
	}
}

func TestGoalUpsert(t *testing.T) {
	db := goalsTestDB(t)
	today := time.Now().Format("2006-01-02")

	if err := setGoal(db, today, "first"); err != nil {
		t.Fatal(err)
	}
	if err := setGoal(db, today, "second"); err != nil {
		t.Fatal(err)
	}
	g, err := getGoal(db, today)
	if err != nil {
		t.Fatal(err)
	}
	if g.Goal != "second" {
		t.Fatalf("upsert failed, goal=%q", g.Goal)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM day_goals WHERE date=?`, today).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row for date, got %d", n)
	}
}

func TestGoalDone(t *testing.T) {
	db := goalsTestDB(t)
	today := time.Now().Format("2006-01-02")

	if err := setGoal(db, today, "finish tests"); err != nil {
		t.Fatal(err)
	}
	if err := completeGoal(db, today, true); err != nil {
		t.Fatalf("completeGoal: %v", err)
	}
	g, err := getGoal(db, today)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Completed {
		t.Fatal("goal not marked completed")
	}
}

func TestGoalEmptyDate(t *testing.T) {
	db := goalsTestDB(t)
	_, err := getGoal(db, "1999-01-01")
	if err != nil {
		t.Fatalf("getGoal on empty date should not error, got %v", err)
	}
	if err := completeGoal(db, "1999-01-01", true); err == nil {
		t.Fatal("completeGoal on missing goal should error")
	}
}

// seedGoal inserts a goal row directly with a fixed completion state.
func seedGoal(t *testing.T, db *sql.DB, date string, done bool) {
	t.Helper()
	v := 0
	if done {
		v = 1
	}
	if _, err := db.Exec(`INSERT INTO day_goals(date, goal, completed, created_at)
	  VALUES(?,?,?,0)`, date, "g-"+date, v); err != nil {
		t.Fatal(err)
	}
}

func TestGoalStreakConsecutive(t *testing.T) {
	db := goalsTestDB(t)
	seedGoal(t, db, "2026-09-17", true)
	seedGoal(t, db, "2026-09-18", true)
	seedGoal(t, db, "2026-09-19", true)
	// Viewed day pending — streak counts back through yesterday.
	g, err := getGoal(db, "2026-09-20")
	if err != nil {
		t.Fatal(err)
	}
	if g.Streak.Current != 3 || g.Streak.Best != 3 || g.Streak.Total != 3 {
		t.Fatalf("streak=%+v, want {3 3 3}", g.Streak)
	}
	// Completing the viewed day extends it.
	seedGoal(t, db, "2026-09-20", true)
	g, _ = getGoal(db, "2026-09-20")
	if g.Streak.Current != 4 {
		t.Fatalf("current=%d, want 4", g.Streak.Current)
	}
}

func TestGoalStreakGapBreaks(t *testing.T) {
	db := goalsTestDB(t)
	seedGoal(t, db, "2026-09-15", true)
	seedGoal(t, db, "2026-09-16", true)
	// 09-17 missing entirely → break.
	seedGoal(t, db, "2026-09-18", true)
	seedGoal(t, db, "2026-09-19", true)
	g, err := getGoal(db, "2026-09-19")
	if err != nil {
		t.Fatal(err)
	}
	if g.Streak.Current != 2 {
		t.Fatalf("current=%d, want 2 (gap on 09-17)", g.Streak.Current)
	}
	if g.Streak.Best != 2 {
		t.Fatalf("best=%d, want 2 (two runs of 2)", g.Streak.Best)
	}
	if g.Streak.Total != 4 {
		t.Fatalf("total=%d, want 4", g.Streak.Total)
	}
}

func TestGoalStreakIncompleteBreaks(t *testing.T) {
	db := goalsTestDB(t)
	seedGoal(t, db, "2026-09-18", true)
	seedGoal(t, db, "2026-09-19", false) // set but cleared/incomplete
	g, err := getGoal(db, "2026-09-19")
	if err != nil {
		t.Fatal(err)
	}
	// Incomplete viewed day is pending → counts from 09-18.
	if g.Streak.Current != 1 {
		t.Fatalf("current=%d, want 1", g.Streak.Current)
	}
	// But an incomplete PAST day breaks the run.
	seedGoal(t, db, "2026-09-20", true)
	g, _ = getGoal(db, "2026-09-20")
	if g.Streak.Current != 1 {
		t.Fatalf("current=%d, want 1 (09-19 incomplete breaks)", g.Streak.Current)
	}
}

func TestGoalStreakEmpty(t *testing.T) {
	db := goalsTestDB(t)
	g, err := getGoal(db, "2026-09-20")
	if err != nil {
		t.Fatal(err)
	}
	if g.Streak != (GoalStreak{}) {
		t.Fatalf("empty history streak=%+v, want zero", g.Streak)
	}
}
