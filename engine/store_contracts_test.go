package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// store_contracts_test.go — semantic drift-watch contracts (U6a / R6 / KTD5).
//
// Each test pins one source's store shape as a committed fixture under
// testdata/stores/ (.sql for the sqlite stores, .jsonl for the transcript
// roots) and asserts the adapter's own Scan — the same entry point
// `dayflow agents` drives — extracts the expected sentinel session. A
// schema assertion can't do this job: it false-positives on Cursor's two
// legal layouts and misses payload-shape drift. When upstream changes a
// store, `dayflow fixtures capture <source>` regenerates the fixture; the
// .sql diff shows the schema delta and these tests fail naming the source
// until the adapter catches up. See docs/maintenance.md.

// contractDay is the local day containing the fixtures' sentinel instant.
// Fixtures cluster timestamps around fixtureEpochS, so the day containing
// it always overlaps them regardless of test-timezone.
func contractDay() time.Time { return time.Unix(fixtureEpochS, 0) }

// applySQLFixture replays a committed .sql store fixture into a fresh
// database at dbPath. Statements are blocks of lines terminated by a line
// ending in ';'; comment-only chunks are skipped.
func applySQLFixture(t *testing.T, sqlPath, dbPath string) {
	t.Helper()
	raw, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stmt strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		stmt.WriteString(line + "\n")
		if !strings.HasSuffix(strings.TrimSpace(line), ";") {
			continue
		}
		chunk := stmt.String()
		stmt.Reset()
		blank := true
		for _, l := range strings.Split(chunk, "\n") {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "--") {
				blank = false
			}
		}
		if blank {
			continue
		}
		if _, err := db.Exec(chunk); err != nil {
			t.Fatalf("%s: statement failed: %v\n%s", filepath.Base(sqlPath), err, chunk)
		}
	}
	if rest := stmt.String(); strings.TrimSpace(rest) != "" {
		for _, l := range strings.Split(rest, "\n") {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "--") {
				t.Fatalf("%s: unterminated statement: %q", filepath.Base(sqlPath), rest)
			}
		}
	}
}

// stageTranscriptFixture installs a committed .jsonl fixture under root and
// stamps its mtime inside the contract day — jsonlFiles bounds on mtime.
func stageTranscriptFixture(t *testing.T, name, dstDir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "stores", name))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dstDir, name)
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	mt := contractDay().Add(11 * time.Hour)
	if err := os.Chtimes(dst, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func fixtureStore(name string) string {
	return filepath.Join("testdata", "stores", name+".sql")
}

// contractScan runs the full source scan for the contract day and returns
// this source's sessions plus its scan status.
func contractScan(t *testing.T, source string) ([]AgentSession, sourceScanStatus) {
	t.Helper()
	sessions, statuses := scanAgentSources(contractDay())
	return sessions, sourceStatus(t, statuses, source)
}

// requireContractSession is the shared contract assertion: the adapter
// extracted >=1 session from a fixture of its store's shape. Failure names
// the source — that is the drift alarm.
func requireContractSession(t *testing.T, source string, sessions []AgentSession, st sourceScanStatus) AgentSession {
	t.Helper()
	if len(sessions) == 0 {
		t.Fatalf("%s contract: adapter extracted no session from a fixture of its store shape "+
			"(status=%q note=%q) — upstream store drifted or fixture rotted; "+
			"regenerate per docs/maintenance.md", source, st.Status, st.Note)
	}
	s := sessions[0]
	if s.Source != source {
		t.Fatalf("%s contract: extracted session is from %q", source, s.Source)
	}
	if s.Messages < 2 || s.Start == 0 || s.End < s.Start {
		t.Fatalf("%s contract: session fields wrong — %+v", source, s)
	}
	return s
}

func TestStoreContractClaude(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	root := filepath.Join(dir, "claude")
	t.Setenv("DAYFLOW_CLAUDE_DIR", root)
	stageTranscriptFixture(t, "claude.jsonl", filepath.Join(root, "-fixture-proj"))

	sessions, st := contractScan(t, "claude")
	s := requireContractSession(t, "claude", sessions, st)
	if s.Title != "__fixture_title_1__" || s.Project != "proj" || s.Messages != 2 {
		t.Fatalf("claude contract: bad session fields — %+v", s)
	}
	if s.Start != fixtureEpochS {
		t.Fatalf("claude contract: start=%d want %d", s.Start, fixtureEpochS)
	}
}

func TestStoreContractCodex(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	root := filepath.Join(dir, "codex")
	t.Setenv("DAYFLOW_CODEX_DIR", root)
	stageTranscriptFixture(t, "codex.jsonl", filepath.Join(root, "2026", "01", "05"))

	sessions, st := contractScan(t, "codex")
	s := requireContractSession(t, "codex", sessions, st)
	if s.Title != "__fixture_title_1__" || s.Project != "proj" || s.Messages != 2 {
		t.Fatalf("codex contract: bad session fields — %+v", s)
	}
}

func TestStoreContractOpencode(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	applySQLFixture(t, fixtureStore("opencode"), filepath.Join(dir, "opencode.db"))

	sessions, st := contractScan(t, "opencode")
	s := requireContractSession(t, "opencode", sessions, st)
	if s.Title != "__fixture_title_1__" || s.Project != "proj" || s.Messages != 2 {
		t.Fatalf("opencode contract: bad session fields — %+v", s)
	}
	if s.Start != fixtureEpochS {
		t.Fatalf("opencode contract: start=%d want %d", s.Start, fixtureEpochS)
	}
	if !strings.HasPrefix(s.File, "opencode://") {
		t.Fatalf("opencode contract: bad File key %q", s.File)
	}
}

// The old-generation layout (message+part, session_message empty) is a
// second legal shape — the contract covers it, not just the current schema.
func TestStoreContractOpencodeOldLayout(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	applySQLFixture(t, fixtureStore("opencode-old"), filepath.Join(dir, "opencode.db"))

	sessions, st := contractScan(t, "opencode")
	s := requireContractSession(t, "opencode", sessions, st)
	if s.Title != "__fixture_title_1__" || s.Messages != 2 {
		t.Fatalf("opencode(old) contract: bad session fields — %+v", s)
	}
}

func TestStoreContractDevin(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)
	applySQLFixture(t, fixtureStore("devin"), filepath.Join(devin, "sessions.db"))

	sessions, st := contractScan(t, "devin")
	s := requireContractSession(t, "devin", sessions, st)
	if s.Title != "__fixture_title_1__" || s.Project != "proj" || s.Messages != 2 {
		t.Fatalf("devin contract: bad session fields — %+v", s)
	}
	if !strings.HasPrefix(s.File, "devin://sessions.db/") {
		t.Fatalf("devin contract: bad File key %q", s.File)
	}
}

func TestStoreContractCursor(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	applySQLFixture(t, fixtureStore("cursor"), filepath.Join(dir, "state.vscdb"))
	ws := filepath.Join(dir, "workspaceStorage", "fixture-ws-1")
	if err := os.MkdirAll(ws, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "workspace.json"),
		[]byte(`{"folder": "file:///fixture/proj"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	sessions, st := contractScan(t, "cursor")
	s := requireContractSession(t, "cursor", sessions, st)
	if s.Title != "__fixture_title_1__" || s.Project != "proj" || s.Messages != 2 {
		t.Fatalf("cursor contract: bad session fields — %+v", s)
	}
	if s.File != "cursor://__fixture_composer_1__" {
		t.Fatalf("cursor contract: bad File key %q", s.File)
	}
}

// Cursor's composerData-only layout (no composerHeaders table) is the
// second legal shape — the contract covers the fallback path too.
func TestStoreContractCursorFallbackLayout(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	applySQLFixture(t, fixtureStore("cursor-fallback"), filepath.Join(dir, "state.vscdb"))

	sessions, st := contractScan(t, "cursor")
	s := requireContractSession(t, "cursor", sessions, st)
	if s.Title != "__fixture_title_1__" || s.Messages != 2 {
		t.Fatalf("cursor(fallback) contract: bad session fields — %+v", s)
	}
}

// --- Drift-negative cases: a fixture whose shape moved must fail the
// extraction contract, and DB sources must surface "unavailable" (naming
// the missing/renamed column where the adapter sees it), never "empty". ---

func TestStoreContractDriftOpencodeRenamedColumn(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	dbPath := filepath.Join(dir, "opencode.db")
	applySQLFixture(t, fixtureStore("opencode"), dbPath)
	alterStore(t, dbPath, `ALTER TABLE session RENAME COLUMN title TO label`)

	sessions, st := contractScan(t, "opencode")
	if len(sessions) != 0 || st.Status != "unavailable" || !strings.Contains(st.Note, "s.title") {
		t.Fatalf("opencode column drift should report unavailable naming the column, got status=%q note=%q sessions=%v",
			st.Status, st.Note, sessions)
	}
}

func TestStoreContractDriftOpencodeRenamedPayload(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	dbPath := filepath.Join(dir, "opencode.db")
	applySQLFixture(t, fixtureStore("opencode"), dbPath)
	alterStore(t, dbPath, `ALTER TABLE session_message RENAME COLUMN data TO payload`)

	sessions, st := contractScan(t, "opencode")
	if len(sessions) != 0 || st.Status != "unavailable" || !strings.Contains(st.Note, "failed to read") {
		t.Fatalf("opencode payload drift should report unavailable, got status=%q note=%q", st.Status, st.Note)
	}
}

func TestStoreContractDriftDevinRenamedColumn(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)
	dbPath := filepath.Join(devin, "sessions.db")
	applySQLFixture(t, fixtureStore("devin"), dbPath)
	alterStore(t, dbPath, `ALTER TABLE sessions RENAME COLUMN title TO label`)

	sessions, st := contractScan(t, "devin")
	if len(sessions) != 0 || st.Status != "unavailable" || !strings.Contains(st.Note, "s.title") {
		t.Fatalf("devin column drift should report unavailable naming the column, got status=%q note=%q",
			st.Status, st.Note)
	}
}

func TestStoreContractDriftDevinRenamedPayload(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	devin := filepath.Join(dir, "devin")
	t.Setenv("DAYFLOW_DEVIN_DIR", devin)
	dbPath := filepath.Join(devin, "sessions.db")
	applySQLFixture(t, fixtureStore("devin"), dbPath)
	alterStore(t, dbPath, `ALTER TABLE message_nodes RENAME COLUMN chat_message TO body`)

	sessions, st := contractScan(t, "devin")
	if len(sessions) != 0 || st.Status != "unavailable" || !strings.Contains(st.Note, "failed to read") {
		t.Fatalf("devin payload drift should report unavailable, got status=%q note=%q", st.Status, st.Note)
	}
}

func TestStoreContractDriftCursorRenamedColumn(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	dbPath := filepath.Join(dir, "state.vscdb")
	applySQLFixture(t, fixtureStore("cursor"), dbPath)
	alterStore(t, dbPath, `ALTER TABLE composerHeaders RENAME COLUMN createdAt TO created`)

	sessions, st := contractScan(t, "cursor")
	if len(sessions) != 0 || st.Status != "unavailable" || !strings.Contains(st.Note, "createdAt") {
		t.Fatalf("cursor column drift should report unavailable naming the column, got status=%q note=%q",
			st.Status, st.Note)
	}
}

// JSONL sources have no schema note vocabulary — transcript shape drift
// reads as silence (zero extracted sessions), which the contract pins so
// the day the adapter gains a vocabulary the test says so.
func TestStoreContractDriftClaudeRenamedField(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir)
	root := filepath.Join(dir, "claude")
	t.Setenv("DAYFLOW_CLAUDE_DIR", root)
	writeJSONL(t, filepath.Join(root, "-fixture-proj", "s.jsonl"), []string{
		// "timestamp" renamed to "ts" — upstream line-shape drift.
		`{"type":"user","ts":"2026-01-05T10:00:00Z","cwd":"/fixture/proj","message":{"role":"user","content":"__fixture_title_1__"}}`,
	}, contractDay().Add(11*time.Hour))

	sessions, st := contractScan(t, "claude")
	if len(sessions) != 0 {
		t.Fatalf("claude drifted transcript must yield no session, got %+v", sessions)
	}
	if st.Status == "ok" {
		t.Fatalf("claude drifted transcript must not report ok, got %+v", st)
	}
}

// An absent store is absence, not failure — the status taxonomy the doctor
// check relies on.
func TestStoreContractAbsentStoreIsEmptyNotDrift(t *testing.T) {
	dir := t.TempDir()
	setAgentDirs(t, dir) // every override points at a path that does not exist

	for _, source := range []string{"claude", "codex", "opencode", "devin", "cursor"} {
		_, statuses := scanAgentSources(contractDay())
		st := sourceStatus(t, statuses, source)
		if st.Status != "empty" || st.Drift {
			t.Fatalf("%s: absent store must report empty, not fail/drift — got %+v", source, st)
		}
	}
}

func alterStore(t *testing.T, dbPath, ddl string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
}

// --- fixtures capture ---

// Capture must emit schema + sentinel rows and nothing else: no real row
// payloads, ids, paths, or titles from the source store.
func TestFixturesCaptureSchemaOnly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	writeOpencodeDB(t, dbPath, ocFixtureSession{
		id: "ses_realsecret", title: "real session title", dir: "/home/real/proj",
		msgs: []ocFixtureMsg{
			{id: "rm1", typ: "user", seq: 1, text: "real user prompt secret-token-123",
				created: ms(contractDay().Add(10 * time.Hour)), updated: ms(contractDay().Add(10 * time.Hour))},
			{id: "rm2", typ: "assistant", seq: 2, text: "real assistant reply body",
				created: ms(contractDay().Add(10*time.Hour + time.Minute)), updated: ms(contractDay().Add(10*time.Hour + time.Minute))},
		},
	})
	out := filepath.Join(dir, "captured.sql")
	if err := captureFixture("opencode", dbPath, out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"CREATE TABLE session", "session_message", "pragma_table_info",
		"__fixture_title_1__", "INSERT INTO",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("captured fixture missing %q:\n%s", want, s)
		}
	}
	for _, leak := range []string{
		"ses_realsecret", "real session title", "secret-token-123",
		"real user prompt", "real assistant reply body", "/home/real/proj",
	} {
		if strings.Contains(s, leak) {
			t.Fatalf("captured fixture leaked real content %q:\n%s", leak, s)
		}
	}
}

// The captured fixture must itself satisfy the contract — the regeneration
// loop produces a store the adapter can still extract from.
func TestFixturesCaptureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	srcDB := filepath.Join(dir, "src", "opencode.db")
	if err := os.MkdirAll(filepath.Dir(srcDB), 0o700); err != nil {
		t.Fatal(err)
	}
	writeOpencodeDB(t, srcDB, ocFixtureSession{
		id: "ses_real", title: "ignored", dir: "/x/real",
		msgs: []ocFixtureMsg{
			{id: "m1", typ: "user", seq: 1, text: "real content",
				created: ms(contractDay().Add(10 * time.Hour)), updated: ms(contractDay().Add(10 * time.Hour))},
		},
	})
	out := filepath.Join(dir, "opencode.sql")
	if err := captureFixture("opencode", srcDB, out); err != nil {
		t.Fatal(err)
	}

	setAgentDirs(t, dir)
	applySQLFixture(t, out, filepath.Join(dir, "opencode.db"))
	sessions, st := contractScan(t, "opencode")
	s := requireContractSession(t, "opencode", sessions, st)
	if s.Title != "__fixture_title_1__" {
		t.Fatalf("round-trip fixture session title = %q", s.Title)
	}
}

func TestFixturesCaptureGuards(t *testing.T) {
	dir := t.TempDir()
	// A store path that doesn't look like the source's known location must
	// be refused — capture reads schema metadata, never arbitrary DBs.
	other := filepath.Join(dir, "unrelated.db")
	db, err := sql.Open("sqlite", other)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE t (id INTEGER)`)
	db.Close()

	for _, tc := range []struct {
		source, db string
	}{
		{"opencode", other},
		{"devin", other},
		{"cursor", other}, // no .vscdb suffix
	} {
		if err := captureFixture(tc.source, tc.db, filepath.Join(dir, "x.sql")); err == nil {
			t.Fatalf("%s: capture accepted foreign store %q", tc.source, tc.db)
		}
	}
	// Unknown source / bad argv / missing store.
	if err := runFixtures([]string{"capture", "notasource"}); err == nil {
		t.Fatal("unknown source accepted")
	}
	if err := runFixtures(nil); err == nil {
		t.Fatal("missing args accepted")
	}
	if err := runFixtures([]string{"capture", "--db"}); err == nil {
		t.Fatal("flag-as-source accepted")
	}
	if err := captureFixture("opencode", filepath.Join(dir, "opencode.db"), filepath.Join(dir, "x.sql")); err == nil {
		t.Fatal("missing store file accepted")
	}
	// --db on a transcript source is meaningless.
	if err := captureFixture("claude", other, filepath.Join(dir, "x.jsonl")); err == nil {
		t.Fatal("--db accepted for a JSONL source")
	}
}

func TestFixturesCaptureTranscript(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "claude.jsonl")
	if err := captureFixture("claude", "", out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "__fixture_title_1__") ||
		!strings.Contains(string(raw), `"timestamp"`) {
		t.Fatalf("claude transcript fixture malformed: %s", raw)
	}
}

func TestOpenStoreProbe(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	writeOpencodeDB(t, dbPath, ocFixtureSession{
		id: "s1", title: "t", dir: "/x",
		msgs: []ocFixtureMsg{{id: "m1", typ: "user", seq: 1, text: "x",
			created: ms(contractDay()), updated: ms(contractDay())}},
	})
	db, err := openStoreProbe(dbPath)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("probe schema read failed: n=%d err=%v", n, err)
	}
	db.Close()
	if _, err := openStoreProbe(filepath.Join(dir, "missing.db")); err == nil {
		t.Fatal("probe of missing store must fail, not temp-copy")
	}
}
