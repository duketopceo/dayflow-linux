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
