package main

import (
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

	n, err := ingestAgentChats(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 indexed turns, got %d", n)
	}

	// Dedup: a second pass writes nothing.
	if n2, err := ingestAgentChats(db); err != nil || n2 != 0 {
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
