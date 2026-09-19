package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultJevModel  = "typesafe/jev-1.13"
	defaultJevLatest = "~typesafe/jev-latest"
)

// var so tests can point at a stub server.
var decisionsURL = "https://openrouter.ai/api/alpha/decisions"

type jevRequest struct {
	Model     string                 `json:"model"`
	State     any                    `json:"state"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
	True         string `json:"true,omitempty"`
	False        string `json:"false,omitempty"`
}

type jevResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   *struct {
		PromptTokens int     `json:"prompt_tokens"`
		Cost         float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type jevChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type jevNoulAnswer struct {
	Noul float64 `json:"noul"`
}

type jevClassification struct {
	Category   string
	Productive bool
	Noul       float64
}

// jevModelForTask resolves the Jev model for classification: routing task
// override, provider model, then the pinned default.
func jevModelForTask(cfg Config) (string, error) {
	if id := cfg.Routing.TaskProvider["classification"]; id != "" {
		p, err := providerForTask(cfg, "classification")
		if err == nil && p.Model != "" {
			return p.Model, nil
		}
	}
	if cfg.ClassificationModel != "" {
		return cfg.ClassificationModel, nil
	}
	return defaultJevModel, nil
}

func buildClassificationQuestions(cfg Config) map[string]jevQuestion {
	criteria := make(map[string]string, len(cfg.Categories))
	for _, c := range cfg.Categories {
		criteria[c.Name] = c.Description
	}
	return map[string]jevQuestion{
		"category": {
			Type:         "choice",
			Instructions: "Which single category best describes this activity block?",
			Criteria:     criteria,
		},
		"productive": {
			Type:         "noul",
			Instructions: "Was the user actively making progress on meaningful work during this block?",
			True:         "Actively creating, debugging, configuring, writing, planning a project, or solving a concrete problem",
			False:        "Passively consuming entertainment, idle browsing, social scrolling, or no visible goal",
		},
	}
}

func classificationState(res *blockResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "title: %s\nsummary: %s\n", res.Title, res.Summary)
	if len(res.Activities) > 0 {
		acts, _ := json.Marshal(res.Activities)
		fmt.Fprintf(&b, "activities: %s\n", string(acts))
	}
	if res.Category != "" {
		fmt.Fprintf(&b, "vision_category_hint: %s\n", res.Category)
	}
	return b.String()
}

func validCategory(cfg Config, name string) bool {
	for _, c := range cfg.Categories {
		if c.Name == name {
			return true
		}
	}
	return false
}

// classifyWithJev asks Jev to pick category and productive flag from the
// vision model's title/summary/activities. Returns token count for logging.
func classifyWithJev(cfg Config, res *blockResult) (int, error) {
	if res == nil {
		return 0, fmt.Errorf("nil block result")
	}
	model, err := jevModelForTask(cfg)
	if err != nil {
		return 0, err
	}
	p, err := providerForTask(cfg, "classification")
	if err != nil {
		// Fall back to default provider credentials for Jev calls.
		p, err = providerForTask(cfg, "chat")
		if err != nil {
			p, err = providerForTask(cfg, "vision")
			if err != nil {
				return 0, err
			}
		}
	}
	apiKey := resolveProviderKey(p)
	if apiKey == "" {
		return 0, fmt.Errorf("no API key for Jev classification")
	}

	reqBody, _ := json.Marshal(jevRequest{
		Model:     model,
		State:     classificationState(res),
		Questions: buildClassificationQuestions(cfg),
	})
	req, err := http.NewRequest("POST", decisionsURL, bytes.NewReader(reqBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/duketopceo/dayflow-linux")
	req.Header.Set("X-Title", cfg.SiteName)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("jev request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("jev api %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var jr jevResponse
	if err := json.Unmarshal(body, &jr); err != nil {
		return 0, fmt.Errorf("jev response was not valid JSON: %w", err)
	}
	if jr.Error != nil {
		return 0, fmt.Errorf("jev error: %s", jr.Error.Message)
	}

	cls, err := parseJevClassification(cfg, jr.Answers)
	if err != nil {
		return 0, err
	}
	res.Category = cls.Category
	res.Productive = &cls.Productive
	for i := range res.Activities {
		res.Activities[i].Category = cls.Category
		res.Activities[i].Productive = &cls.Productive
	}

	pt := 0
	if jr.Usage != nil {
		pt = jr.Usage.PromptTokens
	}
	debugf(cfg, "jev classify: model=%s category=%q productive=%v noul=%.2f",
		jr.Model, cls.Category, cls.Productive, cls.Noul)
	return pt, nil
}

func parseJevClassification(cfg Config, answers map[string]json.RawMessage) (jevClassification, error) {
	var out jevClassification
	rawCat, ok := answers["category"]
	if !ok {
		return out, fmt.Errorf("jev missing category answer")
	}
	var cat jevChoiceAnswer
	if err := json.Unmarshal(rawCat, &cat); err != nil {
		return out, fmt.Errorf("jev category parse: %w", err)
	}
	if cat.Choice == "" {
		return out, fmt.Errorf("jev returned empty category")
	}
	if !validCategory(cfg, cat.Choice) {
		return out, fmt.Errorf("jev category %q not in configured categories", cat.Choice)
	}
	out.Category = cat.Choice

	rawProd, ok := answers["productive"]
	if !ok {
		return out, fmt.Errorf("jev missing productive answer")
	}
	var prod jevNoulAnswer
	if err := json.Unmarshal(rawProd, &prod); err != nil {
		return out, fmt.Errorf("jev productive parse: %w", err)
	}
	out.Noul = prod.Noul
	out.Productive = prod.Noul >= 0.5
	return out, nil
}
