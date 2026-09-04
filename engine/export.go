package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func blocksBetween(db *sql.DB, start, end time.Time) ([]Block, error) {
	rows, err := db.Query(`SELECT start_ts,end_ts,title,summary,category,frame_count FROM blocks
	  WHERE start_ts >= ? AND start_ts < ? AND status='done' ORDER BY start_ts`,
		start.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Block
	for rows.Next() {
		var b Block
		var s, e int64
		if err := rows.Scan(&s, &e, &b.Title, &b.Summary, &b.Category, &b.FrameCount); err != nil {
			return nil, err
		}
		b.Start = time.Unix(s, 0).Local()
		b.End = time.Unix(e, 0).Local()
		b.StartStr = b.Start.Format("15:04")
		b.EndStr = b.End.Format("15:04")
		out = append(out, b)
	}
	return out, rows.Err()
}

func dayBounds(t time.Time) (time.Time, time.Time) {
	t = t.Local()
	s := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return s, s.AddDate(0, 0, 1)
}

func weekBounds(t time.Time) (time.Time, time.Time) {
	t = t.Local()
	s, _ := dayBounds(t)
	// week starts Monday
	for s.Weekday() != time.Monday {
		s = s.AddDate(0, 0, -1)
	}
	return s, s.AddDate(0, 0, 7)
}

func monthBounds(t time.Time) (time.Time, time.Time) {
	t = t.Local()
	s := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	return s, s.AddDate(0, 1, 0)
}

// markdownTimeline renders blocks grouped by day as a markdown document.
func markdownTimeline(blocks []Block, title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	day := ""
	for _, blk := range blocks {
		d := blk.Start.Format("Monday, 2 January 2006")
		if d != day {
			day = d
			fmt.Fprintf(&b, "## %s\n\n", d)
		}
		fmt.Fprintf(&b, "- **%s–%s — %s** `[%s]`\n  %s\n", blk.StartStr, blk.EndStr, blk.Title, blk.Category, blk.Summary)
	}
	if len(blocks) == 0 {
		fmt.Fprintln(&b, "_No summarized blocks in this range._")
	}
	return b.String()
}
