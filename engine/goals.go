package main

import (
	"database/sql"
	"errors"
	"time"
)

// DayGoal is a user-set goal for a calendar day (schema: day_goals).
type DayGoal struct {
	Date      string     `json:"date"`
	Goal      string     `json:"goal"`
	Completed bool       `json:"completed"`
	Streak    GoalStreak `json:"streak"`
}

// GoalStreak summarizes completion runs across goal history. Current counts
// consecutive completed days ending at the viewed date — a pending (set but
// not done) goal today doesn't break it, the run just counts through
// yesterday. A missing goal or an explicitly-cleared day breaks the run.
// Best is the longest run ever recorded.
type GoalStreak struct {
	Current int `json:"current"`
	Best    int `json:"best"`
	Total   int `json:"total"` // all-time completed goals
}

func getGoal(db *sql.DB, date string) (DayGoal, error) {
	var g DayGoal
	var done int
	err := db.QueryRow(`SELECT date, goal, completed FROM day_goals WHERE date = ?`, date).
		Scan(&g.Date, &g.Goal, &done)
	if errors.Is(err, sql.ErrNoRows) {
		g = DayGoal{Date: date}
	} else if err != nil {
		return g, err
	} else {
		g.Completed = done != 0
	}
	g.Streak = goalStreak(db, date)
	return g, nil
}

// goalStreak counts consecutive-day completion runs over day_goals history.
// The viewed date is treated as pending when it has no completed goal — the
// current run then counts back from yesterday. Any past day without a
// completed goal (missing row or cleared) breaks a run.
func goalStreak(db *sql.DB, date string) GoalStreak {
	rows, err := db.Query(`SELECT date, completed FROM day_goals
	  WHERE date <= ? ORDER BY date`, date)
	if err != nil {
		return GoalStreak{}
	}
	defer rows.Close()

	done := map[string]bool{}
	var dates []string
	for rows.Next() {
		var d string
		var c int
		if rows.Scan(&d, &c) != nil {
			continue
		}
		dates = append(dates, d)
		done[d] = c != 0
	}
	if err := rows.Err(); err != nil {
		return GoalStreak{}
	}

	prevDay := func(d string) string {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			return ""
		}
		return t.AddDate(0, 0, -1).Format("2006-01-02")
	}

	var s GoalStreak
	// Current: from the viewed day if done, else from yesterday (pending).
	d := date
	if !done[d] {
		d = prevDay(d)
	}
	for done[d] {
		s.Current++
		d = prevDay(d)
	}

	// Best + total over all history.
	run := 0
	prev := ""
	for _, d := range dates {
		if !done[d] {
			run, prev = 0, ""
			continue
		}
		s.Total++
		if prev == "" || prevDay(d) != prev {
			run = 0
		}
		run++
		prev = d
		if run > s.Best {
			s.Best = run
		}
	}
	return s
}

// setGoal upserts the goal text for a date. The unique day_goals_date index
// (schema v3) makes this race-safe via ON CONFLICT.
func setGoal(db *sql.DB, date, goal string) error {
	_, err := db.Exec(`INSERT INTO day_goals(date, goal, completed, created_at)
	  VALUES(?,?,0,?)
	  ON CONFLICT(date) DO UPDATE SET goal = excluded.goal`,
		date, goal, time.Now().Unix())
	return err
}

func completeGoal(db *sql.DB, date string, done bool) error {
	v := 0
	if done {
		v = 1
	}
	res, err := db.Exec(`UPDATE day_goals SET completed = ? WHERE date = ?`, v, date)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("no goal set for " + date)
	}
	return nil
}
