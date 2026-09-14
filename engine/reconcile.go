package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// framePathSet returns every path currently recorded in the frames table.
func framePathSet(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT path FROM frames`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := make(map[string]bool)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		set[p] = true
	}
	return set, rows.Err()
}

// frameFiles returns all regular files under the frames directory.
func frameFiles() ([]string, error) {
	var out []string
	root := framesDir()
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if fi.Mode().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	sort.Strings(out)
	return out, err
}

// orphanFrameFiles lists files under framesDir() that no frames row references.
func orphanFrameFiles(db *sql.DB) ([]string, error) {
	tracked, err := framePathSet(db)
	if err != nil {
		return nil, err
	}
	files, err := frameFiles()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range files {
		if !tracked[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

func quarantineDir() string { return filepath.Join(dataDir(), "quarantine") }

// reconcileGrace is how long a file may exist without a frames row before it
// counts as an orphan — covers the write-then-insert window in captureOnce.
const reconcileGrace = time.Minute

type reconcileResult struct {
	Orphans     []string `json:"orphans"`
	Quarantined int      `json:"quarantined"`
	StaleRows   int      `json:"stale_rows"`
	Purged      int      `json:"purged"`
	Skipped     int      `json:"skipped_fresh"`
}

// reconcileFrames makes the frames directory and the frames table agree.
// Orphan files move to quarantine/<date-dir>/ (dry-run reports only), stale
// rows are deleted, and quarantined files older than retention_days are purged.
func reconcileFrames(db *sql.DB, cfg Config, dryRun bool) (reconcileResult, error) {
	var res reconcileResult
	orphans, err := orphanFrameFiles(db)
	if err != nil {
		return res, err
	}
	res.Orphans = orphans
	if dryRun {
		return res, nil
	}

	freshCutoff := time.Now().Add(-reconcileGrace)
	for _, p := range orphans {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(freshCutoff) {
			res.Skipped++
			continue
		}
		rel, err := filepath.Rel(framesDir(), p)
		if err != nil {
			rel = filepath.Base(p)
		}
		dst := filepath.Join(quarantineDir(), rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			logEvent(db, "reconcile_error", err.Error())
			continue
		}
		if err := os.Rename(p, dst); err != nil {
			logEvent(db, "reconcile_error", p+": "+err.Error())
			continue
		}
		res.Quarantined++
	}
	if res.Quarantined > 0 {
		logEvent(db, "reconcile_quarantined", fmt.Sprintf("%d files", res.Quarantined))
	}

	// stale rows: frames entries whose file no longer exists
	rows, err := db.Query(`SELECT ts, path FROM frames`)
	if err != nil {
		return res, err
	}
	var stale [][2]any
	for rows.Next() {
		var ts int64
		var p string
		if err := rows.Scan(&ts, &p); err != nil {
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			stale = append(stale, [2]any{ts, p})
		}
	}
	rows.Close()
	for _, s := range stale {
		if _, err := db.Exec(`DELETE FROM frames WHERE ts=? AND path=?`, s[0], s[1]); err == nil {
			res.StaleRows++
		}
	}
	if res.StaleRows > 0 {
		logEvent(db, "reconcile_stale_rows", fmt.Sprintf("%d rows", res.StaleRows))
	}

	// purge quarantined files past the retention window; retention off keeps them
	if cfg.RetentionDays > 0 {
		purgeCutoff := time.Now().Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour)
		filepath.Walk(quarantineDir(), func(p string, fi os.FileInfo, err error) error {
			if err != nil || !fi.Mode().IsRegular() {
				return nil
			}
			if fi.ModTime().Before(purgeCutoff) && os.Remove(p) == nil {
				res.Purged++
			}
			return nil
		})
		if res.Purged > 0 {
			logEvent(db, "reconcile_purged", fmt.Sprintf("%d files", res.Purged))
		}
	}

	// drop emptied date dirs under frames/ and quarantine/
	for _, root := range []string{framesDir(), quarantineDir()} {
		days, _ := filepath.Glob(filepath.Join(root, "*"))
		for _, d := range days {
			if entries, _ := os.ReadDir(d); len(entries) == 0 {
				os.Remove(d)
			}
		}
	}
	return res, nil
}
