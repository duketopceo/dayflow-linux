package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func configMtime() time.Time {
	if fi, err := os.Stat(configPath()); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

func paused() bool {
	_, err := os.Stat(pausePath())
	return err == nil
}

func setPaused(p bool) {
	if p {
		os.MkdirAll(dataDir(), 0o700)
		os.WriteFile(pausePath(), []byte("paused\n"), 0o600)
	} else {
		os.Remove(pausePath())
	}
}

// activeWindowClass returns the class of the focused window on Hyprland.
// It falls back to the window title when no class is reported, and returns
// "" when there is no focused window / not running under Hyprland.
func activeWindowClass() string {
	out, err := exec.Command("hyprctl", "activewindow", "-j").Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	var w struct {
		Class        string `json:"class"`
		InitialClass string `json:"initialClass"`
		Title        string `json:"title"`
		InitialTitle string `json:"initialTitle"`
	}
	if json.Unmarshal(out, &w) != nil {
		return ""
	}
	if w.Class != "" {
		return w.Class
	}
	if w.InitialClass != "" {
		return w.InitialClass
	}
	if w.Title != "" {
		return w.Title
	}
	return w.InitialTitle
}

func isIgnored(cfg Config, class string) bool {
	if class == "" {
		return false
	}
	for _, ig := range cfg.IgnoreApps {
		if strings.EqualFold(strings.TrimSpace(ig), class) {
			return true
		}
	}
	return false
}

// screenLocked uses loginctl to detect whether the current graphical session is locked.
// It is best-effort: if loginctl is unavailable or the session cannot be identified,
// it returns false so capture continues.
func screenLocked() bool {
	sessions := []string{os.Getenv("XDG_SESSION_ID")}
	if sessions[0] == "" {
		// Fall back to the active graphical session for the current user.
		out, err := exec.Command("loginctl", "list-sessions", "--no-legend").Output()
		if err != nil {
			return false
		}
		user := os.Getenv("USER")
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			// fields: ID UID USER SEAT [TTY ...]
			if fields[2] == user && fields[3] != "-" {
				sessions = append(sessions, fields[0])
			}
		}
		if len(sessions) == 1 {
			return false
		}
		sessions = sessions[1:]
	}
	for _, sid := range sessions {
		if sid == "" {
			continue
		}
		out, err := exec.Command("loginctl", "show-session", sid, "--property=LockedHint").Output()
		if err != nil || len(out) == 0 {
			continue
		}
		if strings.Contains(string(out), "yes") {
			return true
		}
	}
	return false
}

// frameHash is a 256-bit perceptual hash (16x16 grayscale average hash).
type frameHash [4]uint64

// ahash computes a 16x16 grayscale average-hash of the image.
func ahash(img image.Image) frameHash {
	const size = 16
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var px [size * size]uint32
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sx := b.Min.X + x*w/size
			sy := b.Min.Y + y*h/size
			r, g, bl, _ := img.At(sx, sy).RGBA()
			px[y*size+x] = (r + g + bl) / 3 >> 8
		}
	}
	var sum uint64
	for _, v := range px {
		sum += uint64(v)
	}
	avg := uint32(sum / (size * size))
	var hash frameHash
	for i, v := range px {
		if v >= avg {
			hash[i/64] |= 1 << (i % 64)
		}
	}
	return hash
}

func hamming(a, b frameHash) int {
	n := 0
	for i := range a {
		for d := a[i] ^ b[i]; d != 0; d >>= 1 {
			n += int(d & 1)
		}
	}
	return n
}

// resolveCaptureCommand picks the screenshot tool. grim (wlroots: Hyprland,
// sway, river, ...) is the only built-in backend; capture_command in config can
// point at anything that writes a JPEG/PNG to stdout.
func resolveCaptureCommand(cfg Config) ([]string, error) {
	if cfg.CaptureCommand != "" {
		return strings.Fields(cfg.CaptureCommand), nil
	}
	if _, err := exec.LookPath("grim"); err != nil {
		return nil, fmt.Errorf("grim not found — install it (wlroots compositors) or set capture_command in %s", configPath())
	}
	args := []string{"grim", "-t", "jpeg", "-q", fmt.Sprint(cfg.JPEGQuality)}
	if cfg.Output != "" {
		args = append(args, "-o", cfg.Output)
	}
	return append(args, "-"), nil
}

func grabFrame(cmdArgs []string) ([]byte, error) {
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

const dedupThreshold = 5 // hamming distance out of 256 bits

// captureOnce samples the screen and stores a frame when it has changed.
// lastHash is the previous frame's hash, or nil when no frame has been
// sampled yet (e.g. after unlock or pause). The returned hash is the latest
// sampled hash — unchanged on early returns.
func captureOnce(db *sql.DB, cfg Config, cmdArgs []string, lastHash *frameHash) (*frameHash, error) {
	if paused() {
		return lastHash, nil
	}
	cls := activeWindowClass()
	if isIgnored(cfg, cls) {
		logEvent(db, "capture_ignored", cls)
		debugf(cfg, "capture: ignored app %s", cls)
		return lastHash, nil
	}
	raw, err := grabFrame(cmdArgs)
	if err != nil {
		logEvent(db, "capture_error", err.Error())
		debugf(cfg, "capture: grab failed: %v", err)
		return lastHash, fmt.Errorf("capture: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		logEvent(db, "capture_error", "decode: "+err.Error())
		debugf(cfg, "capture: decode failed: %v", err)
		return lastHash, fmt.Errorf("decode: %w", err)
	}
	h := ahash(img)
	if lastHash != nil && hamming(h, *lastHash) <= dedupThreshold {
		logEvent(db, "capture_deduped", "")
		debugf(cfg, "capture: deduped (hamming %d, app %s)", hamming(h, *lastHash), cls)
		return lastHash, nil // screen unchanged
	}

	now := time.Now()
	dayDir := filepath.Join(framesDir(), now.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		return lastHash, err
	}
	path := filepath.Join(dayDir, now.Format("150405")+".jpg")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return lastHash, err
	}
	if err := insertFrameApp(db, now, path, cls); err != nil {
		os.Remove(path)
		return lastHash, err
	}
	logEvent(db, "capture_saved", path)
	debugf(cfg, "capture: saved %s (%d bytes, app %s)", filepath.Base(path), len(raw), cls)
	return &h, nil
}

// runRetention reconciles the frames dir with the frames table, then deletes
// frames and old log rows past the retention window.
func runRetention(db *sql.DB, cfg Config) {
	if _, err := reconcileFrames(db, cfg, false); err != nil {
		logEvent(db, "reconcile_error", err.Error())
	}
	if cfg.RetentionDays > 0 {
		cutoff := time.Now().Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour)
		paths, err := framesBefore(db, cutoff)
		if err != nil {
			logEvent(db, "retention_error", err.Error())
		} else {
			for _, p := range paths {
				os.Remove(p)
			}
			if len(paths) > 0 {
				logEvent(db, "retention_pruned", fmt.Sprintf("%d frames", len(paths)))
			}
		}
		pruneOldEvents(db, cutoff)
	}
	// the storage cap is independent of day retention — it always runs
	enforceStorageCap(db, cfg)
	// drop empty day directories
	days, _ := filepath.Glob(filepath.Join(framesDir(), "*"))
	for _, d := range days {
		if entries, _ := os.ReadDir(d); len(entries) == 0 {
			os.Remove(d)
		}
	}
}

// enforceStorageCap keeps the whole data dir (quarantine + frames + db + wal)
// under max_storage_mb, deleting cheapest-to-regenerate data first:
// quarantined orphans, then oldest already-summarized frames, then trimmed log
// rows, then bounded chunks of the oldest blocks as a last resort.
func enforceStorageCap(db *sql.DB, cfg Config) {
	if cfg.MaxStorageMB <= 0 {
		return
	}
	limit := int64(cfg.MaxStorageMB) << 20
	total := dataDirSize()
	if total <= limit {
		return
	}

	// phase 0: quarantined orphans — already superseded, cheapest to drop
	qpurged := 0
	for _, p := range quarantineFiles() {
		if total <= limit {
			break
		}
		if fi, err := os.Stat(p); err == nil && os.Remove(p) == nil {
			total -= fi.Size()
			qpurged++
		}
	}
	if qpurged > 0 {
		logEvent(db, "storage_cap_quarantine", fmt.Sprintf("%d files", qpurged))
	}
	if total <= limit {
		return
	}

	// phase 1: oldest already-summarized frames, stopping at the boundary
	rows, err := db.Query(`SELECT ts, path FROM frames ORDER BY ts ASC`)
	if err == nil {
		var toDelete [][2]any
		var freed int64
		for rows.Next() && total-freed > limit {
			var ts int64
			var p string
			if err := rows.Scan(&ts, &p); err != nil {
				continue
			}
			done, err := blockExists(db, blockStart(time.Unix(ts, 0), cfg.BlockMinutes))
			if err != nil || !done {
				continue
			}
			if fi, err := os.Stat(p); err == nil {
				freed += fi.Size()
			}
			toDelete = append(toDelete, [2]any{ts, p})
		}
		rows.Close()
		for _, d := range toDelete {
			os.Remove(d[1].(string))
			db.Exec(`DELETE FROM frames WHERE ts=? AND path=?`, d[0], d[1])
		}
		total -= freed
		if len(toDelete) > 0 {
			logEvent(db, "storage_cap_frames", fmt.Sprintf("%d frames", len(toDelete)))
		}
	}
	if total <= limit {
		return
	}

	// phase 2: trim log tables (rowid order = insertion order; immune to ts
	// ties that would make a ts-based cutoff delete nothing) and reclaim pages
	db.Exec(`DELETE FROM events WHERE rowid <= (
		SELECT rowid FROM events ORDER BY rowid DESC LIMIT 1 OFFSET 2000)`)
	db.Exec(`DELETE FROM api_calls WHERE rowid <= (
		SELECT rowid FROM api_calls ORDER BY rowid DESC LIMIT 1 OFFSET 2000)`)
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		logEvent(db, "storage_cap_error", "checkpoint: "+err.Error())
	}
	if _, err := db.Exec(`VACUUM`); err != nil {
		logEvent(db, "storage_cap_error", "vacuum: "+err.Error())
	}
	total = dataDirSize()
	if total <= limit {
		return
	}

	// phase 3: last resort — drop the oldest journal blocks, one bounded
	// chunk per pass (the cap runs hourly, so oversized dirs converge)
	pruned, _ := db.Exec(`DELETE FROM blocks WHERE start_ts IN (
		SELECT start_ts FROM blocks ORDER BY start_ts ASC LIMIT 100)`)
	pb, _ := pruned.RowsAffected()
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		logEvent(db, "storage_cap_error", "checkpoint: "+err.Error())
	}
	if _, err := db.Exec(`VACUUM`); err != nil {
		logEvent(db, "storage_cap_error", "vacuum: "+err.Error())
	}
	logEvent(db, "storage_cap_db", fmt.Sprintf("%d oldest blocks pruned, vacuumed", pb))
	if total = dataDirSize(); total > limit {
		logEvent(db, "storage_cap_floor",
			fmt.Sprintf("data dir still %dMB over cap after pruning; will retry next pass",
				(total-limit)>>20))
	}
}

func dataDirSize() int64 {
	var total int64
	filepath.Walk(dataDir(), func(_ string, fi os.FileInfo, _ error) error {
		if fi != nil && fi.Mode().IsRegular() {
			total += fi.Size()
		}
		return nil
	})
	return total
}

func runDaemon(cfg Config) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	cmdArgs, err := resolveCaptureCommand(cfg)
	if err != nil {
		return err
	}
	logEvent(db, "daemon_start", strings.Join(cmdArgs, " "))
	cfgMtime := configMtime()

	// hot-reload config when the file changes so `dayflow config set` and
	// `ignore` apply without a restart
	reloadIfChanged := func() {
		m := configMtime()
		if m.Equal(cfgMtime) {
			return
		}
		if nc, err := loadConfig(); err == nil {
			cfg = nc
			cfgMtime = m
			if na, err := resolveCaptureCommand(cfg); err == nil {
				cmdArgs = na
			}
			logEvent(db, "config_reloaded", "")
			debugf(cfg, "config reloaded: provider=%s model=%s interval=%ds block=%dm debug=%v",
				cfg.Provider, cfg.Model, cfg.CaptureIntervalSec, cfg.BlockMinutes, cfg.Debug)
		}
	}

	var lastHash *frameHash // nil = no prior sample; force first capture
	locked := false
	tick := time.NewTicker(time.Duration(cfg.CaptureIntervalSec) * time.Second)
	defer tick.Stop()
	retentionTick := time.NewTicker(time.Hour)
	defer retentionTick.Stop()
	lockTick := time.NewTicker(time.Minute)
	defer lockTick.Stop()
	log.Printf("dayflow daemon: capturing every %ds -> %s", cfg.CaptureIntervalSec, framesDir())
	debugf(cfg, "daemon start: provider=%s model=%s endpoint=%s interval=%ds block=%dm quality=%d retention=%dd keep_frames=%v debug=%v",
		cfg.Provider, cfg.Model, chatURL(cfg), cfg.CaptureIntervalSec, cfg.BlockMinutes,
		cfg.JPEGQuality, cfg.RetentionDays, cfg.KeepFrames, cfg.Debug)

	capture := func() {
		reloadIfChanged()
		h, err := captureOnce(db, cfg, cmdArgs, lastHash)
		if err != nil {
			log.Printf("capture: %v", err)
			return
		}
		lastHash = h
	}
	capture()
	runRetention(db, cfg)
	for {
		select {
		case <-tick.C:
			if cfg.AutoPauseLocked && locked {
				// locked: do not capture, reset hash so we don't leak last frame
				lastHash = nil
				continue
			}
			capture()
		case <-retentionTick.C:
			runRetention(db, cfg)
			debugf(cfg, "retention run complete; data dir %s", humanBytes(dataDirSize()))
		case <-lockTick.C:
			if !cfg.AutoPauseLocked {
				continue
			}
			if screenLocked() && !paused() && !locked {
				locked = true
				lastHash = nil
				logEvent(db, "auto_paused", "screen locked")
				debugf(cfg, "auto-paused: screen locked")
			} else if !screenLocked() && locked {
				locked = false
				logEvent(db, "auto_resumed", "screen unlocked")
				debugf(cfg, "auto-resumed: screen unlocked")
			}
		}
	}
}
