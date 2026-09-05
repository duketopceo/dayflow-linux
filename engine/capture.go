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

// activeWindowClass returns the class of the focused window on Hyprland,
// or "" when there is no focused window / not running under Hyprland.
func activeWindowClass() string {
	out, err := exec.Command("hyprctl", "activewindow", "-j").Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	var w struct {
		Class        string `json:"class"`
		InitialClass string `json:"initialClass"`
	}
	if json.Unmarshal(out, &w) != nil {
		return ""
	}
	if w.Class != "" {
		return w.Class
	}
	return w.InitialClass
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

// ahash computes a 16x16 grayscale average-hash of the image.
func ahash(img image.Image) uint64 {
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
	var hash uint64
	for i, v := range px {
		if v >= avg {
			hash |= 1 << i
		}
	}
	return hash
}

func hamming(a, b uint64) int {
	d := a ^ b
	n := 0
	for d != 0 {
		n += int(d & 1)
		d >>= 1
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

func captureOnce(db *sql.DB, cfg Config, cmdArgs []string, lastHash *uint64) error {
	if paused() {
		return nil
	}
	cls := activeWindowClass()
	if isIgnored(cfg, cls) {
		logEvent(db, "capture_ignored", cls)
		debugf(cfg, "capture: ignored app %s", cls)
		return nil
	}
	raw, err := grabFrame(cmdArgs)
	if err != nil {
		logEvent(db, "capture_error", err.Error())
		debugf(cfg, "capture: grab failed: %v", err)
		return fmt.Errorf("capture: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		logEvent(db, "capture_error", "decode: "+err.Error())
		debugf(cfg, "capture: decode failed: %v", err)
		return fmt.Errorf("decode: %w", err)
	}
	h := ahash(img)
	if lastHash != nil && hamming(h, *lastHash) <= dedupThreshold {
		logEvent(db, "capture_deduped", "")
		debugf(cfg, "capture: deduped (hamming %d, app %s)", hamming(h, *lastHash), cls)
		return nil // screen unchanged
	}
	*lastHash = h

	now := time.Now()
	dayDir := filepath.Join(framesDir(), now.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dayDir, now.Format("150405")+".jpg")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	if err := insertFrameApp(db, now, path, cls); err != nil {
		os.Remove(path)
		return err
	}
	logEvent(db, "capture_saved", path)
	debugf(cfg, "capture: saved %s (%d bytes, app %s)", filepath.Base(path), len(raw), cls)
	return nil
}

// runRetention deletes frames and old log rows past the retention window.
func runRetention(db *sql.DB, cfg Config) {
	if cfg.RetentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour)
	paths, err := framesBefore(db, cutoff)
	if err != nil {
		logEvent(db, "retention_error", err.Error())
		return
	}
	for _, p := range paths {
		os.Remove(p)
	}
	if len(paths) > 0 {
		logEvent(db, "retention_pruned", fmt.Sprintf("%d frames", len(paths)))
	}
	pruneOldEvents(db, cutoff)
	enforceStorageCap(db, cfg)
	// drop empty day directories
	days, _ := filepath.Glob(filepath.Join(framesDir(), "*"))
	for _, d := range days {
		if entries, _ := os.ReadDir(d); len(entries) == 0 {
			os.Remove(d)
		}
	}
}

// enforceStorageCap keeps the whole data dir (frames + db + wal) under
// max_storage_mb. Oldest data goes first: already-summarized frames, then the
// oldest blocks/events/api_calls rows, then a VACUUM to reclaim pages.
func enforceStorageCap(db *sql.DB, cfg Config) {
	if cfg.MaxStorageMB <= 0 {
		return
	}
	limit := int64(cfg.MaxStorageMB) << 20
	total := dataDirSize()
	if total <= limit {
		return
	}

	// phase 1: oldest already-summarized frames
	rows, err := db.Query(`SELECT ts, path FROM frames ORDER BY ts ASC`)
	if err == nil {
		var toDelete [][2]any
		var freed int64
		for rows.Next() && total > limit {
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

	// phase 2: drop the oldest journal data, one chunk at a time (the cap
	// run is hourly, so oversized dirs converge over a few passes)
	pruned, _ := db.Exec(`DELETE FROM blocks WHERE start_ts IN (
		SELECT start_ts FROM blocks ORDER BY start_ts ASC LIMIT 100)`)
	pb, _ := pruned.RowsAffected()
	db.Exec(`DELETE FROM events WHERE ts < (
		SELECT ts FROM events ORDER BY ts DESC LIMIT 1 OFFSET 2000)`)
	db.Exec(`DELETE FROM api_calls WHERE ts < (
		SELECT ts FROM api_calls ORDER BY ts DESC LIMIT 1 OFFSET 2000)`)
	// reclaim disk pages now that rows are gone
	db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	db.Exec(`VACUUM`)
	logEvent(db, "storage_cap_db", fmt.Sprintf("%d oldest blocks pruned, vacuumed", pb))
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

	var lastHash uint64
	var haveHash bool
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
		var h *uint64
		if haveHash {
			h = &lastHash
		} else {
			h = new(uint64)
			*h = ^uint64(0) // force first frame to be saved
		}
		if err := captureOnce(db, cfg, cmdArgs, h); err != nil {
			log.Printf("capture: %v", err)
			return
		}
		lastHash = *h
		haveHash = true
	}
	capture()
	runRetention(db, cfg)
	for {
		select {
		case <-tick.C:
			if cfg.AutoPauseLocked && locked {
				// locked: do not capture, reset hash so we don't leak last frame
				haveHash = false
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
				haveHash = false
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
