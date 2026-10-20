package main

import (
	"path/filepath"
	"testing"
)

func TestSeedDayRefusesUnset(t *testing.T) {
	t.Setenv("DAYFLOW_DATA_DIR", "")
	if err := devSeedDay("2026-10-19"); err == nil {
		t.Fatal("expected refusal without DAYFLOW_DATA_DIR")
	}
}

func TestSeedDayRefusesDefault(t *testing.T) {
	// Relocate HOME so a regressed guard writes into the temp dir, never
	// the real store.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DAYFLOW_DATA_DIR", defaultDataDir())
	if err := devSeedDay("2026-10-19"); err == nil {
		t.Fatal("expected refusal on default data dir")
	}
}

func TestSeedDaySeedsAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	if err := devSeedDay("2026-10-19"); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blocks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("want 7 blocks, got %d", n)
	}
	var failed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM blocks WHERE status='failed'`).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("want 1 failed block, got %d", failed)
	}
	db.Close()
	if err := devSeedDay("2026-10-19"); err != nil {
		t.Fatalf("second seed failed: %v", err)
	}
	db2, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var n2 int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM blocks`).Scan(&n2); err != nil {
		t.Fatal(err)
	}
	if n2 != 7 {
		t.Fatalf("idempotency: want 7 blocks after reseed, got %d", n2)
	}
}

func TestSeedDayRejectsRealDefault(t *testing.T) {
	// Even with HOME relocated, the refusal compares resolved paths.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DAYFLOW_DATA_DIR", filepath.Join(home, ".local", "share", "dayflow"))
	if err := devSeedDay("2026-10-19"); err == nil {
		t.Fatal("expected refusal when scratch equals the (relocated) default")
	}
}
