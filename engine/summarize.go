package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// var so tests can point at a stub server.
var openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

const summarizePromptTemplate = `You are analyzing screen captures from a personal activity tracker.
These images are frames sampled across a %d-minute window of the user's screen.

Classify the activity into exactly one category from the list below. Use the description to decide:
%s
%s
Respond with ONLY a JSON object (no markdown fences) in this exact shape:
{"title": "a descriptive 4-8 word title naming the concrete task, project, or topic — e.g. 'Debugging Hyprland audio routing in Omarchy', 'Reviewing PR feedback on dayflow panel', 'Reading OpenRouter docs for vision API'",
 "summary": "2-4 sentences describing what the user was actually doing, in second person past tense. Be specific and descriptive — name the project, files, apps, sites, and the goal or problem being worked on, e.g. 'You were debugging why the Dayflow panel buttons rendered white in Quickshell, editing Panel.qml and restarting the shell to verify the accent color fix.' — not generic like 'Coding and testing'.",
 "category": "one of: %s",
 "productive": true or false — true if the user was actively making progress on work (coding, writing, debugging, configuring, planning a project, applying for a job, etc.), false if they were passively consuming, social browsing, idle, or in entertainment. For example: Ghostty with a terminal build is productive=true; YouTube/Reddit/music is productive=false; managing OpenRouter keys in a browser is productive=true because it is task work.",
 "activities": [{"app": "window class or app name, lowercase, e.g. 'neovim' or 'firefox'", "title": "3-6 word descriptive title naming the specific thing done in that app", "summary": "1-2 sentences, second person past tense, concrete details", "category": "same enum", "productive": true or false}]}

"activities" breaks the window into per-app segments in chronological order (usually 1-3 entries; 1 if the user stayed in one app).
Be concrete and descriptive: name apps, sites, files, repos, doc pages, and the actual topic or task visible on screen. Avoid generic labels like "Software Development" or "Coding and Testing" — say WHAT was being developed or tested. If the screen was locked, idle, or unchanged the whole time, use category "idle" and return activities: [].
If the screen contains explicit sexual or adult content, do not describe it. Instead produce a generic, non-graphic summary such as "Personal activity" and use category "personal". Never name adult sites or describe explicit material.
Do not include credit card numbers, bank account details, ID numbers, Social Security numbers, passwords, API keys, access tokens, or other sensitive personal information. If the screen is dominated by such sensitive information, produce a generic summary such as "Personal activity" and use category "personal".`

func buildSummarizePrompt(cfg Config) string {
	var catList strings.Builder
	for _, c := range cfg.Categories {
		fmt.Fprintf(&catList, "- %s: %s\n", c.Name, c.Description)
	}
	catNames := make([]string, len(cfg.Categories))
	for i, c := range cfg.Categories {
		catNames[i] = c.Name
	}
	extra := ""
	if cfg.ClassificationPrompt != "" {
		extra = "\nThe user has also provided the following extra classification guidance:\n" + cfg.ClassificationPrompt + "\n"
	}
	return fmt.Sprintf(summarizePromptTemplate, cfg.BlockMinutes, catList.String(), extra, strings.Join(catNames, ", "))
}

// buildPromptForProvider returns the summarize prompt for a provider, honoring
// a per-provider summary_prompt override; the classification_prompt is still
// appended so user guidance applies to every provider.
func buildPromptForProvider(p Provider, cfg Config) string {
	if p.PromptOverrides.SummaryPrompt == "" {
		return buildSummarizePrompt(cfg)
	}
	prompt := p.PromptOverrides.SummaryPrompt
	if cfg.ClassificationPrompt != "" {
		prompt += "\n\nThe user has also provided the following extra classification guidance:\n" + cfg.ClassificationPrompt
	}
	return prompt
}

type orContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type orMessage struct {
	Role    string      `json:"role"`
	Content []orContent `json:"content"`
}

type orRequest struct {
	Model    string      `json:"model"`
	Messages []orMessage `json:"messages"`
}

type orResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type blockResult struct {
	Title      string     `json:"title"`
	Summary    string     `json:"summary"`
	Category   string     `json:"category"`
	Productive *bool      `json:"productive,omitempty"`
	Activities []Activity `json:"activities"`
}

func chatURL(cfg Config) string {
	if cfg.APIBaseURL != "" {
		u := strings.TrimSuffix(cfg.APIBaseURL, "/")
		return u + "/chat/completions"
	}
	return openRouterURL
}

// callOpenRouter summarizes a block's frames via the provider routed for the
// "vision" task.
func callOpenRouter(cfg Config, frames []string) (*blockResult, int, int, error) {
	p, err := providerForTask(cfg, "vision")
	if err != nil {
		return nil, 0, 0, err
	}
	content := []orContent{{Type: "text", Text: buildPromptForProvider(p, cfg)}}
	for _, f := range frames {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		c := orContent{Type: "image_url"}
		c.ImageURL = &struct {
			URL string `json:"url"`
		}{URL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw)}
		content = append(content, c)
	}
	if len(content) == 1 {
		return nil, 0, 0, fmt.Errorf("no readable frames")
	}

	text, pt, ct, err := callProviderChat(cfg, p, []orMessage{{Role: "user", Content: content}})
	if err != nil {
		return nil, 0, 0, err
	}
	var res blockResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return nil, 0, 0, fmt.Errorf("bad model JSON: %w (raw: %s)", err, truncate(text, 200))
	}
	sanitizeResult(cfg, &res)
	return &res, pt, ct, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

var sensitiveTerms = []string{
	// adult / explicit
	"porn", "pornhub", "xvideos", "xhamster", "redtube", "youporn",
	"adult content", "adult video", "adult site", "adult website",
	"pornographic", "sex video", "explicit content", "explicit video",
	// financial / PII
	"bank account", "checking account", "savings account", "routing number",
	"account number", "IBAN", "sort code", "wire transfer",
	"credit card", "debit card", "card number", "cvv", "expiration date",
	"social security", "SSN", "passport", "driver's license", "ID number",
	"national ID", "tax ID", "tax identification", "government ID",
}

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{4}[ -]\d{4}[ -]\d{4}[ -]\d{4}\b`), // credit-card-like
	regexp.MustCompile(`\b\d{16}\b`),                           // 16-digit PAN
	regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),                // SSN-like
}

func containsSensitive(text string) bool {
	lower := strings.ToLower(text)
	for _, term := range sensitiveTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	for _, re := range sensitivePatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func fallbackProductive(res *blockResult) bool {
	return !isDistractionCategory(res.Category)
}

func sanitizeResult(cfg Config, res *blockResult) {
	if res == nil {
		return
	}
	if res.Productive == nil {
		p := fallbackProductive(res)
		res.Productive = &p
	}
	for i := range res.Activities {
		if res.Activities[i].Productive == nil {
			p := !isDistractionCategory(res.Activities[i].Category)
			res.Activities[i].Productive = &p
		}
	}
	if !cfg.FilterInappropriate {
		return
	}
	dirty := containsSensitive(res.Title) ||
		containsSensitive(res.Summary) ||
		containsSensitive(strings.Join(func() []string {
			var parts []string
			for _, a := range res.Activities {
				parts = append(parts, a.Title, a.Summary)
			}
			return parts
		}(), " "))
	if dirty {
		res.Title = "Personal time"
		res.Summary = "Personal activity not recorded."
		res.Category = "personal"
		for i := range res.Activities {
			if containsSensitive(res.Activities[i].Title) ||
				containsSensitive(res.Activities[i].Summary) {
				res.Activities[i].Title = "Personal activity"
				res.Activities[i].Summary = "Personal activity not recorded."
				res.Activities[i].Category = "personal"
			}
		}
	}
}

// blockStart floors t to the nearest local wall-clock block boundary.
func blockStart(t time.Time, mins int) time.Time {
	t = t.Local()
	m := t.Minute() - t.Minute()%mins
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, t.Location())
}

// pendingBlocks finds complete block windows (aligned to local wall clock) that
// have frames but no summarized block yet.
func pendingBlocks(db *sql.DB, cfg Config, now time.Time) ([]time.Time, error) {
	var minTS, maxTS sql.NullInt64
	if err := db.QueryRow(`SELECT MIN(ts), MAX(ts) FROM frames`).Scan(&minTS, &maxTS); err != nil {
		return nil, err
	}
	if !minTS.Valid || !maxTS.Valid {
		return nil, nil
	}
	block := time.Duration(cfg.BlockMinutes) * time.Minute
	start := blockStart(time.Unix(minTS.Int64, 0), cfg.BlockMinutes)
	lastComplete := blockStart(time.Unix(maxTS.Int64, 0), cfg.BlockMinutes)
	currentBlock := blockStart(now, cfg.BlockMinutes)
	var out []time.Time
	for b := start; b.Before(currentBlock) && !b.After(lastComplete); b = b.Add(block) {
		ok, err := blockExists(db, b)
		if err != nil {
			return nil, err
		}
		if !ok {
			out = append(out, b)
		}
	}
	return out, nil
}

func summarizePending(db *sql.DB, cfg Config, includeCurrent bool) (int, error) {
	if _, err := providerForTask(cfg, "vision"); err != nil {
		return 0, err
	}
	now := time.Now()
	if includeCurrent {
		now = now.Add(time.Duration(cfg.BlockMinutes) * time.Minute)
	}
	blocks, err := pendingBlocks(db, cfg, now)
	if err != nil {
		return 0, err
	}
	const maxAttempts = 3
	done := 0
	for _, start := range blocks {
		end := start.Add(time.Duration(cfg.BlockMinutes) * time.Minute)
		attempts := blockAttempts(db, start)
		if attempts >= maxAttempts {
			db.Exec(`UPDATE blocks SET status='dead' WHERE start_ts=?`, start.Unix())
			logEvent(db, "block_dead", start.Format("15:04"))
			continue
		}
		frames, err := framesBetween(db, start, end)
		if err != nil {
			return done, err
		}
		if len(frames) == 0 {
			// mark as done with idle so we don't retry forever
			upsertBlock(db, start, end, "No activity", "Screen was off or idle.", "idle", 0, "done", "")
			continue
		}
		paths := sampleFrames(frames, cfg.FramesPerBlock)
		debugf(cfg, "summarize %s: sending %d/%d frames to %s (%s)",
			start.Format("15:04"), len(paths), len(frames), cfg.Model, chatURL(cfg))
		t0 := time.Now()
		res, pt, ct, err := callOpenRouter(cfg, paths)
		latency := int(time.Since(t0).Milliseconds())
		if err != nil {
			upsertBlock(db, start, end, "", "", "", len(frames), "failed", err.Error())
			db.Exec(`UPDATE blocks SET attempts=? WHERE start_ts=?`, attempts+1, start.Unix())
			logAPICall(db, start, cfg.Model, len(paths), 0, 0, latency, "error", err.Error())
			logEvent(db, "summarize_error", start.Format("15:04")+": "+err.Error())
			log.Printf("summarize %s: %v", start.Format("15:04"), err)
			debugf(cfg, "summarize %s: error after %dms: %v", start.Format("15:04"), latency, err)
			continue
		}
		debugf(cfg, "summarize %s: ok in %dms, tokens in=%d out=%d, title=%q",
			start.Format("15:04"), latency, pt, ct, res.Title)
		logAPICall(db, start, cfg.Model, len(paths), pt, ct, latency, "ok", "")
		logEvent(db, "summarized", start.Format("15:04")+" "+res.Title)
		app := dominantApp(db, start, end)
		actsJSON := ""
		if len(res.Activities) > 0 {
			if b, e := json.Marshal(res.Activities); e == nil {
				actsJSON = string(b)
			}
		}
		if res.Title == "" && len(res.Activities) > 0 {
			res.Title = res.Activities[0].Title
		}
		if err := upsertBlockFull(db, start, end, res.Title, res.Summary, res.Category, app, actsJSON, len(frames), 0, "done", "", res.Productive); err != nil {
			return done, err
		}
		done++
		log.Printf("summarized %s-%s: %s", start.Format("15:04"), end.Format("15:04"), res.Title)
		if !cfg.KeepFrames {
			for _, f := range frames {
				os.Remove(f.Path)
			}
			deleteFrames(db, start, end)
			// clean empty day dir
			dayDir := filepath.Dir(frames[0].Path)
			if entries, _ := os.ReadDir(dayDir); len(entries) == 0 {
				os.Remove(dayDir)
			}
		}
	}
	return done, nil
}

// sampleFrames picks up to n evenly spaced frame paths.
func sampleFrames(frames []struct {
	TS   int64
	Path string
}, n int) []string {
	if len(frames) <= n {
		out := make([]string, len(frames))
		for i, f := range frames {
			out[i] = f.Path
		}
		return out
	}
	out := make([]string, 0, n)
	step := float64(len(frames)) / float64(n)
	for i := 0; i < n; i++ {
		out = append(out, frames[int(float64(i)*step)].Path)
	}
	return out
}
