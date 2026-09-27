package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net"
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

func TestWaylandReachable(t *testing.T) {
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)

	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	if waylandReachable() {
		t.Fatal("socket absent — should be unreachable")
	}
	// a regular file is not a wayland socket
	if err := os.WriteFile(filepath.Join(rt, "wayland-1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if waylandReachable() {
		t.Fatal("regular file is not a socket — should be unreachable")
	}
	// a real unix socket makes it reachable
	if err := os.Remove(filepath.Join(rt, "wayland-1")); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(rt, "wayland-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !waylandReachable() {
		t.Fatal("socket present — should be reachable")
	}
	// no display configured: can't check — let the capture command decide
	t.Setenv("WAYLAND_DISPLAY", "")
	if !waylandReachable() {
		t.Fatal("empty WAYLAND_DISPLAY should defer to the command")
	}
}

func TestWaylandReachableAbsolutePath(t *testing.T) {
	// An absolute WAYLAND_DISPLAY is used as-is, without XDG_RUNTIME_DIR.
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	sock := filepath.Join(t.TempDir(), "abs-display")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Setenv("WAYLAND_DISPLAY", sock)
	if !waylandReachable() {
		t.Fatal("absolute socket path should be reachable")
	}
	ln.Close()
	os.Remove(sock)
	if waylandReachable() {
		t.Fatal("removed absolute socket should be unreachable")
	}
	// Unset runtime dir can't resolve a relative display — defer to command.
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-9")
	if !waylandReachable() {
		t.Fatal("relative display with no XDG_RUNTIME_DIR should defer to the command")
	}
}

func TestCaptureFailVisible(t *testing.T) {
	// First failure always logs; the streak then reports every ~5 min
	// (30 ticks at the default 10s interval) so a dead-session window
	// can't spam the debug log, events table, and journald.
	if !captureFailVisible(1) {
		t.Fatal("first failure must be visible")
	}
	for s := 2; s < 30; s++ {
		if captureFailVisible(s) {
			t.Fatalf("streak %d should be quiet", s)
		}
	}
	if !captureFailVisible(30) || !captureFailVisible(60) {
		t.Fatal("streak multiples of 30 must be visible")
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

// sizedImage builds a non-uniform w×h frame by writing Pix directly —
// per-pixel Set on multi-megapixel fixtures would dominate the test run.
func sizedImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		p := i / 4
		img.Pix[i] = uint8(p % 251)
		img.Pix[i+1] = uint8((p / w) % 241)
		img.Pix[i+2] = uint8((p % w) / 7)
		img.Pix[i+3] = 255
	}
	return img
}

func jpegBytes(t *testing.T, img image.Image, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// captureJPEG points captureOnce's frame source at a fixture file — `cat`
// writes the bytes to stdout exactly like `grim -t jpeg -` does.
func captureJPEG(t *testing.T, db *sql.DB, cfg Config, frame []byte, lastHash *frameHash) (*frameHash, bool, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "frame.jpg")
	if err := os.WriteFile(p, frame, 0o600); err != nil {
		t.Fatal(err)
	}
	return captureOnce(db, cfg, []string{"cat", p}, lastHash)
}

// lastStoredFrame decodes the most recently inserted frame file and checks
// frames.bytes matches the file on disk. rowid order — not ts — disambiguates
// captures that land in the same second.
func lastStoredFrame(t *testing.T, db *sql.DB) (image.Image, int64) {
	t.Helper()
	var p string
	var recorded int64
	if err := db.QueryRow(`SELECT path, bytes FROM frames ORDER BY rowid DESC LIMIT 1`).Scan(&p, &recorded); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(raw)) != recorded {
		t.Fatalf("frames.bytes=%d but file is %d bytes", recorded, len(raw))
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("stored frame is not JPEG-decodable: %v", err)
	}
	return img, recorded
}

func TestFrameNormalizationDownscales(t *testing.T) {
	cfg := testEnv(t) // FrameMaxDim = 1920
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	src := jpegBytes(t, sizedImage(3456, 2160), 90)
	h, attempted, err := captureJPEG(t, db, cfg, src, nil)
	if err != nil || !attempted || h == nil {
		t.Fatalf("attempted=%v hash=%v err=%v", attempted, h, err)
	}
	img, b := lastStoredFrame(t, db)
	if d := img.Bounds().Dx(); d != 1920 {
		t.Fatalf("stored width=%d, want 1920", d)
	}
	if d := img.Bounds().Dy(); d != 1200 {
		t.Fatalf("stored height=%d, want 1200", d)
	}
	if b >= int64(len(src)) {
		t.Fatalf("stored %d bytes, source was %d — normalization didn't shrink", b, len(src))
	}
}

func TestFrameNormalizationComposite(t *testing.T) {
	// grim emits an all-outputs composite — a docked frame is far wider than
	// any single panel. The cap bounds the composite's long edge only.
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	src := jpegBytes(t, sizedImage(6900, 1440), 90)
	if _, _, err := captureJPEG(t, db, cfg, src, nil); err != nil {
		t.Fatal(err)
	}
	img, _ := lastStoredFrame(t, db)
	if img.Bounds().Dx() > cfg.FrameMaxDim || img.Bounds().Dy() > cfg.FrameMaxDim {
		t.Fatalf("stored bounds %v exceed frame_max_dim %d", img.Bounds(), cfg.FrameMaxDim)
	}
	if img.Bounds().Dx() != cfg.FrameMaxDim {
		t.Fatalf("long edge should reach the cap exactly, got %v", img.Bounds())
	}
}

func TestFrameNormalizationPassthrough(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// frame_max_dim=0 disables normalization — byte-identical storage.
	cfg.FrameMaxDim = 0
	big := jpegBytes(t, sizedImage(3456, 2160), 90)
	if _, _, err := captureJPEG(t, db, cfg, big, nil); err != nil {
		t.Fatal(err)
	}
	var p string
	if err := db.QueryRow(`SELECT path FROM frames ORDER BY rowid DESC LIMIT 1`).Scan(&p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, big) {
		t.Fatal("frame_max_dim=0 must store the capture bytes untouched")
	}

	// A frame already under the cap passes through without a re-encode.
	cfg.FrameMaxDim = 1920
	small := jpegBytes(t, sizedImage(800, 600), 90)
	if _, _, err := captureJPEG(t, db, cfg, small, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT path FROM frames ORDER BY rowid DESC LIMIT 1`).Scan(&p); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, small) {
		t.Fatal("under-cap frame should keep its original bytes")
	}
}

func TestFrameNormalizationPortrait(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	src := jpegBytes(t, sizedImage(1080, 2400), 90)
	if _, _, err := captureJPEG(t, db, cfg, src, nil); err != nil {
		t.Fatal(err)
	}
	img, _ := lastStoredFrame(t, db)
	if img.Bounds().Dy() != 1920 || img.Bounds().Dx() != 864 {
		t.Fatalf("stored bounds %v — taller edge should hit the cap", img.Bounds())
	}
}

func TestFrameDedupHashBeforeResize(t *testing.T) {
	// ahash runs on the decoded frame before normalization, so an identical
	// recapture dedups exactly as it did before resizing existed.
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	src := jpegBytes(t, sizedImage(3456, 2160), 90)
	h1, _, err := captureJPEG(t, db, cfg, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	h2, attempted, err := captureJPEG(t, db, cfg, src, h1)
	if err != nil || !attempted {
		t.Fatalf("attempted=%v err=%v", attempted, err)
	}
	if h2 != h1 {
		t.Fatal("deduped capture must carry the prior hash forward")
	}
	var n, ev int
	db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&n)
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='capture_deduped'`).Scan(&ev)
	if n != 1 || ev != 1 {
		t.Fatalf("frames=%d dedup_events=%d — identical capture was stored again", n, ev)
	}
}

func TestFrameCorruptSourceKeepsErrorPath(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	h, attempted, err := captureJPEG(t, db, cfg, []byte("definitely not a jpeg"), nil)
	if err == nil || !attempted || h != nil {
		t.Fatalf("attempted=%v hash=%v err=%v", attempted, h, err)
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error should surface the decode path, got %v", err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&n)
	if n != 0 {
		t.Fatal("corrupt frame was stored")
	}
}

func TestConfigSetFrameMaxDim(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	os.WriteFile(cp, []byte(`{}`), 0o600)

	cfg, err := loadConfig()
	if err != nil || cfg.FrameMaxDim != 1920 {
		t.Fatalf("default frame_max_dim=%d err=%v", cfg.FrameMaxDim, err)
	}
	if err := setConfigValue("frame_max_dim", "2560"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = loadConfig()
	if cfg.FrameMaxDim != 2560 {
		t.Fatalf("frame_max_dim=%d after set", cfg.FrameMaxDim)
	}
	// explicit 0 disables normalization and must survive a reload
	if err := setConfigValue("frame_max_dim", "0"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = loadConfig()
	if cfg.FrameMaxDim != 0 {
		t.Fatalf("frame_max_dim=%d after set 0", cfg.FrameMaxDim)
	}
}
