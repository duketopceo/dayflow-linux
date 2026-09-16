package main

import (
	"database/sql"
	"errors"
	"time"
)

// DayGoal is a user-set goal for a calendar day (schema: day_goals).
type DayGoal struct {
	Date      string `json:"date"`
	Goal      string `json:"goal"`
	Completed bool   `json:"completed"`
}

func getGoal(db *sql.DB, date string) (DayGoal, error) {
	var g DayGoal
	var done int
	err := db.QueryRow(`SELECT date, goal, completed FROM day_goals WHERE date = ?`, date).
		Scan(&g.Date, &g.Goal, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return DayGoal{Date: date}, nil
	}
	if err != nil {
		return g, err
	}
	g.Completed = done != 0
	return g, nil
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
