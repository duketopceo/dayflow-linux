package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// var so tests can point at a stub server.
var openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

const summarizePrompt = `You are analyzing screen captures from a personal activity tracker.
These images are frames sampled across a %d-minute window of the user's screen.

Respond with ONLY a JSON object (no markdown fences) in this exact shape:
{"title": "2-5 word activity title",
 "summary": "1-3 sentences describing what the user was actually doing, in second person past tense, e.g. 'You were editing dayflow-linux's summarize.go in Neovim and reading the OpenRouter docs in Chromium.'",
 "category": "one of: coding, browsing, communication, writing, design, media, meetings, system, idle, other",
 "activities": [{"app": "window class or app name, lowercase, e.g. 'neovim' or 'firefox'", "title": "2-5 word title", "summary": "1-2 sentences, second person past tense", "category": "same enum"}]}

"activities" breaks the window into per-app segments in chronological order (usually 1-3 entries; 1 if the user stayed in one app).
Be concrete: name apps, sites, files, and topics you can see. If the screen was locked, idle, or unchanged the whole time, use category "idle" and return activities: [].`

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
	Activities []Activity `json:"activities"`
}

func chatURL(cfg Config) string {
	if cfg.APIBaseURL != "" {
		u := strings.TrimSuffix(cfg.APIBaseURL, "/")
		return u + "/chat/completions"
	}
	return openRouterURL
}

func apiKey(cfg Config) string {
	return cfg.OpenRouterAPIKey
}

func callOpenRouter(cfg Config, frames []string) (*blockResult, int, int, error) {
	if cfg.APIBaseURL == "" && apiKey(cfg) == "" {
		return nil, 0, 0, fmt.Errorf("no API key: set openrouter_api_key in %s or OPENROUTER_API_KEY", configPath())
	}
	content := []orContent{{Type: "text", Text: fmt.Sprintf(summarizePrompt, cfg.BlockMinutes)}}
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

	reqBody, _ := json.Marshal(orRequest{
		Model:    cfg.Model,
		Messages: []orMessage{{Role: "user", Content: content}},
	})
	req, err := http.NewRequest("POST", chatURL(cfg), bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0, 0, err
	}
	if apiKey(cfg) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey(cfg))
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIBaseURL == "" {
		req.Header.Set("HTTP-Referer", "https://github.com/duketopceo/dayflow-linux")
		req.Header.Set("X-Title", cfg.SiteName)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, 0, 0, fmt.Errorf("openrouter %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var or orResponse
	if err := json.Unmarshal(body, &or); err != nil {
		return nil, 0, 0, err
	}
	if or.Error != nil {
		return nil, 0, 0, fmt.Errorf("openrouter: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return nil, 0, 0, fmt.Errorf("openrouter: no choices")
	}
	text := stripFences(or.Choices[0].Message.Content)
	var res blockResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		return nil, 0, 0, fmt.Errorf("bad model JSON: %w (raw: %s)", err, truncate(text, 200))
	}
	pt, ct := 0, 0
	if or.Usage != nil {
		pt, ct = or.Usage.PromptTokens, or.Usage.CompletionTokens
	}
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
	if cfg.OpenRouterAPIKey == "" {
		return 0, fmt.Errorf("no API key: set openrouter_api_key in %s or OPENROUTER_API_KEY", configPath())
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
		t0 := time.Now()
		res, pt, ct, err := callOpenRouter(cfg, paths)
		latency := int(time.Since(t0).Milliseconds())
		if err != nil {
			upsertBlock(db, start, end, "", "", "", len(frames), "failed", err.Error())
			db.Exec(`UPDATE blocks SET attempts=? WHERE start_ts=?`, attempts+1, start.Unix())
			logAPICall(db, start, cfg.Model, len(paths), 0, 0, latency, "error", err.Error())
			logEvent(db, "summarize_error", start.Format("15:04")+": "+err.Error())
			log.Printf("summarize %s: %v", start.Format("15:04"), err)
			continue
		}
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
		if err := upsertBlockFull(db, start, end, res.Title, res.Summary, res.Category, app, actsJSON, len(frames), 0, "done", ""); err != nil {
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
