package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BlockEdit is one user correction to a timeline block. The raw `blocks` row
// is never rewritten; edits are overlaid at read time so the original
// summarizer output stays auditable.
type BlockEdit struct {
	ID       int64  `json:"id"`
	StartTs  int64  `json:"start_ts"`
	Field    string `json:"field"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
	EditedAt int64  `json:"edited_at"`
}

var editableFields = map[string]bool{
	"title":      true,
	"category":   true,
	"summary":    true,
	"productive": true,
}

// saveBlockEdit records a correction for the block starting at startTs.
// The block must exist; the previous value is captured for the audit log.
func saveBlockEdit(db *sql.DB, cfg Config, startTs int64, field, newValue string) error {
	if !editableFields[field] {
		return fmt.Errorf("cannot edit field %q (choose title, category, summary, or productive)", field)
	}
	newValue = strings.TrimSpace(newValue)
	if field == "productive" {
		v, err := strconv.ParseBool(newValue)
		if err != nil {
			return fmt.Errorf("productive must be true or false, got %q", newValue)
		}
		newValue = strconv.FormatBool(v)
	} else {
		if newValue == "" {
			return fmt.Errorf("%s cannot be empty", field)
		}
		if cfg.FilterInappropriate && containsSensitive(newValue) {
			return fmt.Errorf("edit rejected: value looks like sensitive content (filter_inappropriate is on)")
		}
	}

	var title, summary, category string
	var prod sql.NullBool
	err := db.QueryRow(`SELECT title, summary, category, productive FROM blocks WHERE start_ts = ?`,
		startTs).Scan(&title, &summary, &category, &prod)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no block starts at %d (%s)", startTs,
			time.Unix(startTs, 0).Local().Format("2006-01-02 15:04"))
	}
	if err != nil {
		return err
	}
	old := ""
	switch field {
	case "title":
		old = title
	case "summary":
		old = summary
	case "category":
		old = category
	case "productive":
		if prod.Valid {
			old = strconv.FormatBool(prod.Bool)
		}
	}

	_, err = db.Exec(`INSERT INTO block_edits(start_ts, field, old_value, new_value, edited_at)
	  VALUES(?,?,?,?,?)`, startTs, field, old, newValue, time.Now().Unix())
	return err
}

// editsForBlock returns every edit for a block, newest first.
func editsForBlock(db *sql.DB, startTs int64) ([]BlockEdit, error) {
	rows, err := db.Query(`SELECT id, start_ts, field, old_value, new_value, edited_at
	  FROM block_edits WHERE start_ts = ? ORDER BY edited_at DESC, id DESC`, startTs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdits(rows)
}

// editsForRange returns edits grouped by block start_ts for [start, end),
// each group ordered newest first — ready for applyEdits.
func editsForRange(db *sql.DB, start, end int64) (map[int64][]BlockEdit, error) {
	rows, err := db.Query(`SELECT id, start_ts, field, old_value, new_value, edited_at
	  FROM block_edits WHERE start_ts >= ? AND start_ts < ?
	  ORDER BY edited_at DESC, id DESC`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanEdits(rows)
	if err != nil {
		return nil, err
	}
	m := make(map[int64][]BlockEdit)
	for _, e := range all {
		m[e.StartTs] = append(m[e.StartTs], e)
	}
	return m, nil
}

func scanEdits(rows *sql.Rows) ([]BlockEdit, error) {
	var out []BlockEdit
	for rows.Next() {
		var e BlockEdit
		if err := rows.Scan(&e.ID, &e.StartTs, &e.Field, &e.OldValue, &e.NewValue, &e.EditedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// applyEdits overlays the latest edit per field onto b. edits must be
// ordered newest first (as returned by editsForBlock/editsForRange).
func applyEdits(b Block, edits []BlockEdit) Block {
	seen := make(map[string]bool, len(edits))
	for _, e := range edits {
		if seen[e.Field] {
			continue
		}
		seen[e.Field] = true
		switch e.Field {
		case "title":
			b.Title = e.NewValue
		case "summary":
			b.Summary = e.NewValue
		case "category":
			b.Category = e.NewValue
		case "productive":
			if v, err := strconv.ParseBool(e.NewValue); err == nil {
				b.Productive = &v
			}
		}
	}
	return b
}

// applyBlockEdits overlays user edits on all blocks whose start_ts falls in
// [start, end). One query for the whole range, no N+1.
func applyBlockEdits(db *sql.DB, blocks []Block, start, end int64) ([]Block, error) {
	m, err := editsForRange(db, start, end)
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return blocks, nil
	}
	for i, b := range blocks {
		if es, ok := m[b.StartTs]; ok {
			blocks[i] = applyEdits(b, es)
		}
	}
	return blocks, nil
}

// loadBlockWithEdits returns the single block at startTs with edits applied.
func loadBlockWithEdits(db *sql.DB, startTs int64) (Block, error) {
	t := time.Unix(startTs, 0)
	blocks, err := blocksBetween(db, t, t.Add(time.Second))
	if err != nil {
		return Block{}, err
	}
	if len(blocks) == 0 {
		return Block{}, fmt.Errorf("no block starts at %d", startTs)
	}
	return blocks[0], nil
}

// parseBlockStart parses the leading CLI arg(s) as a block start time: either
// a Unix timestamp, "YYYY-MM-DD HH:MM" (two args or one quoted arg), or a bare
// "YYYY-MM-DD" date. It returns the timestamp and the remaining args.
func parseBlockStart(args []string) (int64, []string, error) {
	if len(args) == 0 {
		return 0, nil, fmt.Errorf("missing block start time")
	}
	if n, err := strconv.ParseInt(args[0], 10, 64); err == nil {
		return n, args[1:], nil
	}
	if len(args) >= 2 {
		if t, err := time.ParseInLocation("2006-01-02 15:04", args[0]+" "+args[1], time.Local); err == nil {
			return t.Unix(), args[2:], nil
		}
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, args[0], time.Local); err == nil {
			return t.Unix(), args[1:], nil
		}
	}
	return 0, nil, fmt.Errorf("cannot parse block start %q (use a Unix timestamp or YYYY-MM-DD HH:MM)", args[0])
}
