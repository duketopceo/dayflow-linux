package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// decisionsTestServer returns a test server that responds to POST / with a
// canned decisions JSON body, recording request bodies for assertion.
func decisionsTestServer(status int, respBody string) (*httptest.Server, *[]string) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, respBody)
	}))
	return srv, &bodies
}

// pointDecisionsAt redirects the decisions endpoint to srv for the test's
// duration and registers cleanup. Returns srv (already closed on failure).
func pointDecisionsAt(t *testing.T, srv *httptest.Server) {
	t.Helper()
	old := decisionsURL
	decisionsURL = srv.URL
	t.Cleanup(func() { decisionsURL = old })
}

func TestDecideHappy(t *testing.T) {
	testEnv(t)
	srv, bodies := decisionsTestServer(200, `{
		"model": "typesafe/jev-1.13-20260917",
		"answers": {"a": {"type": "noul", "noul": 0.9}, "b": {"type": "noul", "noul": 0.1}},
		"usage": {"input_tokens": 100, "output_tokens": 20},
		"provider": "TypeSafe"
	}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{OpenRouterAPIKey: "k", JevModel: "jev-latest"}
	ans, model, err := decide(db, cfg, "category", "state text", map[string]string{"a": "q a", "b": "q b"})
	if err != nil {
		t.Fatal(err)
	}
	if ans["a"] != 0.9 || ans["b"] != 0.1 {
		t.Fatalf("answers = %v", ans)
	}
	if model != "typesafe/jev-1.13-20260917" {
		t.Fatalf("model = %q", model)
	}
	// request shape: questions record, noul type, instructions
	var req struct {
		Model     string `json:"model"`
		State     string `json:"state"`
		Questions map[string]struct {
			Type         string `json:"type"`
			Instructions string `json:"instructions"`
		} `json:"questions"`
	}
	if len(*bodies) != 1 {
		t.Fatalf("bodies = %d", len(*bodies))
	}
	if err := json.Unmarshal([]byte((*bodies)[0]), &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "jev-latest" || req.State != "state text" {
		t.Fatalf("req = %+v", req)
	}
	if req.Questions["a"].Type != "noul" || req.Questions["a"].Instructions != "q a" {
		t.Fatalf("questions = %+v", req.Questions)
	}
	// llm_calls ledger row
	var task, provider, m string
	var pt, ct int
	err = db.QueryRow(`SELECT task, provider, model, prompt_tokens, completion_tokens FROM llm_calls ORDER BY id DESC LIMIT 1`).
		Scan(&task, &provider, &m, &pt, &ct)
	if err != nil {
		t.Fatal(err)
	}
	if task != "judge:category" || provider != "typesafe" || m != "typesafe/jev-1.13-20260917" || pt != 100 || ct != 20 {
		t.Fatalf("llm_calls row: %s %s %s %d %d", task, provider, m, pt, ct)
	}
}

func TestDecideMissingKey(t *testing.T) {
	testEnv(t)
	srv, _ := decisionsTestServer(200, `{"model":"m","answers":{"a":{"type":"noul","noul":0.9}},"usage":{}}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)

	cfg := Config{OpenRouterAPIKey: "k"}
	ans, _, err := decide(nil, cfg, "k", "s", map[string]string{"a": "x", "missing": "y"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ans["missing"]; ok {
		t.Fatalf("missing key present: %v", ans)
	}
	if ans["a"] != 0.9 {
		t.Fatalf("ans = %v", ans)
	}
}

func TestDecideEmptyQuestions(t *testing.T) {
	ans, _, err := decide(nil, Config{OpenRouterAPIKey: "k"}, "k", "s", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ans) != 0 {
		t.Fatalf("ans = %v", ans)
	}
}

func TestDecideHTTPError(t *testing.T) {
	testEnv(t)
	srv, _ := decisionsTestServer(400, `{"error":"questions: expected record"}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, _, err = decide(db, Config{OpenRouterAPIKey: "k"}, "category", "s", map[string]string{"a": "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	var status, emsg string
	if err := db.QueryRow(`SELECT status, error FROM llm_calls ORDER BY id DESC LIMIT 1`).Scan(&status, &emsg); err != nil {
		t.Fatal(err)
	}
	if status != "error" || !strings.Contains(emsg, "400") {
		t.Fatalf("llm_calls: %s %q", status, emsg)
	}
}

func TestDecideTimeout(t *testing.T) {
	testEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()
	pointDecisionsAt(t, srv)

	// decide uses a 15s timeout; for the test we can't wait that long, so we
	// verify a mid-request abort produces an error rather than a hang by
	// closing the server instead.
	srv.Close()
	_, _, err := decide(nil, Config{OpenRouterAPIKey: "k"}, "k", "s", map[string]string{"a": "x"})
	if err == nil {
		t.Fatal("expected error on unreachable server")
	}
}

func TestBoundState(t *testing.T) {
	long := strings.Repeat("x", 5000)
	out := boundState(long, 4000)
	if len(out) > 4010 {
		t.Fatalf("len = %d", len(out))
	}
	if boundState("short", 4000) != "short" {
		t.Fatal("short passthrough failed")
	}
}

func judgeTestCfg() Config {
	return Config{
		OpenRouterAPIKey: "k",
		Categories: []Category{
			{Name: "coding", Description: "Software development"},
			{Name: "comms", Description: "Communication"},
			{Name: "browsing", Description: "Web browsing"},
			{Name: "idle", Description: "Idle or locked"},
		},
	}
}

func TestJudgeBlockArgmax(t *testing.T) {
	testEnv(t)
	srv, bodies := decisionsTestServer(200, `{"model":"m","answers":{
		"cat_coding":{"type":"noul","noul":0.9},
		"cat_comms":{"type":"noul","noul":0.05},
		"cat_browsing":{"type":"noul","noul":0.03},
		"cat_idle":{"type":"noul","noul":0.02},
		"productive":{"type":"noul","noul":0.8},
		"quality":{"type":"noul","noul":0.7},
		"same_as_prev":{"type":"noul","noul":0.6}
	},"usage":{}}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)

	res := &blockResult{Title: "Refactor engine", Summary: "Split store.go", Category: "browsing"}
	j, err := judgeBlock(nil, judgeTestCfg(), res, "neovim", "Earlier coding", "neovim", true)
	if err != nil {
		t.Fatal(err)
	}
	if j.Category != "coding" || j.Confidence == nil || *j.Confidence != 0.9 {
		t.Fatalf("j = %+v", j)
	}
	if j.Productive == nil || !*j.Productive {
		t.Fatalf("productive = %v", j.Productive)
	}
	if j.Quality == nil || *j.Quality != 0.7 {
		t.Fatalf("quality = %v", j.Quality)
	}
	if j.SameAsPrev == nil || !*j.SameAsPrev {
		t.Fatalf("same_as_prev = %v", j.SameAsPrev)
	}
	// same_as_prev question present only because hasPrev
	if !strings.Contains((*bodies)[0], "same_as_prev") {
		t.Fatal("same_as_prev question missing")
	}
}

func TestJudgeBlockNoPrev(t *testing.T) {
	testEnv(t)
	srv, bodies := decisionsTestServer(200, `{"model":"m","answers":{"cat_coding":{"type":"noul","noul":0.9}},"usage":{}}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)

	res := &blockResult{Title: "t", Summary: "s", Category: "coding"}
	j, err := judgeBlock(nil, judgeTestCfg(), res, "neovim", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains((*bodies)[0], "same_as_prev") {
		t.Fatal("same_as_prev question sent without prev block")
	}
	if j.SameAsPrev != nil {
		t.Fatalf("same_as_prev = %v", j.SameAsPrev)
	}
}

func TestJudgeBlockPartialAnswers(t *testing.T) {
	testEnv(t)
	// Jev answers only the category questions — productive/quality absent.
	srv, _ := decisionsTestServer(200, `{"model":"m","answers":{"cat_coding":{"type":"noul","noul":0.9}},"usage":{}}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)

	res := &blockResult{Title: "t", Summary: "s", Category: "browsing"}
	j, err := judgeBlock(nil, judgeTestCfg(), res, "neovim", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if j.Category != "coding" {
		t.Fatalf("category = %q", j.Category)
	}
	if j.Productive != nil || j.Quality != nil {
		t.Fatalf("absent answers should be nil: %+v", j)
	}
}

func TestApplyJudgmentFallback(t *testing.T) {
	chatProd := true
	res := &blockResult{Title: "t", Summary: "s", Category: "browsing", Productive: &chatProd}
	// jev has no opinion — chat fields stand
	applyJudgment(res, &blockJudgment{})
	if res.Category != "browsing" || res.Productive == nil || !*res.Productive {
		t.Fatalf("res = %+v", res)
	}
	// jev overrides
	jevProd := false
	conf := 0.9
	applyJudgment(res, &blockJudgment{Category: "coding", Confidence: &conf, Productive: &jevProd})
	if res.Category != "coding" || *res.Productive != false {
		t.Fatalf("res = %+v", res)
	}
}

func TestJudgeRetryable(t *testing.T) {
	testEnv(t)
	start := time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)
	cases := []struct {
		name  string
		score float64
		want  bool
	}{
		{"transient 429", 0.8, true},
		{"terminal 401", 0.1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := decisionsTestServer(200, fmt.Sprintf(
				`{"model":"m","answers":{"retryable":{"type":"noul","noul":%v}},"usage":{}}`, tc.score))
			defer srv.Close()
			old := decisionsURL
			decisionsURL = srv.URL
			defer func() { decisionsURL = old }()
			cfg := Config{OpenRouterAPIKey: "k"}
			if got := judgeRetryable(nil, cfg, start, "api error"); got != tc.want {
				t.Fatalf("judgeRetryable = %v, want %v", got, tc.want)
			}
		})
	}
	// judge unreachable → false (dead, the pre-Jev behavior)
	srv, _ := decisionsTestServer(500, `{}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)
	if judgeRetryable(nil, Config{OpenRouterAPIKey: "k"}, start, "boom") {
		t.Fatal("unreachable judge should return false")
	}
}

func standupBlock(startMin int, title string) Block {
	s := time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local).Add(time.Duration(startMin) * time.Minute)
	return Block{Start: s, End: s.Add(15 * time.Minute), StartTs: s.Unix(),
		StartStr: s.Format("15:04"), EndStr: s.Add(15 * time.Minute).Format("15:04"),
		Title: title, Summary: "did " + title, Category: "coding", Status: "done", App: "neovim"}
}

func TestWorthyBlocks(t *testing.T) {
	testEnv(t)
	blocks := []Block{standupBlock(0, "Deep work"), standupBlock(15, "Trivia"), standupBlock(30, "More work")}
	// jev scores only the second block low
	srv, _ := decisionsTestServer(200, fmt.Sprintf(`{"model":"m","answers":{
		"worthy_%d":{"type":"noul","noul":0.9},
		"worthy_%d":{"type":"noul","noul":0.1},
		"worthy_%d":{"type":"noul","noul":0.8}
	},"usage":{}}`, blocks[0].StartTs, blocks[1].StartTs, blocks[2].StartTs))
	defer srv.Close()
	pointDecisionsAt(t, srv)

	out := worthyBlocks(nil, Config{OpenRouterAPIKey: "k"}, blocks)
	if len(out) != 2 || out[0].Title != "Deep work" || out[1].Title != "More work" {
		t.Fatalf("out = %+v", out)
	}
}

func TestWorthyBlocksAllDroppedFallback(t *testing.T) {
	testEnv(t)
	blocks := []Block{standupBlock(0, "a"), standupBlock(15, "b"), standupBlock(30, "c"), standupBlock(45, "d")}
	srv, _ := decisionsTestServer(200, `{"model":"m","answers":{},"usage":{}}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)
	// empty answers → nothing scored <0.5 → all pass through (no-opinion rule)
	out := worthyBlocks(nil, Config{OpenRouterAPIKey: "k"}, blocks)
	if len(out) != 4 {
		t.Fatalf("out = %d", len(out))
	}
}

func TestWorthyBlocksJudgeDown(t *testing.T) {
	testEnv(t)
	srv, _ := decisionsTestServer(500, `{}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)
	blocks := []Block{standupBlock(0, "a"), standupBlock(15, "b")}
	out := worthyBlocks(nil, Config{OpenRouterAPIKey: "k"}, blocks)
	if len(out) != 2 {
		t.Fatalf("out = %d", len(out))
	}
}

func TestWorthyBlocksTop3Fallback(t *testing.T) {
	testEnv(t)
	blocks := []Block{standupBlock(0, "a"), standupBlock(15, "b"), standupBlock(30, "c"), standupBlock(45, "d"), standupBlock(60, "e")}
	// every block judged unworthy → top-3 by minutes (all equal here → first 3 sorted)
	ans := `{"model":"m","answers":{`
	for i, b := range blocks {
		if i > 0 {
			ans += ","
		}
		ans += fmt.Sprintf(`"worthy_%d":{"type":"noul","noul":0.1}`, b.StartTs)
	}
	ans += `},"usage":{}}`
	srv, _ := decisionsTestServer(200, ans)
	defer srv.Close()
	pointDecisionsAt(t, srv)
	out := worthyBlocks(nil, Config{OpenRouterAPIKey: "k"}, blocks)
	if len(out) != 3 {
		t.Fatalf("out = %d", len(out))
	}
}

func TestJudgeForecast(t *testing.T) {
	testEnv(t)
	fc := Forecast{
		Date: "2026-09-19", Weekday: "Friday", Samples: 4, TotalMinutes: 300,
		Items: []ForecastItem{{Category: "coding", Pct: 60, Minutes: 180}},
	}
	srv, _ := decisionsTestServer(200, `{"model":"m","answers":{"confident":{"type":"noul","noul":0.72}},"usage":{}}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)

	s := judgeForecast(nil, Config{OpenRouterAPIKey: "k"}, fc)
	if s == nil || *s != 0.72 {
		t.Fatalf("score = %v", s)
	}
	// no items → nil without a call
	if judgeForecast(nil, Config{OpenRouterAPIKey: "k"}, Forecast{}) != nil {
		t.Fatal("empty forecast should return nil")
	}
	// judge down → nil
	srv2, _ := decisionsTestServer(500, `{}`)
	defer srv2.Close()
	pointDecisionsAt(t, srv2)
	if judgeForecast(nil, Config{OpenRouterAPIKey: "k"}, fc) != nil {
		t.Fatal("unreachable judge should return nil")
	}
}

func TestSalientShifts(t *testing.T) {
	testEnv(t)
	shifts := []ContextShift{
		{Source: "coding", Target: "comms", Count: 5, Minutes: 40},
		{Source: "coding", Target: "browsing", Count: 2, Minutes: 10},
		{Source: "idle", Target: "coding", Count: 3, Minutes: 20},
	}
	srv, _ := decisionsTestServer(200, `{"model":"m","answers":{
		"real_shift_0":{"type":"noul","noul":0.7},
		"real_shift_1":{"type":"noul","noul":0.1},
		"real_shift_2":{"type":"noul","noul":0.6}
	},"usage":{}}`)
	defer srv.Close()
	pointDecisionsAt(t, srv)

	out := salientShifts(nil, Config{OpenRouterAPIKey: "k"}, shifts)
	if len(out) != 2 || out[0].Target != "comms" || out[1].Target != "coding" {
		t.Fatalf("out = %+v", out)
	}
	// judge down → unfiltered
	srv2, _ := decisionsTestServer(500, `{}`)
	defer srv2.Close()
	pointDecisionsAt(t, srv2)
	if out := salientShifts(nil, Config{OpenRouterAPIKey: "k"}, shifts); len(out) != 3 {
		t.Fatalf("out = %d", len(out))
	}
}
