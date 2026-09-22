package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func f64ptr(v float64) *float64 { return &v }

// writeClaudeTranscript builds a fake Claude Code JSONL transcript with n
// user/assistant message pairs.
func writeClaudeTranscript(t *testing.T, dir string, n int) string {
	t.Helper()
	path := filepath.Join(dir, "sess-1.jsonl")
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"type":"user","timestamp":"2026-09-20T10:%02d:00Z",`+
			`"message":{"role":"user","content":"implement feature %d"}`, i, i)
		b.WriteString("}\n")
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-09-20T10:%02d:30Z",`+
			`"message":{"role":"assistant","content":[{"type":"text","text":"done with %d"}]}`, i, i)
		b.WriteString("}\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// decisionsResponse builds a canned decisions body; scores maps question → noul.
func cannedDecisions(scores map[string]float64) string {
	answers := make([]string, 0, len(scores))
	for k, v := range scores {
		answers = append(answers, fmt.Sprintf(`%q: {"type":"noul","noul":%f}`, k, v))
	}
	return `{"model":"typesafe/jev-1.13","answers":{` + strings.Join(answers, ",") +
		`},"usage":{"input_tokens":10,"output_tokens":5}}`
}

// scriptedDecisions serves a sequence of canned responses (last repeats on
// overrun) and records request bodies.
func scriptedDecisions(t *testing.T, bodies ...string) *[]string {
	t.Helper()
	var reqs []string
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		reqs = append(reqs, string(buf))
		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 0 {
			w.WriteHeader(500)
			return
		}
		if i >= len(bodies) {
			i = len(bodies) - 1
		}
		fmt.Fprint(w, bodies[i])
		i++
	}))
	t.Cleanup(srv.Close)
	pointDecisionsAt(t, srv)
	return &reqs
}

func recapSess(path string) AgentSession {
	st, _ := os.Stat(path)
	return AgentSession{
		Source: "claude", Project: "dayflow", Start: st.ModTime().Unix() - 600,
		End: st.ModTime().Unix(), Messages: 8, Title: "implement feature 0", File: path,
	}
}

func TestRecapHappyPathAndCache(t *testing.T) {
	cfg := testEnv(t)
	reqs := scriptedDecisions(t, cannedDecisions(map[string]float64{
		"worthy": 0.9, "quality": 0.9,
	}))
	stubOpenRouter(t, "Wired the forecast pane to weekly trends.")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 4)
	sessions := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "Wired the forecast pane to weekly trends." {
		t.Fatalf("recap=%q", sessions[0].Recap)
	}
	if sessions[0].RecapConfidence == nil || *sessions[0].RecapConfidence != 0.9 {
		t.Fatalf("confidence=%v", sessions[0].RecapConfidence)
	}
	// Cache hit: zero new judge calls, stored value actually served.
	before := len(*reqs)
	served := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, served)
	if len(*reqs) != before {
		t.Fatalf("cache miss: %d new judge calls", len(*reqs)-before)
	}
	if served[0].Recap != "Wired the forecast pane to weekly trends." {
		t.Fatalf("cached recap not served: %q", served[0].Recap)
	}
	var stored string
	if err := db.QueryRow(`SELECT recap FROM agent_recaps WHERE path=?`, path).Scan(&stored); err != nil || stored == "" {
		t.Fatalf("stored=%q err=%v", stored, err)
	}
}

func TestRecapWorthyGate(t *testing.T) {
	cfg := testEnv(t)
	reqs := scriptedDecisions(t, cannedDecisions(map[string]float64{"worthy": 0.1}))
	chat := stubOpenRouter(t, "should never be used")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 2)
	sessions := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "" {
		t.Fatalf("unworthy session got recap %q", sessions[0].Recap)
	}
	// Unworthy verdict is persisted — second attach re-judges nothing.
	before := len(*reqs)
	attachRecaps(db, cfg, sessions[:])
	if len(*reqs) != before {
		t.Fatalf("unworthy session re-judged")
	}
	_ = chat
}

func TestRecapFingerprintInvalidation(t *testing.T) {
	cfg := testEnv(t)
	// First generation: worthy .9, quality .9. Second (after mtime bump):
	// worthy .9, quality .9 — same body; the chat stub reply differs only if
	// we swap it, so assert on judge-call count + updated mtime instead.
	reqs := scriptedDecisions(t, cannedDecisions(map[string]float64{
		"worthy": 0.9, "quality": 0.9,
	}))
	stubOpenRouter(t, "Regenerated recap.")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 3)
	sessions := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	firstCalls := len(*reqs)

	// Mutate the transcript: append + force a new mtime.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"user","message":{"role":"user","content":"more work"}}` + "\n")
	f.Close()
	newMtime := time.Now().Add(time.Hour)
	os.Chtimes(path, newMtime, newMtime)

	attachRecaps(db, cfg, sessions[:])
	if len(*reqs) == firstCalls {
		t.Fatal("changed transcript did not invalidate the cache")
	}
	var mtime int64
	if err := db.QueryRow(`SELECT file_mtime FROM agent_recaps WHERE path=?`, path).Scan(&mtime); err != nil {
		t.Fatal(err)
	}
	if mtime != newMtime.Unix() {
		t.Fatalf("stored mtime %d, want %d", mtime, newMtime.Unix())
	}
}

func TestRecapQualityRegen(t *testing.T) {
	cfg := testEnv(t)
	// worthy .9 → quality .3 (below .55 threshold) → quality .8 on regen.
	scriptedDecisions(t,
		cannedDecisions(map[string]float64{"worthy": 0.9}),
		cannedDecisions(map[string]float64{"quality": 0.3}),
		cannedDecisions(map[string]float64{"quality": 0.8}),
	)
	stubOpenRouter(t, "Concrete recap.")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 3)
	sessions := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "Concrete recap." {
		t.Fatalf("recap=%q", sessions[0].Recap)
	}
	if sessions[0].RecapConfidence == nil || *sessions[0].RecapConfidence != 0.8 {
		t.Fatalf("quality=%v, want .8 after regen", sessions[0].RecapConfidence)
	}
}

func TestRecapJevDisabledStillGenerates(t *testing.T) {
	cfg := testEnv(t)
	cfg.JevClassification = false
	stubOpenRouter(t, "Recap without judges.")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 3)
	sessions := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "Recap without judges." {
		t.Fatalf("recap=%q", sessions[0].Recap)
	}
	if sessions[0].RecapConfidence != nil {
		t.Fatal("confidence should be nil with Jev off")
	}
}

func TestRecapJevDownStillGenerates(t *testing.T) {
	cfg := testEnv(t)
	// decisionsURL points at the dead testEnv endpoint — decide() fails,
	// generation proceeds without judgment (documented degrade).
	stubOpenRouter(t, "Recap despite dead Jev.")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 3)
	sessions := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "Recap despite dead Jev." {
		t.Fatalf("recap=%q", sessions[0].Recap)
	}
	if sessions[0].RecapConfidence != nil {
		t.Fatal("confidence should be nil when Jev is unreachable")
	}
}

func TestRecapDisableJudgesServesCacheOnly(t *testing.T) {
	cfg := testEnv(t)
	stubOpenRouter(t, "Generated.")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 3)
	sess := recapSess(path)
	fp, _ := fingerprint(path)
	putRecap(db, sess, fp, "Cached recap.", "m", nil, nil)

	// DisableJudges: cached row serves, no new generation.
	cfg.DisableJudges = true
	sessions := []AgentSession{sess}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "Cached recap." {
		t.Fatalf("recap=%q", sessions[0].Recap)
	}

	// Uncached session: nothing generated under DisableJudges.
	path2 := writeClaudeTranscript(t, t.TempDir(), 3)
	sessions2 := []AgentSession{recapSess(path2)}
	attachRecaps(db, cfg, sessions2)
	if sessions2[0].Recap != "" {
		t.Fatalf("DisableJudges generated %q", sessions2[0].Recap)
	}
}

func TestAttachRecapsCapAndOrder(t *testing.T) {
	cfg := testEnv(t)
	// One canned body serves every decide call — "worthy" and "quality"
	// questions each read their own key. The server repeats the last body.
	reqs := scriptedDecisions(t, cannedDecisions(map[string]float64{
		"worthy": 0.9, "quality": 0.9,
	}))
	stubOpenRouter(t, "Session recap.")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 10 sessions, sizes 10..100 msgs — the cap keeps the 8 largest.
	dir := t.TempDir()
	var sessions []AgentSession
	for n := 1; n <= 10; n++ {
		sub := filepath.Join(dir, fmt.Sprint(n))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		p := writeClaudeTranscript(t, sub, 3)
		s := recapSess(p)
		s.Messages = n * 10
		sessions = append(sessions, s)
	}
	attachRecaps(db, cfg, sessions)

	withRecap := 0
	for _, s := range sessions {
		if s.Recap != "" {
			withRecap++
			if s.Messages < 30 {
				t.Fatalf("smallest sessions should be skipped, got recap on %d msgs", s.Messages)
			}
		}
	}
	if withRecap != maxNewRecaps {
		t.Fatalf("generated %d recaps, want %d", withRecap, maxNewRecaps)
	}
	// 8 sessions × 2 decide calls (worthy + quality) — no chat calls here.
	if len(*reqs) != 16 {
		t.Fatalf("decide calls=%d, want 16", len(*reqs))
	}
}

func TestMCPServesCachedRecapOnly(t *testing.T) {
	cfg := testEnv(t)
	reqs := scriptedDecisions(t, cannedDecisions(map[string]float64{
		"worthy": 0.9, "quality": 0.9,
	}))
	stubOpenRouter(t, "must not be called")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dir := t.TempDir()
	t.Setenv("DAYFLOW_CLAUDE_DIR", dir)
	t.Setenv("DAYFLOW_CODEX_DIR", filepath.Join(dir, "none"))
	// Session transcript whose message timestamp lands today.
	path := writeClaudeTranscript(t, dir, 3)

	// Seed the cache as if a previous UI view generated it.
	fp, ok := fingerprint(path)
	if !ok {
		t.Fatal("fingerprint failed")
	}
	sess := recapSess(path)
	if err := putRecap(db, sess, fp, "Cached recap text.", "test-model", f64ptr(0.9), f64ptr(0.8)); err != nil {
		t.Fatal(err)
	}

	// Fixture timestamps are 2026-09-20 — request that day, not "today".
	res, err := mcpCall(db, cfg, true, "get_agent_sessions", map[string]any{
		"date": "2026-09-20",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*reqs) != 0 {
		t.Fatalf("MCP made %d judge calls — cache-only contract broken", len(*reqs))
	}
	body, _ := json.Marshal(res)
	if !strings.Contains(string(body), "Cached recap text.") {
		t.Fatalf("cached recap not served through MCP: %s", body)
	}
}

func TestJevStateRedactsSessionFields(t *testing.T) {
	cfg := testEnv(t)
	reqs := scriptedDecisions(t, cannedDecisions(map[string]float64{"worthy": 0.9}))
	stubOpenRouter(t, "whatever")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	home, _ := os.UserHomeDir()
	secret := "sk-" + strings.Repeat("a", 20)
	path := writeClaudeTranscript(t, t.TempDir(), 3)
	sess := recapSess(path)
	sess.Title = "rotate " + secret
	sess.Project = home + "/work/secretproj"
	attachRecaps(db, cfg, []AgentSession{sess})

	for _, r := range *reqs {
		if strings.Contains(r, secret) {
			t.Fatalf("secret reached judge state: %s", r)
		}
		if home != "" && strings.Contains(r, home) {
			t.Fatalf("home path reached judge state: %s", r)
		}
	}
}

func TestSessionExcerptBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.jsonl")
	big := strings.Repeat("x", 5000)
	os.WriteFile(path, []byte(
		`{"type":"user","message":{"role":"user","content":"`+big+`"}}`+"\n"+
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"`+big+`"}]}}`+"\n"),
		0o644)
	ex := sessionExcerpt(path, "claude")
	if len(ex) > 2000 {
		t.Fatalf("excerpt %d bytes, want ≤2000", len(ex))
	}
	if ex == "" {
		t.Fatal("empty excerpt from populated transcript")
	}
	// The per-message 600 bound is what keeps later sections alive — the
	// outer truncate alone would let the first field eat the whole excerpt.
	if !strings.Contains(ex, "last assistant reply") {
		t.Fatalf("per-message bound lost — later section truncated away: %q", ex[:120])
	}
}

func TestSessionExcerptCodex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.jsonl")
	os.WriteFile(path, []byte(
		`{"timestamp":"2026-09-20T10:00:00Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the bug"}]}}`+"\n"+
			`{"timestamp":"2026-09-20T10:01:00Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"shipped the fix"}]}}`+"\n"),
		0o644)
	ex := sessionExcerpt(path, "codex")
	if !strings.Contains(ex, "fix the bug") {
		t.Fatalf("codex excerpt=%q", ex)
	}
	if !strings.Contains(ex, "last assistant reply: shipped the fix") {
		t.Fatalf("codex output_text not extracted: %q", ex)
	}
}

func TestSessionExcerptSkipsEnvelopes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.jsonl")
	os.WriteFile(path, []byte(
		`{"type":"user","message":{"role":"user","content":"<environment_context>cwd=/x</environment_context>"}}`+"\n"+
			`{"type":"user","message":{"role":"user","content":"real first prompt"}}`+"\n"),
		0o644)
	ex := sessionExcerpt(path, "claude")
	if !strings.Contains(ex, "first user message: real first prompt") {
		t.Fatalf("envelope not skipped: %q", ex)
	}
	if strings.Contains(ex, "environment_context") {
		t.Fatalf("envelope text in excerpt: %q", ex)
	}
}

func TestSessionExcerptRedacts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	home, _ := os.UserHomeDir()
	os.WriteFile(path, []byte(
		`{"type":"user","message":{"role":"user","content":"key is sk-`+
			strings.Repeat("a", 20)+` and file `+home+`/secret.txt"}}`+"\n"),
		0o644)
	ex := sessionExcerpt(path, "claude")
	if strings.Contains(ex, "sk-"+strings.Repeat("a", 20)) {
		t.Fatalf("secret not redacted: %q", ex)
	}
	if home != "" && strings.Contains(ex, home) {
		t.Fatalf("home path not scrubbed: %q", ex)
	}
}

func TestRecapFailureNotCached(t *testing.T) {
	cfg := testEnv(t)
	scriptedDecisions(t, cannedDecisions(map[string]float64{"worthy": 0.9}))
	// Chat endpoint dead — generation fails; the failure must NOT persist as
	// an unworthy verdict.
	old := openRouterURL
	openRouterURL = "http://127.0.0.1:1/"
	t.Cleanup(func() { openRouterURL = old })
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := writeClaudeTranscript(t, t.TempDir(), 3)
	sessions := []AgentSession{recapSess(path)}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "" {
		t.Fatal("recap generated with dead chat endpoint")
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM agent_recaps WHERE path=?`, path).Scan(&n)
	if n != 0 {
		t.Fatal("transient failure was persisted — would suppress retries forever")
	}
}

func TestRecapMissingFile(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := []AgentSession{{Source: "claude", File: filepath.Join(t.TempDir(), "gone.jsonl")}}
	attachRecaps(db, cfg, sessions) // must not panic or generate
	if sessions[0].Recap != "" {
		t.Fatal("recap for missing file")
	}
}

func TestRecapRowRoundtrip(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sess := AgentSession{Source: "claude", File: "/tmp/x.jsonl", Start: 5}
	w, q := 0.9, 0.7
	putRecap(db, sess, recapFingerprint{Mtime: 10, Size: 20}, "r", "m", &w, &q)
	recap, quality, fresh := cachedRecap(db, sess.File, recapFingerprint{Mtime: 10, Size: 20})
	if !fresh || recap != "r" || quality == nil || *quality != 0.7 {
		t.Fatalf("roundtrip: fresh=%v recap=%q q=%v", fresh, recap, quality)
	}
	// Stale fingerprint → not fresh.
	_, _, fresh = cachedRecap(db, sess.File, recapFingerprint{Mtime: 11, Size: 20})
	if fresh {
		t.Fatal("stale fingerprint served")
	}
}
