package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func callLLMText(cfg Config, system, user string) (string, int, int, error) {
	if cfg.APIBaseURL == "" && cfg.OpenRouterAPIKey == "" {
		return "", 0, 0, fmt.Errorf("no API key: set openrouter_api_key in %s or OPENROUTER_API_KEY", configPath())
	}

	messages := []orMessage{
		{Role: "system", Content: []orContent{{Type: "text", Text: system}}},
		{Role: "user", Content: []orContent{{Type: "text", Text: user}}},
	}

	reqBody, _ := json.Marshal(orRequest{Model: cfg.Model, Messages: messages})
	req, err := http.NewRequest("POST", chatURL(cfg), bytes.NewReader(reqBody))
	if err != nil {
		return "", 0, 0, err
	}

	if cfg.OpenRouterAPIKey != "" && (cfg.Provider == "openrouter" || cfg.Provider == "custom") {
		req.Header.Set("Authorization", "Bearer "+cfg.OpenRouterAPIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	if useOpenRouterHeaders(cfg) {
		req.Header.Set("HTTP-Referer", "https://github.com/duketopceo/dayflow-linux")
		req.Header.Set("X-Title", cfg.SiteName)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return "", 0, 0, fmt.Errorf("api %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var or orResponse
	if err := json.Unmarshal(body, &or); err != nil {
		return "", 0, 0, fmt.Errorf("api response was not valid JSON: %w", err)
	}
	if or.Error != nil {
		return "", 0, 0, fmt.Errorf("api error: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return "", 0, 0, fmt.Errorf("api returned no choices")
	}

	text := strings.TrimSpace(or.Choices[0].Message.Content)
	text = stripFences(text)
	pt, ct := 0, 0
	if or.Usage != nil {
		pt, ct = or.Usage.PromptTokens, or.Usage.CompletionTokens
	}
	return text, pt, ct, nil
}
