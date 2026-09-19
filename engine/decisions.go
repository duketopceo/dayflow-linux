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
