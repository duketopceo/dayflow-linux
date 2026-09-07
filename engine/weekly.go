package main

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// DonutItem is one wedge of the weekly category donut chart.
type DonutItem struct {
	Name       string  `json:"name"`
	Display    string  `json:"display"`
	Minutes    float64 `json:"minutes"`
	Color      string  `json:"color"`
	Percentage float64 `json:"percentage"`
}

// TreemapItem is one rectangle in the top-apps treemap.
type TreemapItem struct {
	Name       string  `json:"name"`
	Display    string  `json:"display"`
	Minutes    float64 `json:"minutes"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// SankeyLink is a source -> target flow for context-shift visualisation.
type SankeyLink struct {
	Source  string  `json:"source"`
	Target  string  `json:"target"`
	Count   int     `json:"count"`
	Minutes float64 `json:"minutes"`
}

// ContextShift is an alias for SankeyLink so the weekly payload can expose
// both names without duplicating the structure.
type ContextShift = SankeyLink

// HourHeatmap is one hour bucket in the week heatmap.
type HourHeatmap struct {
	Hour     int     `json:"hour"`
	Category string  `json:"category"`
	Minutes  float64 `json:"minutes"`
}

// DayHeatmap is one day bucket in the week heatmap.
type DayHeatmap struct {
	Day   int           `json:"day"`
	Hours []HourHeatmap `json:"hours"`
}

// WeekPayload is the full JSON payload consumed by the Quickshell Week tab.
type WeekPayload struct {
	Start              string           `json:"start"`
	End                string           `json:"end"`
	TotalMinutes       float64          `json:"total_minutes"`
	FocusMinutes       float64          `json:"focus_minutes"`
	DistractionMinutes float64          `json:"distraction_minutes"`
	IdleMinutes        float64          `json:"idle_minutes"`
	CategoryDonut      []DonutItem      `json:"category_donut"`
	AppTreemap         []TreemapItem    `json:"app_treemap"`
	ContextShifts      []ContextShift   `json:"context_shifts"`
	ContextShiftCount  int              `json:"context_shift_count"`
	TopDistractions    []map[string]any `json:"top_distractions"`
	FocusBlocks        []Block          `json:"focus_blocks"`
	Highlights         []string         `json:"highlights"`
	Suggestions        []string         `json:"suggestions"`
	Heatmap            []DayHeatmap     `json:"heatmap"`
}

func weekRange(now time.Time) (time.Time, time.Time) {
	return weekBounds(now)
}

func generateWeeklyPayload(db *sql.DB, cfg Config, start, end time.Time) (WeekPayload, error) {
	in, err := generateInsights(db, cfg, start, end)
	if err != nil {
		return WeekPayload{}, err
	}

	p := WeekPayload{
		Start:              start.Format("2006-01-02"),
		End:                end.Format("2006-01-02"),
		TotalMinutes:       in.TotalMins,
		FocusMinutes:       in.FocusMins,
		DistractionMinutes: in.DistractionMins,
		IdleMinutes:        in.IdleMins,
		CategoryDonut:      buildCategoryDonut(in.Categories, in.TotalMins, cfg),
		AppTreemap:         buildAppTreemap(in.Apps, in.TotalMins),
		TopDistractions:    distList(in.TopDistractions, appDisplayName),
		FocusBlocks:        in.FocusBlocks,
	}

	blocks, err := blocksBetween(db, start, end)
	if err != nil {
		return WeekPayload{}, err
	}
	p.ContextShifts, p.ContextShiftCount = buildContextShifts(blocks)
	p.Heatmap = buildHeatmap(blocks, start)

	highlights := buildHighlights(in)
	prev, _ := previousWeekInsights(db, cfg, start)
	if len(prev.Categories) > 0 {
		highlights = append(highlights, biggestImprovement(in.Categories, prev.Categories)...)
	}
	p.Highlights = highlights
	p.Suggestions = buildSuggestions(in)

	return p, nil
}

func buildCategoryDonut(cats []insightDist, total float64, cfg Config) []DonutItem {
	if total == 0 {
		return nil
	}
	var out []DonutItem
	sum := 0.0
	for i, c := range cats {
		if i < 6 {
			pct := round1(100.0 * c.Mins / total)
			out = append(out, DonutItem{
				Name:       c.Name,
				Display:    catDisplay(c.Name),
				Minutes:    round1(c.Mins),
				Color:      categoryColorHex(c.Name, cfg),
				Percentage: pct,
			})
			sum += pct
		}
	}
	if len(cats) > 6 {
		other := 0.0
		for i := 6; i < len(cats); i++ {
			other += cats[i].Mins
		}
		if other > 0 {
			pct := round1(100.0 * other / total)
			if sum+pct > 100.0 {
				pct = round1(100.0 - sum)
			}
			out = append(out, DonutItem{
				Name:       "other",
				Display:    "Other",
				Minutes:    round1(other),
				Color:      "#808080",
				Percentage: pct,
			})
		}
	}
	return out
}

func buildAppTreemap(apps []insightDist, total float64) []TreemapItem {
	if total == 0 {
		return nil
	}
	var out []TreemapItem
	for i, a := range apps {
		if i >= 10 {
			break
		}
		out = append(out, TreemapItem{
			Name:       a.Name,
			Display:    appDisplayName(a.Name),
			Minutes:    round1(a.Mins),
			Count:      a.Count,
			Percentage: round1(100.0 * a.Mins / total),
		})
	}
	return out
}

func buildContextShifts(blocks []Block) ([]ContextShift, int) {
	if len(blocks) < 2 {
		return nil, 0
	}
	type key struct{ src, tgt string }
	m := map[key]*ContextShift{}
	total := 0
	for i := 1; i < len(blocks); i++ {
		prev := blocks[i-1].Category
		cur := blocks[i].Category
		if prev == cur || cur == "" {
			continue
		}
		dur := blocks[i].End.Sub(blocks[i].Start).Minutes()
		k := key{prev, cur}
		if v, ok := m[k]; ok {
			v.Count++
			v.Minutes += dur
		} else {
			m[k] = &ContextShift{
				Source:  prev,
				Target:  cur,
				Count:   1,
				Minutes: dur,
			}
		}
		total++
	}
	var out []ContextShift
	for _, v := range m {
		v.Minutes = round1(v.Minutes)
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Minutes > out[j].Minutes })
	return out, total
}

func buildHeatmap(blocks []Block, start time.Time) []DayHeatmap {
	var days []DayHeatmap
	for d := 0; d < 7; d++ {
		ds := start.Add(time.Duration(d) * 24 * time.Hour)
		var hours []HourHeatmap
		for h := 0; h < 24; h++ {
			hs := ds.Add(time.Duration(h) * time.Hour)
			he := hs.Add(time.Hour)
			type catDur struct {
				cat     string
				minutes float64
			}
			best := ""
			bestMin := 0.0
			total := 0.0
			for _, b := range blocks {
				if b.Category == "" {
					continue
				}
				s := b.Start
				if s.Before(hs) {
					s = hs
				}
				e := b.End
				if e.After(he) {
					e = he
				}
				if !s.Before(e) {
					continue
				}
				overlap := e.Sub(s).Minutes()
				total += overlap
				if overlap > bestMin {
					bestMin = overlap
					best = b.Category
				}
			}
			hours = append(hours, HourHeatmap{Hour: h, Category: best, Minutes: round1(total)})
		}
		days = append(days, DayHeatmap{Day: d, Hours: hours})
	}
	return days
}

func buildHighlights(in insights) []string {
	var h []string
	if len(in.FocusBlocks) > 0 {
		b := in.FocusBlocks[0]
		m := b.End.Sub(b.Start).Minutes()
		h = append(h, fmt.Sprintf("Longest focus block: %s (%.0f min)", b.Title, m))
	}
	if len(in.Categories) > 0 {
		c := in.Categories[0]
		h = append(h, fmt.Sprintf("Top category: %s (%.1f hr)", catDisplay(c.Name), c.Mins/60))
	}
	if len(in.Apps) > 0 {
		a := in.Apps[0]
		h = append(h, fmt.Sprintf("Top app: %s (%.1f hr)", appDisplayName(a.Name), a.Mins/60))
	}
	return h
}

func biggestImprovement(curr, prev []insightDist) []string {
	prevMins := map[string]float64{}
	for _, p := range prev {
		prevMins[p.Name] += p.Mins
	}
	var best string
	var bestDelta float64
	for _, c := range curr {
		delta := c.Mins - prevMins[c.Name]
		if delta > bestDelta {
			bestDelta = delta
			best = c.Name
		}
	}
	if best == "" || bestDelta <= 0 {
		return nil
	}
	return []string{fmt.Sprintf("Biggest improvement vs last week: %s (+%.0f min)", catDisplay(best), bestDelta)}
}

func previousWeekInsights(db *sql.DB, cfg Config, start time.Time) (insights, error) {
	prevStart, prevEnd := weekBounds(start.Add(-24 * time.Hour))
	return generateInsights(db, cfg, prevStart, prevEnd)
}

func buildSuggestions(in insights) []string {
	var s []string
	if len(in.TopDistractions) > 0 {
		d := in.TopDistractions[0]
		disp := appDisplayName(d.Name)
		if disp == "" {
			disp = catDisplay(d.Name)
		}
		if d.Mins >= 30 {
			s = append(s, fmt.Sprintf("Consider limiting %s (%.1f hr) by blocking it during focus hours.", disp, d.Mins/60))
		}
	}
	if in.IdleMins > 60 {
		s = append(s, fmt.Sprintf("Idle time is at %.1f hr — try shorter breaks or a focus timer.", in.IdleMins/60))
	}
	if len(s) == 0 && in.FocusMins > 0 {
		s = append(s, "Good focus balance this week. Protect your longest productive streaks.")
	}
	return s
}

func categoryColorHex(name string, cfg Config) string {
	for _, c := range cfg.Categories {
		if strings.EqualFold(c.Name, name) && c.Color != "" {
			return c.Color
		}
	}
	colors := map[string]string{
		"coding":        "#388bee",
		"communication": "#f27326",
		"browsing":      "#8b5cf6",
		"writing":       "#34d399",
		"meetings":      "#f3b13d",
		"design":        "#f53d7a",
		"media":         "#f53d3d",
		"system":        "#80808f",
		"idle":          "#6b7280",
		"personal":      "#f85a6a",
		"other":         "#9ca3af",
	}
	return colors[strings.ToLower(name)]
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func distList(d []insightDist, display func(string) string) []map[string]any {
	var out []map[string]any
	for _, v := range d {
		out = append(out, map[string]any{
			"name":    v.Name,
			"display": display(v.Name),
			"minutes": v.Mins,
			"count":   v.Count,
		})
	}
	return out
}

func formatWeeklyPayload(p WeekPayload, start, end time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Weekly analytics — %s to %s\n\n", start.Format("Mon Jan 2"), end.Format("Mon Jan 2"))
	fmt.Fprintf(&b, "- Total: %.1f hr\n", p.TotalMinutes/60)
	fmt.Fprintf(&b, "- Focus: %.1f hr\n", p.FocusMinutes/60)
	fmt.Fprintf(&b, "- Distraction / idle: %.1f hr\n", (p.DistractionMinutes+p.IdleMinutes)/60)
	if len(p.Highlights) > 0 {
		b.WriteString("\n## Highlights\n")
		for _, h := range p.Highlights {
			fmt.Fprintf(&b, "- %s\n", h)
		}
	}
	if len(p.Suggestions) > 0 {
		b.WriteString("\n## Suggestions\n")
		for _, s := range p.Suggestions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	return b.String()
}
