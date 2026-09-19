package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// playbackCapMB is the standard on-disk allowance applied when frame
// playback is enabled — mirrors the macOS app's ~10GB default.
const playbackCapMB = 10240

type frameEntry struct {
	TS     int64  `json:"ts"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// framesForDay lists every captured frame inside [dayStart, dayEnd) for a
// date, oldest first, with an on-disk existence flag so the UI can skip
// frames retention already reclaimed.
func framesForDay(db *sql.DB, d time.Time) ([]frameEntry, error) {
	s, e := dayBounds(d)
	rows, err := db.Query(`SELECT ts, path FROM frames WHERE ts>=? AND ts<? ORDER BY ts ASC`, s.Unix(), e.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// One directory listing instead of a stat per row — a heavy capture day
	// is ~8.6k frames.
	dir := filepath.Join(framesDir(), d.Local().Format("2006-01-02"))
	present := map[string]bool{}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.Type().IsRegular() {
				present[e.Name()] = true
			}
		}
	}

	out := []frameEntry{}
	for rows.Next() {
		var f frameEntry
		if err := rows.Scan(&f.TS, &f.Path); err != nil {
			return nil, err
		}
		if filepath.Dir(f.Path) == dir {
			f.Exists = present[filepath.Base(f.Path)]
		} else {
			_, err := os.Stat(f.Path)
			f.Exists = err == nil
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func printFrames(db *sql.DB, d time.Time, jsonOut bool) {
	frames, err := framesForDay(db, d)
	fatal(err)
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"date":   d.Local().Format("2006-01-02"),
			"frames": frames,
			"count":  len(frames),
		})
		return
	}
	if len(frames) == 0 {
		fmt.Println("no frames for", d.Local().Format("2006-01-02"))
		return
	}
	for _, f := range frames {
		flag := ""
		if !f.Exists {
			flag = " (deleted)"
		}
		fmt.Printf("%s  %s%s\n", time.Unix(f.TS, 0).Local().Format("15:04:05"), f.Path, flag)
	}
}

// playbackOn reports whether frame playback is enabled — keep_frames is the
// gate: retention keeps summarized frames only while it is on.
func playbackOn(cfg Config) bool { return cfg.KeepFrames }

// setPlayback toggles frame playback. Enabling applies the standard storage
// cap when the config has no explicit cap (or an unlimited one); disabling
// leaves the cap alone and lets retention reclaim summarized frames.
func setPlayback(cfg Config, on bool) (Config, bool) {
	changed := cfg.KeepFrames != on
	cfg.KeepFrames = on
	if on && cfg.MaxStorageMB == 0 {
		cfg.MaxStorageMB = playbackCapMB
		changed = true
	}
	return cfg, changed
}

func printPlaybackStatus(cfg Config, jsonOut bool) {
	st := map[string]any{
		"enabled":        playbackOn(cfg),
		"keep_frames":    cfg.KeepFrames,
		"max_storage_mb": cfg.MaxStorageMB,
	}
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(st)
		return
	}
	state := "off"
	if playbackOn(cfg) {
		state = "on"
	}
	cap := "unlimited"
	if cfg.MaxStorageMB > 0 {
		cap = fmt.Sprintf("%d MB", cfg.MaxStorageMB)
	}
	fmt.Printf("playback: %s (keep_frames=%v, storage cap=%s)\n", state, cfg.KeepFrames, cap)
}
