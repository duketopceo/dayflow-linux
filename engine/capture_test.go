package main

import (
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// addFrameFile writes a sized file into a dated frames dir and inserts its row.
func addFrameFile(t *testing.T, db *sql.DB, ts time.Time, sizeKB int) string {
	t.Helper()
	dir := filepath.Join(framesDir(), ts.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, ts.Format("150405")+".jpg")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", sizeKB*1024)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO frames(ts, path) VALUES(?, ?)`, ts.Unix(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAHashCoversWholeImage(t *testing.T) {
	// Regression: the old ahash truncated 256 bits into a uint64, so changes
	// in the lower 3/4 of the frame were invisible to dedup. Two images that
	// differ only in the bottom half must exceed the dedup threshold.
	mk := func(bottomDark bool) image.Image {
		img := image.NewRGBA(image.Rect(0, 0, 320, 200))
		for y := 0; y < 200; y++ {
			for x := 0; x < 320; x++ {
				c := color.RGBA{200, 200, 200, 255}
				if bottomDark && y >= 100 {
					c = color.RGBA{20, 20, 20, 255}
				}
				img.Set(x, y, c)
			}
		}
		return img
	}
	d := hamming(ahash(mk(false)), ahash(mk(true)))
	if d <= dedupThreshold {
		t.Fatalf("bottom-half change undetected: hamming=%d", d)
	}
}

func markSummarized(t *testing.T, db *sql.DB, cfg Config, ts time.Time) {
	t.Helper()
	bs := blockStart(ts, cfg.BlockMinutes)
	end := bs.Add(time.Duration(cfg.BlockMinutes) * time.Minute)
	if err := upsertBlock(db, bs, end, "work", "summary", "coding", 1, "done", ""); err != nil {
		t.Fatal(err)
	}
}

func TestStorageCapStopsAtBoundary(t *testing.T) {
	cfg := testEnv(t)
	cfg.MaxStorageMB = 1
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 5 frames of 400KB each ≈ 2MB; all summarized. Cap should delete the
	// oldest ~3 and keep the newest, not wipe everything.
	base := time.Now().Add(-24 * time.Hour)
	var paths []string
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		markSummarized(t, db, cfg, ts)
		paths = append(paths, addFrameFile(t, db, ts, 400))
	}
	enforceStorageCap(db, cfg)

	kept := 0
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			kept++
		}
	}
	if kept == 0 || kept == len(paths) {
		t.Fatalf("cap kept %d of %d files — boundary accounting broken", kept, len(paths))
	}
	if _, err := os.Stat(paths[len(paths)-1]); err != nil {
		t.Fatal("newest frame was deleted — oldest-first ordering broken")
	}
	if dataDirSize() > int64(cfg.MaxStorageMB)<<20 {
		t.Fatalf("data dir still over cap: %d bytes", dataDirSize())
	}
}

func TestStorageCapRunsWithRetentionOff(t *testing.T) {
	cfg := testEnv(t)
	cfg.RetentionDays = 0 // day-retention off must not skip the storage cap
	cfg.MaxStorageMB = 1
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Now().Add(-24 * time.Hour)
	var paths []string
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		markSummarized(t, db, cfg, ts)
		paths = append(paths, addFrameFile(t, db, ts, 400))
	}
	runRetention(db, cfg)
	if dataDirSize() > int64(cfg.MaxStorageMB)<<20 {
		t.Fatal("storage cap not enforced when retention_days=0")
	}
}

// Frames under terminal-but-unsummarized blocks (failed/dead — e.g. during a
// provider outage) must be cap-reclaimable; otherwise the cap starves and
// phase 3 eats journal blocks while dead frames sit unreclaimed.
func TestStorageCapReclaimsDeadBlockFrames(t *testing.T) {
	cfg := testEnv(t)
	cfg.MaxStorageMB = 1
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Now().Add(-24 * time.Hour)
	var dead []string
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		bs := blockStart(ts, cfg.BlockMinutes)
		if err := upsertBlock(db, bs, bs.Add(time.Duration(cfg.BlockMinutes)*time.Minute),
			"", "", "", 1, "dead", "provider down"); err != nil {
			t.Fatal(err)
		}
		dead = append(dead, addFrameFile(t, db, ts, 400))
	}
	pending := addFrameFile(t, db, base.Add(30*time.Minute), 400)

	enforceStorageCap(db, cfg)
	kept := 0
	for _, p := range dead {
		if _, err := os.Stat(p); err == nil {
			kept++
		}
	}
	if kept == len(dead) {
		t.Fatal("dead-block frames were never reclaimed")
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatal("pending frame deleted while dead-block frames could cover the cap")
	}
}

// When terminal frames alone can't cover the cap, the oldest remaining
// frames go too — a screenshot is cheaper to lose than a journal block.
func TestStorageCapFallsBackToPendingFrames(t *testing.T) {
	cfg := testEnv(t)
	cfg.MaxStorageMB = 1
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Now().Add(-24 * time.Hour)
	ts := base
	bs := blockStart(ts, cfg.BlockMinutes)
	if err := upsertBlock(db, bs, bs.Add(time.Duration(cfg.BlockMinutes)*time.Minute),
		"", "", "", 1, "dead", "provider down"); err != nil {
		t.Fatal(err)
	}
	deadPath := addFrameFile(t, db, ts, 400)
	// Pending frames alone still exceed the cap once the dead one is gone.
	var pending []string
	for i := 1; i <= 4; i++ {
		pending = append(pending, addFrameFile(t, db, base.Add(time.Duration(i*10)*time.Minute), 400))
	}

	enforceStorageCap(db, cfg)
	if _, err := os.Stat(deadPath); err == nil {
		t.Fatal("terminal frame not reclaimed first")
	}
	if _, err := os.Stat(pending[len(pending)-1]); err != nil {
		t.Fatal("newest pending frame deleted — oldest-first ordering broken")
	}
	if dataDirSize() > int64(cfg.MaxStorageMB)<<20 {
		t.Fatal("cap still not met — pending frames were not reclaimed")
	}
}

func TestStorageCapPreservesUnsummarized(t *testing.T) {
	cfg := testEnv(t)
	cfg.MaxStorageMB = 1
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Now().Add(-24 * time.Hour)
	var summarized []string
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		markSummarized(t, db, cfg, ts)
		summarized = append(summarized, addFrameFile(t, db, ts, 400))
	}
	// newest frame has no done block — still pending summarization
	pending := addFrameFile(t, db, base.Add(10*time.Minute), 400)

	enforceStorageCap(db, cfg)
	if _, err := os.Stat(pending); err != nil {
		t.Fatal("unsummarized frame deleted by storage cap")
	}
}

func TestStorageCapZeroIsUnlimited(t *testing.T) {
	cfg := testEnv(t)
	cfg.MaxStorageMB = 0
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Now().Add(-24 * time.Hour)
	var paths []string
	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		markSummarized(t, db, cfg, ts)
		paths = append(paths, addFrameFile(t, db, ts, 400))
	}
	enforceStorageCap(db, cfg)
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("frame deleted despite unlimited storage cap")
		}
	}
}

func TestStorageCapDeletesStaleRowsToo(t *testing.T) {
	cfg := testEnv(t)
	cfg.MaxStorageMB = 1
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ts := time.Now().Add(-24 * time.Hour)
	markSummarized(t, db, cfg, ts)
	p := addFrameFile(t, db, ts, 1500)
	enforceStorageCap(db, cfg)

	var n int
	db.QueryRow(`SELECT COUNT(1) FROM frames WHERE path=?`, p).Scan(&n)
	if n != 0 {
		t.Fatal("frame row survived file deletion")
	}
	var ev int
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='storage_cap_frames'`).Scan(&ev)
	if ev != 1 {
		t.Fatal("missing storage_cap_frames event")
	}
}

func TestStorageCapFloorBoundsBlockPruning(t *testing.T) {
	cfg := testEnv(t)
	cfg.MaxStorageMB = 1
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// >1MB of event rows with no frames to delete: cap must trim logs and
	// prune at most one bounded chunk of blocks per pass, then give up.
	for i := 0; i < 2500; i++ {
		db.Exec(`INSERT INTO events(ts, type, detail) VALUES(?, 'noise', ?)`,
			time.Now().Unix(), strings.Repeat("d", 500))
	}
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < 150; i++ {
		bs := base.Add(time.Duration(i) * time.Duration(cfg.BlockMinutes) * time.Minute)
		upsertBlock(db, bs, bs.Add(time.Duration(cfg.BlockMinutes)*time.Minute),
			"work", strings.Repeat("s", 1000), "coding", 1, "done", "")
	}
	enforceStorageCap(db, cfg)

	var events, blocks int
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='noise'`).Scan(&events)
	db.QueryRow(`SELECT COUNT(1) FROM blocks`).Scan(&blocks)
	if events > 2000 {
		t.Fatalf("events not trimmed: %d", events)
	}
	if blocks < 50 {
		t.Fatalf("block pruning unbounded: only %d of 150 remain", blocks)
	}
	fmt.Printf("after pass: events=%d blocks=%d size=%d\n", events, blocks, dataDirSize())
}
