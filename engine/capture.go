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
	if cls := activeWindowClass(); isIgnored(cfg, cls) {
		logEvent(db, "capture_ignored", cls)
		return nil
	}
	raw, err := grabFrame(cmdArgs)
	if err != nil {
		logEvent(db, "capture_error", err.Error())
		return fmt.Errorf("capture: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		logEvent(db, "capture_error", "decode: "+err.Error())
		return fmt.Errorf("decode: %w", err)
	}
	h := ahash(img)
	if lastHash != nil && hamming(h, *lastHash) <= dedupThreshold {
		logEvent(db, "capture_deduped", "")
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
	logEvent(db, "capture_saved", path)
	return insertFrame(db, now, path)
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
	// drop empty day directories
	days, _ := filepath.Glob(filepath.Join(framesDir(), "*"))
	for _, d := range days {
		if entries, _ := os.ReadDir(d); len(entries) == 0 {
			os.Remove(d)
		}
	}
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

	var lastHash uint64
	var haveHash bool
	tick := time.NewTicker(time.Duration(cfg.CaptureIntervalSec) * time.Second)
	defer tick.Stop()
	retentionTick := time.NewTicker(time.Hour)
	defer retentionTick.Stop()
	log.Printf("dayflow daemon: capturing every %ds -> %s", cfg.CaptureIntervalSec, framesDir())

	capture := func() {
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
			capture()
		case <-retentionTick.C:
			runRetention(db, cfg)
		}
	}
}
