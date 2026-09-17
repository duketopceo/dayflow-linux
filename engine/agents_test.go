package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeJSONL(t *testing.T, path string, lines []string, mt time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func TestAgentSessionsClaude(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_CLAUDE_DIR", dir)
	t.Setenv("DAYFLOW_CODEX_DIR", filepath.Join(dir, "empty-codex"))

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	mt := day.Add(10 * time.Hour)

	// in-day session
	writeJSONL(t, filepath.Join(dir, "-proj", "s1.jsonl"), []string{
		`{"type":"user","timestamp":"2026-09-15T10:00:00Z","cwd":"/home/x/proj","message":{"role":"user","content":"fix the build"}}`,
		`{"type":"assistant","timestamp":"2026-09-15T10:05:00Z","cwd":"/home/x/proj","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
	}, mt)
	// out-of-day session must be skipped (mtime yesterday)
	writeJSONL(t, filepath.Join(dir, "-proj", "s0.jsonl"), []string{
		`{"type":"user","timestamp":"2026-09-10T10:00:00Z","cwd":"/home/x/proj","message":{"role":"user","content":"old"}}`,
	}, day.Add(-time.Hour))

	sessions := agentSessionsForDay(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.Source != "claude" || s.Project != "proj" || s.Messages != 2 || s.Title != "fix the build" {
		t.Fatalf("bad session: %+v", s)
	}
	if s.Start == 0 || s.End <= s.Start {
		t.Fatalf("bad time range: %+v", s)
	}
}

func TestAgentSessionsCodex(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_CLAUDE_DIR", filepath.Join(dir, "empty-claude"))
	t.Setenv("DAYFLOW_CODEX_DIR", dir)

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	mt := day.Add(9 * time.Hour)

	writeJSONL(t, filepath.Join(dir, "2026/09/15", "r1.jsonl"), []string{
		`{"timestamp":"2026-09-15T09:00:00Z","type":"session_meta","payload":{"cwd":"/home/x/work","session_id":"abc"}}`,
		`{"timestamp":"2026-09-15T09:01:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"refactor the parser"}]}}`,
		`{"timestamp":"2026-09-15T09:20:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`,
	}, mt)

	sessions := agentSessionsForDay(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.Source != "codex" || s.Project != "work" || s.Messages != 2 || s.Title != "refactor the parser" {
		t.Fatalf("bad session: %+v", s)
	}
}
