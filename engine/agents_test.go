package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	t.Setenv("DAYFLOW_DEVIN_DIR", filepath.Join(dir, "no-devin"))
	t.Setenv("DAYFLOW_CURSOR_DB", filepath.Join(dir, "no-cursor.vscdb"))

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

	sessions, _ := scanAgentSources(day)
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
	t.Setenv("DAYFLOW_DEVIN_DIR", filepath.Join(dir, "no-devin"))
	t.Setenv("DAYFLOW_CURSOR_DB", filepath.Join(dir, "no-cursor.vscdb"))

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	mt := day.Add(9 * time.Hour)

	writeJSONL(t, filepath.Join(dir, "2026/09/15", "r1.jsonl"), []string{
		`{"timestamp":"2026-09-15T09:00:00Z","type":"session_meta","payload":{"cwd":"/home/x/work","session_id":"abc"}}`,
		`{"timestamp":"2026-09-15T09:01:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"refactor the parser"}]}}`,
		`{"timestamp":"2026-09-15T09:20:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`,
	}, mt)

	sessions, _ := scanAgentSources(day)
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
	t.Setenv("DAYFLOW_DEVIN_DIR", filepath.Join(dir, "no-devin"))
	t.Setenv("DAYFLOW_CURSOR_DB", filepath.Join(dir, "no-cursor.vscdb"))

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

	today, _ := scanAgentSources(day)
	if len(today) != 2 {
		t.Fatalf("expected 2 sessions on day, got %d: %+v", len(today), today)
	}
	tomorrow, _ := scanAgentSources(next)
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
	t.Setenv("DAYFLOW_DEVIN_DIR", filepath.Join(dir, "no-devin"))
	t.Setenv("DAYFLOW_CURSOR_DB", filepath.Join(dir, "state.vscdb"))
	t.Setenv("DAYFLOW_CURSOR_WORKSPACES", filepath.Join(dir, "workspaceStorage"))
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

	sessions, _ := scanAgentSources(day)
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
	tomorrow, _ := scanAgentSources(next)
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

	sessions, _ := scanAgentSources(day)
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

	sessions, _ := scanAgentSources(day)
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
	// A missing store is absence, not corruption: status "empty" (no
	// drift flag) with the informational note kept for JSON detail.
	if oc.Status != "empty" || oc.Note == "" {
		t.Fatalf("expected empty status with absence note, got %+v", oc)
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

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	src := &opencodeSource{}
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
	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	// A session rebuilt from JSON (store/sessionID stripped) must still
	// resolve its store through the File key.
	rebuilt := AgentSession{Source: "opencode", File: sessions[0].File}
	fp, ok := (&opencodeSource{}).Fingerprint(rebuilt)
	if !ok {
		t.Fatal("fingerprint failed for rebuilt session key")
	}
	ex := (&opencodeSource{}).Excerpt(rebuilt)
	if ex == "" {
		t.Fatal("excerpt empty for rebuilt session key")
	}
	want, _ := (&opencodeSource{}).Fingerprint(sessions[0])
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
	driftEvents := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='agent_source_drift'`).Scan(&n)
		return n
	}

	// Productive scans mark the sources; a healthy-but-idle scan is just
	// "empty", and a never-productive unavailable store isn't drift.
	recordAgentSourceScans(db, []sourceScanStatus{
		{Source: "opencode", Sessions: 3, Status: "ok"},
		{Source: "claude", Sessions: 2, Status: "ok"},
		{Source: "codex", Sessions: 0, Status: "unavailable"},
	})
	statuses := []sourceScanStatus{
		{Source: "opencode", Sessions: 0, Status: "unavailable"},
		{Source: "claude", Sessions: 0, Status: "empty"},
		{Source: "codex", Sessions: 0, Status: "unavailable"},
	}
	recordAgentSourceScans(db, statuses)
	if !statuses[0].Drift {
		t.Fatal("previously-productive source gone unavailable not flagged")
	}
	if statuses[1].Drift {
		t.Fatal("healthy-but-idle source must not be flagged as drift")
	}
	if statuses[2].Drift {
		t.Fatal("never-productive source must not be flagged as drift")
	}
	if n := driftEvents(); n != 1 {
		t.Fatalf("expected 1 drift event, got %d", n)
	}

	// A repeat unavailable scan keeps the flag but doesn't spam events.
	statuses[0].Drift = false
	recordAgentSourceScans(db, statuses)
	if !statuses[0].Drift {
		t.Fatal("ongoing unavailability should stay flagged")
	}
	if n := driftEvents(); n != 1 {
		t.Fatalf("drift event spammed on repeat scan: got %d", n)
	}

	// Recovery clears the marker; a later outage logs a fresh event.
	recordAgentSourceScans(db, []sourceScanStatus{
		{Source: "opencode", Sessions: 1, Status: "ok"},
	})
	again := []sourceScanStatus{{Source: "opencode", Sessions: 0, Status: "unavailable"}}
	recordAgentSourceScans(db, again)
	if !again[0].Drift {
		t.Fatal("second outage not flagged")
	}
	if n := driftEvents(); n != 2 {
		t.Fatalf("expected a fresh drift event after recovery, got %d", n)
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

	sessions, _ := scanAgentSources(day)
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
	served, _ := scanAgentSources(day)
	attachRecaps(db, cfg, served)
	if len(*reqs) != before {
		t.Fatalf("cache miss on identical messages: %d new calls", len(*reqs)-before)
	}
	if served[0].Recap != "Wired the OpenCode adapter into the agents scan." {
		t.Fatalf("cached recap not served: %q", served[0].Recap)
	}
}

// --- Devin fixtures ---

// dvFixtureMsg is one message_nodes row in a fixture store. role is the
// chat_message.role value; userInput sets metadata.is_user_input (true only
// on real typed turns — injected continuations leave it false). Devin
// timestamps are epoch seconds.
type dvFixtureMsg struct {
	role      string
	text      string
	created   int64 // epoch s
	userInput bool
}

type dvFixtureSession struct {
	id, title, dir string
	msgs           []dvFixtureMsg
}

// writeDevinDB builds a fixture sessions.db matching the real store's
// relevant schema: sessions (id/working_directory/title/epoch-second
// timestamps) + message_nodes (chat_message JSON per node).
func writeDevinDB(t *testing.T, path string, sessions ...dvFixtureSession) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT NOT NULL,
		  backend_type TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
		  agent_mode TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL,
		  last_activity_at INTEGER NOT NULL, title TEXT)`,
		`CREATE TABLE message_nodes (row_id INTEGER PRIMARY KEY AUTOINCREMENT,
		  session_id TEXT NOT NULL, node_id INTEGER NOT NULL, parent_node_id INTEGER,
		  chat_message TEXT NOT NULL, created_at INTEGER NOT NULL)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	for i, s := range sessions {
		var lo, hi int64
		for j, m := range s.msgs {
			nodeID := i*100 + j + 1
			if lo == 0 || m.created < lo {
				lo = m.created
			}
			if m.created > hi {
				hi = m.created
			}
			meta := map[string]any{}
			if m.userInput {
				meta["is_user_input"] = true
			}
			cm, _ := json.Marshal(map[string]any{
				"message_id": "msg-" + strconv.Itoa(nodeID),
				"role":       m.role,
				"content":    m.text,
				"metadata":   meta,
			})
			if _, err := db.Exec(`INSERT INTO message_nodes
			  (session_id, node_id, chat_message, created_at) VALUES(?,?,?,?)`,
				s.id, nodeID, string(cm), m.created); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(`INSERT INTO sessions
		  (id, working_directory, created_at, last_activity_at, title)
		  VALUES(?,?,?,?,?)`, s.id, s.dir, lo, hi, s.title); err != nil {
			t.Fatal(err)
		}
	}
}

// dvFixtureStep is one ATIF transcript step. source is the ATIF actor —
// "user" | "agent" | "system".
type dvFixtureStep struct {
	source string
	text   string
	ts     time.Time
}

// writeDevinTranscript writes transcripts/<sessionID>.json in ATIF shape.
// mt sets the file mtime — the scanner bounds on it like jsonlFiles.
func writeDevinTranscript(t *testing.T, dir, sessionID, schemaVersion string, mt time.Time, steps []dvFixtureStep) string {
	t.Helper()
	type step struct {
		StepID    int    `json:"step_id"`
		Timestamp string `json:"timestamp"`
		Source    string `json:"source"`
		Message   string `json:"message"`
	}
	doc := map[string]any{
		"schema_version": schemaVersion,
		"session_id":     sessionID,
		"steps":          []step{},
	}
	arr := doc["steps"].([]step)
	for i, s := range steps {
		arr = append(arr, step{
			StepID:    i + 1,
			Timestamp: s.ts.UTC().Format(time.RFC3339Nano),
			Source:    s.source,
			Message:   s.text,
		})
	}
	doc["steps"] = arr
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "transcripts", sessionID+".json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

// sourceStatus pulls one source's scan status out of a scanAgentSources
// result.
func sourceStatus(t *testing.T, statuses []sourceScanStatus, name string) sourceScanStatus {
	t.Helper()
	for _, st := range statuses {
		if st.Source == name {
			return st
		}
	}
	t.Fatalf("no %s source status", name)
	return sourceScanStatus{}
}

func devinStatus(t *testing.T, statuses []sourceScanStatus) sourceScanStatus {
	t.Helper()
	return sourceStatus(t, statuses, "devin")
}

func TestAgentSessionsDevin(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	writeDevinDB(t, filepath.Join(devin, "sessions.db"), dvFixtureSession{
		id: "calm-fox", title: "Parity work", dir: "/home/x/dayflow",
		msgs: []dvFixtureMsg{
			{role: "user", text: "implement the devin adapter",
				created: day.Add(10 * time.Hour).Unix(), userInput: true},
			{role: "assistant", text: "done",
				created: day.Add(10*time.Hour + 5*time.Minute).Unix()},
			// Injected continuation — counted as a message but never
			// excerpt or title material.
			{role: "user", text: "continue",
				created: day.Add(10*time.Hour + 10*time.Minute).Unix()},
		},
	})
	// A matching transcript exists — the DB row wins, no double-listing.
	writeDevinTranscript(t, devin, "calm-fox", "ATIF-v1.7", day.Add(11*time.Hour), []dvFixtureStep{
		{source: "user", text: "implement the devin adapter", ts: day.Add(10 * time.Hour)},
		{source: "agent", text: "done", ts: day.Add(10*time.Hour + 5*time.Minute)},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d: %+v", len(sessions), sessions)
	}
	s := sessions[0]
	if s.Source != "devin" || s.Project != "dayflow" || s.Messages != 3 || s.Title != "Parity work" {
		t.Fatalf("bad session: %+v", s)
	}
	if s.Start != day.Add(10*time.Hour).Unix() ||
		s.End != day.Add(10*time.Hour+10*time.Minute).Unix() {
		t.Fatalf("bad range: start=%d end=%d", s.Start, s.End)
	}
	if _, err := os.Stat(s.File); err == nil {
		t.Fatalf("File key should be synthetic, %q stats fine", s.File)
	}
}

func TestAgentSessionsDevinDBOnly(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	// Session row with no transcript file — listed from the DB alone.
	writeDevinDB(t, filepath.Join(devin, "sessions.db"), dvFixtureSession{
		id: "loud-owl", title: "API fix", dir: "/home/x/api",
		msgs: []dvFixtureMsg{
			{role: "user", text: "fix the api route",
				created: day.Add(14 * time.Hour).Unix(), userInput: true},
			{role: "assistant", text: "fixed",
				created: day.Add(14*time.Hour + 2*time.Minute).Unix()},
		},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d: %+v", len(sessions), sessions)
	}
	s := sessions[0]
	if s.Source != "devin" || s.Project != "api" || s.Title != "API fix" || s.Messages != 2 {
		t.Fatalf("bad session: %+v", s)
	}
}

func TestAgentSessionsDevinTranscriptOnly(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	// sessions.db holds a different session — the orphan transcript is the
	// ATIF fallback path.
	writeDevinDB(t, filepath.Join(devin, "sessions.db"), dvFixtureSession{
		id: "db-sess", title: "DB session", dir: "/home/x/dbproj",
		msgs: []dvFixtureMsg{
			{role: "user", text: "db work",
				created: day.Add(9 * time.Hour).Unix(), userInput: true},
			{role: "assistant", text: "ok",
				created: day.Add(9*time.Hour + time.Minute).Unix()},
		},
	})
	writeDevinTranscript(t, devin, "orphan-slug", "ATIF-v1.7", day.Add(15*time.Hour), []dvFixtureStep{
		{source: "system", text: "context", ts: day.Add(14 * time.Hour)},
		{source: "user", text: "orphan transcript work", ts: day.Add(14*time.Hour + time.Minute)},
		{source: "agent", text: "working", ts: day.Add(14*time.Hour + 2*time.Minute)},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %+v", len(sessions), sessions)
	}
	var orphan *AgentSession
	for i := range sessions {
		if sessions[i].Title == "orphan transcript work" {
			orphan = &sessions[i]
		}
	}
	if orphan == nil {
		t.Fatalf("transcript-only session not listed: %+v", sessions)
	}
	if orphan.Source != "devin" || orphan.Messages != 2 {
		t.Fatalf("bad orphan session: %+v", orphan)
	}
	// No working_directory anywhere — project stays empty, not guessed.
	if orphan.Project != "" || orphan.Cwd != "" {
		t.Fatalf("orphan project must be empty, got %+v", orphan)
	}
	if orphan.Start != day.Add(14*time.Hour).Unix() ||
		orphan.End != day.Add(14*time.Hour+2*time.Minute).Unix() {
		t.Fatalf("bad orphan range: start=%d end=%d", orphan.Start, orphan.End)
	}
}

func TestAgentSessionsDevinBadSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	// Unrecognized schema_version skips only that file.
	writeDevinTranscript(t, devin, "future-fmt", "ATIF-v9.9", day.Add(12*time.Hour), []dvFixtureStep{
		{source: "user", text: "skipped work", ts: day.Add(10 * time.Hour)},
	})
	writeDevinTranscript(t, devin, "good-slug", "ATIF-v1.7", day.Add(12*time.Hour), []dvFixtureStep{
		{source: "user", text: "kept work", ts: day.Add(11 * time.Hour)},
		{source: "agent", text: "ok", ts: day.Add(11*time.Hour + time.Minute)},
	})

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 1 || sessions[0].Title != "kept work" {
		t.Fatalf("expected only the good transcript, got %+v", sessions)
	}
	st := devinStatus(t, statuses)
	if !strings.Contains(st.Note, "skipped") {
		t.Fatalf("expected a skipped-transcript note, got %+v", st)
	}
}

func TestAgentSessionsDevinMissing(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir) // DAYFLOW_DEVIN_DIR points at a dir that does not exist
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
	st := devinStatus(t, statuses)
	// The whole store dir is absent — absence reports "empty" with the
	// note kept, never "unavailable" (a never-installed tool can't drift).
	if st.Status != "empty" || st.Note == "" {
		t.Fatalf("expected empty status with absence note, got %+v", st)
	}
}

func TestDevinFingerprintInvalidation(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	dbPath := filepath.Join(devin, "sessions.db")
	writeDevinDB(t, dbPath, dvFixtureSession{
		id: "ses-1", title: "T", dir: "/home/x/p",
		msgs: []dvFixtureMsg{
			{role: "user", text: "hi", created: day.Add(10 * time.Hour).Unix(), userInput: true},
		},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	src := &devinSource{}
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
	cm, _ := json.Marshal(map[string]any{
		"message_id": "msg-2", "role": "assistant", "content": "ok",
		"metadata": map[string]any{},
	})
	if _, err := db.Exec(`INSERT INTO message_nodes
	  (session_id, node_id, chat_message, created_at) VALUES('ses-1',2,?,?)`,
		string(cm), day.Add(11*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	fp3, ok := src.Fingerprint(sessions[0])
	if !ok || fp3 == fp1 {
		t.Fatal("appended message did not change fingerprint")
	}
}

// TestDevinSessionKeyRebuild is the devin half of the KTD2 regression: a
// session rebuilt from its serialized File key (store/sessionID stripped)
// must still resolve its store for both DB-backed and transcript-only
// sessions.
func TestDevinSessionKeyRebuild(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	writeDevinDB(t, filepath.Join(devin, "sessions.db"), dvFixtureSession{
		id: "ses-1", title: "T", dir: "/home/x/p",
		msgs: []dvFixtureMsg{
			{role: "user", text: "hi there", created: day.Add(10 * time.Hour).Unix(), userInput: true},
		},
	})
	writeDevinTranscript(t, devin, "t-only", "ATIF-v1.7", day.Add(12*time.Hour), []dvFixtureStep{
		{source: "user", text: "transcript hi", ts: day.Add(12 * time.Hour)},
		{source: "agent", text: "yo", ts: day.Add(12*time.Hour + time.Minute)},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	src := &devinSource{}
	for _, s := range sessions {
		rebuilt := AgentSession{Source: "devin", File: s.File}
		fp, ok := src.Fingerprint(rebuilt)
		if !ok {
			t.Fatalf("fingerprint failed for rebuilt key %q", s.File)
		}
		want, _ := src.Fingerprint(s)
		if fp != want {
			t.Fatalf("rebuilt key fingerprint differs for %q", s.File)
		}
		if ex := src.Excerpt(rebuilt); ex == "" {
			t.Fatalf("excerpt empty for rebuilt key %q", s.File)
		}
	}
}

func TestDevinExcerpt(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	writeDevinDB(t, filepath.Join(devin, "sessions.db"), dvFixtureSession{
		id: "ses-1", title: "T", dir: "/home/x/p",
		msgs: []dvFixtureMsg{
			{role: "user", text: "first real prompt",
				created: day.Add(10 * time.Hour).Unix(), userInput: true},
			{role: "assistant", text: "answer",
				created: day.Add(10*time.Hour + time.Minute).Unix()},
			{role: "user", text: "continue", // injected — not excerpt material
				created: day.Add(10*time.Hour + 2*time.Minute).Unix()},
			{role: "user", text: "last real prompt",
				created: day.Add(10*time.Hour + 3*time.Minute).Unix(), userInput: true},
		},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	ex := (&devinSource{}).Excerpt(sessions[0])
	if !strings.Contains(ex, "first user message: first real prompt") ||
		!strings.Contains(ex, "last user message: last real prompt") ||
		!strings.Contains(ex, "last assistant reply: answer") {
		t.Fatalf("bad excerpt: %q", ex)
	}
	if strings.Contains(ex, "continue") {
		t.Fatalf("injected prompt leaked into excerpt: %q", ex)
	}
}

// --- Cursor fixtures ---

// cuFixtureBubble is one composer turn: a fullConversationHeadersOnly entry
// plus its bubbleId:<composerId>:<id> body row. typ is the Cursor bubble
// type — 1 = user, 2 = assistant (matching the real store).
type cuFixtureBubble struct {
	id   string
	typ  int
	text string
	ms   int64 // epoch ms; 0 = no timestamp on the bubble row
}

type cuFixtureComposer struct {
	id, name, wsid   string
	created, updated int64 // epoch ms
	draft            bool
	bubbles          []cuFixtureBubble
	contentBlob      string // optional raw composer.content.<id> value
}

// writeCursorDB builds a fixture state.vscdb matching the real store's
// relevant schema: composerHeaders (epoch-ms timestamps) + cursorDiskKV
// carrying composerData:<id> indexes and bubbleId:<id>:<bid> bodies.
func writeCursorDB(t *testing.T, path string, composers ...cuFixtureComposer) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{
		`CREATE TABLE composerHeaders (composerId TEXT PRIMARY KEY, workspaceId TEXT,
		  createdAt INTEGER, lastUpdatedAt INTEGER, isArchived INTEGER, isSubagent INTEGER,
		  recency INTEGER, checkpointAt INTEGER, subagentTypeName TEXT, value TEXT)`,
		`CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`,
		`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range composers {
		head, _ := json.Marshal(map[string]any{
			"type": "head", "composerId": c.id, "createdAt": c.created,
			"lastUpdatedAt": c.updated, "isDraft": c.draft,
			"name": c.name, "workspaceIdentifier": map[string]any{"id": c.wsid},
		})
		if _, err := db.Exec(`INSERT INTO composerHeaders
		  (composerId, workspaceId, createdAt, lastUpdatedAt, isArchived, isSubagent, value)
		  VALUES(?,?,?,?,0,0,?)`, c.id, c.wsid, c.created, c.updated, string(head)); err != nil {
			t.Fatal(err)
		}
		var heads []map[string]any
		for _, b := range c.bubbles {
			heads = append(heads, map[string]any{"bubbleId": b.id, "type": b.typ})
		}
		data, _ := json.Marshal(map[string]any{
			"_v": 18, "composerId": c.id, "createdAt": c.created,
			"lastUpdatedAt": c.updated, "fullConversationHeadersOnly": heads,
			"conversationMap": map[string]any{},
		})
		if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES(?,?)`,
			"composerData:"+c.id, string(data)); err != nil {
			t.Fatal(err)
		}
		for _, b := range c.bubbles {
			body := map[string]any{"type": b.typ, "bubbleId": b.id, "text": b.text}
			if b.ms > 0 {
				body["createdAt"] = b.ms
			}
			raw, _ := json.Marshal(body)
			if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES(?,?)`,
				"bubbleId:"+c.id+":"+b.id, string(raw)); err != nil {
				t.Fatal(err)
			}
		}
		if c.contentBlob != "" {
			if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES(?,?)`,
				"composer.content."+c.id, c.contentBlob); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func cursorStatus(t *testing.T, statuses []sourceScanStatus) sourceScanStatus {
	t.Helper()
	return sourceStatus(t, statuses, "cursor")
}

func TestAgentSessionsCursor(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	// workspaceId -> project dir via workspaceStorage/<id>/workspace.json.
	ws := filepath.Join(dir, "workspaceStorage", "ws-1")
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(ws, "workspace.json"),
		[]byte(`{"folder": "file:///home/x/widget"}`), 0o600)

	start := day.Add(10 * time.Hour)
	writeCursorDB(t, filepath.Join(dir, "state.vscdb"), cuFixtureComposer{
		id: "comp-1", name: "Widget fix", wsid: "ws-1",
		created: start.UnixMilli(), updated: start.Add(20 * time.Minute).UnixMilli(),
		bubbles: []cuFixtureBubble{
			{id: "b1", typ: 1, text: "fix the widget crash", ms: start.UnixMilli()},
			{id: "b2", typ: 2, text: "looking at the stack", ms: start.Add(5 * time.Minute).UnixMilli()},
			{id: "b3", typ: 1, text: "also add a test", ms: start.Add(15 * time.Minute).UnixMilli()},
		},
	})
	// A draft composer in range must not produce a session.
	writeCursorDB2(t, filepath.Join(dir, "state.vscdb"), cuFixtureComposer{
		id: "draft-aaaa", wsid: "empty-window", draft: true,
		created: day.Add(11 * time.Hour).UnixMilli(), updated: day.Add(11 * time.Hour).UnixMilli(),
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d: %+v", len(sessions), sessions)
	}
	s := sessions[0]
	if s.Source != "cursor" || s.Project != "widget" || s.Messages != 3 || s.Title != "Widget fix" {
		t.Fatalf("bad session: %+v", s)
	}
	if s.Start != start.Unix() || s.End != start.Add(20*time.Minute).Unix() {
		t.Fatalf("bad range: start=%d end=%d", s.Start, s.End)
	}
	if s.File != "cursor://comp-1" {
		t.Fatalf("bad File key: %q", s.File)
	}
	if _, err := os.Stat(s.File); err == nil {
		t.Fatalf("File key should be synthetic, %q stats fine", s.File)
	}
}

// writeCursorDB2 appends a composer to an existing fixture DB (mirrors how a
// live store gains rows between scans).
func writeCursorDB2(t *testing.T, path string, composers ...cuFixtureComposer) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, c := range composers {
		head, _ := json.Marshal(map[string]any{
			"type": "head", "composerId": c.id, "createdAt": c.created,
			"lastUpdatedAt": c.updated, "isDraft": c.draft,
		})
		if _, err := db.Exec(`INSERT INTO composerHeaders
		  (composerId, workspaceId, createdAt, lastUpdatedAt, isArchived, isSubagent, value)
		  VALUES(?,?,?,?,0,0,?)`, c.id, c.wsid, c.created, c.updated, string(head)); err != nil {
			t.Fatal(err)
		}
		var heads []map[string]any
		for _, b := range c.bubbles {
			heads = append(heads, map[string]any{"bubbleId": b.id, "type": b.typ})
		}
		data, _ := json.Marshal(map[string]any{
			"_v": 18, "composerId": c.id, "createdAt": c.created,
			"lastUpdatedAt": c.updated, "fullConversationHeadersOnly": heads,
			"conversationMap": map[string]any{},
		})
		db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES(?,?)`,
			"composerData:"+c.id, string(data))
		for _, b := range c.bubbles {
			raw, _ := json.Marshal(map[string]any{
				"type": b.typ, "bubbleId": b.id, "text": b.text, "createdAt": b.ms,
			})
			db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES(?,?)`,
				"bubbleId:"+c.id+":"+b.id, string(raw))
		}
	}
}

func TestAgentSessionsCursorNoUserTurns(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	writeCursorDB(t, filepath.Join(dir, "state.vscdb"), cuFixtureComposer{
		id: "comp-ai", wsid: "ws-1",
		created: day.Add(10 * time.Hour).UnixMilli(),
		updated: day.Add(10*time.Hour + time.Minute).UnixMilli(),
		bubbles: []cuFixtureBubble{
			{id: "b1", typ: 2, text: "unsolicited reply", ms: day.Add(10 * time.Hour).UnixMilli()},
		},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
}

func TestAgentSessionsCursorMalformedBlob(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	dbPath := filepath.Join(dir, "state.vscdb")
	writeCursorDB(t, dbPath)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Header row whose content is unparseable garbage in every location.
	head, _ := json.Marshal(map[string]any{
		"type": "head", "composerId": "comp-bad", "createdAt": day.Add(10 * time.Hour).UnixMilli(),
	})
	if _, err := db.Exec(`INSERT INTO composerHeaders
	  (composerId, workspaceId, createdAt, lastUpdatedAt, isArchived, isSubagent, value)
	  VALUES('comp-bad','ws-1',?,?,0,0,?)`,
		day.Add(10*time.Hour).UnixMilli(), day.Add(10*time.Hour).UnixMilli(), string(head)); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES('composerData:comp-bad','{{not json')`)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES('composer.content.comp-bad','# just a markdown doc')`)
	db.Close()

	// A claude session proves the cursor failure didn't sink other sources.
	writeJSONL(t, filepath.Join(dir, "claude-root", "-proj", "s1.jsonl"), []string{
		`{"type":"user","timestamp":"2026-09-15T10:00:00Z","cwd":"/home/x/proj","message":{"role":"user","content":"fix it"}}`,
	}, day.Add(10*time.Hour))
	t.Setenv("DAYFLOW_CLAUDE_DIR", filepath.Join(dir, "claude-root"))

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 1 || sessions[0].Source != "claude" {
		t.Fatalf("other sources must still scan, got %+v", sessions)
	}
	st := cursorStatus(t, statuses)
	if st.Sessions != 0 || st.Note == "" {
		t.Fatalf("expected skipped-composer note, got %+v", st)
	}
}

// A composer that exists but never produced user turns — opened and left
// idle, or assistant-only — is a normal empty day, not store degradation:
// it must not contribute a skipped note or trip the drift flag.
func TestAgentSessionsCursorIdleComposers(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	dbPath := filepath.Join(dir, "state.vscdb")
	writeCursorDB(t, dbPath,
		cuFixtureComposer{id: "comp-empty", name: "untitled", wsid: "ws-1",
			created: day.Add(10 * time.Hour).UnixMilli(),
			updated: day.Add(10 * time.Hour).UnixMilli()},
		cuFixtureComposer{id: "comp-ast", name: "assistant only", wsid: "ws-1",
			created: day.Add(11 * time.Hour).UnixMilli(),
			updated: day.Add(11 * time.Hour).UnixMilli(),
			bubbles: []cuFixtureBubble{
				{id: "b1", typ: 2, text: "unsolicited reply", ms: day.Add(11 * time.Hour).UnixMilli()},
			}},
	)

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("idle composers must not yield sessions, got %+v", sessions)
	}
	st := cursorStatus(t, statuses)
	if st.Status != "empty" || st.Note != "" || st.Drift {
		t.Fatalf("idle composers must report empty with no note, got %+v", st)
	}
}

// A user head whose bubbleId body row is present but malformed is store
// corruption, not an idle composer — the retained empty-text turn would
// otherwise let the composer pass as having user turns and report "empty",
// suppressing drift reporting.
func TestAgentSessionsCursorMalformedBubble(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	dbPath := filepath.Join(dir, "state.vscdb")
	writeCursorDB(t, dbPath,
		cuFixtureComposer{id: "comp-corrupt", name: "broken", wsid: "ws-1",
			created: day.Add(10 * time.Hour).UnixMilli(),
			updated: day.Add(10 * time.Hour).UnixMilli(),
			bubbles: []cuFixtureBubble{
				{id: "b1", typ: 1, text: "fix it", ms: day.Add(10 * time.Hour).UnixMilli()},
			}},
	)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES(?,?)`,
		"bubbleId:comp-corrupt:b1", "{{not json"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("corrupt composer must not yield a session, got %+v", sessions)
	}
	st := cursorStatus(t, statuses)
	if st.Status != "unavailable" || st.Note == "" {
		t.Fatalf("malformed user bubble must report unavailable, got %+v", st)
	}
}

func TestAgentSessionsCursorMissingTables(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	db, err := sql.Open("sqlite", filepath.Join(dir, "state.vscdb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
	st := cursorStatus(t, statuses)
	if st.Status != "unavailable" || st.Note == "" {
		t.Fatalf("expected unavailable status with note, got %+v", st)
	}
}

func TestCursorFingerprintInvalidation(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	dbPath := filepath.Join(dir, "state.vscdb")
	writeCursorDB(t, dbPath, cuFixtureComposer{
		id: "comp-1", wsid: "ws-1",
		created: day.Add(10 * time.Hour).UnixMilli(),
		updated: day.Add(10 * time.Hour).UnixMilli(),
		bubbles: []cuFixtureBubble{
			{id: "b1", typ: 1, text: "hi", ms: day.Add(10 * time.Hour).UnixMilli()},
		},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	src := &cursorSource{}
	fp1, ok := src.Fingerprint(sessions[0])
	if !ok {
		t.Fatal("fingerprint failed on scanned session")
	}
	fp2, ok := src.Fingerprint(sessions[0])
	if !ok || fp2 != fp1 {
		t.Fatal("identical turns produced different fingerprints")
	}

	// Append a turn → fingerprint must invalidate the cached recap.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	newUp := day.Add(11 * time.Hour).UnixMilli()
	data, _ := json.Marshal(map[string]any{
		"_v": 18, "composerId": "comp-1", "createdAt": day.Add(10 * time.Hour).UnixMilli(),
		"lastUpdatedAt": newUp,
		"fullConversationHeadersOnly": []map[string]any{
			{"bubbleId": "b1", "type": 1}, {"bubbleId": "b2", "type": 2},
		},
		"conversationMap": map[string]any{},
	})
	if _, err := db.Exec(`UPDATE cursorDiskKV SET value=? WHERE key='composerData:comp-1'`,
		string(data)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"type": 2, "bubbleId": "b2", "text": "ok", "createdAt": newUp,
	})
	if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES('bubbleId:comp-1:b2',?)`,
		string(raw)); err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE composerHeaders SET lastUpdatedAt=? WHERE composerId='comp-1'`, newUp)
	db.Close()

	fp3, ok := src.Fingerprint(sessions[0])
	if !ok || fp3 == fp1 {
		t.Fatal("appended turn did not change fingerprint")
	}
}

// TestCursorSessionKeyRebuild is the cursor half of the KTD2 regression: a
// session rebuilt from its serialized File key (store/sessionID stripped)
// must still resolve its store.
func TestCursorSessionKeyRebuild(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	writeCursorDB(t, filepath.Join(dir, "state.vscdb"), cuFixtureComposer{
		id: "comp-1", wsid: "ws-1",
		created: day.Add(10 * time.Hour).UnixMilli(),
		updated: day.Add(10 * time.Hour).UnixMilli(),
		bubbles: []cuFixtureBubble{
			{id: "b1", typ: 1, text: "hi there", ms: day.Add(10 * time.Hour).UnixMilli()},
			{id: "b2", typ: 2, text: "hello", ms: day.Add(10*time.Hour + time.Minute).UnixMilli()},
		},
	})
	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	rebuilt := AgentSession{Source: "cursor", File: sessions[0].File}
	src := &cursorSource{}
	fp, ok := src.Fingerprint(rebuilt)
	if !ok {
		t.Fatal("fingerprint failed for rebuilt session key")
	}
	want, _ := src.Fingerprint(sessions[0])
	if fp != want {
		t.Fatal("rebuilt key fingerprint differs from scanned")
	}
	if ex := src.Excerpt(rebuilt); ex == "" {
		t.Fatal("excerpt empty for rebuilt session key")
	}
}

func TestCursorExcerpt(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	writeCursorDB(t, filepath.Join(dir, "state.vscdb"), cuFixtureComposer{
		id: "comp-1", wsid: "ws-1",
		created: day.Add(10 * time.Hour).UnixMilli(),
		updated: day.Add(10*time.Hour + 3*time.Minute).UnixMilli(),
		bubbles: []cuFixtureBubble{
			// Injected envelope first — never excerpt material.
			{id: "b0", typ: 1, text: "<environment_context>os=linux</environment_context>",
				ms: day.Add(10 * time.Hour).UnixMilli()},
			{id: "b1", typ: 1, text: "first real prompt",
				ms: day.Add(10*time.Hour + time.Minute).UnixMilli()},
			{id: "b2", typ: 2, text: "answer",
				ms: day.Add(10*time.Hour + 2*time.Minute).UnixMilli()},
			{id: "b3", typ: 1, text: "last real prompt",
				ms: day.Add(10*time.Hour + 3*time.Minute).UnixMilli()},
		},
	})
	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	ex := (&cursorSource{}).Excerpt(sessions[0])
	if !strings.Contains(ex, "first user message: first real prompt") ||
		!strings.Contains(ex, "last user message: last real prompt") ||
		!strings.Contains(ex, "last assistant reply: answer") {
		t.Fatalf("bad excerpt: %q", ex)
	}
	if strings.Contains(ex, "environment_context") {
		t.Fatalf("envelope leaked into excerpt: %q", ex)
	}
}

func TestAgentSessionsCursorContentBlobFallback(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	dbPath := filepath.Join(dir, "state.vscdb")
	writeCursorDB(t, dbPath)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Header row but the body lives only in a composer.content.<id> JSON blob
	// (the plan-described alternate layout) — an array of role/text turns.
	head, _ := json.Marshal(map[string]any{
		"type": "head", "composerId": "comp-c", "createdAt": day.Add(9 * time.Hour).UnixMilli(),
		"lastUpdatedAt": day.Add(9*time.Hour + 5*time.Minute).UnixMilli(),
	})
	if _, err := db.Exec(`INSERT INTO composerHeaders
	  (composerId, workspaceId, createdAt, lastUpdatedAt, isArchived, isSubagent, value)
	  VALUES('comp-c','ws-1',?,?,0,0,?)`,
		day.Add(9*time.Hour).UnixMilli(), day.Add(9*time.Hour+5*time.Minute).UnixMilli(),
		string(head)); err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(map[string]any{
		"turns": []map[string]any{
			{"role": "user", "text": "content blob work", "id": "t1"},
			{"role": "assistant", "text": "did it", "id": "t2"},
		},
	})
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES('composer.content.comp-c',?)`,
		string(blob))
	db.Close()

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 || sessions[0].Source != "cursor" ||
		sessions[0].Title != "content blob work" || sessions[0].Messages != 2 {
		t.Fatalf("expected content-blob session, got %+v", sessions)
	}
}

// --- Store absence / failure taxonomy (R3b) ---

// writeOpencodeDBDrifted builds a store whose session_message table exists
// and resolves candidates, but dropped the type/data columns — per-session
// extraction fails, which must surface as "failed to read", not a silent
// empty scan.
func writeOpencodeDBDrifted(t *testing.T, path, sessID string, created int64) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT,
		  directory TEXT, time_created INTEGER, time_updated INTEGER)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT,
		  seq INTEGER, time_created INTEGER, time_updated INTEGER)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO session
	  (id, title, directory, time_created, time_updated) VALUES(?,?,?,?,?)`,
		sessID, "T", "/x/p", created, created); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session_message
	  (id, session_id, seq, time_created, time_updated) VALUES(?,?,?,?,?)`,
		"m1", sessID, 1, created, created); err != nil {
		t.Fatal(err)
	}
}

// An absent sibling sub-store is the idle case: when another store scans
// clean or produces sessions, the missing one's note is suppressed entirely
// — no false degradation, no drift bait.
func TestAgentSessionsOpencodeAbsentSibling(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	// Primary store absent; the opencode-next.db sibling exists and scans
	// clean — the missing primary must not surface a note at all.
	writeOpencodeDB(t, filepath.Join(dir, "opencode-next.db"))
	_, statuses := scanAgentSources(day)
	oc := sourceStatus(t, statuses, "opencode")
	if oc.Status != "empty" || oc.Note != "" {
		t.Fatalf("absent sibling should report empty with no note, got %+v", oc)
	}

	// Same suppression when the sibling produces sessions.
	if err := os.Remove(filepath.Join(dir, "opencode-next.db")); err != nil {
		t.Fatal(err)
	}
	writeOpencodeDB(t, filepath.Join(dir, "opencode-next.db"), ocFixtureSession{
		id: "ses_1", title: "Next work", dir: "/x/next",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "next-gen work",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
		},
	})
	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session from the sibling store, got %+v", sessions)
	}
	oc = sourceStatus(t, statuses, "opencode")
	if oc.Status != "ok" || oc.Note != "" {
		t.Fatalf("absent sibling should be invisible when sibling produced sessions, got %+v", oc)
	}
}

// A store removed after productive use is absence, not corruption: the scan
// reports "empty" with the note kept and no drift is flagged. Drift only
// fires when the store exists but can't be read.
func TestAgentSourceAbsentStoreNotDrift(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	dbPath := filepath.Join(dir, "opencode.db")
	driftEvents := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='agent_source_drift'`).Scan(&n)
		return n
	}

	// Productive scan marks the source as seen.
	writeOpencodeDB(t, dbPath, ocFixtureSession{
		id: "ses_1", title: "T", dir: "/x/p",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "real work",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
		},
	})
	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %+v", sessions)
	}
	recordAgentSourceScans(db, statuses)

	// Store removed entirely → "empty" + kept note, no drift flag/event.
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	sessions, statuses = scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
	recordAgentSourceScans(db, statuses)
	oc := sourceStatus(t, statuses, "opencode")
	if oc.Status != "empty" || oc.Note == "" {
		t.Fatalf("removed store should report empty + note, got %+v", oc)
	}
	if oc.Drift {
		t.Fatal("absent store must not flag drift")
	}
	if n := driftEvents(); n != 0 {
		t.Fatalf("absent store logged %d drift events", n)
	}

	// Store exists but is corrupt (message table present, required column
	// gone) → real degradation: unavailable + drift.
	writeOpencodeDBDrifted(t, dbPath, "ses_1", ms(day.Add(10*time.Hour)))
	sessions, statuses = scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
	recordAgentSourceScans(db, statuses)
	oc = sourceStatus(t, statuses, "opencode")
	if oc.Status != "unavailable" || !strings.Contains(oc.Note, "failed to read") {
		t.Fatalf("corrupt store should report unavailable + failure note, got %+v", oc)
	}
	if !oc.Drift {
		t.Fatal("present-but-corrupt store not flagged as drift")
	}
	if n := driftEvents(); n != 1 {
		t.Fatalf("expected 1 drift event, got %d", n)
	}
}

// A healthy-but-idle store reports "empty" through the real adapter path:
// schema valid, zero in-window sessions, no note.
func TestAgentSessionsOpencodeHealthyEmpty(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	writeOpencodeDB(t, filepath.Join(dir, "opencode.db")) // schema only
	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
	oc := sourceStatus(t, statuses, "opencode")
	if oc.Status != "empty" || oc.Note != "" {
		t.Fatalf("healthy-empty store should report empty, got %+v", oc)
	}
}

// The day window is [s,e): a message at exactly s is included, one at
// exactly e is excluded.
func TestAgentSessionsOpencodeDayBoundary(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	s, e := dayBounds(day)

	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"),
		ocFixtureSession{id: "lo", title: "Lo", dir: "/x/lo",
			msgs: []ocFixtureMsg{
				{id: "lo1", typ: "user", seq: 1, text: "at day start",
					created: ms(s), updated: ms(s)},
			}},
		ocFixtureSession{id: "hi", title: "Hi", dir: "/x/hi",
			msgs: []ocFixtureMsg{
				{id: "hi1", typ: "user", seq: 1, text: "at day end",
					created: ms(e), updated: ms(e)},
			}})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 || sessions[0].Title != "Lo" {
		t.Fatalf("boundary session mismatch, got %+v", sessions)
	}
	if sessions[0].Start != s.Unix() {
		t.Fatalf("start = %d, want %d (day open bound)", sessions[0].Start, s.Unix())
	}
}

// A NULL title/directory must not sink the day's whole candidate scan —
// the session falls back to the first user prompt for its title.
func TestAgentSessionsOpencodeNullTitleDir(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	db, err := sql.Open("sqlite", filepath.Join(dir, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT,
		  directory TEXT, time_created INTEGER, time_updated INTEGER)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT,
		  type TEXT, seq INTEGER, time_created INTEGER, time_updated INTEGER, data TEXT)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	ts := ms(day.Add(10 * time.Hour))
	if _, err := db.Exec(`INSERT INTO session_message
	  (id, session_id, type, seq, time_created, time_updated, data)
	  VALUES('m1','ses_null','user',1,?,?,'{"text":"null-title prompt"}')`, ts, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session
	  (id, title, directory, time_created, time_updated) VALUES('ses_null',NULL,NULL,?,?)`, ts, ts); err != nil {
		t.Fatal(err)
	}
	db.Close()

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 || sessions[0].Title != "null-title prompt" {
		t.Fatalf("NULL title/dir session not scanned: %+v", sessions)
	}
}

// Devin's message_nodes table present but with chat_message dropped is
// column-level schema drift: candidates resolve, per-session extraction
// fails — the note must read as unavailable, never "empty".
func TestAgentSessionsDevinColumnDrift(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)

	dbPath := filepath.Join(devin, "sessions.db")
	if err := os.MkdirAll(devin, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT,
		  created_at INTEGER, last_activity_at INTEGER, title TEXT)`,
		// message_nodes exists but dropped chat_message.
		`CREATE TABLE message_nodes (row_id INTEGER PRIMARY KEY,
		  session_id TEXT, node_id INTEGER, created_at INTEGER)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	ts := day.Add(10 * time.Hour).Unix()
	if _, err := db.Exec(`INSERT INTO sessions
	  (id, working_directory, created_at, last_activity_at, title)
	  VALUES('ses-1','/x/p',?,?,'T')`, ts, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO message_nodes
	  (session_id, node_id, created_at) VALUES('ses-1',1,?)`, ts); err != nil {
		t.Fatal(err)
	}
	db.Close()

	sessions, statuses := scanAgentSources(day)
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %+v", sessions)
	}
	st := devinStatus(t, statuses)
	if st.Status != "unavailable" || !strings.Contains(st.Note, "failed to read") {
		t.Fatalf("column drift should report unavailable + failure note, got %+v", st)
	}
}

// An in-place message rewrite — same node ids and timestamps, different
// body — must still invalidate the cached recap: the fingerprint folds in
// the chat_message byte length.
func TestDevinFingerprintTextRewrite(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)
	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	dbPath := filepath.Join(devin, "sessions.db")
	writeDevinDB(t, dbPath, dvFixtureSession{
		id: "ses-1", title: "T", dir: "/x/p",
		msgs: []dvFixtureMsg{
			{role: "user", text: "hi", created: day.Add(10 * time.Hour).Unix(), userInput: true},
		},
	})

	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	src := &devinSource{}
	fp1, ok := src.Fingerprint(sessions[0])
	if !ok {
		t.Fatal("fingerprint failed on scanned session")
	}

	// Rewrite the body in place — node_id and created_at untouched.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	cm, _ := json.Marshal(map[string]any{
		"message_id": "msg-1", "role": "user",
		"content":  "a much longer rewritten prompt body",
		"metadata": map[string]any{"is_user_input": true},
	})
	if _, err := db.Exec(`UPDATE message_nodes SET chat_message=?
	  WHERE session_id='ses-1' AND node_id=1`, string(cm)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	fp2, ok := src.Fingerprint(sessions[0])
	if !ok || fp2 == fp1 {
		t.Fatal("in-place body rewrite did not change fingerprint")
	}
}

// The mergeCards heuristic positive arm: adjacent blocks sharing
// app+category fold into one card even when the titles differ; the latest
// block's title wins.
func TestMergeCardsAdjacentSameApp(t *testing.T) {
	base := time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)
	mk := func(i int, title string) Block {
		s := base.Add(time.Duration(i*15) * time.Minute)
		return Block{Start: s, End: s.Add(15 * time.Minute), Title: title,
			App: "neovim", Category: "coding", Status: "done",
			StartStr: s.Format("15:04"), EndStr: s.Add(15 * time.Minute).Format("15:04")}
	}
	cards := mergeCards([]Block{mk(0, "Refactor engine"), mk(1, "Fix engine test")})
	if len(cards) != 1 || cards[0].Blocks != 2 {
		t.Fatalf("cards = %+v, want one merged card", cards)
	}
	if cards[0].Title != "Fix engine test" {
		t.Fatalf("latest title should win, got %q", cards[0].Title)
	}
}

// A composerHeaders workspaceId containing traversal or separators must
// never reach filepath.Join under workspaceStorage — the regexp gate
// rejects it outright, before any filesystem read.
func TestCursorWorkspaceFolderRejectsUnsafeID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_CURSOR_WORKSPACES", dir)
	// A trap file a traversal would resolve if the id weren't gated.
	trap := filepath.Join(filepath.Dir(dir), "escape-ws-trap")
	if err := os.MkdirAll(trap, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trap, "workspace.json"),
		[]byte(`{"folder":"file:///trap"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(trap)
	for _, wsid := range []string{"../escape-ws-trap", "a/b", "a b", "..", ""} {
		if got := cursorWorkspaceFolder(wsid); got != "" {
			t.Fatalf("cursorWorkspaceFolder(%q) = %q, want rejected", wsid, got)
		}
	}
}

// --- agentSource.Turns (briefing turn extraction) ---

func TestJSONLSourceTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	writeJSONL(t, path, []string{
		`{"type":"user","timestamp":"2026-09-15T10:00:00Z","cwd":"/home/x/proj","message":{"role":"user","content":"fix the build"}}`,
		`{"type":"assistant","timestamp":"2026-09-15T10:05:00Z","cwd":"/home/x/proj","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"user","timestamp":"2026-09-15T10:10:00Z","cwd":"/home/x/proj","message":{"role":"user","content":"<environment_context>cwd=/home/x</environment_context>"}}`,
		`{"type":"summary","summary":"not a message"}`,
	}, time.Date(2026, 9, 15, 12, 0, 0, 0, time.Local))
	src := jsonlSource{name: "claude", lineTurn: claudeLineTurn}
	turns := src.Turns(AgentSession{File: path})
	if len(turns) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(turns))
	}
	if turns[0].role != "user" || turns[0].text != "fix the build" || turns[0].unixTs == 0 || !turns[0].usableUser {
		t.Fatalf("bad user turn: %+v", turns[0])
	}
	if turns[1].role != "assistant" || turns[1].text != "done" {
		t.Fatalf("bad assistant turn: %+v", turns[1])
	}
	if turns[2].role != "user" || turns[2].usableUser {
		t.Fatalf("envelope turn must be non-usable: %+v", turns[2])
	}
	if got := src.Turns(AgentSession{File: filepath.Join(dir, "gone.jsonl")}); got != nil {
		t.Fatalf("missing file should give nil turns, got %d", len(got))
	}
}

func TestOpencodeTurns(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	day := time.Date(2026, 9, 20, 0, 0, 0, 0, time.Local)
	writeOpencodeDB(t, filepath.Join(dir, "opencode.db"), ocFixtureSession{
		id: "ses_1", title: "Adapter work", dir: "/home/x/dayflow",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "add the adapter",
				created: ms(day.Add(10 * time.Hour)), updated: ms(day.Add(10 * time.Hour))},
			{id: "m2", typ: "assistant", seq: 2, text: "done",
				created: ms(day.Add(10*time.Hour + time.Minute)), updated: ms(day.Add(10*time.Hour + time.Minute))},
		},
	})
	sessions, _ := scanAgentSources(day)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %+v", sessions)
	}
	turns := (&opencodeSource{}).Turns(sessions[0])
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %+v", turns)
	}
	if turns[0].role != "user" || turns[0].text != "add the adapter" || turns[0].unixTs == 0 {
		t.Fatalf("bad turn: %+v", turns[0])
	}
}
