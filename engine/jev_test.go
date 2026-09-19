package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func stubJev(t *testing.T, category string, noul float64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/alpha/decisions" {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"model": "typesafe/jev-1.13",
			"answers": map[string]any{
				"category": map[string]any{
					"choice":        category,
					"probabilities": map[string]float64{category: 0.92},
				},
				"productive": map[string]any{"noul": noul},
			},
			"usage": map[string]int{"prompt_tokens": 120},
		})
	}))
	t.Cleanup(srv.Close)
	old := decisionsURL
	decisionsURL = srv.URL + "/api/alpha/decisions"
	t.Cleanup(func() { decisionsURL = old })
	return srv
}

func TestClassifyWithJev(t *testing.T) {
	cfg := testEnv(t)
	stubJev(t, "coding", 0.91)
	res := &blockResult{
		Title:    "Debugging dayflow panel",
		Summary:  "You were editing Panel.qml and restarting quickshell.",
		Category: "browsing", // vision hint — Jev should override
	}
	pt, err := classifyWithJev(cfg, res)
	if err != nil {
		t.Fatal(err)
	}
	if pt != 120 {
		t.Fatalf("tokens=%d want 120", pt)
	}
	if res.Category != "coding" {
		t.Fatalf("category=%q want coding", res.Category)
	}
	if res.Productive == nil || !*res.Productive {
		t.Fatalf("productive=%v want true", res.Productive)
	}
}

func TestSummarizeUsesJevClassification(t *testing.T) {
	cfg := testEnv(t)
	cfg.JevClassification = true
	stubOpenRouter(t, `{"title":"Testing","summary":"You were testing Go code.","category":"media"}`)
	stubJev(t, "coding", 0.88)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	start := blockStart(now, cfg.BlockMinutes).Add(-time.Duration(cfg.BlockMinutes) * time.Minute)
	p := writeFrame(t, t.TempDir(), "f.jpg", start)
	insertFrame(db, start.Add(time.Minute), p)

	cfg.KeepFrames = true
	n, err := summarizePending(db, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("summarized %d blocks, want 1", n)
	}
	blocks, _ := blocksForDay(db, start, false)
	if len(blocks) != 1 {
		t.Fatalf("blocks=%d", len(blocks))
	}
	if blocks[0].Category != "coding" {
		t.Fatalf("category=%q want coding from Jev", blocks[0].Category)
	}
	if blocks[0].Productive == nil || !*blocks[0].Productive {
		t.Fatalf("productive=%v want true from Jev", blocks[0].Productive)
	}
}

func TestParseJevClassificationInvalidCategory(t *testing.T) {
	cfg := defaultConfig()
	answers := map[string]json.RawMessage{
		"category":   mustRaw(`{"choice":"not-a-real-category"}`),
		"productive": mustRaw(`{"noul":0.2}`),
	}
	if _, err := parseJevClassification(cfg, answers); err == nil {
		t.Fatal("expected error for unknown category")
	}
}

func mustRaw(s string) json.RawMessage {
	return json.RawMessage(s)
}
