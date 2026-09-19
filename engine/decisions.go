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

// decisionsURL is the OpenRouter decisions endpoint (alpha). Overridable in tests.
var decisionsURL = "https://openrouter.ai/api/alpha/decisions"

// judgeQuestion describes one calibrated noul judgment requested from Jev.
type judgeQuestion struct {
	Instructions string `json:"instructions"`
	Type         string `json:"type"` // always "noul"
}

type decisionsRequest struct {
	Model     string                   `json:"model"`
	State     string                   `json:"state"`
	Questions map[string]judgeQuestion `json:"questions"`
}

type decisionsResponse struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Type string   `json:"type"`
		Noul *float64 `json:"noul"`
	} `json:"answers"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Provider string `json:"provider"`
}

// decide asks Jev for calibrated judgments. state is the textual context;
// questions maps caller-chosen keys to instruction strings. Returns the
// resolved noul scores by key plus the model that answered. Any HTTP/API
// failure returns an error — callers fall back to heuristic behavior.
func decide(db *sql.DB, cfg Config, kind, state string, questions map[string]string) (map[string]float64, string, error) {
	if len(questions) == 0 {
		return map[string]float64{}, "", nil
	}
	if cfg.OpenRouterAPIKey == "" {
		return nil, "", fmt.Errorf("no OpenRouter API key")
	}
	model := cfg.JevModel
	if model == "" {
		model = "jev-latest"
	}
	qs := make(map[string]judgeQuestion, len(questions))
	for k, ins := range questions {
		qs[k] = judgeQuestion{Instructions: ins, Type: "noul"}
	}
	payload := decisionsRequest{Model: model, State: state, Questions: qs}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}

	start := time.Now()
	req, err := http.NewRequest(http.MethodPost, decisionsURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.OpenRouterAPIKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logLLMCall(db, "judge:"+kind, "typesafe", model, 0, 0, int(time.Since(start).Milliseconds()), "error", err.Error())
		return nil, "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		logLLMCall(db, "judge:"+kind, "typesafe", model, 0, 0, int(time.Since(start).Milliseconds()), "error", err.Error())
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("decisions API %d: %s", resp.StatusCode, truncate(string(raw), 200))
		logLLMCall(db, "judge:"+kind, "typesafe", model, 0, 0, int(time.Since(start).Milliseconds()), "error", msg)
		return nil, "", fmt.Errorf("%s", msg)
	}

	var dr decisionsResponse
	if err := json.Unmarshal(raw, &dr); err != nil {
		logLLMCall(db, "judge:"+kind, "typesafe", model, 0, 0, int(time.Since(start).Milliseconds()), "error", "bad JSON")
		return nil, "", fmt.Errorf("bad decisions JSON: %w", err)
	}
	resolved := dr.Model
	if resolved == "" {
		resolved = model
	}
	out := make(map[string]float64, len(dr.Answers))
	for k, a := range dr.Answers {
		if a.Type == "noul" && a.Noul != nil {
			out[k] = *a.Noul
		}
	}
	logLLMCall(db, "judge:"+kind, "typesafe", resolved, dr.Usage.InputTokens, dr.Usage.OutputTokens,
		int(time.Since(start).Milliseconds()), "ok", "")
	return out, resolved, nil
}

// boundState trims a state payload for the decisions endpoint. The API
// tolerates long text but judgments stay focused (and cheap) on bounded input.
func boundState(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n…"
}

// lowConfidenceThreshold marks a judgment (or produced artifact) the user
// should treat as suspect. Shared by engine consumers and mirrored in QML.
const lowConfidenceThreshold = 0.55

// blockJudgment carries Jev's calibrated take on one block. Nil fields mean
// "no opinion" — never conflate with a confident zero.
type blockJudgment struct {
	Category   string
	Confidence *float64
	Productive *bool
	Quality    *float64
	SameAsPrev *bool
}

// prevBlock returns the title and dominant app of the most recent done block
// before start — the merge-continuity anchor for the same_as_prev judgment.
func prevBlock(db *sql.DB, start time.Time) (title, app string, ok bool) {
	err := db.QueryRow(`SELECT title, app FROM blocks
	  WHERE start_ts < ? AND status='done' ORDER BY start_ts DESC LIMIT 1`, start.Unix()).
		Scan(&title, &app)
	return title, app, err == nil
}

// judgeBlock asks Jev for the per-block judgment batch: category enum,
// productivity, summary quality, and merge continuity. One decisions call
// carries all questions. Returns nil + error on any API failure — callers
// keep the chat model's fields as the fallback baseline.
func judgeBlock(db *sql.DB, cfg Config, res *blockResult, app, prevTitle, prevApp string, hasPrev bool) (*blockJudgment, error) {
	qs := map[string]string{}
	for _, c := range cfg.Categories {
		qs["cat_"+c.Name] = fmt.Sprintf("The primary activity category for this block is %q (%s).", c.Name, c.Description)
	}
	qs["productive"] = "This block represents focused, intentional task work rather than distraction or idle drift."
	qs["quality"] = "The generated title and summary are specific and accurate — they name the actual activity rather than a generic label."
	if hasPrev {
		qs["same_as_prev"] = fmt.Sprintf("This block continues the same activity as the previous block (previous: %q in %s).", prevTitle, prevApp)
	}
	var acts strings.Builder
	for _, a := range res.Activities {
		fmt.Fprintf(&acts, "- %s: %s — %s\n", a.App, a.Title, a.Summary)
	}
	state := boundState(fmt.Sprintf("Dominant app: %s\nTitle: %s\nSummary: %s\nModel's category guess: %s\nActivities:\n%s",
		app, res.Title, res.Summary, res.Category, acts.String()), 4000)

	ans, _, err := decide(db, cfg, "block", state, qs)
	if err != nil {
		return nil, err
	}
	j := &blockJudgment{}
	best, bestScore := "", -1.0
	for _, c := range cfg.Categories {
		if s, ok := ans["cat_"+c.Name]; ok && s > bestScore {
			best, bestScore = c.Name, s
		}
	}
	if best != "" {
		j.Category = best
		s := bestScore
		j.Confidence = &s
	}
	if s, ok := ans["productive"]; ok {
		v := s >= 0.5
		j.Productive = &v
	}
	if s, ok := ans["quality"]; ok {
		v := s
		j.Quality = &v
	}
	if s, ok := ans["same_as_prev"]; ok {
		v := s >= 0.5
		j.SameAsPrev = &v
	}
	return j, nil
}

// applyJudgment merges jev's verdict into the chat-produced result. Jev
// overrides category and productivity when it has an opinion; the chat
// values stand otherwise.
func applyJudgment(res *blockResult, j *blockJudgment) {
	if j == nil {
		return
	}
	if j.Category != "" {
		res.Category = j.Category
	}
	if j.Productive != nil {
		res.Productive = j.Productive
	}
}
