package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
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
