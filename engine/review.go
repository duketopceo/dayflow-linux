package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const reviewSystemPrompt = `You are a concise productivity coach reviewing a user's tracked activity.
They use Dayflow, which captures 15-minute blocks of their screen and labels them with an app, category, title, and a productivity flag.
Your job:
1. Summarize the week in 2-3 plain sentences.
2. Point out any obvious misclassifications or corrections (e.g. gaming counted as coding, work apps labeled personal, long idle gaps, etc.).
3. Give one concrete, actionable suggestion for improving focus or balance next week.

Keep it short and direct. Use markdown bullet points. Do not include raw code or JSON.`

const reviewInstructions = `Format your response as markdown with exactly these sections:
- **Summary**: ...
- **Corrections**: ...
- **Suggestion**: ...`

func reviewRange(db *sql.DB, cfg Config, start, end time.Time, label string) (string, int, int, error) {
	blocks, err := blocksBetween(db, start, end)
	if err != nil {
		return "", 0, 0, err
	}
	if len(blocks) == 0 {
		return "No activity recorded for this period.", 0, 0, nil
	}

	var lines []string
	for _, b := range blocks {
		mins := (b.End.Unix() - b.Start.Unix()) / 60
		prod := "unknown"
		if b.Productive != nil {
			if *b.Productive {
				prod = "yes"
			} else {
				prod = "no"
			}
		}
		app := b.App
		if app == "" {
			app = "unknown"
		}
		lines = append(lines, fmt.Sprintf("- %s–%s | %s | %s | productive=%s | %s | %s",
			b.Start.Format("Mon 3:04 PM"),
			b.End.Format("3:04 PM"),
			app,
			b.Category,
			prod,
			b.Title,
			b.Summary,
		))
		_ = mins
	}

	userPrompt := fmt.Sprintf("Review the following %s activity log. Each line is a 15-minute block.\n\n%s\n\n%s",
		label, strings.Join(lines, "\n"), reviewInstructions)

	return callLLMText(cfg, reviewSystemPrompt, userPrompt)
}

// callLLMText sends a system+user text request to the provider routed for the
// "review" task.
func callLLMText(cfg Config, system, user string) (string, int, int, error) {
	return callProviderText(cfg, "review", system, user)
}
