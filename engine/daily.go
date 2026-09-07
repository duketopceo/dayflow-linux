package main

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// WorkflowSlot is one fixed-size cell of the daily workflow grid
// (a 15-minute slot by default, matching cfg.BlockMinutes).
type WorkflowSlot struct {
	Time       string `json:"time"`
	Category   string `json:"category"`
	Productive bool   `json:"productive"`
	Title      string `json:"title"`
}

// CategoryRow is one row of the grid: a category plus its total minutes.
type CategoryRow struct {
	Name       string `json:"name"`
	Display    string `json:"display"`
	Minutes    int    `json:"minutes"`
	Productive bool   `json:"productive"`
}

// DailyWorkflow is the macOS-style daily activity grid for one day:
// a contiguous run of time slots colored by category, plus per-category
// totals used as the grid's row labels.
type DailyWorkflow struct {
	Date         string         `json:"date"`
	SlotMinutes  int            `json:"slot_minutes"`
	TotalMinutes int            `json:"total_minutes"`
	Slots        []WorkflowSlot `json:"slots"`
	Categories   []CategoryRow  `json:"categories"`
}

// generateDailyWorkflow builds the slot grid for a day. Slots span from the
// first block's start (floored to the slot boundary) through the last block's
// end (rounded up). A block fills every slot it overlaps; when blocks overlap
// a slot keeps the earliest block's category. A day with no blocks returns an
// empty grid rather than an error.
func generateDailyWorkflow(db *sql.DB, cfg Config, day time.Time) (DailyWorkflow, error) {
	slotMin := cfg.BlockMinutes
	if slotMin <= 0 {
		slotMin = 15
	}
	wf := DailyWorkflow{
		Date:        day.Format("2006-01-02"),
		SlotMinutes: slotMin,
		Slots:       []WorkflowSlot{},
		Categories:  []CategoryRow{},
	}

	blocks, err := blocksForDay(db, day, false)
	if err != nil {
		return wf, err
	}
	if len(blocks) == 0 {
		return wf, nil
	}

	// Grid range: first block floored to a slot boundary through the last
	// block's end rounded up to a slot boundary.
	slot := time.Duration(slotMin) * time.Minute
	gridStart := blocks[0].Start.Truncate(slot)
	gridEnd := blocks[len(blocks)-1].End
	if rem := gridEnd.Sub(gridStart) % slot; rem != 0 {
		gridEnd = gridEnd.Add(slot - rem)
	}
	if !gridEnd.After(gridStart) {
		gridEnd = gridStart.Add(slot)
	}

	n := int(gridEnd.Sub(gridStart) / slot)
	wf.Slots = make([]WorkflowSlot, n)
	for i := range wf.Slots {
		t := gridStart.Add(time.Duration(i) * slot)
		wf.Slots[i].Time = t.Format("15:04")
	}

	// Fill each slot with the category of the earliest block overlapping it.
	for _, b := range blocks {
		bs, be := b.Start, b.End
		if be.Before(gridStart) || !bs.Before(gridEnd) {
			continue
		}
		first := int(bs.Sub(gridStart) / slot)
		if first < 0 {
			first = 0
		}
		last := int((be.Sub(gridStart) - 1) / slot) // inclusive index
		if last >= n {
			last = n - 1
		}
		for i := first; i <= last; i++ {
			if wf.Slots[i].Category == "" {
				wf.Slots[i].Category = b.Category
				wf.Slots[i].Productive = b.IsProductive()
				wf.Slots[i].Title = b.Title
			}
		}
	}

	// Per-category totals (minutes = filled slots * slot size).
	type agg struct {
		minutes, productive int
	}
	totals := map[string]*agg{}
	filled := 0
	for _, s := range wf.Slots {
		if s.Category == "" {
			continue
		}
		filled++
		a, ok := totals[s.Category]
		if !ok {
			a = &agg{}
			totals[s.Category] = a
		}
		a.minutes += slotMin
		if s.Productive {
			a.productive++
		}
	}
	for name, a := range totals {
		wf.Categories = append(wf.Categories, CategoryRow{
			Name:       name,
			Display:    catDisplay(name),
			Minutes:    a.minutes,
			Productive: a.productive*2 >= a.minutes/slotMin,
		})
	}
	sort.Slice(wf.Categories, func(i, j int) bool {
		return wf.Categories[i].Minutes > wf.Categories[j].Minutes
	})
	wf.TotalMinutes = filled * slotMin
	return wf, nil
}

// StandupDraft is the editable part of a standup: free-text fields the user
// fills in, persisted per date and merged into generated standup output.
type StandupDraft struct {
	Date       string `json:"date"`
	Highlights string `json:"highlights"`
	Tasks      string `json:"tasks"`
	Blockers   string `json:"blockers"`
	Priorities string `json:"priorities"`
	UpdatedAt  int64  `json:"updated_at,omitempty"`
}

// saveStandupDraft upserts the draft row for a YYYY-MM-DD date.
func saveStandupDraft(db *sql.DB, date, highlights, tasks, blockers, priorities string) error {
	if _, err := time.ParseInLocation("2006-01-02", date, time.Local); err != nil {
		return fmt.Errorf("standup draft requires a date in YYYY-MM-DD form: %w", err)
	}
	_, err := db.Exec(`INSERT INTO standup_drafts(date, highlights, tasks, blockers, priorities, updated_at)
	  VALUES(?,?,?,?,?,?)
	  ON CONFLICT(date) DO UPDATE SET highlights=excluded.highlights,
	    tasks=excluded.tasks, blockers=excluded.blockers,
	    priorities=excluded.priorities, updated_at=excluded.updated_at`,
		date, highlights, tasks, blockers, priorities, time.Now().Unix())
	return err
}

// loadStandupDraft returns the saved draft for a date, or an empty draft
// (with Date set) when none exists.
func loadStandupDraft(db *sql.DB, date string) (StandupDraft, error) {
	d := StandupDraft{Date: date}
	err := db.QueryRow(`SELECT highlights, tasks, blockers, priorities, updated_at
	  FROM standup_drafts WHERE date = ?`, date).
		Scan(&d.Highlights, &d.Tasks, &d.Blockers, &d.Priorities, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return d, nil
	}
	return d, err
}
