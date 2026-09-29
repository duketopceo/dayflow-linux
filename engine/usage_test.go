package main

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedAPICall(t *testing.T, db *sql.DB, ts time.Time, model string, pt, ct, lat int, status string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO api_calls(ts, block_start, model, frames_sent, prompt_tokens, completion_tokens, latency_ms, status, error)
	  VALUES(?,?,?,1,?,?,?,?,?)`, ts.Unix(), ts.Unix(), model, pt, ct, lat, status, "")
	if err != nil {
		t.Fatal(err)
	}
}

func seedLLMCall(t *testing.T, db *sql.DB, ts time.Time, task, provider, model string, pt, ct, lat int, status string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO llm_calls(ts, task, provider, model, prompt_tokens, completion_tokens, latency_ms, status, error)
	  VALUES(?,?,?,?,?,?,?,?,?)`, ts.Unix(), task, provider, model, pt, ct, lat, status, "")
	if err != nil {
		t.Fatal(err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

func TestUsageSummaryWindowGroups(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	seedAPICall(t, db, now, "m1", 100, 10, 100, "ok")
	seedAPICall(t, db, now, "m1", 200, 20, 300, "ok")
	seedAPICall(t, db, now, "m2", 50, 5, 200, "error") // status != 'ok' convention
	seedLLMCall(t, db, now, "chat", "local", "m1", 300, 30, 600, "ok")
	seedLLMCall(t, db, now, "review", "openrouter", "m3", 400, 40, 400, "failed")

	sum, err := usageSummaryWindow(db, 0, defaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if sum["api_calls"].(int) != 3 || sum["other_llm_calls"].(int) != 2 {
		t.Fatalf("totals: %+v", sum)
	}
	if sum["failed"].(int) != 1 || sum["other_failed"].(int) != 1 {
		t.Fatalf("failed counts: %+v", sum)
	}
	if sum["total_prompt_tokens"].(int) != 1050 || sum["total_completion_tokens"].(int) != 105 {
		t.Fatalf("token totals: %+v", sum)
	}
	// api avg latency: (100+300+200)/3 = 200; failure rate 1/3.
	if got := sum["avg_latency_ms"].(float64); got != 200 {
		t.Fatalf("avg_latency_ms=%v", got)
	}
	if got := sum["failure_rate"].(float64); got < 0.33 || got > 0.34 {
		t.Fatalf("failure_rate=%v", got)
	}

	bd := sum["breakdown"].(map[string]any)
	byTask := bd["by_task"].(map[string]usageRow)
	if byTask["summarize"].Calls != 3 || byTask["chat"].Calls != 1 || byTask["review"].Calls != 1 {
		t.Fatalf("by_task: %+v", byTask)
	}
	if byTask["review"].Failed != 1 || byTask["review"].FailureRate != 1 {
		t.Fatalf("by_task failure fields: %+v", byTask["review"])
	}
	byProvider := bd["by_provider"].(map[string]usageRow)
	// openrouter = 3 folded api_calls + the review llm_call; local = chat.
	if byProvider["openrouter"].Calls != 4 || byProvider["local"].Calls != 1 {
		t.Fatalf("by_provider: %+v", byProvider)
	}
	byModel := bd["by_model"].(map[string]usageRow)
	if byModel["m1"].Calls != 3 || byModel["m2"].Calls != 1 || byModel["m3"].Calls != 1 {
		t.Fatalf("by_model: %+v", byModel)
	}
	// m1 merges api_calls (100+300 ms) and llm_calls (600 ms): 1000/3.
	if got := byModel["m1"].AvgLatencyMs; got < 333 || got > 334 {
		t.Fatalf("m1 avg latency=%v", got)
	}
	if byModel["m2"].FailureRate != 1 {
		t.Fatalf("m2 failure rate=%v", byModel["m2"].FailureRate)
	}
	// Per-day localtime buckets merge both ledgers.
	byDay := bd["by_day"].(map[string]usageRow)
	today := now.Local().Format("2006-01-02")
	if byDay[today].Calls != 5 {
		t.Fatalf("by_day[%q]: %+v", today, byDay)
	}
	// Coverage floor reports the earliest counted row.
	if sum["data_since"].(string) != today {
		t.Fatalf("data_since=%q want %q", sum["data_since"], today)
	}
	// No pricing configured: token-only output, no dollar fields anywhere.
	if _, ok := sum["est_cost_usd"]; ok {
		t.Fatal("est_cost_usd present without pricing config")
	}
	if byModel["m1"].EstCostUSD != nil {
		t.Fatal("per-model est_cost_usd set without pricing config")
	}
}

func TestUsageSummaryDaysWindow(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	// Noon avoids midnight-edge flakes; both rows use localtime bucketing.
	yNoon := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 12, 0, 0, 0, time.Local)
	seedAPICall(t, db, yNoon, "m1", 100, 10, 100, "ok")
	seedAPICall(t, db, now, "m1", 100, 10, 100, "ok")
	seedLLMCall(t, db, yNoon, "chat", "local", "m1", 50, 5, 50, "ok")

	cfg := defaultConfig()
	sum, err := usageSummaryWindow(db, 1, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if sum["api_calls"].(int) != 1 || sum["other_llm_calls"].(int) != 0 {
		t.Fatalf("--days 1 should exclude yesterday's rows: %+v", sum)
	}
	if sum["window_days"].(int) != 1 {
		t.Fatalf("window_days=%v", sum["window_days"])
	}
	if sum["data_since"].(string) != now.Local().Format("2006-01-02") {
		t.Fatalf("data_since=%q", sum["data_since"])
	}

	sum, err = usageSummaryWindow(db, 2, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if sum["api_calls"].(int) != 2 || sum["other_llm_calls"].(int) != 1 {
		t.Fatalf("--days 2 should include yesterday: %+v", sum)
	}
	byDay := sum["breakdown"].(map[string]any)["by_day"].(map[string]usageRow)
	if len(byDay) != 2 {
		t.Fatalf("by_day should have both days: %+v", byDay)
	}
}

func TestUsageSummaryPricing(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	seedAPICall(t, db, now, "priced/model", 1_000_000, 500_000, 10, "ok")
	seedLLMCall(t, db, now, "chat", "local", "unpriced/model", 1000, 10, 10, "ok")

	cfg := defaultConfig()
	cfg.Pricing = map[string]float64{"priced/model": 2.0} // $2 per 1M tokens
	sum, err := usageSummaryWindow(db, 0, cfg)
	if err != nil {
		t.Fatal(err)
	}
	byModel := sum["breakdown"].(map[string]any)["by_model"].(map[string]usageRow)
	r := byModel["priced/model"]
	if r.EstCostUSD == nil || *r.EstCostUSD != 3.0 { // 1.5M tok × $2/1M
		t.Fatalf("est_cost_usd=%v want 3.0", r.EstCostUSD)
	}
	if byModel["unpriced/model"].EstCostUSD != nil {
		t.Fatal("unpriced model got a dollar estimate")
	}
	if sum["est_cost_usd"].(float64) != 3.0 {
		t.Fatalf("total est_cost_usd=%v", sum["est_cost_usd"])
	}
	unpriced, ok := sum["unpriced_models"].([]string)
	if !ok || len(unpriced) != 1 || unpriced[0] != "unpriced/model" {
		t.Fatalf("unpriced_models=%v", sum["unpriced_models"])
	}
}

func TestPricingConfigPatch(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := os.WriteFile(cp, []byte(`{"model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := patchConfig(`{"pricing":{"a/b":1.5}}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pricing["a/b"] != 1.5 {
		t.Fatalf("pricing=%v", cfg.Pricing)
	}
	// config patch merges nested JSON: unmarshal reuses the loaded Pricing
	// map, so a second patch adds/overwrites keys without dropping others.
	if err := patchConfig(`{"pricing":{"c/d":0.5}}`); err != nil {
		t.Fatal(err)
	}
	cfg, _ = loadConfig()
	if len(cfg.Pricing) != 2 || cfg.Pricing["a/b"] != 1.5 || cfg.Pricing["c/d"] != 0.5 {
		t.Fatalf("pricing patch should merge keys: %v", cfg.Pricing)
	}
}

func TestUsageSummaryEmptyFloor(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sum, err := usageSummaryWindow(db, 7, defaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if sum["data_since"].(string) != "" {
		t.Fatalf("empty ledger should report empty data_since, got %q", sum["data_since"])
	}
	if sum["api_calls"].(int) != 0 || sum["other_llm_calls"].(int) != 0 {
		t.Fatalf("empty ledger totals: %+v", sum)
	}
}

func TestPrintUsageRenders(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	seedAPICall(t, db, now, "m1", 1_000_000, 0, 100, "ok")
	seedLLMCall(t, db, now, "chat", "local", "m1", 10, 10, 10, "ok")
	db.Close() // printUsage opens its own handle

	// Text output: coverage floor, latency, failure rate — and no panic on
	// the extended summary shape.
	out := captureStdout(t, func() { printUsage(cfg, false, 0) })
	if !strings.Contains(out, "data since "+now.Local().Format("2006-01-02")) {
		t.Fatalf("missing coverage floor:\n%s", out)
	}
	if !strings.Contains(out, "by day:") || !strings.Contains(out, "avg") {
		t.Fatalf("missing day grouping/latency:\n%s", out)
	}
	if strings.Contains(out, "$") {
		t.Fatalf("dollars rendered without pricing:\n%s", out)
	}

	cfg.Pricing = map[string]float64{"m1": 2.0}
	out = captureStdout(t, func() { printUsage(cfg, true, 0) })
	if !strings.Contains(out, `"est_cost_usd":2`) {
		t.Fatalf("json missing dollar total:\n%s", out)
	}
	out = captureStdout(t, func() { printUsage(cfg, false, 30) })
	if !strings.Contains(out, "estimated cost: $2.00") || !strings.Contains(out, "last 30 day(s)") {
		t.Fatalf("priced text output wrong:\n%s", out)
	}

	// Empty db → explicit empty-state line, still no panic.
	testEnv(t)
	out = captureStdout(t, func() { printUsage(cfg, false, 7) })
	if !strings.Contains(out, "no call rows recorded") {
		t.Fatalf("empty state missing:\n%s", out)
	}
}

func TestMCPGetUsageParity(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	yNoon := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 12, 0, 0, 0, time.Local)
	seedAPICall(t, db, yNoon, "m1", 10, 5, 10, "ok")
	seedAPICall(t, db, now, "m1", 10, 5, 10, "ok")

	res, err := mcpCall(db, cfg, false, "get_usage", map[string]any{"days": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	sum := res.(map[string]any)
	if sum["window_days"].(int) != 1 || sum["api_calls"].(int) != 1 {
		t.Fatalf("mcp get_usage days window: %+v", sum)
	}
	if _, ok := sum["breakdown"].(map[string]any)["by_day"]; !ok {
		t.Fatal("mcp get_usage missing by_day breakdown")
	}
}

func TestPruneOldEventsIncludesLLMCalls(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	old := time.Now().Add(-10 * 24 * time.Hour)
	recent := time.Now()
	seedLLMCall(t, db, old, "chat", "local", "m1", 1, 1, 1, "ok")
	seedLLMCall(t, db, recent, "chat", "local", "m1", 1, 1, 1, "ok")
	seedAPICall(t, db, old, "m1", 1, 1, 1, "ok")
	db.Exec(`INSERT INTO events(ts, type, detail) VALUES(?, 'x', '')`, old.Unix())

	pruneOldEvents(db, time.Now().Add(-7*24*time.Hour))

	var llm, api, ev int
	db.QueryRow(`SELECT COUNT(1) FROM llm_calls`).Scan(&llm)
	db.QueryRow(`SELECT COUNT(1) FROM api_calls`).Scan(&api)
	db.QueryRow(`SELECT COUNT(1) FROM events`).Scan(&ev)
	if llm != 1 {
		t.Fatalf("llm_calls rows=%d — old row must join the retention prune", llm)
	}
	if api != 0 || ev != 0 {
		t.Fatalf("api_calls=%d events=%d", api, ev)
	}
}
