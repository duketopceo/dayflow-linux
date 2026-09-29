package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Store drift-watch fixtures (U6a / R6 / KTD5). The committed contract
// fixtures under testdata/stores/ are text: captured CREATE TABLE DDL,
// pragma_table_info comments, and synthesized sentinel rows — never real
// payloads and never binary sqlite (undiffable, and the freelist retains
// deleted plaintext).
//
// `dayflow fixtures capture <source> [--db P] [--out F]` regenerates a
// fixture from a live store. For the sqlite-backed sources it reads schema
// only (sqlite_master + pragma_table_info) and synthesizes __fixture_*__
// sentinel rows that exercise the adapter's extraction contract. For the
// JSONL transcript sources (claude, codex) there is no schema to read —
// the fixture is a canonical sentinel transcript. Stores are opened
// read-only (mode=ro, then immutable) with no temp-copy fallback: Devin's
// sessions.db can be gigabytes and Cursor's state.vscdb hundreds of MB, so
// a schema probe must never pay that copy. See docs/maintenance.md.

// fixtureEpochS is the canonical instant sentinel rows anchor to
// (2026-01-05T10:00:00Z). Contract tests scan the local day containing it,
// so no timezone assumption leaks into generated fixtures.
const fixtureEpochS = 1767607200

// Sentinel values — fixed strings make generated diffs reviewable and make
// a real payload leaking into a fixture obvious.
const (
	fixtureSessionID = "__fixture_session_1__"
	fixtureTitle     = "__fixture_title_1__"
	fixtureAssistant = "__fixture_assistant_1__"
	fixtureDir       = "/fixture/proj"
	fixtureComposer  = "__fixture_composer_1__"
	fixtureWSID      = "fixture-ws-1"
)

// fixtureOutDir is the default fixture destination relative to the engine
// module root — run `fixtures capture` from engine/.
const fixtureOutDir = "testdata/stores"

func fixtureTime(off time.Duration) time.Time {
	return time.Unix(fixtureEpochS, 0).UTC().Add(off)
}

// openStoreProbe opens a sqlite store read-only for schema probing:
// mode=ro first, then mode=ro&immutable=1 (an immutable open skips the shm
// handshake a live WAL store may refuse). Unlike openROStore it never
// falls back to a temp-dir copy — the caller gets a fast failure instead
// of a multi-GB file copy. The doctor agent-stores check uses this same
// probe (one drift vocabulary, one open discipline).
func openStoreProbe(path string) (*sql.DB, error) {
	probes := []string{
		"?mode=ro&_pragma=busy_timeout(3000)&_pragma=query_only(1)",
		"?mode=ro&immutable=1&_pragma=busy_timeout(3000)&_pragma=query_only(1)",
	}
	var lastErr error
	for _, q := range probes {
		db, err := sql.Open("sqlite", "file:"+path+q)
		if err != nil {
			lastErr = err
			continue
		}
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil {
			lastErr = err
			db.Close()
			continue
		}
		return db, nil
	}
	return nil, fmt.Errorf("read-only open failed: %w", lastErr)
}

// agentStoreDBs resolves the sqlite store path(s) each DB-backed agent
// source reads, via the adapters' own resolvers (including the DAYFLOW_*
// test overrides). The doctor-side agent-stores check iterates this map,
// opens each path with openStoreProbe, and asserts adapter-level
// extraction — never copying a store to a temp dir.
func agentStoreDBs() map[string][]string {
	return map[string][]string{
		"opencode": opencodeDBPaths(),
		"devin":    {filepath.Join(devinDir(), "sessions.db")},
		"cursor":   {cursorDBPath()},
	}
}

// fixtureStorePaths is the same resolution for the capture command.
func fixtureStorePaths(source string) ([]string, error) {
	paths, ok := agentStoreDBs()[source]
	if !ok {
		return nil, fmt.Errorf("unknown source %q (want claude|codex|opencode|devin|cursor)", source)
	}
	return paths, nil
}

// firstStorePath returns the first existing resolved store for a source.
func firstStorePath(source string) (string, error) {
	paths, err := fixtureStorePaths(source)
	if err != nil {
		return "", err
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no %s store found", source)
}

// fixturePathOK is the location-shape guard: capture only runs against
// paths that look like that source's known store, so a mispointed --db or
// env override can't dump an arbitrary database's schema.
func fixturePathOK(source, path string) bool {
	base := filepath.Base(path)
	switch source {
	case "opencode":
		return strings.HasPrefix(base, "opencode") && strings.HasSuffix(base, ".db")
	case "devin":
		return base == "sessions.db"
	case "cursor":
		// .vscdb is the VS Code-family editor state db suffix.
		return strings.HasSuffix(base, ".vscdb")
	}
	return false
}

// fixtureFileName names the emitted fixture after the store: opencode
// keeps the store basename (opencode.db/opencode-next.db are two
// generations); the other sources emit one file per source.
func fixtureFileName(source, storePath string) string {
	if source == "opencode" {
		return strings.TrimSuffix(filepath.Base(storePath), ".db") + ".sql"
	}
	return source + ".sql"
}

// fixtureColumn is one pragma_table_info row — the writable-column view
// (generated columns are hidden from table_info, so INSERTs built from it
// only ever name real columns).
type fixtureColumn struct {
	name, declType string
	notnull        bool
	hasDefault     bool
	pk             int
}

// fixtureTable is one user table's captured DDL + columns.
type fixtureTable struct {
	name string
	ddl  string
	cols []fixtureColumn
}

// captureStoreSchema extracts the full table schema: sqlite_master DDL
// plus pragma_table_info per table. Deterministic order so fixtures diff
// cleanly.
func captureStoreSchema(db *sql.DB) ([]fixtureTable, error) {
	rows, err := db.Query(`SELECT name, COALESCE(sql,'') FROM sqlite_master
	  WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []fixtureTable
	for rows.Next() {
		var t fixtureTable
		if err := rows.Scan(&t.name, &t.ddl); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range tables {
		ci, err := db.Query(`SELECT name, COALESCE(type,''), "notnull",
		  dflt_value IS NOT NULL, pk FROM pragma_table_info(?)`, tables[i].name)
		if err != nil {
			return nil, err
		}
		for ci.Next() {
			var c fixtureColumn
			var nn, hd int
			if err := ci.Scan(&c.name, &c.declType, &nn, &hd, &c.pk); err != nil {
				ci.Close()
				return nil, err
			}
			c.notnull, c.hasDefault = nn != 0, hd != 0
			tables[i].cols = append(tables[i].cols, c)
		}
		ci.Close()
	}
	return tables, nil
}

// sqlLit renders a Go value as a sqlite literal for generated INSERTs.
func sqlLit(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "1"
		}
		return "0"
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprint(t), "'", "''") + "'"
	}
}

// fillerValue synthesizes a type-shaped placeholder for a NOT NULL column
// the sentinel row doesn't name — e.g. a column upstream added after the
// fixture was authored. Keeps the INSERT applicable against the captured
// shape.
func fillerValue(declType string) any {
	d := strings.ToUpper(declType)
	switch {
	case strings.Contains(d, "INT"), strings.Contains(d, "REAL"),
		strings.Contains(d, "FLOA"), strings.Contains(d, "DOUB"):
		return int64(0)
	default:
		return ""
	}
}

// fixtureInsert emits one INSERT covering every schema column: sentinel
// values where the row names them, NULL for the rest (a NOT NULL column
// gets a type-shaped filler — passing NULL never triggers its DEFAULT;
// only omitting the column would). Row keys the schema lacks are reported
// in a comment — drift made visible in the fixture diff.
func fixtureInsert(b *strings.Builder, t fixtureTable, row map[string]any, warned map[string]bool) {
	colSet := map[string]bool{}
	var names, vals []string
	for _, c := range t.cols {
		colSet[c.name] = true
		v, ok := row[c.name]
		if !ok {
			if c.notnull {
				v = fillerValue(c.declType)
			} else {
				v = nil
			}
		}
		names = append(names, `"`+strings.ReplaceAll(c.name, `"`, `""`)+`"`)
		vals = append(vals, sqlLit(v))
	}
	for k := range row {
		if !colSet[k] && !warned[t.name+"."+k] {
			warned[t.name+"."+k] = true
			fmt.Fprintf(b, "-- contract column %s.%s absent in captured schema\n", t.name, k)
		}
	}
	if len(names) == 0 {
		fmt.Fprintf(b, "-- %s: no writable columns; sentinel row omitted\n", t.name)
		return
	}
	fmt.Fprintf(b, "INSERT INTO %q (%s) VALUES(%s);\n",
		strings.ReplaceAll(t.name, `"`, `""`),
		strings.Join(names, ", "), strings.Join(vals, ", "))
}

// sentinelRow is one synthesized row bound for a table.
type sentinelRow struct {
	table string
	row   map[string]any
}

func fixtureMs(off time.Duration) int64 { return fixtureTime(off).UnixMilli() }
func fixtureSec(off time.Duration) int64 { return fixtureTime(off).Unix() }

// fixtureSentinels returns the sentinel rows a captured fixture inserts,
// in emission order. For opencode, layout (the adapter's own generation
// picker) decides which message-table generation gets rows — the other
// generation's table stays empty so the fixture pins the layout actually
// captured.
func fixtureSentinels(source string, db *sql.DB) []sentinelRow {
	switch source {
	case "opencode":
		var rows []sentinelRow
		sessionRow := sentinelRow{"session", map[string]any{
			"id": fixtureSessionID, "title": fixtureTitle, "directory": fixtureDir,
			"time_created": fixtureMs(0), "time_updated": fixtureMs(5 * time.Minute)}}
		nextMsgs := func() []sentinelRow {
			return []sentinelRow{
				{"session_message", map[string]any{
					"id": "__fixture_msg_1__", "session_id": fixtureSessionID,
					"type": "user", "seq": 1,
					"time_created": fixtureMs(0), "time_updated": fixtureMs(0),
					"data": `{"text":"` + fixtureTitle + `"}`}},
				{"session_message", map[string]any{
					"id": "__fixture_msg_2__", "session_id": fixtureSessionID,
					"type": "assistant", "seq": 2,
					"time_created": fixtureMs(5 * time.Minute), "time_updated": fixtureMs(5 * time.Minute),
					"data": `{"content":[{"type":"text","text":"` + fixtureAssistant + `"}]}`}},
			}
		}
		oldMsgs := func() []sentinelRow {
			var out []sentinelRow
			for i, m := range []struct {
				typ, text string
				off       time.Duration
			}{{"user", fixtureTitle, 0}, {"assistant", fixtureAssistant, 5 * time.Minute}} {
				mid := "__fixture_msg_" + strconv.Itoa(i+1) + "__"
				out = append(out,
					sentinelRow{"message", map[string]any{
						"id": mid, "session_id": fixtureSessionID,
						"time_created": fixtureMs(m.off), "time_updated": fixtureMs(m.off),
						"data": `{"role":"` + m.typ + `"}`}},
					sentinelRow{"part", map[string]any{
						"id": "__fixture_part_" + strconv.Itoa(i+1) + "__",
						"message_id": mid, "session_id": fixtureSessionID,
						"time_created": fixtureMs(m.off), "time_updated": fixtureMs(m.off),
						"data": `{"type":"text","text":"` + m.text + `"}`}})
			}
			return out
		}
		// Which message-table generation gets sentinel rows is decided by
		// the adapter's own layout pick on the live store — the fixture
		// reproduces the layout it captured.
		switch opencodeLayout(db) {
		case "next":
			rows = append([]sentinelRow{sessionRow}, nextMsgs()...)
		case "old":
			rows = append([]sentinelRow{sessionRow}, oldMsgs()...)
		default:
			// No data to pick a layout from — emit both generations so the
			// fixture still exercises whichever the schema carries.
			rows = append([]sentinelRow{sessionRow}, nextMsgs()...)
			rows = append(rows, oldMsgs()...)
		}
		return rows

	case "devin":
		return []sentinelRow{
			{"sessions", map[string]any{
				"id": fixtureSessionID, "working_directory": fixtureDir,
				"backend_type": "__fixture_backend_type__", "model": "__fixture_model__",
				"agent_mode": "__fixture_agent_mode__",
				"created_at": fixtureSec(0), "last_activity_at": fixtureSec(5 * time.Minute),
				"title": fixtureTitle}},
			{"message_nodes", map[string]any{
				"session_id": fixtureSessionID, "node_id": 1, "parent_node_id": 0,
				"chat_message": `{"message_id":"__fixture_msg_1__","role":"user","content":"` +
					fixtureTitle + `","metadata":{"is_user_input":true}}`,
				"created_at": fixtureSec(0)}},
			{"message_nodes", map[string]any{
				"session_id": fixtureSessionID, "node_id": 2, "parent_node_id": 0,
				"chat_message": `{"message_id":"__fixture_msg_2__","role":"assistant","content":"` +
					fixtureAssistant + `","metadata":{}}`,
				"created_at": fixtureSec(5 * time.Minute)}},
		}

	case "cursor":
		head := `{"type":"head","composerId":"` + fixtureComposer + `","createdAt":` +
			strconv.FormatInt(fixtureMs(0), 10) + `,"lastUpdatedAt":` +
			strconv.FormatInt(fixtureMs(5*time.Minute), 10) +
			`,"name":"` + fixtureTitle + `","isDraft":false}`
		data := `{"_v":18,"composerId":"` + fixtureComposer + `","createdAt":` +
			strconv.FormatInt(fixtureMs(0), 10) + `,"lastUpdatedAt":` +
			strconv.FormatInt(fixtureMs(5*time.Minute), 10) +
			`,"name":"` + fixtureTitle +
			`","fullConversationHeadersOnly":[` +
			`{"bubbleId":"__fixture_bubble_1__","type":1},` +
			`{"bubbleId":"__fixture_bubble_2__","type":2}],` +
			`"conversationMap":{}}`
		return []sentinelRow{
			{"composerHeaders", map[string]any{
				"composerId": fixtureComposer, "workspaceId": fixtureWSID,
				"createdAt": fixtureMs(0), "lastUpdatedAt": fixtureMs(5 * time.Minute),
				"isArchived": 0, "isSubagent": 0, "recency": 0, "checkpointAt": 0,
				"subagentTypeName": "__fixture_subagentTypeName__", "value": head}},
			{"cursorDiskKV", map[string]any{
				"key": "composerData:" + fixtureComposer, "value": data}},
			{"cursorDiskKV", map[string]any{
				"key": "bubbleId:" + fixtureComposer + ":__fixture_bubble_1__",
				"value": `{"type":1,"bubbleId":"__fixture_bubble_1__","text":"` +
					fixtureTitle + `","createdAt":` + strconv.FormatInt(fixtureMs(0), 10) + `}`}},
			{"cursorDiskKV", map[string]any{
				"key": "bubbleId:" + fixtureComposer + ":__fixture_bubble_2__",
				"value": `{"type":2,"bubbleId":"__fixture_bubble_2__","text":"` +
					fixtureAssistant + `","createdAt":` + strconv.FormatInt(fixtureMs(5*time.Minute), 10) + `}`}},
		}
	}
	return nil
}

// emitFixtureText renders the .sql fixture: header comments, then per
// table the captured DDL, a pragma_table_info summary line, and that
// table's sentinel INSERTs. Contract-relevant tables absent from the
// captured schema get warning comments at the end — a dropped table must
// be visible in the diff, not silently skipped.
func emitFixtureText(source, storeBase string, tables []fixtureTable, sents []sentinelRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-- dayflow agent-store contract fixture: %s\n", source)
	fmt.Fprintf(&b, "-- captured from: %s (schema + synthesized sentinel rows only — never real row payloads)\n", storeBase)
	fmt.Fprintf(&b, "-- regenerate:    dayflow fixtures capture %s\n", source)
	fmt.Fprintf(&b, "-- sentinel instant: %s (unix %d) — contract tests scan its local day\n",
		fixtureTime(0).Format("2006-01-02T15:04:05Z"), fixtureEpochS)
	fmt.Fprintf(&b, "-- contract:      store_contracts_test.go replays this file and asserts the %s adapter\n", source)
	b.WriteString("--                extracts >=1 session; failure means upstream store drift.\n")
	b.WriteString("--                See docs/maintenance.md.\n")

	sentByTable := map[string][]sentinelRow{}
	var sentTableOrder []string
	for _, s := range sents {
		if _, seen := sentByTable[s.table]; !seen {
			sentTableOrder = append(sentTableOrder, s.table)
		}
		sentByTable[s.table] = append(sentByTable[s.table], s)
	}
	warned := map[string]bool{}
	emitted := map[string]bool{}
	for _, t := range tables {
		emitted[t.name] = true
		b.WriteString("\n")
		if t.ddl != "" {
			b.WriteString(strings.TrimSpace(t.ddl))
			b.WriteString(";\n")
		} else {
			fmt.Fprintf(&b, "-- table %s (no DDL in sqlite_master)\n", t.name)
		}
		var cols []string
		for _, c := range t.cols {
			d := c.name + " " + c.declType
			if c.pk > 0 {
				d += " PK"
			}
			if c.notnull {
				d += " NOT NULL"
			}
			if c.hasDefault {
				d += " DEFAULT"
			}
			cols = append(cols, strings.TrimSpace(d))
		}
		fmt.Fprintf(&b, "-- pragma_table_info(%s): %s\n", t.name, strings.Join(cols, ", "))
		for _, s := range sentByTable[t.name] {
			fixtureInsert(&b, t, s.row, warned)
		}
	}
	for _, table := range sentTableOrder {
		if !emitted[table] {
			fmt.Fprintf(&b, "\n-- %s: table absent in captured schema; sentinel rows omitted\n", table)
		}
	}
	return b.String()
}

// transcriptFixture renders the canonical sentinel JSONL transcript for a
// file-backed source — there is no schema to capture, so the fixture is a
// fixed synthetic transcript in the line shape the adapter parses.
func transcriptFixture(source string) (string, error) {
	ts := func(off time.Duration) string {
		return fixtureTime(off).Format(time.RFC3339Nano)
	}
	switch source {
	case "claude":
		return fmt.Sprintf(
			`{"type":"user","timestamp":%q,"cwd":%q,"message":{"role":"user","content":%q}}`+"\n"+
				`{"type":"assistant","timestamp":%q,"cwd":%q,"message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n",
			ts(0), fixtureDir, fixtureTitle,
			ts(5*time.Minute), fixtureDir, fixtureAssistant), nil
	case "codex":
		return fmt.Sprintf(
			`{"timestamp":%q,"type":"session_meta","payload":{"cwd":%q,"session_id":%q}}`+"\n"+
				`{"timestamp":%q,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}}`+"\n"+
				`{"timestamp":%q,"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}}`+"\n",
			ts(0), fixtureDir, fixtureSessionID,
			ts(time.Minute), fixtureTitle,
			ts(5*time.Minute), fixtureAssistant), nil
	}
	return "", fmt.Errorf("no transcript fixture for %q", source)
}

// captureFixture writes one contract fixture for a source. For
// sqlite-backed sources dbPath names the live store ("" = resolve the
// source's default path); the emitted file holds DDL + pragma_table_info
// comments + sentinel INSERTs — row payloads are never read. For the
// JSONL sources dbPath must be empty; the fixture is the canonical
// sentinel transcript.
func captureFixture(source, dbPath, outPath string) error {
	switch source {
	case "claude", "codex":
		if dbPath != "" {
			return fmt.Errorf("--db is meaningless for %s — transcript stores are JSONL files, not sqlite", source)
		}
		body, err := transcriptFixture(source)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(outPath, []byte(body), 0o644)
	case "opencode", "devin", "cursor":
	default:
		return fmt.Errorf("unknown source %q (want claude|codex|opencode|devin|cursor)", source)
	}

	if dbPath == "" {
		p, err := firstStorePath(source)
		if err != nil {
			return err
		}
		dbPath = p
	}
	if !fixturePathOK(source, dbPath) {
		return fmt.Errorf("%q doesn't look like a %s store (%s) — refusing to capture an unrelated database",
			dbPath, source, map[string]string{
				"opencode": "opencode*.db",
				"devin":    "sessions.db",
				"cursor":   "*.vscdb",
			}[source])
	}
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("store not found: %w", err)
	}
	db, err := openStoreProbe(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	tables, err := captureStoreSchema(db)
	if err != nil {
		return fmt.Errorf("schema read failed: %w", err)
	}
	text := emitFixtureText(source, filepath.Base(dbPath), tables, fixtureSentinels(source, db))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, []byte(text), 0o644)
}

// runFixtures implements `dayflow fixtures capture <source> [--db P]
// [--out F]`. With neither flag it captures every resolved store for the
// source to its canonical file under testdata/stores/ (run from engine/).
func runFixtures(args []string) error {
	pos := positionalArgs(args)
	if len(pos) < 2 || pos[0] != "capture" {
		return fmt.Errorf("usage: dayflow fixtures capture <source> [--db <store>] [--out <file>]  (sources: claude codex opencode devin cursor)")
	}
	source := pos[1]
	dbPath, outPath := flagValue(args, "--db"), flagValue(args, "--out")

	switch source {
	case "claude", "codex":
		if dbPath != "" {
			return fmt.Errorf("--db is meaningless for %s — transcript stores are JSONL files, not sqlite", source)
		}
		if outPath == "" {
			outPath = filepath.Join(fixtureOutDir, source+".jsonl")
		}
		if err := captureFixture(source, "", outPath); err != nil {
			return err
		}
		fmt.Println("wrote", outPath)
		return nil
	case "opencode", "devin", "cursor":
	default:
		return fmt.Errorf("unknown source %q (want claude|codex|opencode|devin|cursor)", source)
	}

	if dbPath != "" || outPath != "" {
		// Single-store capture — layout variants (e.g. opencode-old.sql,
		// cursor-fallback.sql) are produced by pointing --db at a store of
		// that generation and --out at the variant's fixture file.
		if dbPath == "" {
			p, err := firstStorePath(source)
			if err != nil {
				return err
			}
			dbPath = p
		}
		if outPath == "" {
			outPath = filepath.Join(fixtureOutDir, fixtureFileName(source, dbPath))
		}
		if err := captureFixture(source, dbPath, outPath); err != nil {
			return err
		}
		fmt.Println("wrote", outPath)
		return nil
	}

	paths, err := fixtureStorePaths(source)
	if err != nil {
		return err
	}
	wrote := 0
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			fmt.Printf("fixtures: %s store %s not found, skipped\n", source, filepath.Base(p))
			continue
		}
		out := filepath.Join(fixtureOutDir, fixtureFileName(source, p))
		if err := captureFixture(source, p, out); err != nil {
			return err
		}
		fmt.Println("wrote", out)
		wrote++
	}
	if wrote == 0 {
		return fmt.Errorf("no %s store found to capture", source)
	}
	return nil
}
