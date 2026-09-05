package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func blocksBetween(db *sql.DB, start, end time.Time) ([]Block, error) {
	rows, err := db.Query(`SELECT start_ts,end_ts,title,summary,category,frame_count,app,activities,productive FROM blocks
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
		var prod sql.NullBool
		if err := rows.Scan(&s, &e, &b.Title, &b.Summary, &b.Category, &b.FrameCount, &b.App, &acts, &prod); err != nil {
			return nil, err
		}
		if prod.Valid {
			b.Productive = &prod.Bool
		}
		if acts != "" {
			json.Unmarshal([]byte(acts), &b.Activities)
		}
		b.Start = time.Unix(s, 0).Local()
		b.End = time.Unix(e, 0).Local()
		b.StartTs = s
		b.EndTs = e
		b.StartStr = b.Start.Format("3:04 PM")
		b.EndStr = b.End.Format("3:04 PM")
		b.AppName = appDisplayName(b.App)
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

// Card is a merged activity card: consecutive blocks about the same thing
// (same title, or same dominant app + category) folded into one span.
type Card struct {
	Start      time.Time `json:"-"`
	End        time.Time `json:"-"`
	StartStr   string    `json:"start"`
	EndStr     string    `json:"end"`
	App        string    `json:"app"`
	AppName    string    `json:"app_name"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
	Category   string    `json:"category"`
	Productive bool      `json:"productive"`
	Blocks     int       `json:"blocks"`
	Minutes    int       `json:"minutes"`
	Children   []Block   `json:"children"`
}

// mergeCards folds adjacent blocks with the same title or the same dominant
// app + category into one span. The latest block's title/summary win since
// they describe where the span ended up; children keep the raw blocks.
func mergeCards(blocks []Block) []Card {
	sorted := make([]Block, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	var out []Card
	for _, b := range sorted {
		mins := int(b.End.Sub(b.Start).Minutes())
		if n := len(out); n > 0 &&
			(b.Title == out[n-1].Title ||
				(b.App != "" && out[n-1].App == b.App && b.Category == out[n-1].Category)) {
			out[n-1].End = b.End
			out[n-1].EndStr = b.EndStr
			out[n-1].Blocks++
			out[n-1].Minutes += mins
			out[n-1].Productive = out[n-1].Productive || b.IsProductive()
			out[n-1].Title = b.Title
			out[n-1].Summary = b.Summary
			out[n-1].Children = append(out[n-1].Children, b)
			continue
		}
		out = append(out, Card{
			Start: b.Start, End: b.End, StartStr: b.StartStr, EndStr: b.EndStr,
			App: b.App, AppName: b.AppName, Title: b.Title, Summary: b.Summary,
			Category: b.Category, Productive: b.IsProductive(), Blocks: 1, Minutes: mins, Children: []Block{b},
		})
	}
	return out
}

// markdownTimeline renders merged activity cards grouped by day as markdown.
// Consecutive blocks about the same thing collapse into one span like
// "2:30 PM–4:00 PM — Title [coding] (1h 30m, 6 blocks)" with the per-block
// titles kept as sub-bullets.
func markdownTimeline(blocks []Block, title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	day := ""
	for _, c := range mergeCards(blocks) {
		d := c.Start.Format("Monday, 2 January 2006")
		if d != day {
			day = d
			fmt.Fprintf(&b, "## %s\n\n", d)
		}
		app := ""
		if c.AppName != "" {
			app = " · " + c.AppName
		}
		focus := ""
		if c.Productive {
			focus = " ⚡"
		}
		if c.Blocks > 1 {
			fmt.Fprintf(&b, "- **%s–%s — %s** `[%s]`%s%s _(%s, %d blocks)_\n  %s\n",
				c.StartStr, c.EndStr, c.Title, catDisplay(c.Category), app, focus, fmtDur(c.Minutes), c.Blocks, c.Summary)
			for _, ch := range c.Children {
				fmt.Fprintf(&b, "  - %s — %s\n", ch.StartStr, ch.Title)
			}
		} else {
			fmt.Fprintf(&b, "- **%s–%s — %s** `[%s]`%s%s\n  %s\n",
				c.StartStr, c.EndStr, c.Title, catDisplay(c.Category), app, focus, c.Summary)
		}
	}
	if len(blocks) == 0 {
		fmt.Fprintln(&b, "_No summarized blocks in this range._")
	}
	return b.String()
}
