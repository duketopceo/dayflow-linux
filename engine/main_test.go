package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEnv(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("DAYFLOW_CONFIG", filepath.Join(dir, "nonexistent.json"))
	t.Setenv("OPENROUTER_API_KEY", "")
	cfg := defaultConfig()
	cfg.OpenRouterAPIKey = "test-key"
	return cfg
}

func solidImage(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestAhashDedup(t *testing.T) {
	a := ahash(solidImage(color.RGBA{40, 40, 40, 255}))
	b := ahash(solidImage(color.RGBA{42, 42, 42, 255}))
	if hamming(a, b) > dedupThreshold {
		t.Fatalf("near-identical frames should dedup, hamming=%d", hamming(a, b))
	}
	// noise vs solid should differ a lot
	noise := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			v := uint8((x*31 + y*17) % 255)
			noise.Set(x, y, color.RGBA{v, 255 - v, v, 255})
		}
	}
	if hamming(a, ahash(noise)) <= dedupThreshold {
		t.Fatal("distinct frames wrongly deduped")
	}
}

func TestBlockStart(t *testing.T) {
	ts := time.Date(2026, 9, 4, 14, 37, 22, 0, time.Local)
	got := blockStart(ts, 15)
	want := time.Date(2026, 9, 4, 14, 30, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("blockStart=%v want %v", got, want)
	}
	// crossing midnight
	ts2 := time.Date(2026, 9, 4, 0, 7, 0, 0, time.Local)
	got2 := blockStart(ts2, 15)
	if got2.Hour() != 0 || got2.Minute() != 0 {
		t.Fatalf("blockStart midnight=%v", got2)
	}
}

func TestStripFences(t *testing.T) {
	in := "```json\n{\"title\":\"x\"}\n```"
	if stripFences(in) != `{"title":"x"}` {
		t.Fatalf("stripFences: %q", stripFences(in))
	}
}

func TestIsIgnored(t *testing.T) {
	cfg := defaultConfig()
	cfg.IgnoreApps = []string{"1password", "Slack"}
	if !isIgnored(cfg, "1Password") || !isIgnored(cfg, "slack") {
		t.Fatal("case-insensitive ignore failed")
	}
	if isIgnored(cfg, "firefox") || isIgnored(cfg, "") {
		t.Fatal("false positive ignore")
	}
}

func TestSampleFrames(t *testing.T) {
	var frames []struct {
		TS   int64
		Path string
	}
	for i := 0; i < 90; i++ {
		frames = append(frames, struct {
			TS   int64
			Path string
		}{int64(i), "f"})
	}
	got := sampleFrames(frames, 30)
	if len(got) != 30 {
		t.Fatalf("sampled %d", len(got))
	}
	got = sampleFrames(frames, 200)
	if len(got) != 90 {
		t.Fatalf("oversampled: %d", len(got))
	}
}

func writeFrame(t *testing.T, dir, name string, ts time.Time) string {
	t.Helper()
	p := filepath.Join(dir, name)
	var buf bytes.Buffer
	jpeg.Encode(&buf, solidImage(color.RGBA{uint8(ts.Second()), 20, 20, 255}), nil)
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func stubOpenRouter(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": reply}},
			},
			"usage": map[string]int{"prompt_tokens": 5000, "completion_tokens": 40},
		})
	}))
	t.Cleanup(srv.Close)
	old := openRouterURL
	openRouterURL = srv.URL
	t.Cleanup(func() { openRouterURL = old })
	return srv
}

func TestSummarizePendingEndToEnd(t *testing.T) {
	cfg := testEnv(t)
	stubOpenRouter(t, `{"title":"Testing","summary":"You were testing.","category":"coding"}`)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// seed 3 frames in a completed 15-min block
	now := time.Now()
	start := blockStart(now, cfg.BlockMinutes).Add(-time.Duration(cfg.BlockMinutes) * time.Minute)
	for i := 0; i < 3; i++ {
		ts := start.Add(time.Duration(i) * time.Minute)
		p := writeFrame(t, t.TempDir(), ts.Format("150405")+".jpg", ts)
		if err := insertFrame(db, ts, p); err != nil {
			t.Fatal(err)
		}
	}

	cfg.KeepFrames = true // keep files so nothing gets deleted under TempDir paths
	n, err := summarizePending(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("summarized %d blocks, want 1", n)
	}
	blocks, err := blocksForDay(db, start, false)
	if err != nil || len(blocks) != 1 {
		t.Fatalf("blocks=%v err=%v", blocks, err)
	}
	if blocks[0].Title != "Testing" {
		t.Fatalf("title=%q", blocks[0].Title)
	}
	// api call logged with usage
	var calls, pt int
	db.QueryRow(`SELECT COUNT(1), COALESCE(SUM(prompt_tokens),0) FROM api_calls`).Scan(&calls, &pt)
	if calls != 1 || pt != 5000 {
		t.Fatalf("api_calls: calls=%d pt=%d", calls, pt)
	}
	// second run must not re-summarize
	n, _ = summarizePending(db, cfg, false)
	if n != 0 {
		t.Fatalf("re-summarized %d blocks", n)
	}
}

func TestSummarizeBadJSONRetried(t *testing.T) {
	cfg := testEnv(t)
	stubOpenRouter(t, "not json at all")
	db, _ := openDB()
	defer db.Close()
	now := time.Now()
	start := blockStart(now, cfg.BlockMinutes).Add(-time.Duration(cfg.BlockMinutes) * time.Minute)
	p := writeFrame(t, t.TempDir(), "f.jpg", start)
	insertFrame(db, start.Add(time.Minute), p)
	n, err := summarizePending(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("summarized %d", n)
	}
	var status, apiErr string
	db.QueryRow(`SELECT status, error FROM blocks WHERE start_ts=?`, start.Unix()).Scan(&status, &apiErr)
	if status != "failed" || apiErr == "" {
		t.Fatalf("block status=%q err=%q", status, apiErr)
	}
	// failed block is retried next run (still pending)
	pending, _ := pendingBlocks(db, cfg, now)
	if len(pending) != 1 {
		t.Fatalf("pending=%d", len(pending))
	}
}

func TestSummarizeNoFramesMarksIdle(t *testing.T) {
	cfg := testEnv(t)
	stubOpenRouter(t, `{"title":"x","summary":"y","category":"coding"}`)
	db, _ := openDB()
	defer db.Close()
	// frames two blocks apart leave an empty block between them; it must be
	// marked idle ("No activity") without an API call
	now := time.Now()
	block := time.Duration(cfg.BlockMinutes) * time.Minute
	first := blockStart(now, cfg.BlockMinutes).Add(-3 * block)
	p := writeFrame(t, t.TempDir(), "f.jpg", first)
	insertFrame(db, first.Add(time.Minute), p)
	p2 := writeFrame(t, t.TempDir(), "f2.jpg", first)
	insertFrame(db, first.Add(2*block).Add(time.Minute), p2)
	n, _ := summarizePending(db, cfg, false)
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
	var title string
	db.QueryRow(`SELECT title FROM blocks WHERE start_ts=?`,
		first.Add(block).Unix()).Scan(&title)
	if title != "No activity" {
		t.Fatalf("empty block title=%q", title)
	}
}

func TestConfigSet(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	os.WriteFile(cp, []byte(`{}`), 0o600)
	if err := setConfigValue("model", "openai/gpt-5"); err != nil {
		t.Fatal(err)
	}
	if err := setConfigValue("ignore_apps", "a, B ,c"); err != nil {
		t.Fatal(err)
	}
	if err := setConfigValue("bogus_key", "x"); err == nil {
		t.Fatal("expected error for unknown key")
	}
	cfg, _ := loadConfig()
	if cfg.Model != "openai/gpt-5" || len(cfg.IgnoreApps) != 3 || cfg.IgnoreApps[1] != "B" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestRetention(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	old := time.Now().Add(-10 * 24 * time.Hour)
	p := writeFrame(t, t.TempDir(), "old.jpg", old)
	insertFrame(db, old, p)
	recent := writeFrame(t, t.TempDir(), "new.jpg", time.Now())
	insertFrame(db, time.Now(), recent)
	runRetention(db, cfg)
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&n)
	if n != 1 {
		t.Fatalf("frames left=%d", n)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("old frame file not deleted")
	}
}

func TestChatURL(t *testing.T) {
	cfg := defaultConfig()
	if chatURL(cfg) != openRouterURL {
		t.Fatalf("default chatURL=%q", chatURL(cfg))
	}
	cfg.APIBaseURL = "http://localhost:11434/v1"
	if chatURL(cfg) != "http://localhost:11434/v1/chat/completions" {
		t.Fatalf("ollama chatURL=%q", chatURL(cfg))
	}
	cfg.APIBaseURL = "http://localhost:11434/v1/"
	if chatURL(cfg) != "http://localhost:11434/v1/chat/completions" {
		t.Fatalf("trailing slash chatURL=%q", chatURL(cfg))
	}
}

func TestStandupAndInsights(t *testing.T) {
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()

	yesterday := time.Now().AddDate(0, 0, -1)
	yStart := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 10, 0, 0, 0, yesterday.Location())
	yEnd := yStart.Add(15 * time.Minute)
	upsertBlockFull(db, yStart, yEnd, "Auth refactor", "Extracted token logic", "coding", "neovim", "", 3, 0, "done", "", nil)

	md, _, err := generateStandup(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "Auth refactor") {
		t.Fatalf("standup missing block: %s", md)
	}

	in, err := generateInsights(db, cfg, yStart, yEnd.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if in.TotalMins != 15 {
		t.Fatalf("insights total=%f", in.TotalMins)
	}
	if len(in.Categories) != 1 || in.Categories[0].Name != "coding" {
		t.Fatalf("insights categories=%v", in.Categories)
	}
	if len(in.Apps) != 1 || in.Apps[0].Name != "neovim" {
		t.Fatalf("insights apps=%v", in.Apps)
	}
}
