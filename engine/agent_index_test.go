package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Indexing ingests usable turns (scrubbed), dedupes on re-run, advances
// the watermark, and serves keyword search with citation metadata.
func TestAgentIndexIngestSearchDedup(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dir := t.TempDir()
	setAgentDirs(t, dir)
	now := time.Now()
	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_1", title: "Auth bug", dir: "/home/x/dayflow",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "fix the auth bug, key is sk-live1234567890secret",
				created: ms(now.Add(-time.Hour)), updated: ms(now.Add(-time.Hour))},
			{id: "m2", typ: "assistant", seq: 2, text: "rotated the token and patched middleware",
				created: ms(now.Add(-time.Hour + time.Minute)), updated: ms(now.Add(-time.Hour + time.Minute))},
		},
	})

	n, _, err := ingestAgentChats(db, agentIngestBudget)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 indexed turns, got %d", n)
	}

	// Dedup: a second pass writes nothing.
	if n2, _, err := ingestAgentChats(db, agentIngestBudget); err != nil || n2 != 0 {
		t.Fatalf("re-ingest must be a no-op, got n=%d err=%v", n2, err)
	}

	// Scrub: the pasted key must not survive into the index.
	var body string
	db.QueryRow(`SELECT text FROM agent_msgs_fts WHERE role='user'`).Scan(&body)
	if strings.Contains(body, "sk-live1234567890secret") {
		t.Fatalf("secret leaked into index: %q", body)
	}

	// Watermark advanced to today.
	if metaGet(db, metaAgentIngestDay) != now.Format("2006-01-02") {
		t.Fatalf("watermark not advanced: %q", metaGet(db, metaAgentIngestDay))
	}

	// Search hits cite the session.
	hits, err := searchAgentSessions(db, cfg, "auth bug")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected a hit for 'auth bug'")
	}
	if hits[0].Source != "opencode" || hits[0].Project != "dayflow" {
		t.Fatalf("bad citation: %+v", hits[0])
	}

	// FTS-syntax-shaped user input must not break the query.
	if _, err := searchAgentSessions(db, cfg, `what's "unclosed`); err != nil {
		t.Fatalf("query with quote should be sanitized: %v", err)
	}
}

// A read-only (mode=ro, query_only) handle must degrade to empty hits
// when the index was never built — never error on the CREATE DDL.
func TestAgentIndexReadOnlyDegrades(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	ro, err := openDBReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if !dbReadOnly(ro) {
		t.Fatal("expected query_only handle")
	}
	hits, err := searchAgentSessions(ro, cfg, "anything")
	if err != nil {
		t.Fatalf("read-only search must degrade, got: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("unindexed read-only db must return no hits: %+v", hits)
	}
}

// An in-place session rewrite (fingerprint change) replaces the indexed
// turns — stale text must not linger.
func TestAgentIndexSessionEditReplaces(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dir := t.TempDir()
	setAgentDirs(t, dir)
	now := time.Now()
	dbPath := filepath.Join(dir, "opencode.db")
	writeOpencodeDB(t, dbPath, ocFixtureSession{
		id: "ses_e", title: "edit me", dir: "/home/x/dayflow",
		msgs: []ocFixtureMsg{{id: "m1", typ: "user", seq: 1, text: "original wording here",
			created: ms(now), updated: ms(now)}},
	})
	if _, _, err := ingestAgentChats(db, agentIngestBudget); err != nil {
		t.Fatal(err)
	}

	// Rewrite the session in place — new text, new fingerprint.
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	writeOpencodeDB(t, dbPath, ocFixtureSession{
		id: "ses_e", title: "edit me", dir: "/home/x/dayflow",
		msgs: []ocFixtureMsg{{id: "m1", typ: "user", seq: 1, text: "completely replaced wording",
			created: ms(now), updated: ms(now.Add(time.Minute))}},
	})
	if _, _, err := ingestAgentChats(db, agentIngestBudget); err != nil {
		t.Fatal(err)
	}

	hits, err := searchAgentSessions(db, cfg, "original wording")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Source == "opencode" {
			t.Fatalf("stale turn survived session edit: %+v", h)
		}
	}
	hits, err = searchAgentSessions(db, cfg, "replaced")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("edited session's new text was not indexed")
	}
}

// With nothing ingested, search reports an empty result, not an error.
func TestAgentIndexEmpty(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	setAgentDirs(t, t.TempDir())

	hits, err := searchAgentSessions(db, cfg, "anything")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("empty index must return no hits: %+v", hits)
	}
	if _, err := searchAgentSessions(db, cfg, ""); err == nil {
		t.Fatal("empty query must error")
	}
}

// An expired budget must stop the pass immediately — no decode work runs
// and the watermark stays put so the next pass does the work.
func TestAgentIndexDeadlineStopsPass(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dir := t.TempDir()
	setAgentDirs(t, dir)
	now := time.Now()
	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_b", title: "Budget", dir: "/home/x/p",
		msgs: []ocFixtureMsg{{id: "m1", typ: "user", seq: 1, text: "do work",
			created: ms(now), updated: ms(now)}},
	})

	n, early, err := ingestAgentChats(db, -time.Second) // already expired
	if err != nil {
		t.Fatal(err)
	}
	if !early || n != 0 {
		t.Fatalf("expired budget must stop with no work, got n=%d early=%v", n, early)
	}
	if metaGet(db, metaAgentIngestDay) != "" {
		t.Fatal("watermark must not advance on an aborted pass")
	}

	// A funded pass resumes and completes.
	n, early, err = ingestAgentChats(db, agentIngestCLIBudget)
	if err != nil || early || n != 1 {
		t.Fatalf("resume should index 1 turn, got n=%d early=%v err=%v", n, early, err)
	}
}

// The decoded-byte ceiling aborts a pass mid-session: partial rows persist
// (dedup), the fingerprint is withheld, and a later pass completes coverage.
func TestAgentIndexByteLimitAbortsAndResumes(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dir := t.TempDir()
	setAgentDirs(t, dir)
	now := time.Now()
	var msgs []ocFixtureMsg
	for i := 0; i < 70; i++ { // >64 turns so the mid-session check fires
		msgs = append(msgs, ocFixtureMsg{
			id: fmt.Sprintf("m%d", i), typ: "user", seq: i + 1,
			text:    fmt.Sprintf("turn %d of the long session body text", i),
			created: ms(now.Add(time.Duration(i) * time.Minute)),
			updated: ms(now.Add(time.Duration(i) * time.Minute)),
		})
	}
	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_long", title: "Long", dir: "/home/x/p", msgs: msgs})

	old := agentIngestByteLimit
	agentIngestByteLimit = 500 // trips inside the first session
	defer func() { agentIngestByteLimit = old }()

	n, early, err := ingestAgentChats(db, agentIngestCLIBudget)
	if err != nil {
		t.Fatal(err)
	}
	if !early {
		t.Fatal("byte ceiling must abort the pass early")
	}
	if n == 0 || n >= 70 {
		t.Fatalf("expected partial coverage, got %d", n)
	}
	var fpCount int
	db.QueryRow(`SELECT count(*) FROM agent_sess_fp`).Scan(&fpCount)
	if fpCount != 0 {
		t.Fatal("fingerprint must be withheld on a mid-session abort")
	}

	// Resume with a real byte budget — dedup skips indexed turns, the
	// rest land.
	agentIngestByteLimit = old
	n2, early, err := ingestAgentChats(db, agentIngestCLIBudget)
	if err != nil || early {
		t.Fatalf("resume failed: n=%d early=%v err=%v", n2, early, err)
	}
	var total int
	db.QueryRow(`SELECT count(*) FROM agent_ingest`).Scan(&total)
	if total != 70 {
		t.Fatalf("resume must complete coverage, got %d rows", total)
	}
	hits, err := searchAgentSessions(db, cfg, "long session")
	if err != nil || len(hits) == 0 {
		t.Fatal("resumed content not searchable")
	}
}

// Transcript files beyond agentTranscriptCap are skipped without decode —
// the guard fires in the shared line reader so scan/excerpt/index all
// avoid the file.
func TestAgentIndexFileCapSkips(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dir := t.TempDir()
	setAgentDirs(t, dir)
	now := time.Now()
	claudeDir := filepath.Join(dir, "claude")
	t.Setenv("DAYFLOW_CLAUDE_DIR", claudeDir)
	writeJSONL(t, filepath.Join(claudeDir, "-proj", "big.jsonl"), []string{
		fmt.Sprintf(`{"type":"user","timestamp":%q,"cwd":"/home/x/proj","message":{"role":"user","content":"oversized session question"}}`,
			now.Format(time.RFC3339Nano)),
	}, now)

	old := agentTranscriptCap
	agentTranscriptCap = 16 // the fixture line alone exceeds this
	defer func() { agentTranscriptCap = old }()

	n, _, err := ingestAgentChats(db, agentIngestCLIBudget)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("oversized transcript must be skipped, got %d turns", n)
	}
	hits, err := searchAgentSessions(db, cfg, "oversized")
	if err != nil || len(hits) != 0 {
		t.Fatalf("oversized content must not be indexed: %+v", hits)
	}
}
