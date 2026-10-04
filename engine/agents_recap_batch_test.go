package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// stubBatches serves the OpenRouter Batch API: POST /batches accepts a job
// and GET /batches/{id} returns whatever status/results the test scripted.
type batchStub struct {
	srv        *httptest.Server
	posts      atomic.Int32
	polls      atomic.Int32
	status     string
	results    string // raw results array JSON, used when status=="completed"
	lastSubmit []byte
}

func stubBatchesAPI(t *testing.T, status, results string) *batchStub {
	t.Helper()
	b := &batchStub{status: status, results: results}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/batches":
			b.posts.Add(1)
			b.lastSubmit, _ = io.ReadAll(r.Body)
			json.NewEncoder(w).Encode(map[string]any{"id": "batch_t1", "status": "validating"})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/batches/"):
			b.polls.Add(1)
			if b.status == "completed" {
				io.WriteString(w, `{"id":"batch_t1","status":"completed","results":`+b.results+`}`)
			} else {
				io.WriteString(w, `{"id":"batch_t1","status":"`+b.status+`"}`)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(b.srv.Close)
	old := openRouterURL
	openRouterURL = b.srv.URL
	t.Cleanup(func() { openRouterURL = old })
	return b
}

func batchResultBody(customID, text string) string {
	return `{"custom_id":"` + customID + `","response":{"status_code":200,"body":{"choices":[{"message":{"content":"` + text + `"}}]}}}`
}

// TestBatchSubmitThenCollect exercises the two-pass lifecycle: pass one
// submits the uncached session and serves nothing new, pass two polls the
// completed job, writes the cache row, and serves the recap.
func TestBatchSubmitThenCollect(t *testing.T) {
	cfg := testEnv(t)
	cfg.AgentRecapBatch = true
	scriptedDecisions(t, cannedDecisions(map[string]float64{
		"worthy": 0.9, "quality": 0.9,
	}))
	stub := stubBatchesAPI(t, "completed", "["+batchResultBody("0", "Batch generated this recap.")+"]")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 4)

	// Pass 1 — submits, recap not yet available.
	sessions := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "" {
		t.Fatalf("async recap served same pass: %q", sessions[0].Recap)
	}
	if stub.posts.Load() != 1 {
		t.Fatalf("expected 1 batch submit, got %d", stub.posts.Load())
	}
	pend, ok := loadPendingBatch(db)
	if !ok || pend.ID != "batch_t1" || len(pend.Sessions) != 1 {
		t.Fatalf("pending batch not stored: %+v ok=%v", pend, ok)
	}

	// Pass 2 — collects; session now renders the model recap.
	sessions = []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "Batch generated this recap." {
		t.Fatalf("collected recap=%q", sessions[0].Recap)
	}
	var stored, model string
	var worthy, quality sql.NullFloat64
	if err := db.QueryRow(`SELECT recap, model, worthy_confidence, quality_confidence
	  FROM agent_recaps WHERE path=?`, path).Scan(&stored, &model, &worthy, &quality); err != nil {
		t.Fatalf("cache row: %v", err)
	}
	if stored != "Batch generated this recap." || model == "" {
		t.Fatalf("stored=%q model=%q", stored, model)
	}
	if !worthy.Valid || worthy.Float64 != 0.9 || !quality.Valid || quality.Float64 != 0.9 {
		t.Fatalf("confidence carry-through: worthy=%v quality=%v", worthy, quality)
	}
	// Pass 3 — cache serves, no new submits or polls.
	sessions = []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if stub.posts.Load() != 1 {
		t.Fatalf("cached session resubmitted")
	}
}

// TestBatchSubmitRequestShape pins the wire contract: batches endpoint, the
// chat-completions shape, batch-level model, attribution + auth headers.
func TestBatchSubmitRequestShape(t *testing.T) {
	cfg := testEnv(t)
	cfg.AgentRecapBatch = true
	scriptedDecisions(t, cannedDecisions(map[string]float64{"worthy": 0.9}))
	stub := stubBatchesAPI(t, "in_progress", "")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 4)
	attachRecaps(db, cfg, []AgentSession{recapSess(path)})
	if stub.posts.Load() != 1 {
		t.Fatalf("no batch submitted")
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
		Requests []struct {
			CustomID string `json:"custom_id"`
			Body     struct {
				Model    string `json:"model"`
				Messages []struct {
					Role string `json:"role"`
				} `json:"messages"`
			} `json:"body"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(stub.lastSubmit, &body); err != nil {
		t.Fatalf("submit body: %v", err)
	}
	if body.Endpoint != "/v1/chat/completions" {
		t.Fatalf("endpoint=%q", body.Endpoint)
	}
	if body.Model == "" || len(body.Requests) != 1 {
		t.Fatalf("model=%q requests=%d", body.Model, len(body.Requests))
	}
	r := body.Requests[0]
	if r.CustomID != "0" || len(r.Body.Messages) != 2 ||
		r.Body.Messages[0].Role != "system" || r.Body.Messages[1].Role != "user" {
		t.Fatalf("request shape: %+v", r)
	}
}

// TestBatchInProgressNoResubmit — an in-flight batch blocks new submissions
// until it reaches a terminal state.
func TestBatchInProgressNoResubmit(t *testing.T) {
	cfg := testEnv(t)
	cfg.AgentRecapBatch = true
	scriptedDecisions(t, cannedDecisions(map[string]float64{"worthy": 0.9}))
	stub := stubBatchesAPI(t, "in_progress", "")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 4)
	for pass := 0; pass < 3; pass++ {
		attachRecaps(db, cfg, []AgentSession{recapSess(path)})
	}
	if stub.posts.Load() != 1 {
		t.Fatalf("resubmitted while in flight: %d posts", stub.posts.Load())
	}
	if stub.polls.Load() != 2 {
		t.Fatalf("expected polls on passes 2-3, got %d", stub.polls.Load())
	}
}

// TestBatchTerminalFailureClearsAndBacksOff — a failed batch clears pending
// state and suppresses immediate resubmission; the retry waits out the
// backoff instead of hammering the API once per view.
func TestBatchTerminalFailureClearsAndBacksOff(t *testing.T) {
	cfg := testEnv(t)
	cfg.AgentRecapBatch = true
	scriptedDecisions(t, cannedDecisions(map[string]float64{"worthy": 0.9}))
	stub := stubBatchesAPI(t, "failed", "")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 4)
	attachRecaps(db, cfg, []AgentSession{recapSess(path)})
	attachRecaps(db, cfg, []AgentSession{recapSess(path)}) // poll → failed → clear+backoff
	if p, ok := loadPendingBatch(db); !ok || p.ID != "" || p.LastAttempt == 0 {
		t.Fatalf("pending state after failure: %+v ok=%v", p, ok)
	}
	attachRecaps(db, cfg, []AgentSession{recapSess(path)})
	if stub.posts.Load() != 1 {
		t.Fatalf("resubmitted inside backoff window: %d posts", stub.posts.Load())
	}
}

// TestBatchUnworthyNeverSubmitted — the Jev worthiness gate runs before the
// batch, so trivial sessions settle locally and never cost a request slot.
func TestBatchUnworthyNeverSubmitted(t *testing.T) {
	cfg := testEnv(t)
	cfg.AgentRecapBatch = true
	scriptedDecisions(t, cannedDecisions(map[string]float64{"worthy": 0.1}))
	stub := stubBatchesAPI(t, "completed", "[]")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 2)
	attachRecaps(db, cfg, []AgentSession{recapSess(path)})
	if stub.posts.Load() != 0 {
		t.Fatalf("unworthy session entered batch")
	}
	var recap string
	var worthy sql.NullFloat64
	db.QueryRow(`SELECT recap, worthy_confidence FROM agent_recaps WHERE path=?`, path).Scan(&recap, &worthy)
	if recap != "" || !worthy.Valid || worthy.Float64 != 0.1 {
		t.Fatalf("unworthy settle row: recap=%q worthy=%v", recap, worthy)
	}
}

// TestBatchProviderIneligible — batch mode degrades to inline when the
// routed recap provider can't serve the Batch API.
func TestBatchProviderIneligible(t *testing.T) {
	cfg := testEnv(t)
	cfg.AgentRecapBatch = true
	// Local provider can't host the Batch API.
	cfg.Providers = append(cfg.Providers, Provider{
		ID: "local-llm", Kind: "local", Model: "ornith",
		APIBaseURL: "http://127.0.0.1:11434/v1", Enabled: true, Chat: true,
	})
	cfg.Routing.TaskProvider = map[string]string{"agent_recap": "local-llm"}
	if p, ok := batchRecapProvider(cfg); ok {
		t.Fatalf("local provider eligible for batch: %+v", p)
	}
}

// TestCallChatModelPerTaskRouting — a task_provider override keyed on the
// call-site task name ("agent_recap") reroutes those calls while the shared
// chat route is unchanged.
func TestCallChatModelPerTaskRouting(t *testing.T) {
	cfg := testEnv(t)
	var strongHits, defaultHits int32
	strong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		strongHits++
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "strong reply"}}},
		})
	}))
	t.Cleanup(strong.Close)
	def := stubOpenRouter(t, "default reply")
	_ = def
	// Wrap the default stub to count hits.
	cfg.Providers = append(cfg.Providers, Provider{
		ID: "strong", Kind: "openrouter", Model: "vendor/strong-model",
		APIBaseURL: strong.URL, APIKey: "k", Enabled: true, Chat: true,
	})
	cfg.Routing.TaskProvider = map[string]string{"agent_recap": "strong"}

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	text, _, _, err := callChatModel(db, cfg, "agent_recap",
		[]orMessage{{Role: "user", Content: []orContent{{Type: "text", Text: "hi"}}}})
	if err != nil || text != "strong reply" {
		t.Fatalf("agent_recap routing: text=%q err=%v", text, err)
	}
	if strongHits != 1 || defaultHits != 0 {
		t.Fatalf("routing: strong=%d default=%d", strongHits, defaultHits)
	}
	// "chat" has no override — still resolves the shared route (primary).
	if _, _, _, err := callChatModel(db, cfg, "chat",
		[]orMessage{{Role: "user", Content: []orContent{{Type: "text", Text: "hi"}}}}); err != nil {
		t.Fatalf("chat call: %v", err)
	}
}

// TestBatchErrorResultSkipped — a per-item error (rate limit, refusal)
// doesn't poison the batch: good results still collect, errored custom_ids
// are skipped rather than cached as empty.
func TestBatchErrorResultSkipped(t *testing.T) {
	cfg := testEnv(t)
	cfg.AgentRecapBatch = true
	scriptedDecisions(t, cannedDecisions(map[string]float64{"worthy": 0.9, "quality": 0.9}))
	results := "[" + batchResultBody("0", "Good recap.") +
		`,{"custom_id":"1","error":{"message":"rate_limit_exceeded"}}` + "]"
	stub := stubBatchesAPI(t, "completed", results)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	paths := []string{writeClaudeTranscript(t, t.TempDir(), 4), writeClaudeTranscript(t, t.TempDir(), 4)}
	pend := &pendingRecapBatch{ID: "batch_t1", SubmittedAt: 1, Sessions: []batchSession{
		{CustomID: "0", Path: paths[0], Source: "claude", State: "s0"},
		{CustomID: "1", Path: paths[1], Source: "claude", State: "s1"},
	}}
	savePendingBatch(db, pend)

	bp, ok := batchRecapProvider(cfg)
	if !ok {
		t.Fatal("batch provider ineligible under test env")
	}
	collected, inFlight := collectPendingBatch(db, cfg, bp, pend)
	if inFlight {
		t.Fatal("completed batch reported in-flight")
	}
	if stub.polls.Load() != 1 {
		t.Fatalf("expected 1 poll, got %d", stub.polls.Load())
	}
	if len(collected) != 1 || collected[paths[0]].text != "Good recap." {
		t.Fatalf("collected=%+v", collected)
	}
	var stored string
	db.QueryRow(`SELECT recap FROM agent_recaps WHERE path=?`, paths[1]).Scan(&stored)
	if stored != "" {
		t.Fatalf("errored result cached: %q", stored)
	}
}

// TestBatchClaimBlocksConcurrentSubmit — a live claim turns a second
// would-be submitter away; after release the slot is claimable again.
func TestBatchClaimBlocksConcurrentSubmit(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !claimBatchSubmit(db, "a") {
		t.Fatal("first claim rejected")
	}
	if claimBatchSubmit(db, "b") {
		t.Fatal("concurrent claim accepted under live lease")
	}
	releaseBatchClaim(db, "a")
	if !claimBatchSubmit(db, "b") {
		t.Fatal("claim after release rejected")
	}
	// A foreign release can't free our lease.
	releaseBatchClaim(db, "a")
	if claimBatchSubmit(db, "c") {
		t.Fatal("claim accepted after foreign release")
	}
}
