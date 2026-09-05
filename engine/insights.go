package main

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// insightDist is a sorted duration/count distribution.
type insightDist struct {
	Name  string
	Mins  float64
	Count int
}

type insights struct {
	TotalMins       float64
	FocusMins       float64
	DistractionMins float64
	IdleMins        float64
	Categories      []insightDist
	Apps            []insightDist
	FocusBlocks     []Block
	TopDistractions []insightDist
	Days            int
}

func (i insights) JSON() map[string]any {
	return map[string]any{
		"total_minutes":       i.TotalMins,
		"focus_minutes":       i.FocusMins,
		"distraction_minutes": i.DistractionMins,
		"idle_minutes":        i.IdleMins,
		"categories":          i.distMaps(i.Categories, catDisplay),
		"apps":                i.distMaps(i.Apps, appDisplayName),
		"focus_blocks":        i.FocusBlocks,
		"top_distractions":    i.distMaps(i.TopDistractions, appDisplayName),
		"days":                i.Days,
	}
}

func (i insights) distMaps(d []insightDist, display func(string) string) []map[string]any {
	var out []map[string]any
	for _, v := range d {
		out = append(out, map[string]any{
			"name": v.Name, "display": display(v.Name), "minutes": v.Mins, "count": v.Count})
	}
	return out
}

func isDistractionCategory(cat string) bool {
	switch cat {
	case "media", "browsing", "idle", "personal":
		return true
	}
	return false
}

func isDistractionApp(app string) bool {
	switch strings.ToLower(app) {
	case "youtube", "reddit", "twitter", "x", "facebook", "instagram", "tiktok", "netflix", "twitch":
		return true
	}
	return false
}

func generateInsights(db *sql.DB, cfg Config, start, end time.Time) (insights, error) {
	blocks, err := blocksBetween(db, start, end)
	if err != nil {
		return insights{}, err
	}

	in := insights{
		Categories:      []insightDist{},
		Apps:            []insightDist{},
		FocusBlocks:     []Block{},
		TopDistractions: []insightDist{},
	}
	in.Days = int(end.Sub(start).Hours()/24) + 1
	catMins := map[string]float64{}
	catCount := map[string]int{}
	appMins := map[string]float64{}
	appCount := map[string]int{}

	for _, b := range blocks {
		dur := b.End.Sub(b.Start).Minutes()
		in.TotalMins += dur
		catMins[b.Category] += dur
		catCount[b.Category]++
		if b.App != "" {
			appMins[b.App] += dur
			appCount[b.App]++
		}
		if isDistractionCategory(b.Category) || isDistractionApp(b.App) {
			in.DistractionMins += dur
		} else {
			in.FocusMins += dur
		}
		if b.Category == "idle" {
			in.IdleMins += dur
		}
		if dur >= 45 && !isDistractionCategory(b.Category) && !isDistractionApp(b.App) {
			in.FocusBlocks = append(in.FocusBlocks, b)
		}
	}

	for k, v := range catMins {
		in.Categories = append(in.Categories, insightDist{Name: k, Mins: v, Count: catCount[k]})
	}
	sort.Slice(in.Categories, func(i, j int) bool { return in.Categories[i].Mins > in.Categories[j].Mins })

	for k, v := range appMins {
		in.Apps = append(in.Apps, insightDist{Name: k, Mins: v, Count: appCount[k]})
	}
	sort.Slice(in.Apps, func(i, j int) bool { return in.Apps[i].Mins > in.Apps[j].Mins })

	distApp := map[string]float64{}
	distCount := map[string]int{}
	for _, b := range blocks {
		if isDistractionCategory(b.Category) || isDistractionApp(b.App) {
			k := b.App
			if k == "" {
				k = b.Category
			}
			distApp[k] += b.End.Sub(b.Start).Minutes()
			distCount[k]++
		}
	}
	for k, v := range distApp {
		in.TopDistractions = append(in.TopDistractions, insightDist{Name: k, Mins: v, Count: distCount[k]})
	}
	sort.Slice(in.TopDistractions, func(i, j int) bool { return in.TopDistractions[i].Mins > in.TopDistractions[j].Mins })

	// longest focus blocks first
	sort.Slice(in.FocusBlocks, func(i, j int) bool {
		return in.FocusBlocks[i].End.Sub(in.FocusBlocks[i].Start) > in.FocusBlocks[j].End.Sub(in.FocusBlocks[j].Start)
	})
	if len(in.FocusBlocks) > 5 {
		in.FocusBlocks = in.FocusBlocks[:5]
	}

	return in, nil
}

func formatInsightsMarkdown(i insights, start, end time.Time, label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", label)
	fmt.Fprintf(&b, "**%s** — %s\n\n", start.Format("Mon Jan 2"), end.Format("Mon Jan 2"))
	fmt.Fprintf(&b, "- Total tracked: **%.1f hr**\n", i.TotalMins/60)
	fmt.Fprintf(&b, "- Deep focus: **%.1f hr**\n", i.FocusMins/60)
	fmt.Fprintf(&b, "- Distractions / idle: **%.1f hr**\n", (i.DistractionMins+i.IdleMins)/60)

	if len(i.Categories) > 0 {
		b.WriteString("\n## Categories\n")
		for _, c := range i.Categories {
			fmt.Fprintf(&b, "- %s: %.1f hr (%d blocks)\n", catDisplay(c.Name), c.Mins/60, c.Count)
		}
	}
	if len(i.Apps) > 0 {
		b.WriteString("\n## Top apps\n")
		for _, a := range i.Apps {
			fmt.Fprintf(&b, "- %s: %.1f hr (%d blocks)\n", appDisplayName(a.Name), a.Mins/60, a.Count)
		}
	}
	if len(i.TopDistractions) > 0 {
		b.WriteString("\n## Distractions to watch\n")
		for _, d := range i.TopDistractions {
			fmt.Fprintf(&b, "- %s: %.1f hr (%d blocks)\n", appDisplayName(d.Name), d.Mins/60, d.Count)
		}
	}
	if len(i.FocusBlocks) > 0 {
		b.WriteString("\n## Longest focus blocks\n")
		for _, bl := range i.FocusBlocks {
			dur := bl.End.Sub(bl.Start).Minutes()
			fmt.Fprintf(&b, "- %s–%s — %s (%.0f min)\n", bl.StartStr, bl.EndStr, bl.Title, dur)
		}
	}
	return b.String()
}
