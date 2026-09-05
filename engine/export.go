package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func blocksBetween(db *sql.DB, start, end time.Time) ([]Block, error) {
	rows, err := db.Query(`SELECT start_ts,end_ts,title,summary,category,frame_count,app,activities FROM blocks
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
		var acts string
		if err := rows.Scan(&s, &e, &b.Title, &b.Summary, &b.Category, &b.FrameCount, &b.App, &acts); err != nil {
			return nil, err
		}
		if acts != "" {
			json.Unmarshal([]byte(acts), &b.Activities)
		}
		b.Start = time.Unix(s, 0).Local()
		b.End = time.Unix(e, 0).Local()
		b.StartStr = b.Start.Format("3:04 PM")
		b.EndStr = b.End.Format("3:04 PM")
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

// Card is a merged activity card: consecutive blocks dominated by the same app.
type Card struct {
	Start    time.Time `json:"-"`
	End      time.Time `json:"-"`
	StartStr string    `json:"start"`
	EndStr   string    `json:"end"`
	App      string    `json:"app"`
	Title    string    `json:"title"`
	Summary  string    `json:"summary"`
	Category string    `json:"category"`
	Blocks   int       `json:"blocks"`
}

// mergeCards folds adjacent blocks with the same dominant app into cards.
func mergeCards(blocks []Block) []Card {
	var out []Card
	for _, b := range blocks {
		if n := len(out); n > 0 && b.App != "" && out[n-1].App == b.App && b.Category == out[n-1].Category {
			out[n-1].End = b.End
			out[n-1].EndStr = b.EndStr
			out[n-1].Blocks++
			if !strings.Contains(out[n-1].Summary, b.Summary) {
				out[n-1].Summary += " " + b.Summary
			}
			continue
		}
		out = append(out, Card{
			Start: b.Start, End: b.End, StartStr: b.StartStr, EndStr: b.EndStr,
			App: b.App, Title: b.Title, Summary: b.Summary, Category: b.Category, Blocks: 1,
		})
	}
	return out
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
