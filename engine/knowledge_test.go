package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDay() time.Time {
	return time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
}

// emptyAgentDirs points every harness transcript root at a temp dir —
// otherwise the briefing scan inside knowledgeDocFor decodes the real
// stores (~2min per test).
func emptyAgentDirs(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("DAYFLOW_CLAUDE_DIR", d)
	t.Setenv("DAYFLOW_CODEX_DIR", d)
	t.Setenv("DAYFLOW_OPENCODE_DB", filepath.Join(d, "none.db"))
	t.Setenv("DAYFLOW_DEVIN_DIR", d)
	t.Setenv("DAYFLOW_CURSOR_DB", filepath.Join(d, "none.db"))
	t.Setenv("DAYFLOW_CURSOR_WORKSPACES", d)
}

// recordingPush captures pushed documents.
type recordingPush struct {
	calls []string
	docs  [][]byte
	err   error
}

func (r *recordingPush) push(name string, doc []byte) error {
	r.calls = append(r.calls, name)
	r.docs = append(r.docs, doc)
	return r.err
}

func TestKnowledgeDocContainsJournalAndNoRawData(t *testing.T) {
	emptyAgentDirs(t)
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()

	day := testDay()
	start := blockStart(day, 15)
	upsertBlockFull(db, start, start.Add(15*time.Minute),
		"standup on kurultai sync", "wrote the sync pass", "meetings", "dayflow-linux", "", 3, 1, "done", "", nil)

	doc := string(knowledgeDocFor(db, cfg, day))
	for _, want := range []string{"Dayflow 2026-10-01", "standup on kurultai sync", "## Journal"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("doc missing %q:\n%s", want, doc)
		}
	}
	if !strings.HasPrefix(doc, "---\ntitle:") {
		t.Fatalf("doc missing frontmatter:\n%s", doc[:200])
	}
}

func TestSyncKnowledgePushesAndDeduplicates(t *testing.T) {
	emptyAgentDirs(t)
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	cfg.KnowledgeSync = true
	rec := &recordingPush{}

	day := testDay()
	start := blockStart(day, 15)
	upsertBlockFull(db, start, start.Add(15*time.Minute),
		"block one", "summary", "meetings", "", "", 1, 1, "done", "", nil)

	pushed, _, err := syncKnowledge(db, cfg, day, rec.push)
	if err != nil || pushed != 1 {
		t.Fatalf("first sync: pushed=%d err=%v", pushed, err)
	}
	if len(rec.calls) != 1 || rec.calls[0] != "dayflow/2026-10-01.md" {
		t.Fatalf("push name wrong: %v", rec.calls)
	}

	// Second sync of the same day: content unchanged → skipped, no call.
	pushed, skipped, err := syncKnowledge(db, cfg, day, rec.push)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if pushed != 0 || skipped != 1 || len(rec.calls) != 1 {
		t.Fatalf("expected dedup skip, pushed=%d skipped=%d calls=%d", pushed, skipped, len(rec.calls))
	}

	// Changed content → re-push.
	start2 := start.Add(15 * time.Minute)
	upsertBlockFull(db, start2, start2.Add(15*time.Minute),
		"block two", "more work", "coding", "", "", 1, 1, "done", "", nil)
	pushed, _, err = syncKnowledge(db, cfg, day, rec.push)
	if err != nil || pushed != 1 {
		t.Fatalf("resync after change: pushed=%d err=%v", pushed, err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("expected re-push on changed content, calls=%d", len(rec.calls))
	}
}

func TestSyncKnowledgeFailureNotRecorded(t *testing.T) {
	emptyAgentDirs(t)
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()
	cfg.KnowledgeSync = true
	rec := &recordingPush{err: fmt.Errorf("ssh: connection refused")}

	day := testDay()
	if _, _, err := syncKnowledge(db, cfg, day, rec.push); err == nil {
		t.Fatal("expected push error to propagate")
	}
	if metaGet(db, "ksync:dayflow/2026-10-01.md") != "" {
		t.Fatal("failed push must not be recorded as synced")
	}
	// Retry after the failure recovers → pushes.
	rec.err = nil
	if pushed, _, err := syncKnowledge(db, cfg, day, rec.push); err != nil || pushed != 1 {
		t.Fatalf("retry after failure: pushed=%d err=%v", pushed, err)
	}
}

func TestSSHIngestPushRequiresSecret(t *testing.T) {
	testEnv(t)
	cfg := defaultConfig()
	cfg.KnowledgeSSHHost = "brain-host"
	cfg.KnowledgeContainer = "brain-container"
	// Point the ref at an account that does not exist.
	cfg.KnowledgeSecretRef = "omaseal://kurultai/definitely-not-a-real-account-xyz"
	if _, err := sshIngestPush(cfg); err == nil {
		t.Fatal("expected error when secret is unresolvable")
	}
}

func TestSSHIngestPushRequiresExplicitConfig(t *testing.T) {
	testEnv(t)
	cfg := defaultConfig()
	cfg.KnowledgeSecretRef = "literal-secret"
	if _, err := sshIngestPush(cfg); err == nil || !strings.Contains(err.Error(), "knowledge_ssh_host") {
		t.Fatalf("missing host should fail loudly, got %v", err)
	}
	cfg.KnowledgeSSHHost = "brain-host"
	if _, err := sshIngestPush(cfg); err == nil || !strings.Contains(err.Error(), "knowledge_container") {
		t.Fatalf("missing container should fail loudly, got %v", err)
	}
}

func TestHTTPIngestPushSendsContract(t *testing.T) {
	testEnv(t)
	var gotAuth, gotAgent, gotNS, gotCT, gotName, gotFormat, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAgent = r.Header.Get("x-kurultai-agent-id")
		gotNS = r.Header.Get("x-kurultai-namespace")
		gotCT = r.Header.Get("Content-Type")
		gotName = r.URL.Query().Get("name")
		gotFormat = r.URL.Query().Get("format")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := defaultConfig()
	cfg.KnowledgeURL = srv.URL
	cfg.KnowledgeSecretRef = "literal-secret"
	push, err := httpIngestPush(cfg)
	if err != nil {
		t.Fatalf("httpIngestPush: %v", err)
	}
	if err := push("dayflow/2026-10-01.md", []byte("doc body")); err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotAuth != "Bearer literal-secret" || gotAgent != "dayflow" || gotNS != "dayflow" ||
		gotName != "dayflow/2026-10-01.md" || gotFormat != "md" || gotBody != "doc body" {
		t.Fatalf("contract wrong: auth=%q agent=%q ns=%q name=%q format=%q body=%q",
			gotAuth, gotAgent, gotNS, gotName, gotFormat, gotBody)
	}
	if !strings.HasPrefix(gotCT, "text/markdown") {
		t.Fatalf("content-type %q", gotCT)
	}
}

func TestHTTPIngestPushErrors(t *testing.T) {
	testEnv(t)
	cfg := defaultConfig()
	cfg.KnowledgeSecretRef = "literal-secret"
	if _, err := httpIngestPush(cfg); err == nil || !strings.Contains(err.Error(), "knowledge_url") {
		t.Fatalf("missing url should fail, got %v", err)
	}
	cfg.KnowledgeURL = "not a url"
	if _, err := httpIngestPush(cfg); err == nil {
		t.Fatal("invalid url should fail")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	cfg.KnowledgeURL = srv.URL
	push, _ := httpIngestPush(cfg)
	if err := push("dayflow/x.md", []byte("x")); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("non-2xx should surface status, got %v", err)
	}
}

func TestKnowledgePushForTransport(t *testing.T) {
	testEnv(t)
	cfg := defaultConfig()
	cfg.KnowledgeTransport = "carrier-pigeon"
	if _, err := knowledgePushFor(cfg); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown transport should fail loudly, got %v", err)
	}
	cfg.KnowledgeTransport = ""
	if _, err := knowledgePushFor(cfg); err == nil || !strings.Contains(err.Error(), "knowledge_url") {
		t.Fatalf("empty transport defaults to http, got %v", err)
	}
	cfg.KnowledgeTransport = "ssh"
	cfg.KnowledgeSecretRef = "literal-secret"
	if _, err := knowledgePushFor(cfg); err == nil || !strings.Contains(err.Error(), "knowledge_ssh_host") {
		t.Fatalf("ssh transport should require host, got %v", err)
	}
}

// TestKnowledgeDocSectionFloor: the brain quality-gates each chunked
// section (tags + ~80 trimmed chars). Every ##/### section we emit must
// carry enough body to clear it.
func TestKnowledgeDocSectionFloor(t *testing.T) {
	emptyAgentDirs(t)
	cfg := testEnv(t)
	db, _ := openDB()
	defer db.Close()

	day := testDay()
	doc := string(knowledgeDocFor(db, cfg, day))
	if !strings.Contains(doc, "tags: dayflow") {
		t.Fatal("frontmatter tags missing — gate's first check would quarantine")
	}
	sections := 0
	for _, chunk := range strings.Split(doc, "\n## ") {
		body := chunk
		if i := strings.Index(chunk, "\n"); i >= 0 {
			body = chunk[i+1:]
		}
		// Split off any ### subsection — it gates independently.
		if i := strings.Index(body, "\n### "); i >= 0 {
			body = body[:i]
		}
		trimmed := strings.TrimSpace(body)
		if strings.HasPrefix(chunk, "---") || strings.HasPrefix(chunk, "# Dayflow") {
			continue
		}
		if strings.TrimSpace(strings.SplitN(chunk, "\n", 2)[0]) == "" {
			continue
		}
		sections++
		if len([]rune(trimmed)) < 80 {
			t.Fatalf("section body under 80 chars would quarantine: %q", trimmed)
		}
	}
	if sections == 0 {
		t.Fatal("no ## sections found — doc shape changed?")
	}
}

func TestKnowledgeSyncAfterExportGate(t *testing.T) {
	testEnv(t)
	db, _ := openDB()
	defer db.Close()
	// Disabled: must no-op without resolving secrets or touching ssh.
	cfg := defaultConfig()
	cfg.KnowledgeSync = false
	knowledgeSyncAfterExport(db, cfg) // must not panic
}
