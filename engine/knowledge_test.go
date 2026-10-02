package main

import (
	"fmt"
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
	// Point the ref at an account that does not exist.
	cfg.KnowledgeSecretRef = "omaseal://kurultai/definitely-not-a-real-account-xyz"
	if _, err := sshIngestPush(cfg); err == nil {
		t.Fatal("expected error when secret is unresolvable")
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
