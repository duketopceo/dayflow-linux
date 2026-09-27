package main

import (
	"database/sql"
	"encoding/json"
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
	t.Setenv("DAYFLOW_OPENCODE_DB", filepath.Join(dir, "no-opencode.db"))

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
	t.Setenv("DAYFLOW_OPENCODE_DB", filepath.Join(dir, "no-opencode.db"))

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

func TestAgentSessionsDayBucketing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_CLAUDE_DIR", dir)
	t.Setenv("DAYFLOW_CODEX_DIR", filepath.Join(dir, "empty-codex"))
	t.Setenv("DAYFLOW_OPENCODE_DB", filepath.Join(dir, "no-opencode.db"))

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	next := day.Add(24 * time.Hour)
	line := func(ts, text string) string {
		return `{"type":"user","timestamp":"` + ts + `","cwd":"/home/x/p","message":{"role":"user","content":"` + text + `"}}`
	}

	// Session spanning local midnight (bounds are local; timestamps are UTC):
	// appears on both days.
	writeJSONL(t, filepath.Join(dir, "-p", "span.jsonl"), []string{
		line(day.Add(23*time.Hour+30*time.Minute).UTC().Format(time.RFC3339Nano), "late work"),
		line(next.Add(30*time.Minute).UTC().Format(time.RFC3339Nano), "after midnight"),
	}, next.Add(time.Hour)) // mtime next day

	// File modified next day but session fully inside the day — the dropped
	// mtime < e bound used to lose this on both days.
	writeJSONL(t, filepath.Join(dir, "-p", "carryover.jsonl"), []string{
		line(day.Add(14*time.Hour).UTC().Format(time.RFC3339Nano), "afternoon session"),
	}, next.Add(2*time.Hour))

	// mtime in-range but all messages before the day — excluded by overlap.
	writeJSONL(t, filepath.Join(dir, "-p", "stale.jsonl"), []string{
		line("2026-09-10T09:00:00Z", "old session"),
	}, day.Add(3*time.Hour))

	today := agentSessionsForDay(day)
	if len(today) != 2 {
		t.Fatalf("expected 2 sessions on day, got %d: %+v", len(today), today)
	}
	tomorrow := agentSessionsForDay(next)
	if len(tomorrow) != 1 || tomorrow[0].Title != "late work" {
		t.Fatalf("expected only the spanning session tomorrow, got %+v", tomorrow)
	}
}

// --- OpenCode fixtures ---

// ocFixtureMsg is one message row in a fixture store. typ is the
// session_message.type value (next schema) / message.data role (old schema);
// text becomes data.text for user rows and content[0].text for assistant
// rows.
type ocFixtureMsg struct {
	id      string
	typ     string
	seq     int
	created int64 // epoch ms
	updated int64 // epoch ms
	text    string
}

type ocFixtureSession struct {
	id, title, dir string
	msgs           []ocFixtureMsg
}

func ms(t time.Time) int64 { return t.Unix() * 1000 }

func ocData(typ, text string) string {
	if typ == "assistant" {
		b, _ := json.Marshal(map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}}})
		return string(b)
	}
	b, _ := json.Marshal(map[string]any{"text": text})
	return string(b)
}

// writeOpencodeDB builds a next-generation fixture store: session +
// session_message (all OpenCode timestamps are epoch ms).
func writeOpencodeDB(t *testing.T, path string, sessions ...ocFixtureSession) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT NOT NULL,
		  directory TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
		  type TEXT NOT NULL, seq INTEGER NOT NULL, time_created INTEGER NOT NULL,
		  time_updated INTEGER NOT NULL, data TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range sessions {
		var lo, hi int64
		for _, m := range s.msgs {
			if lo == 0 || m.created < lo {
				lo = m.created
			}
			if m.created > hi {
				hi = m.created
			}
			if _, err := db.Exec(`INSERT INTO session_message
			  (id, session_id, type, seq, time_created, time_updated, data)
			  VALUES(?,?,?,?,?,?,?)`,
				m.id, s.id, m.typ, m.seq, m.created, m.updated, ocData(m.typ, m.text)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(`INSERT INTO session
		  (id, title, directory, time_created, time_updated) VALUES(?,?,?,?,?)`,
			s.id, s.title, s.dir, lo, hi); err != nil {
			t.Fatal(err)
		}
	}
}

// writeOpencodeDBOld builds an old-generation fixture store: session +
// message + part (message.data carries the role; part.data carries text
// parts). Mirrors the pre-session_message schema still on disk.
func writeOpencodeDBOld(t *testing.T, path string, sess ocFixtureSession) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT NOT NULL,
		  directory TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
		  type TEXT NOT NULL, seq INTEGER NOT NULL, time_created INTEGER NOT NULL,
		  time_updated INTEGER NOT NULL, data TEXT NOT NULL)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
		  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL,
		  session_id TEXT NOT NULL, time_created INTEGER NOT NULL,
		  time_updated INTEGER NOT NULL, data TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	var lo, hi int64
	for _, m := range sess.msgs {
		if lo == 0 || m.created < lo {
			lo = m.created
		}
		if m.created > hi {
			hi = m.created
		}
		roleData, _ := json.Marshal(map[string]any{"role": m.typ})
		if _, err := db.Exec(`INSERT INTO message
		  (id, session_id, time_created, time_updated, data) VALUES(?,?,?,?,?)`,
			m.id, sess.id, m.created, m.updated, string(roleData)); err != nil {
			t.Fatal(err)
		}
		if m.text != "" {
			partData, _ := json.Marshal(map[string]any{"type": "text", "text": m.text})
			if _, err := db.Exec(`INSERT INTO part
			  (id, message_id, session_id, time_created, time_updated, data)
			  VALUES(?,?,?,?,?,?)`,
				"part_"+m.id, m.id, sess.id, m.created, m.updated, string(partData)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO session
	  (id, title, directory, time_created, time_updated) VALUES(?,?,?,?,?)`,
		sess.id, sess.title, sess.dir, lo, hi); err != nil {
		t.Fatal(err)
	}
}

// setAgentDirs isolates the scan to this test's fixture dirs.
func setAgentDirs(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("DAYFLOW_CLAUDE_DIR", filepath.Join(dir, "no-claude"))
	t.Setenv("DAYFLOW_CODEX_DIR", filepath.Join(dir, "no-codex"))
	t.Setenv("DAYFLOW_OPENCODE_DB", filepath.Join(dir, "opencode.db"))
}

func TestAgentSessionsOpencode(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	next := day.Add(24 * time.Hour)

	// Session spanning local midnight — appears on both days.
	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_1", title: "Fix the widget", dir: "/home/x/widget",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "fix the widget",
				created: ms(day.Add(23*time.Hour + 30*time.Minute)), updated: ms(day.Add(23*time.Hour + 30*time.Minute))},
			{id: "m2", typ: "assistant", seq: 2, text: "done",
				created: ms(day.Add(23*time.Hour + 45*time.Minute)), updated: ms(day.Add(23*time.Hour + 45*time.Minute))},
			{id: "m3", typ: "user", seq: 3, text: "now the tests",
				created: ms(next.Add(30 * time.Minute)), updated: ms(next.Add(30 * time.Minute))},
		},
	})
	// Fully outside the window — must be excluded.
	writeOpencodeDB(t, filepath.Join(dir, "opencode-next.db"), ocFixtureSession{
		id: "ses_old", title: "Old session", dir: "/home/x/old",
		msgs: []ocFixtureMsg{
			{id: "x1", typ: "user", seq: 1, text: "old work",
				created: ms(day.Add(-2 * time.Hour)), updated: ms(day.Add(-2 * time.Hour))},
		},
	})

	sessions := agentSessionsForDay(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d: %+v", len(sessions), sessions)
	}
	s := sessions[0]
	if s.Source != "opencode" || s.Project != "widget" || s.Messages != 3 || s.Title != "Fix the widget" {
		t.Fatalf("bad session: %+v", s)
	}
	if s.Start != day.Add(23*time.Hour+30*time.Minute).Unix() ||
		s.End != next.Add(30*time.Minute).Unix() {
		t.Fatalf("bad range: start=%d end=%d", s.Start, s.End)
	}
	if _, err := os.Stat(s.File); err == nil {
		t.Fatalf("File key should be synthetic, %q stats fine", s.File)
	}
	// The spanning session lands on tomorrow too.
	tomorrow := agentSessionsForDay(next)
	if len(tomorrow) != 1 || tomorrow[0].Title != "Fix the widget" {
		t.Fatalf("expected spanning session tomorrow, got %+v", tomorrow)
	}
}

func TestAgentSessionsOpencodeOldLayout(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	// Old-generation store: session_message exists but is empty; message +
	// part carry the data.
	writeOpencodeDBOld(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_old", title: "Legacy session", dir: "/home/x/legacy",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", text: "ship the legacy fix",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
			{id: "m2", typ: "assistant", text: "shipped",
				created: ms(day.Add(10*time.Hour + time.Minute)), updated: ms(day.Add(10*time.Hour + time.Minute))},
		},
	})

	sessions := agentSessionsForDay(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d: %+v", len(sessions), sessions)
	}
	s := sessions[0]
	if s.Source != "opencode" || s.Project != "legacy" || s.Messages != 2 || s.Title != "Legacy session" {
		t.Fatalf("bad session: %+v", s)
	}
}

func TestAgentSessionsOpencodeNoUserMessages(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_1", title: "Assistant only", dir: "/home/x/p",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "assistant", seq: 1, text: "unsolicited reply",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
		},
	})

	sessions := agentSessionsForDay(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
}

func TestAgentSessionsOpencodeMissingDB(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir) // DAYFLOW_OPENCODE_DB points at a file that does not exist
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
	var oc sourceScanStatus
	for _, st := range statuses {
		if st.Source == "opencode" {
			oc = st
		}
	}
	if oc.Status != "unavailable" || oc.Note == "" {
		t.Fatalf("expected unavailable status with note, got %+v", oc)
	}
}

func TestAgentSessionsOpencodeSchemaDrift(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	// Store exists but has none of the message tables — schema drift is a
	// logged skip, never an error that sinks the other sources.
	db, err := sql.Open("sqlite", filepath.Join(dir, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	writeJSONL(t, filepath.Join(dir, "claude-root", "-proj", "s1.jsonl"), []string{
		`{"type":"user","timestamp":"2026-09-15T10:00:00Z","cwd":"/home/x/proj","message":{"role":"user","content":"fix it"}}`,
	}, day.Add(10*time.Hour))
	t.Setenv("DAYFLOW_CLAUDE_DIR", filepath.Join(dir, "claude-root"))

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 1 || sessions[0].Source != "claude" {
		t.Fatalf("other sources must still scan, got %+v", sessions)
	}
	for _, st := range statuses {
		if st.Source == "opencode" && st.Status != "unavailable" {
			t.Fatalf("expected opencode unavailable, got %+v", st)
		}
	}
}

func TestOpencodeFingerprintInvalidation(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	dbPath := filepath.Join(dir, "opencode.db")
	writeOpencodeDB(t, dbPath, ocFixtureSession{
		id: "ses_1", title: "T", dir: "/home/x/p",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "hi",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
		},
	})

	sessions := agentSessionsForDay(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	src := opencodeSource{}
	fp1, ok := src.Fingerprint(sessions[0])
	if !ok {
		t.Fatal("fingerprint failed on scanned session")
	}
	fp2, ok := src.Fingerprint(sessions[0])
	if !ok || fp2 != fp1 {
		t.Fatal("identical messages produced different fingerprints")
	}

	// Append a message → fingerprint must invalidate the cached recap.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session_message
	  (id, session_id, type, seq, time_created, time_updated, data)
	  VALUES('m2','ses_1','assistant',2,?,?,'{"content":[{"type":"text","text":"ok"}]}')`,
		ms(day.Add(11*time.Hour)), ms(day.Add(11*time.Hour))); err != nil {
		t.Fatal(err)
	}
	db.Close()
	fp3, ok := src.Fingerprint(sessions[0])
	if !ok || fp3 == fp1 {
		t.Fatal("appended message did not change fingerprint")
	}
}

func TestOpencodeSessionKeyRebuild(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_1", title: "T", dir: "/home/x/p",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "hi",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
		},
	})
	sessions := agentSessionsForDay(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	// A session rebuilt from JSON (store/sessionID stripped) must still
	// resolve its store through the File key.
	rebuilt := AgentSession{Source: "opencode", File: sessions[0].File}
	fp, ok := opencodeSource{}.Fingerprint(rebuilt)
	if !ok {
		t.Fatal("fingerprint failed for rebuilt session key")
	}
	ex := opencodeSource{}.Excerpt(rebuilt)
	if ex == "" {
		t.Fatal("excerpt empty for rebuilt session key")
	}
	want, _ := opencodeSource{}.Fingerprint(sessions[0])
	if fp != want {
		t.Fatal("rebuilt key fingerprint differs from scanned")
	}
}

func TestAgentSourceDrift(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Productive scan marks the source; a later zero-session scan is drift.
	recordAgentSourceScans(db, []sourceScanStatus{
		{Source: "opencode", Sessions: 3, Status: "ok"},
		{Source: "codex", Sessions: 0, Status: "unavailable"},
	})
	statuses := []sourceScanStatus{
		{Source: "opencode", Sessions: 0, Status: "empty"},
		{Source: "codex", Sessions: 0, Status: "unavailable"},
	}
	recordAgentSourceScans(db, statuses)
	if !statuses[0].Drift {
		t.Fatal("previously-productive source gone silent not flagged")
	}
	if statuses[1].Drift {
		t.Fatal("never-productive source must not be flagged as drift")
	}
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='agent_source_drift'`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 drift event, got %d", n)
	}
}

// TestRecapOpencodeSession is the KTD2 regression: a DB-backed session with
// a synthetic non-stat-able File key must still clear the fingerprint stage,
// generate a recap, and cache it under the File key.
func TestRecapOpencodeSession(t *testing.T) {
	cfg := testEnv(t)
	reqs := scriptedDecisions(t, cannedDecisions(map[string]float64{
		"worthy": 0.9, "quality": 0.9,
	}))
	stubOpenRouter(t, "Wired the OpenCode adapter into the agents scan.")
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
			{id: "m2", typ: "assistant", seq: 2, text: "done",
				created: ms(day.Add(10*time.Hour + time.Minute)), updated: ms(day.Add(10*time.Hour + time.Minute))},
		},
	})

	sessions := agentSessionsForDay(day)
	if len(sessions) != 1 || sessions[0].Source != "opencode" {
		t.Fatalf("expected 1 opencode session, got %+v", sessions)
	}
	attachRecaps(db, cfg, sessions)
	if sessions[0].Recap != "Wired the OpenCode adapter into the agents scan." {
		t.Fatalf("recap=%q", sessions[0].Recap)
	}
	var stored string
	if err := db.QueryRow(`SELECT recap FROM agent_recaps WHERE path=?`,
		sessions[0].File).Scan(&stored); err != nil || stored == "" {
		t.Fatalf("no cached row under synthetic key %q: %v", sessions[0].File, err)
	}
	// Second attach: content-hash fingerprint hits — no new judge calls.
	before := len(*reqs)
	served := agentSessionsForDay(day)
	attachRecaps(db, cfg, served)
	if len(*reqs) != before {
		t.Fatalf("cache miss on identical messages: %d new calls", len(*reqs)-before)
	}
	if served[0].Recap != "Wired the OpenCode adapter into the agents scan." {
		t.Fatalf("cached recap not served: %q", served[0].Recap)
	}
}
