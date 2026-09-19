package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// decisionsURL is the OpenRouter decisions endpoint (alpha). Overridable in tests.
const defaultJevModel = "typesafe/jev-1.13"

var decisionsURL = "https://openrouter.ai/api/alpha/decisions"
var decisionsTimeout = 15 * time.Second

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
	if cfg.DisableJudges || !cfg.JevClassification {
		return nil, "", nil // deliberate suppression (read-only MCP / jev_classification off) — no opinion, not a failure
	}
	apiKey := jevAPIKey(cfg)
	if apiKey == "" {
		return nil, "", fmt.Errorf("no API key for Jev decisions")
	}
	model := cfg.ClassificationModel
	if model == "" {
		model = defaultJevModel
	}
	qs := make(map[string]judgeQuestion, len(questions))
	for k, ins := range questions {
		qs[k] = judgeQuestion{Instructions: truncate(ins, 300), Type: "noul"}
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
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/duketopceo/dayflow-linux")
	req.Header.Set("X-Title", cfg.SiteName)

	client := &http.Client{Timeout: decisionsTimeout}
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
		// Accept on noul presence alone — a strict type check turns alpha
		// schema drift into a silent total no-op that fallbacks can't see.
		if a.Noul != nil {
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
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
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
		qs["cat_"+c.Name] = fmt.Sprintf("The primary activity category for this block is %q (%s).",
			c.Name, truncate(c.Description, 200))
	}
	qs["productive"] = "This block represents focused, intentional task work rather than distraction or idle drift."
	qs["quality"] = "The generated title and summary are specific and accurate — they name the actual activity rather than a generic label."
	if hasPrev {
		qs["same_as_prev"] = fmt.Sprintf("This block continues the same activity as the previous block (previous: %q in %s).",
			truncate(prevTitle, 200), truncate(prevApp, 60))
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

// judgeRetryable asks Jev whether a repeatedly-failed block's error is
// plausibly transient. Judge failure returns false — the block goes dead,
// which is the pre-Jev behavior.
func judgeRetryable(db *sql.DB, cfg Config, start time.Time, errText string) bool {
	if errText == "" {
		return false // nothing to classify — dead, the pre-Jev behavior
	}
	// Send only the normalized error class — raw provider errors can echo
	// paths, LAN URLs, and request content, and must not leave the machine.
	state := boundState(fmt.Sprintf("A screen-activity summarization block failed repeatedly.\nTime: %s\nError class: %s",
		start.Format("2006-01-02 15:04"), errorClass(errText)), 2000)
	ans, _, err := decide(db, cfg, "triage", state, map[string]string{
		"retryable": "The failure is plausibly transient (rate limit, network, temporary provider error) and one more retry could succeed — not an auth, billing, or config error.",
	})
	if err != nil {
		debugf(cfg, "triage %s: judge failed: %v", start.Format("15:04"), err)
		return false
	}
	return ans["retryable"] >= 0.5
}

// worthyBlocks filters a day's blocks to those Jev judges worth mentioning in
// a standup. Only explicit low scores drop a block — unjudged or unscored
// blocks pass through, and an all-dropped day falls back to the top-3 spans
// by minutes so the report is never empty. Judge failure returns the input.
func worthyBlocks(db *sql.DB, cfg Config, blocks []Block) []Block {
	var judged []Block
	for _, b := range blocks {
		if b.Status == "done" && b.Category != "idle" {
			judged = append(judged, b)
		}
	}
	if len(judged) == 0 {
		return blocks
	}
	if len(judged) > 40 {
		sort.Slice(judged, func(i, k int) bool {
			return judged[i].End.Sub(judged[i].Start) > judged[k].End.Sub(judged[k].Start)
		})
		judged = judged[:40]
	}
	// Only ask about blocks that fit the state bound — a question Jev can't
	// see the context for returns a score we shouldn't act on.
	var st strings.Builder
	qs := map[string]string{}
	for _, b := range judged {
		line := fmt.Sprintf("- %s-%s [%s/%s]: %s — %s\n", b.StartStr, b.EndStr, b.App, b.Category, b.Title, b.Summary)
		if st.Len()+len(line) > 4000 {
			break
		}
		st.WriteString(line)
		qs[fmt.Sprintf("worthy_%d", b.StartTs)] = "This block is worth mentioning in a daily standup update — meaningful work or a notable event, not routine drift, idle time, or trivial app-hopping."
	}
	ans, _, err := decide(db, cfg, "standup", boundState(st.String(), 4000), qs)
	if err != nil {
		debugf(cfg, "standup judge failed: %v", err)
		return blocks
	}
	var worthy []Block
	for _, b := range blocks {
		if s, ok := ans[fmt.Sprintf("worthy_%d", b.StartTs)]; ok && s < 0.5 {
			continue
		}
		worthy = append(worthy, b)
	}
	if len(worthy) == 0 {
		sorted := make([]Block, len(blocks))
		copy(sorted, blocks)
		sort.Slice(sorted, func(i, k int) bool {
			return sorted[i].End.Sub(sorted[i].Start) > sorted[k].End.Sub(sorted[k].Start)
		})
		if len(sorted) > 3 {
			sorted = sorted[:3]
		}
		return sorted
	}
	return worthy
}

// judgeForecast asks Jev whether the predicted category mix plausibly matches
// the target day's likely shape. Returns nil on any failure or no-opinion —
// the forecast's heuristic confidence stands alone.
func judgeForecast(db *sql.DB, cfg Config, fc Forecast) *float64 {
	if len(fc.Items) == 0 {
		return nil
	}
	var st strings.Builder
	fmt.Fprintf(&st, "Forecast for %s (%s) from %d same-weekday samples over %d days of history (~%.0f min predicted):\n",
		fc.Date, fc.Weekday, fc.Samples, forecastWindowDays, fc.TotalMinutes)
	for _, it := range fc.Items {
		fmt.Fprintf(&st, "- %s: %.0f%% (~%.0f min)\n", it.Category, it.Pct, it.Minutes)
	}
	ans, _, err := decide(db, cfg, "forecast", boundState(st.String(), 3000), map[string]string{
		"confident": "This predicted category mix is a plausible forecast for the target day given typical weekly work patterns.",
	})
	if err != nil {
		debugf(cfg, "forecast judge failed: %v", err)
		return nil
	}
	if s, ok := ans["confident"]; ok {
		return &s
	}
	return nil
}

// salientShifts filters context-shift edges to those Jev judges as real
// context changes. The top 12 edges by minutes are scored; explicit low
// scores drop, unjudged edges pass through. Judge failure returns the input.
func salientShifts(db *sql.DB, cfg Config, shifts []ContextShift) []ContextShift {
	const judgeCap = 12
	n := len(shifts)
	if n > judgeCap {
		n = judgeCap
	}
	if n == 0 {
		return shifts
	}
	var st strings.Builder
	qs := map[string]string{}
	for i := 0; i < n; i++ {
		s := shifts[i]
		fmt.Fprintf(&st, "- %s -> %s: %d transitions, %.0f min downstream\n", s.Source, s.Target, s.Count, s.Minutes)
		qs[fmt.Sprintf("real_shift_%d", i)] = fmt.Sprintf(
			"A %s -> %s transition is a meaningful context change — a real switch in the kind of work, not incidental app noise.",
			s.Source, s.Target)
	}
	ans, _, err := decide(db, cfg, "shifts", boundState(st.String(), 3000), qs)
	if err != nil {
		debugf(cfg, "shifts judge failed: %v", err)
		return shifts
	}
	out := make([]ContextShift, 0, len(shifts))
	for i, s := range shifts {
		if i < judgeCap {
			// 0.4 is deliberately stricter than the 0.55 flag threshold —
			// dropping an edge hides data, so only clear "no" votes filter.
			if v, ok := ans[fmt.Sprintf("real_shift_%d", i)]; ok && v < 0.4 {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// errorClass normalizes a provider error for judgment state — the raw text
// can embed home paths, local endpoint URLs, or provider response echoes,
// none of which belong in an outbound payload.
func errorClass(e string) string {
	l := strings.ToLower(e)
	switch {
	case strings.Contains(l, "401"), strings.Contains(l, "403"), strings.Contains(l, "auth"):
		return "auth"
	case strings.Contains(l, "402"), strings.Contains(l, "credit"), strings.Contains(l, "billing"):
		return "billing"
	case strings.Contains(l, "429"), strings.Contains(l, "rate limit"):
		return "rate_limit"
	case strings.Contains(l, "500"), strings.Contains(l, "502"), strings.Contains(l, "503"):
		return "server_error"
	case strings.Contains(l, "timeout"), strings.Contains(l, "deadline"):
		return "timeout"
	case strings.Contains(l, "dns"), strings.Contains(l, "name resolution"), strings.Contains(l, "connection refused"), strings.Contains(l, "no such host"):
		return "network"
	default:
		return "other"
	}
}

// jevAPIKey resolves the decisions-API credential through the provider chain
// (classification task → chat → vision) then the legacy OpenRouter key.
func jevAPIKey(cfg Config) string {
	for _, task := range []string{"classification", "chat", "vision"} {
		if p, err := providerForTask(cfg, task); err == nil {
			if k := resolveProviderKey(p); k != "" {
				return k
			}
		}
	}
	return cfg.OpenRouterAPIKey
}
