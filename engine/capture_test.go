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

// A sub-320px long edge destroys journal evidence: setConfigValue rejects
// negatives and clamps tiny positives; a config patch bypasses setConfigValue
// entirely, so loadConfig floors the value too.
func TestFrameMaxDimFloor(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	os.WriteFile(cp, []byte(`{}`), 0o600)

	if err := setConfigValue("frame_max_dim", "-5"); err == nil {
		t.Fatal("negative frame_max_dim accepted")
	}
	if err := setConfigValue("frame_max_dim", "64"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := loadConfig()
	if cfg.FrameMaxDim != minFrameMaxDim {
		t.Fatalf("tiny positive should clamp to %d, got %d", minFrameMaxDim, cfg.FrameMaxDim)
	}

	// config patch / hand-edit bypasses setConfigValue — loadConfig floors it.
	os.WriteFile(cp, []byte(`{"frame_max_dim": 100}`), 0o600)
	cfg, _ = loadConfig()
	if cfg.FrameMaxDim != minFrameMaxDim {
		t.Fatalf("patched tiny value not floored: %d", cfg.FrameMaxDim)
	}
	// A patched negative is invalid (0 is the only sub-minimum that means
	// something) — reset to the default.
	os.WriteFile(cp, []byte(`{"frame_max_dim": -10}`), 0o600)
	cfg, _ = loadConfig()
	if cfg.FrameMaxDim != defaultConfig().FrameMaxDim {
		t.Fatalf("patched negative not reset to default: %d", cfg.FrameMaxDim)
	}
	// 0 still means "keep native size".
	os.WriteFile(cp, []byte(`{"frame_max_dim": 0}`), 0o600)
	cfg, _ = loadConfig()
	if cfg.FrameMaxDim != 0 {
		t.Fatalf("frame_max_dim=0 must stay disabled, got %d", cfg.FrameMaxDim)
	}
}

// jpeg_quality owns the re-encode of oversized frames: the same source must
// shrink at q10 vs q95, and both results must still decode as JPEG.
func TestJPEGQualityAffectsStoredSize(t *testing.T) {
	stored := func(t *testing.T, quality int) int64 {
		cfg := testEnv(t)
		cfg.JPEGQuality = quality
		db, err := openDB()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		src := jpegBytes(t, sizedImage(3456, 2160), 90)
		if _, _, err := captureJPEG(t, db, cfg, src, nil); err != nil {
			t.Fatal(err)
		}
		// lastStoredFrame also asserts the file JPEG-decodes and frames.bytes
		// matches the on-disk size.
		_, b := lastStoredFrame(t, db)
		return b
	}
	lo := stored(t, 10)
	hi := stored(t, 95)
	if lo >= hi {
		t.Fatalf("quality had no effect: q10=%d bytes, q95=%d bytes", lo, hi)
	}
}

// A hung capture_command must not stall the capture loop — grabFrame is
// bounded by grabFrameTimeout.
func TestGrabFrameTimeout(t *testing.T) {
	orig := grabFrameTimeout
	grabFrameTimeout = 250 * time.Millisecond
	t.Cleanup(func() { grabFrameTimeout = orig })

	start := time.Now()
	_, err := grabFrame([]string{"sleep", "30"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err=%v, want a timeout error", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("grabFrame did not return promptly after the timeout")
	}
}

// ---------------------------------------------------------------------------
// output=auto: focus-following capture (R1/KTD1)
//
// hyprctl and grim are PATH stubs — tests must never exec the real binaries.
// The grim stub records its argv per call; the hyprctl stub answers
// `monitors -j` from a fixture file and `activewindow -j` with an empty
// object (so activeWindowClass degrades quietly).
// ---------------------------------------------------------------------------

// writeBin installs an executable shell script named name into dir.
func writeBin(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// resetFocusState restores the output=auto package state — the negative
// cache is process-global, not per-daemon, so each test starts clean.
func resetFocusState(t *testing.T) {
	t.Helper()
	focusFails, focusDisabled, lastAutoOutput = 0, false, ""
	t.Cleanup(func() { focusFails, focusDisabled, lastAutoOutput = 0, false, "" })
}

// fakeGrim installs a `grim` stub into dir that appends its argv to
// <dir>/grim-argv.log (one line per call) and writes frameFile to stdout
// like `grim -` does. failOnO makes it exit 1 whenever -o is present —
// the resolve→exec race that captureOnce retries as a composite grab.
func fakeGrim(t *testing.T, dir, frameFile string, failOnO bool) string {
	t.Helper()
	log := filepath.Join(dir, "grim-argv.log")
	body := "echo \"$@\" >> \"" + log + "\"\n"
	if failOnO {
		body += "for a in \"$@\"; do [ \"$a\" = -o ] && exit 1; done\n"
	}
	body += "/bin/cat \"" + frameFile + "\"\n"
	writeBin(t, dir, "grim", body)
	return log
}

// fakeHyprctl installs a `hyprctl` stub into dir. `monitors -j` prints
// monitorsJSON (or exits 1 when failMonitors); any other subcommand
// prints an empty object. Every subcommand name is appended to the
// returned calls log; the returned monFile can be rewritten mid-test to
// simulate a focus move.
func fakeHyprctl(t *testing.T, dir, monitorsJSON string, failMonitors bool) (calls, monFile string) {
	t.Helper()
	calls = filepath.Join(dir, "hyprctl-calls.log")
	monFile = filepath.Join(dir, "monitors.json")
	if err := os.WriteFile(monFile, []byte(monitorsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "echo \"$1\" >> \"" + calls + "\"\n"
	body += "if [ \"$1\" = monitors ]; then\n"
	if failMonitors {
		body += "  exit 1\n"
	} else {
		body += "  /bin/cat \"" + monFile + "\"\n"
	}
	body += "else\n  echo '{}'\nfi\n"
	writeBin(t, dir, "hyprctl", body)
	return calls, monFile
}

// frameFixture writes a decodable JPEG for the grim stub to emit.
func frameFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "frame.jpg")
	if err := os.WriteFile(p, jpegBytes(t, sizedImage(640, 480), 90), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func argvLines(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("grim argv log: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func hasOArg(line string) bool {
	for _, f := range strings.Fields(line) {
		if f == "-o" {
			return true
		}
	}
	return false
}

func countCalls(t *testing.T, log, subcmd string) int {
	t.Helper()
	b, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if l == subcmd {
			n++
		}
	}
	return n
}

func TestAutoOutputFocusedMonitor(t *testing.T) {
	cfg := testEnv(t)
	cfg.Output = "auto"
	resetFocusState(t)
	bin := t.TempDir()
	_, monFile := fakeHyprctl(t, bin, `[
		{"name":"eDP-1","focused":false},
		{"name":"HEADLESS-1","focused":false},
		{"name":"DP-3","focused":true}
	]`, false)
	argvLog := fakeGrim(t, bin, frameFixture(t), false)
	t.Setenv("PATH", bin)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cmdArgs, err := resolveCaptureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, attempted, err := captureOnce(db, cfg, cmdArgs, nil); err != nil || !attempted {
		t.Fatalf("attempted=%v err=%v", attempted, err)
	}
	lines := argvLines(t, argvLog)
	if len(lines) != 1 || !strings.Contains(lines[0], "-o DP-3") {
		t.Fatalf("grim argv %v should carry -o DP-3", lines)
	}
	var ev int
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='capture_output' AND detail='DP-3'`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("capture_output events=%d, want 1", ev)
	}
	// Same output next tick: resolves again but does not re-log.
	if _, _, err := captureOnce(db, cfg, cmdArgs, nil); err != nil {
		t.Fatal(err)
	}
	// Focus moves to another output: exactly one transition event.
	if err := os.WriteFile(monFile, []byte(`[{"name":"eDP-1","focused":true}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := captureOnce(db, cfg, cmdArgs, nil); err != nil {
		t.Fatal(err)
	}
	lines = argvLines(t, argvLog)
	if !strings.Contains(lines[len(lines)-1], "-o eDP-1") {
		t.Fatalf("grim argv %q should carry -o eDP-1 after focus move", lines[len(lines)-1])
	}
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='capture_output'`).Scan(&ev)
	if ev != 2 {
		t.Fatalf("resolution changes should log once each: events=%d", ev)
	}
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&n)
	if n != 3 { // nil lastHash each call — every tick stores
		t.Fatalf("frames=%d, want 3", n)
	}
}

func TestAutoOutputCompositeFallbacks(t *testing.T) {
	cases := []struct {
		name         string
		monitorsJSON string
		failMonitors bool
		noHyprctl    bool
	}{
		{"no focused output", `[{"name":"eDP-1","focused":false}]`, false, false},
		{"only HEADLESS focused",
			`[{"name":"HEADLESS-1","focused":true},{"name":"eDP-1","focused":false}]`, false, false},
		{"hyprctl exits nonzero", ``, true, false},
		{"hyprctl absent", "", false, true},
		{"invalid monitors json", `not json`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testEnv(t)
			cfg.Output = "auto"
			resetFocusState(t)
			bin := t.TempDir()
			if !tc.noHyprctl {
				fakeHyprctl(t, bin, tc.monitorsJSON, tc.failMonitors)
			}
			argvLog := fakeGrim(t, bin, frameFixture(t), false)
			t.Setenv("PATH", bin)

			db, err := openDB()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			cmdArgs, err := resolveCaptureCommand(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, attempted, err := captureOnce(db, cfg, cmdArgs, nil); err != nil || !attempted {
				t.Fatalf("composite capture should succeed: attempted=%v err=%v", attempted, err)
			}
			for _, l := range argvLines(t, argvLog) {
				if hasOArg(l) {
					t.Fatalf("composite argv must not carry -o: %q", l)
				}
			}
			var n int
			db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&n)
			if n != 1 {
				t.Fatalf("frames=%d, want 1", n)
			}
		})
	}
}

func TestAutoOutputGrimOFailureRetriesComposite(t *testing.T) {
	// grim -o fails (resolve→exec race: output unplugged mid-tick) — the
	// tick retries once with the composite argv instead of counting a
	// grab failure.
	cfg := testEnv(t)
	cfg.Output = "auto"
	resetFocusState(t)
	bin := t.TempDir()
	fakeHyprctl(t, bin, `[{"name":"DP-3","focused":true}]`, false)
	argvLog := fakeGrim(t, bin, frameFixture(t), true) // fails when -o present
	t.Setenv("PATH", bin)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cmdArgs, err := resolveCaptureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h, attempted, err := captureOnce(db, cfg, cmdArgs, nil)
	if err != nil || !attempted || h == nil {
		t.Fatalf("composite retry should save the tick: attempted=%v h=%v err=%v", attempted, h, err)
	}
	lines := argvLines(t, argvLog)
	if len(lines) != 2 {
		t.Fatalf("grim calls %v — want exactly one -o attempt + one composite retry", lines)
	}
	if !strings.Contains(lines[0], "-o DP-3") {
		t.Fatalf("first call %q should be the -o grab", lines[0])
	}
	if hasOArg(lines[1]) {
		t.Fatalf("retry %q should be composite", lines[1])
	}
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&n)
	if n != 1 {
		t.Fatalf("frames=%d, want 1", n)
	}
}

func TestAutoOutputNegativeCache(t *testing.T) {
	// After focusFailCeiling consecutive hyprctl failures the daemon stops
	// execing `hyprctl monitors` for the rest of the run — one event, not
	// per-tick spam — and keeps capturing composite frames.
	cfg := testEnv(t)
	cfg.Output = "auto"
	resetFocusState(t)
	bin := t.TempDir()
	calls, _ := fakeHyprctl(t, bin, "", true) // monitors always fails
	argvLog := fakeGrim(t, bin, frameFixture(t), false)
	t.Setenv("PATH", bin)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cmdArgs, err := resolveCaptureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < focusFailCeiling+3; i++ {
		if _, attempted, err := captureOnce(db, cfg, cmdArgs, nil); err != nil || !attempted {
			t.Fatalf("tick %d: attempted=%v err=%v", i, attempted, err)
		}
	}
	if got := countCalls(t, calls, "monitors"); got != focusFailCeiling {
		t.Fatalf("hyprctl monitors execed %d times, want %d (negative cache)", got, focusFailCeiling)
	}
	var ev int
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='capture_output_disabled'`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("capture_output_disabled events=%d, want 1", ev)
	}
	for _, l := range argvLines(t, argvLog) {
		if hasOArg(l) {
			t.Fatalf("disabled focus resolution must stay composite: %q", l)
		}
	}
}

func TestFocusedOutputTimeout(t *testing.T) {
	// A hung hyprctl must degrade to composite, not wedge the tick.
	old := tickExecTimeout
	tickExecTimeout = 100 * time.Millisecond
	t.Cleanup(func() { tickExecTimeout = old })

	bin := t.TempDir()
	// `exec` so the stub process IS the sleeper — a plain `sleep` would be
	// a grandchild keeping the stdout pipe open past the context kill.
	writeBin(t, bin, "hyprctl", "exec sleep 30\n")
	t.Setenv("PATH", bin+":"+os.Getenv("PATH")) // `sleep` resolves; hyprctl stub shadows

	start := time.Now()
	name, err := focusedOutput()
	if err == nil || name != "" {
		t.Fatalf("hung hyprctl: name=%q err=%v, want error", name, err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("focusedOutput did not return promptly after the timeout")
	}
}

func TestExplicitOutputPassthrough(t *testing.T) {
	// An explicit output name bakes -o into the argv once — focus
	// resolution is never consulted for a static output.
	cfg := testEnv(t)
	cfg.Output = "eDP-1"
	resetFocusState(t)
	bin := t.TempDir()
	calls, _ := fakeHyprctl(t, bin, `[{"name":"DP-3","focused":true}]`, false)
	argvLog := fakeGrim(t, bin, frameFixture(t), false)
	t.Setenv("PATH", bin)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cmdArgs, err := resolveCaptureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmdArgs, " "), "-o eDP-1") {
		t.Fatalf("explicit output should bake -o eDP-1 into argv: %v", cmdArgs)
	}
	if _, _, err := captureOnce(db, cfg, cmdArgs, nil); err != nil {
		t.Fatal(err)
	}
	for _, l := range argvLines(t, argvLog) {
		if !strings.Contains(l, "-o eDP-1") || strings.Contains(l, "DP-3") {
			t.Fatalf("grim argv %q should be the static -o eDP-1", l)
		}
	}
	if got := countCalls(t, calls, "monitors"); got != 0 {
		t.Fatalf("explicit output execed hyprctl monitors %d times, want 0", got)
	}
	var ev int
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='capture_output'`).Scan(&ev)
	if ev != 0 {
		t.Fatalf("static output logged %d resolution events, want 0", ev)
	}
}

func TestAutoIgnoredWithCaptureCommand(t *testing.T) {
	// capture_command is used verbatim — output=auto must never inject -o
	// into it and focus resolution must not run.
	cfg := testEnv(t)
	cfg.Output = "auto"
	cfg.CaptureCommand = "/bin/cat " + frameFixture(t)
	resetFocusState(t)
	bin := t.TempDir()
	calls, _ := fakeHyprctl(t, bin, `[{"name":"DP-3","focused":true}]`, false)
	t.Setenv("PATH", bin)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cmdArgs, err := resolveCaptureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, attempted, err := captureOnce(db, cfg, cmdArgs, nil); err != nil || !attempted {
		t.Fatalf("attempted=%v err=%v", attempted, err)
	}
	if got := countCalls(t, calls, "monitors"); got != 0 {
		t.Fatalf("capture_command + auto execed hyprctl monitors %d times, want 0", got)
	}
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&n)
	if n != 1 {
		t.Fatalf("frames=%d, want 1", n)
	}
}

func TestResolveCaptureCommandAuto(t *testing.T) {
	cfg := testEnv(t)
	bin := t.TempDir()
	fakeGrim(t, bin, frameFixture(t), false)
	t.Setenv("PATH", bin)

	// output=auto returns the composite base — the -o target is injected
	// per tick, not baked in at resolve time.
	cfg.Output = "auto"
	args, err := resolveCaptureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if hasOArg(strings.Join(args, " ")) {
		t.Fatalf("auto should not bake -o into argv: %v", args)
	}
	cfg.Output = "DP-3"
	args, err = resolveCaptureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(args, " "), "-o DP-3") {
		t.Fatalf("explicit output should bake -o: %v", args)
	}
	// capture_command wins verbatim, even with output=auto set.
	cfg.CaptureCommand = "/bin/cat /tmp/f.jpg"
	args, err = resolveCaptureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "/bin/cat" {
		t.Fatalf("capture_command argv should pass through: %v", args)
	}
}

func TestConfigSetOutput(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	os.WriteFile(cp, []byte(`{}`), 0o600)

	// "auto" opts into focus-following capture.
	if err := setConfigValue("output", "auto"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := loadConfig()
	if cfg.Output != "auto" {
		t.Fatalf("output=%q after set auto", cfg.Output)
	}
	// explicit names still round-trip
	if err := setConfigValue("output", "DP-3"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = loadConfig()
	if cfg.Output != "DP-3" {
		t.Fatalf("output=%q after set DP-3", cfg.Output)
	}
	// empty clears back to composite
	if err := setConfigValue("output", ""); err != nil {
		t.Fatal(err)
	}
	cfg, _ = loadConfig()
	if cfg.Output != "" {
		t.Fatalf("output=%q after clear", cfg.Output)
	}
}
