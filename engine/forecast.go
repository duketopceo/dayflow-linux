package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// forecastWindowDays is how far back the forecast samples history.
const forecastWindowDays = 56

// ForecastItem is one predicted category share for the target day.
type ForecastItem struct {
	Category string  `json:"category"`
	Pct      float64 `json:"pct"`
	Minutes  float64 `json:"minutes"`
}

// Forecast is the predicted shape of the target day.
type Forecast struct {
	Date         string         `json:"date"`
	Weekday      string         `json:"weekday"`
	TotalMinutes float64        `json:"total_minutes"`
	Items        []ForecastItem `json:"items"`
	Samples      int            `json:"samples"`    // same-weekday days seen
	Confidence   string         `json:"confidence"` // low | medium | high
}

// forecast predicts the category mix for target by blending same-weekday
// history (weight 0.6) with the all-days average (weight 0.4) over the
// recent window. Empty-category and idle time are excluded.
func forecast(db *sql.DB, target time.Time) (Forecast, error) {
	fc := Forecast{
		Date:    target.Local().Format("2006-01-02"),
		Weekday: target.Local().Weekday().String(),
		Items:   []ForecastItem{},
	}
	type dayProfile struct {
		m     map[string]float64
		total float64
	}
	var same, all []dayProfile
	// One range query over the whole window instead of a query per day.
	winStart, _ := dayBounds(target.AddDate(0, 0, -forecastWindowDays))
	winEnd, _ := dayBounds(target)
	rows, err := db.Query(`SELECT start_ts, end_ts, category FROM blocks
		WHERE start_ts>=? AND start_ts<? AND status='done' AND category NOT IN ('','idle')`,
		winStart.Unix(), winEnd.Unix())
	if err != nil {
		return fc, err
	}
	defer rows.Close()
	byDay := map[int64]*dayProfile{}
	for rows.Next() {
		var st, en int64
		var cat string
		if err := rows.Scan(&st, &en, &cat); err != nil {
			return fc, err
		}
		ds, _ := dayBounds(time.Unix(st, 0))
		dp := byDay[ds.Unix()]
		if dp == nil {
			dp = &dayProfile{m: map[string]float64{}}
			byDay[ds.Unix()] = dp
		}
		mins := float64(en-st) / 60
		dp.m[cat] += mins
		dp.total += mins
	}
	if err := rows.Err(); err != nil {
		return fc, err
	}
	for i := 1; i <= forecastWindowDays; i++ {
		d := target.AddDate(0, 0, -i)
		ds, _ := dayBounds(d)
		dp := byDay[ds.Unix()]
		if dp == nil {
			continue
		}
		all = append(all, *dp)
		if d.Weekday() == target.Weekday() {
			same = append(same, *dp)
		}
	}
	fc.Samples = len(same)
	if len(all) == 0 {
		fc.Confidence = "low"
		return fc, nil
	}

	blend := map[string]float64{}
	cats := map[string]bool{}
	avgInto := func(days []dayProfile, w float64) {
		if len(days) == 0 {
			return
		}
		for _, dp := range days {
			for c, mins := range dp.m {
				blend[c] += w * mins / float64(len(days))
				cats[c] = true
			}
		}
	}
	avgInto(same, 0.6)
	avgInto(all, 0.4)

	var total float64
	for c := range cats {
		total += blend[c]
	}
	if total == 0 {
		fc.Confidence = "low"
		return fc, nil
	}
	for c := range cats {
		fc.Items = append(fc.Items, ForecastItem{
			Category: c,
			Pct:      round1(100 * blend[c] / total),
			Minutes:  round1(blend[c]),
		})
	}
	sort.Slice(fc.Items, func(i, j int) bool { return fc.Items[i].Pct > fc.Items[j].Pct })
	fc.TotalMinutes = round1(total)

	switch {
	case fc.Samples >= 4:
		fc.Confidence = "high"
	case fc.Samples >= 2:
		fc.Confidence = "medium"
	default:
		fc.Confidence = "low"
	}
	return fc, nil
}

func printForecast(db *sql.DB, d time.Time, jsonOut bool) {
	fc, err := forecast(db, d)
	fatal(err)
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(fc)
		return
	}
	if len(fc.Items) == 0 {
		fmt.Printf("forecast %s (%s): not enough history yet\n", fc.Date, fc.Weekday)
		return
	}
	var parts []string
	for _, it := range fc.Items {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", it.Category, it.Pct))
	}
	fmt.Printf("forecast %s (%s, %s confidence): %s — ~%s total\n",
		fc.Date, fc.Weekday, fc.Confidence, strings.Join(parts, ", "),
		fmtDur(int(fc.TotalMinutes+0.5)))
}
