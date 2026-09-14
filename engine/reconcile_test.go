package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeOrphan drops a JPEG-shaped file into a dated frames dir with the
// given modification time and returns its path.
func writeOrphan(t *testing.T, name string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(framesDir(), mtime.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("jpeg-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReconcileQuarantinesOrphans(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	old := writeOrphan(t, "old.jpg", time.Now().Add(-48*time.Hour))
	res, err := reconcileFrames(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Quarantined != 1 {
		t.Fatalf("quarantined = %d, want 1", res.Quarantined)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("orphan still in frames dir")
	}
	matches, _ := filepath.Glob(filepath.Join(quarantineDir(), "*", "old.jpg"))
	if len(matches) != 1 {
		t.Fatalf("expected quarantined file, got %v", matches)
	}
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='reconcile_quarantined'`).Scan(&n)
	if n != 1 {
		t.Fatal("missing reconcile_quarantined event")
	}
}

func TestReconcileSkipsFreshFiles(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fresh := writeOrphan(t, "fresh.jpg", time.Now())
	res, err := reconcileFrames(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Quarantined != 0 {
		t.Fatalf("quarantined = %d, want 0", res.Quarantined)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh file was moved")
	}
}

func TestReconcileDryRun(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	p := writeOrphan(t, "orphan.jpg", time.Now().Add(-time.Hour))
	res, err := reconcileFrames(db, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Orphans) != 1 || res.Quarantined != 0 {
		t.Fatalf("dry run: orphans=%v quarantined=%d", res.Orphans, res.Quarantined)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("dry run moved the file")
	}
}

func TestReconcileStaleRows(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	missing := filepath.Join(framesDir(), "2026-01-01", "gone.jpg")
	if err := insertFrame(db, time.Now().Add(-time.Hour), missing); err != nil {
		t.Fatal(err)
	}
	res, err := reconcileFrames(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.StaleRows != 1 {
		t.Fatalf("stale rows = %d, want 1", res.StaleRows)
	}
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM frames WHERE path=?`, missing).Scan(&n)
	if n != 0 {
		t.Fatal("stale frame row not removed")
	}
}

func TestReconcilePurgesExpiredQuarantine(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// quarantine a file, then age it past retention
	writeOrphan(t, "old.jpg", time.Now().Add(-48*time.Hour))
	if _, err := reconcileFrames(db, cfg, false); err != nil {
		t.Fatal(err)
	}
	var qp string
	matches, _ := filepath.Glob(filepath.Join(quarantineDir(), "*", "old.jpg"))
	if len(matches) != 1 {
		t.Fatal("nothing quarantined")
	}
	qp = matches[0]
	past := time.Now().Add(-time.Duration(cfg.RetentionDays+1) * 24 * time.Hour)
	if err := os.Chtimes(qp, past, past); err != nil {
		t.Fatal(err)
	}
	res, err := reconcileFrames(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Purged != 1 {
		t.Fatalf("purged = %d, want 1", res.Purged)
	}
	if _, err := os.Stat(qp); !os.IsNotExist(err) {
		t.Fatal("expired quarantined file not purged")
	}
}

func TestReconcileKeepsQuarantineWhenRetentionOff(t *testing.T) {
	cfg := testEnv(t)
	cfg.RetentionDays = 0
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	writeOrphan(t, "old.jpg", time.Now().Add(-48*time.Hour))
	if _, err := reconcileFrames(db, cfg, false); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(quarantineDir(), "*", "old.jpg"))
	if len(matches) != 1 {
		t.Fatal("nothing quarantined")
	}
	past := time.Now().Add(-365 * 24 * time.Hour)
	os.Chtimes(matches[0], past, past)
	res, err := reconcileFrames(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Purged != 0 {
		t.Fatalf("purged = %d, want 0 with retention off", res.Purged)
	}
	if _, err := os.Stat(matches[0]); err != nil {
		t.Fatal("quarantined file purged despite retention off")
	}
}
