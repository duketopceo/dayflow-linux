package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uturn(role, text string, ts int64) sessionTurn {
	return sessionTurn{role: role, text: text, unixTs: ts, usableUser: role == "user"}
}

func TestDeriveStatus(t *testing.T) {
	now := time.Now()
	base := AgentSession{Start: now.Add(-time.Hour).Unix()}

	active := base
	active.End = now.Add(-2 * time.Minute).Unix()
	if s := deriveStatus(active, nil, now); s != statusInProgress {
		t.Fatalf("recent end should be inProgress, got %s", s)
	}

	quiet := base
	quiet.End = now.Add(-time.Hour).Unix()
	unanswered := []sessionTurn{
		uturn("assistant", "working", 0), uturn("user", "can you also fix lint?", quiet.End),
	}
	if s := deriveStatus(quiet, unanswered, now); s != statusBlocked {
		t.Fatalf("unanswered user end should be blocked, got %s", s)
	}

	done := []sessionTurn{
		uturn("user", "fix it", quiet.Start), uturn("assistant", "done", quiet.End),
	}
	if s := deriveStatus(quiet, done, now); s != statusCompleted {
		t.Fatalf("assistant-ended quiet session should be completed, got %s", s)
	}

	// Non-usable user turns (injected envelopes) don't count as an ask.
	env := []sessionTurn{
		uturn("user", "do the thing", quiet.Start),
		uturn("assistant", "done", quiet.End-10),
		{role: "user", text: "<environment_context>x</environment_context>", unixTs: quiet.End},
	}
	if s := deriveStatus(quiet, env, now); s != statusCompleted {
		t.Fatalf("envelope tail should stay completed, got %s", s)
	}
}

func TestCondenseTurns(t *testing.T) {
	now := time.Now().Unix()
	turns := []sessionTurn{
		uturn("user", "first ask", now-100),
		uturn("user", "second ask", now-90),                                                    // merges into turn 0
		{role: "user", text: "<environment_context>x</environment_context>", unixTs: now - 80}, // dropped
		{role: "assistant", text: "   ", unixTs: now - 70},                                     // empty → dropped
		uturn("assistant", "did the thing", now-60),
		uturn("user", "thanks", now-50),
	}
	out := condenseTurns(turns)
	if len(out) != 3 {
		t.Fatalf("expected 3 condensed turns, got %+v", out)
	}
	if out[0].Role != "user" || !strings.Contains(out[0].Text, "first ask") || !strings.Contains(out[0].Text, "second ask") {
		t.Fatalf("merge failed: %+v", out[0])
	}
	if out[1].Role != "agent" || out[2].Role != "user" {
		t.Fatalf("roles wrong: %+v", out)
	}
}

func TestCondenseTurnsCapPreservesEdges(t *testing.T) {
	now := time.Now().Unix()
	var turns []sessionTurn
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		text := "turn-" + string(rune('a'+i))
		if i == 0 {
			text = "THE FIRST ASK"
		}
		if i == 19 {
			text = "THE FINAL WORD"
		}
		turns = append(turns, uturn(role, text, now+int64(i)))
	}
	out := condenseTurns(turns)
	if len(out) != maxBriefingTurns {
		t.Fatalf("expected %d turns, got %d", maxBriefingTurns, len(out))
	}
	if out[0].Text != "THE FIRST ASK" {
		t.Fatalf("first turn lost: %+v", out[0])
	}
	if out[len(out)-1].Text != "THE FINAL WORD" {
		t.Fatalf("last turn lost: %+v", out[len(out)-1])
	}
	if !strings.Contains(out[5].Text, "condensed") {
		t.Fatalf("missing middle marker: %+v", out[5])
	}
}

func TestGroupWorkstreams(t *testing.T) {
	old := briefingThread{ID: "t1", Project: "proj-a", Title: "a", Status: statusCompleted, EndedAt: 100}
	newer := briefingThread{ID: "t2", Project: "proj-b", Title: "b", Status: statusCompleted, EndedAt: 200}
	anon := briefingThread{ID: "t3", Title: "c", Status: statusBlocked, EndedAt: 150}
	ws := groupWorkstreams([]briefingThread{old, anon, newer})
	if len(ws) != 3 {
		t.Fatalf("expected 3 workstreams, got %+v", ws)
	}
	if ws[0].Name != "proj-b" { // most recent first
		t.Fatalf("ordering wrong: %s", ws[0].Name)
	}
	var misc *briefingWorkstream
	for i := range ws {
		if ws[i].Name == "Miscellaneous" {
			misc = &ws[i]
		}
	}
	if misc == nil || len(misc.Threads) != 1 || misc.Threads[0].ID != "t3" {
		t.Fatalf("empty project must fold to Miscellaneous: %+v", ws)
	}
	if ws[0].ID != kebab("proj-b") {
		t.Fatalf("id not kebab: %s", ws[0].ID)
	}
}

func TestBriefingFingerprintDrift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	writeJSONL(t, path, []string{
		`{"type":"user","timestamp":"2026-09-15T10:00:00Z","message":{"role":"user","content":"hi"}}`,
	}, time.Date(2026, 9, 15, 10, 0, 0, 0, time.Local))
	srcs := []agentSource{jsonlSource{name: "claude", lineTurn: claudeLineTurn}}
	sess := AgentSession{Source: "claude", File: path}
	fp1 := briefingFingerprint([]AgentSession{sess}, srcs)
	writeJSONL(t, path, []string{
		`{"type":"user","timestamp":"2026-09-15T10:00:00Z","message":{"role":"user","content":"hi"}}`,
		`{"type":"user","timestamp":"2026-09-15T10:01:00Z","message":{"role":"user","content":"more"}}`,
	}, time.Date(2026, 9, 15, 10, 1, 0, 0, time.Local))
	fp2 := briefingFingerprint([]AgentSession{sess}, srcs)
	if fp1 == fp2 {
		t.Fatal("transcript growth must invalidate the briefing fingerprint")
	}
}

func TestApplyBriefingPolish(t *testing.T) {
	b := agentBriefing{Workstreams: []briefingWorkstream{{
		ID: "ws-a", Name: "proj-a", Summary: "2 sessions",
		Threads: []briefingThread{
			{ID: "claude-1", Title: "t", Status: statusCompleted,
				Turns: []briefingTurn{{Role: "user", Text: "write report to /tmp/out.md"}, {Role: "agent", Text: "done"}}},
			{ID: "claude-2", Title: "u", Status: statusBlocked,
				Turns: []briefingTurn{{Role: "user", Text: "?"}}},
		},
	}}}
	polish := `{"workstreams":[{"id":"ws-a","name":"Parser Work","summary":"Fixed the parser","bullets":["Fixed X","Shipped Y"],
	  "threads":[
	    {"id":"claude-1","title":"Parser fix","latest_outcome":"Report ready","review_ready":true,
	     "turn_highlights":[{"turn":0,"kind":"keyDecision"},{"turn":9,"kind":"keyInfo"},{"turn":1,"kind":"bogus"}],
	     "artifact_name":"Report","artifact_path":"/tmp/out.md"},
	    {"id":"claude-2","review_ready":true},
	    {"id":"nope","title":"phantom"}
	  ]}]}`
	applyBriefingPolish(&b, polish)
	ws := b.Workstreams[0]
	if ws.Name != "Parser Work" || ws.Summary != "Fixed the parser" || len(ws.Bullets) != 2 {
		t.Fatalf("workstream prose not applied: %+v", ws)
	}
	th := ws.Threads[0]
	if th.Title != "Parser fix" || th.LatestOutcome != "Report ready" || th.Status != statusReviewReady {
		t.Fatalf("thread prose/status wrong: %+v", th)
	}
	if th.Turns[0].Highlight != "keyDecision" {
		t.Fatalf("highlight not applied: %+v", th.Turns)
	}
	if th.Turns[1].Highlight != "" {
		t.Fatal("bogus highlight kind must be rejected")
	}
	if th.Turns[1].ArtifactPath != "/tmp/out.md" {
		t.Fatalf("grounded artifact not applied: %+v", th.Turns[1])
	}
	// blocked thread: review_ready can't downgrade/upgrade it (KTD3).
	if ws.Threads[1].Status != statusBlocked {
		t.Fatal("model must not touch a blocked status")
	}
	// Phantom thread id ignored silently.
	if len(ws.Threads) != 2 {
		t.Fatal("phantom thread added")
	}
}

func TestApplyBriefingPolishArtifactGuard(t *testing.T) {
	b := agentBriefing{Workstreams: []briefingWorkstream{{
		ID: "w", Name: "w", Threads: []briefingThread{
			{ID: "t", Status: statusCompleted,
				Turns: []briefingTurn{{Role: "agent", Text: "all done"}}},
		},
	}}}
	// Path not present in any turn — fabrication must be dropped.
	applyBriefingPolish(&b, `{"workstreams":[{"id":"w","threads":[{"id":"t",
	  "artifact_name":"X","artifact_path":"/etc/invented"}]}]}`)
	if b.Workstreams[0].Threads[0].Turns[0].ArtifactPath != "" {
		t.Fatal("invented artifact path must be dropped")
	}
	// Malformed JSON sinks nothing.
	applyBriefingPolish(&b, `{"workstreams":[{broken`)
	if b.Workstreams[0].Threads[0].Status != statusCompleted {
		t.Fatal("malformed polish must not mutate")
	}
}

func TestBriefingEndToEnd(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 20, 0, 0, 0, 0, time.Local)
	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_1", title: "Adapter work", dir: "/home/x/dayflow",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "add the opencode adapter",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
			{id: "m2", typ: "assistant", seq: 2, text: "merged it",
				created: ms(day.Add(10*time.Hour + time.Minute)), updated: ms(day.Add(10*time.Hour + time.Minute))},
		},
	})

	// Probe pass with recaps off learns the deterministic ids the model
	// response must echo back.
	cfgOff := cfg
	cfgOff.AgentRecaps = false
	probe := agentBriefingFor(db, cfgOff, day, false)
	if len(probe.Workstreams) != 1 || len(probe.Workstreams[0].Threads) != 1 {
		t.Fatalf("bad probe briefing: %+v", probe.Workstreams)
	}
	wsID := probe.Workstreams[0].ID
	thID := probe.Workstreams[0].Threads[0].ID

	stubOpenRouter(t, `{"workstreams":[{"id":"`+wsID+`","name":"Dayflow","summary":"Shipped the adapter",
	  "threads":[{"id":"`+thID+`","title":"Adapter work","latest_outcome":"Merged"}]}]}`)

	// Recaps on: a cached fallback briefing upgrades on next view (no
	// --refresh needed).
	b := agentBriefingFor(db, cfg, day, false)
	ws := b.Workstreams[0]
	if ws.Name != "Dayflow" { // model-renamed from "dayflow"
		t.Fatalf("model polish not applied: %+v", ws)
	}
	if b.Mode != "model" {
		t.Fatalf("mode=%s, want model", b.Mode)
	}
	th := ws.Threads[0]
	if th.Status != statusCompleted || len(th.Turns) != 2 || th.LatestOutcome != "Merged" {
		t.Fatalf("bad thread: %+v", th)
	}

	// Cached model payload serves again without another provider call.
	b2 := agentBriefingFor(db, cfg, day, false)
	if b2.Workstreams[0].Name != "Dayflow" || b2.Mode != "model" {
		t.Fatalf("cached briefing not served: %+v", b2)
	}
	// --refresh bypasses cache and re-polishes.
	b3 := agentBriefingFor(db, cfg, day, true)
	if b3.Workstreams[0].Name != "Dayflow" {
		t.Fatalf("refresh lost polish: %+v", b3)
	}
}

func TestBriefingFallbackMode(t *testing.T) {
	cfg := testEnv(t)
	cfg.AgentRecaps = false // consent off → no provider call, fallback prose
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 20, 0, 0, 0, 0, time.Local)
	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_1", title: "Adapter work", dir: "/home/x/dayflow",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "add it",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
			{id: "m2", typ: "assistant", seq: 2, text: "did it",
				created: ms(day.Add(10*time.Hour + time.Minute)), updated: ms(day.Add(10*time.Hour + time.Minute))},
		},
	})

	b := agentBriefingFor(db, cfg, day, false)
	if b.Mode != "fallback" {
		t.Fatalf("mode=%s, want fallback", b.Mode)
	}
	ws := b.Workstreams[0]
	if ws.Name != "dayflow" || ws.Summary != "1 session" {
		t.Fatalf("deterministic workstream missing: %+v", ws)
	}
	if ws.Threads[0].LatestOutcome != "did it" {
		t.Fatalf("deterministic outcome missing: %+v", ws.Threads[0])
	}
	// No LLM rows may be written in fallback mode.
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM llm_calls WHERE task='agent_briefing'`).Scan(&n)
	if n != 0 {
		t.Fatal("fallback mode must not call the provider")
	}
}

func TestBriefingModelPayloadScrubbed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	b := agentBriefing{Day: "2026-09-20", Workstreams: []briefingWorkstream{{
		ID: "w", Name: "proj", Threads: []briefingThread{{
			ID: "t", Source: "claude", Status: statusCompleted,
			Title: "work in " + home + "/secret",
			Turns: []briefingTurn{{Role: "user", Text: "key is sk-abcdef1234567890 in " + home}},
		}},
	}}}
	p := briefingModelPayload(&b)
	if strings.Contains(p, home) {
		t.Fatalf("home path leaked: %s", p)
	}
	if strings.Contains(p, "sk-abcdef") {
		t.Fatalf("secret leaked: %s", p)
	}
	if len(p) > maxBriefingPayload {
		t.Fatal("payload exceeds cap")
	}
	var js map[string]any
	if json.Unmarshal([]byte(p), &js) != nil {
		t.Fatalf("payload must be valid JSON: %v", p)
	}
}
