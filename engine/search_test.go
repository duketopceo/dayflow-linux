package main

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// writePreFTSDB builds a v3-era database on disk: all tables and column
// patches through schema v3, stamped in schema_migrations, with one block —
// the state a pre-FTS install migrates from.
func writePreFTSDB(t *testing.T) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{schema, schemaV2, schemaV3} {
		if _, err := raw.Exec(ddl); err != nil {
			raw.Close()
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(1,1),(2,1),(3,1)`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := applyColumnPatches(raw); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	return raw
}

func seedBlock(t *testing.T, db *sql.DB, start time.Time, title, summary, category, app, status string) {
	t.Helper()
	if err := upsertBlockFull(db, start, start.Add(15*time.Minute), title, summary, category,
		app, "", 3, 0, status, "", nil); err != nil {
		t.Fatal(err)
	}
}

func ftsCount(t *testing.T, db *sql.DB, term string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM blocks_fts WHERE blocks_fts MATCH ?`, term).Scan(&n); err != nil {
		t.Fatalf("MATCH %q: %v", term, err)
	}
	return n
}

func TestFTSMigrationBackfill(t *testing.T) {
	testEnv(t)
	raw := writePreFTSDB(t)
	if _, err := raw.Exec(`INSERT INTO blocks(start_ts,end_ts,title,summary,category,app,status,created_at)
	  VALUES(1000,1900,'legacy alpha block','worked on alpha tooling','coding','nvim','done',1)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if v := dbSchemaVersion(db); v != schemaVersion {
		t.Fatalf("schema version = %d, want %d", v, schemaVersion)
	}
	if !ftsIndexPresent(db) {
		t.Fatal("blocks_fts missing after migration")
	}
	if ftsCount(t, db, "alpha") != 1 {
		t.Fatal("pre-FTS row not backfilled into the index")
	}
	blocks, err := searchBlocks(db, "alpha")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("searchBlocks: %v %v", blocks, err)
	}
	if !strings.Contains(blocks[0].Title, "alpha") {
		t.Fatalf("hit = %+v", blocks[0])
	}
}

// TestFTSMigrationFailureDegrades: an FTS migration failure must not wedge
// openDB — the index is derived state. The DB opens without it (unstamped so
// the next open retries), an event is logged, and search falls back to LIKE.
func TestFTSMigrationFailureDegrades(t *testing.T) {
	testEnv(t)
	raw := writePreFTSDB(t)
	if _, err := raw.Exec(`INSERT INTO blocks(start_ts,end_ts,title,summary,category,app,status,created_at)
	  VALUES(1000,1900,'legacy alpha block','worked on alpha tooling','coding','nvim','done',1)`); err != nil {
		t.Fatal(err)
	}
	// Squat on the index name with a plain table: CREATE VIRTUAL TABLE IF NOT
	// EXISTS no-ops, then the backfill INSERT fails inside the tx.
	if _, err := raw.Exec(`CREATE TABLE blocks_fts(x TEXT)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := openDB()
	if err != nil {
		t.Fatalf("openDB must not fail on a derived-index migration error: %v", err)
	}
	defer db.Close()
	if v := dbSchemaVersion(db); v != schemaVersionFTS-1 {
		t.Fatalf("schema version = %d, want %d (unstamped)", v, schemaVersionFTS-1)
	}
	var detail string
	if err := db.QueryRow(`SELECT detail FROM events WHERE type='fts_error'`).Scan(&detail); err != nil || detail == "" {
		t.Fatalf("expected an fts_error event, got %q err=%v", detail, err)
	}
	blocks, err := searchBlocks(db, "alpha")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("LIKE fallback: %v %v", blocks, err)
	}
}

func TestSearchBlocksFTSInsert(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedBlock(t, db, time.Now().Add(-time.Hour), "Mikrotik vlan rewrite", "router work", "coding", "winbox", "done")

	blocks, err := searchBlocks(db, "mikrotik")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("searchBlocks: %v %v", blocks, err)
	}
	// prove the hit came from the index, not the LIKE floor
	if ftsCount(t, db, "mikrotik") != 1 {
		t.Fatal("insert not indexed")
	}
}

func TestSearchBlocksFTSEditOverlay(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Now().Add(-time.Hour).Truncate(time.Minute)
	seedBlock(t, db, start, "Original planning doc", "misc notes", "coding", "nvim", "done")

	if err := saveBlockEdit(db, cfg, start.Unix(), "title", "Zebraplant redesign"); err != nil {
		t.Fatal(err)
	}
	// The index holds the effective text: new term matches, old term does not.
	if ftsCount(t, db, "zebraplant") != 1 {
		t.Fatal("edited term not indexed")
	}
	if ftsCount(t, db, "planning") != 0 {
		t.Fatal("pre-edit term still indexed")
	}
	blocks, err := searchBlocks(db, "zebraplant")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("searchBlocks: %v %v", blocks, err)
	}
	if blocks[0].Title != "Zebraplant redesign" {
		t.Fatalf("result should show effective text, got %q", blocks[0].Title)
	}
}

// TestSearchBlocksFTSScrub is the privacy regression test: a scrubbed block
// must leave no searchable ghost in the index.
func TestSearchBlocksFTSScrub(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedBlock(t, db, time.Now().Add(-time.Hour), "acmecorp contract review", "sensitive", "work", "docs", "done")

	blocks, _ := searchBlocks(db, "acmecorp")
	if len(blocks) != 1 {
		t.Fatal("seed not searchable")
	}
	if n, err := deleteBlocksLike(db, "acmecorp"); err != nil || n != 1 {
		t.Fatalf("scrub deleted %d err=%v", n, err)
	}
	if ftsCount(t, db, "acmecorp") != 0 {
		t.Fatal("scrubbed term still in FTS index (ghost)")
	}
	if blocks, err = searchBlocks(db, "acmecorp"); err != nil || len(blocks) != 0 {
		t.Fatalf("scrubbed block still searchable: %v %v", blocks, err)
	}
}

func TestSearchBlocksFTSRetryDelete(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedBlock(t, db, time.Now().Add(-time.Hour), "deadblock xyzzy", "", "", "", "failed")
	if ftsCount(t, db, "xyzzy") != 1 {
		t.Fatal("failed block not indexed")
	}
	if _, err := resetFailedBlocks(db); err != nil {
		t.Fatal(err)
	}
	if ftsCount(t, db, "xyzzy") != 0 {
		t.Fatal("retry delete left an FTS ghost")
	}
}

func TestSearchBlocksFTSCapEvict(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedBlock(t, db, time.Now().Add(-2*time.Hour), "prunable quux block", "", "", "", "done")
	if ftsCount(t, db, "quux") != 1 {
		t.Fatal("block not indexed")
	}
	pruneOldestBlocks(db)
	if ftsCount(t, db, "quux") != 0 {
		t.Fatal("cap-evicted block left an FTS ghost")
	}
}

// FTS5 syntax (parens, colons, quotes) errors where LIKE never did: the raw
// query errors, the quoted-phrase retry rescues it or LIKE takes over.
func TestSearchBlocksFTSSyntaxFallback(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedBlock(t, db, time.Now().Add(-time.Hour), "Weird (parens) fix", "a: b details", "coding", "", "done")

	blocks, err := searchBlocks(db, "(parens")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("(parens should sanitize to a hit: %v %v", blocks, err)
	}
	// Sanitizes to nothing -> LIKE path -> substring match on 'a:' in summary.
	blocks, err = searchBlocks(db, "a:")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("a: should hit via fallback: %v %v", blocks, err)
	}
	// Pure syntax garbage -> LIKE finds nothing, no error surfaced.
	blocks, err = searchBlocks(db, "(((")
	if err != nil || len(blocks) != 0 {
		t.Fatalf("((( should be an empty result, not an error: %v %v", blocks, err)
	}
}

// Substrings that never tokenize to an exact term ("auth" vs token
// "authentication") keep pre-FTS LIKE semantics via the empty-result fallback.
func TestSearchBlocksSubstringFallback(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedBlock(t, db, time.Now().Add(-time.Hour), "authentication refactor", "", "coding", "", "done")

	if ftsCount(t, db, "auth") != 0 {
		t.Fatal("expected no exact-token FTS hit for 'auth'")
	}
	blocks, err := searchBlocks(db, "auth")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("substring fallback: %v %v", blocks, err)
	}
}

// A read-only open of a pre-v4 database never migrates — the helper must fall
// back to LIKE, not error on the absent table.
func TestSearchBlocksReadOnlyNoIndex(t *testing.T) {
	testEnv(t)
	raw := writePreFTSDB(t)
	if _, err := raw.Exec(`INSERT INTO blocks(start_ts,end_ts,title,summary,category,app,status,created_at)
	  VALUES(1000,1900,'readonly alpha block','','coding','nvim','done',1)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := openDBReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if ftsIndexPresent(db) {
		t.Fatal("read-only open should not have an index on a v3 db")
	}
	blocks, err := searchBlocks(db, "alpha")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("read-only LIKE fallback: %v %v", blocks, err)
	}
}

// TestParseSearchArgs covers the old `args[0][0]` panic case: `search` and
// `search ""` yield an empty query (caller shows usage), and --reindex is
// recognized even though it starts with '-'.
func TestParseSearchArgs(t *testing.T) {
	if q, r := parseSearchArgs(nil); q != "" || r {
		t.Fatalf("no args: %q %v", q, r)
	}
	if q, r := parseSearchArgs([]string{""}); q != "" || r {
		t.Fatalf("empty arg: %q %v", q, r)
	}
	if q, r := parseSearchArgs([]string{"--reindex"}); q != "" || !r {
		t.Fatalf("--reindex: %q %v", q, r)
	}
	if q, r := parseSearchArgs([]string{"--json", "foo", "bar"}); q != "foo bar" || r {
		t.Fatalf("flags + terms: %q %v", q, r)
	}
}

func TestSearchReindex(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedBlock(t, db, time.Now().Add(-time.Hour), "gamma uniqueterm block", "", "coding", "", "done")

	// Corrupt the index by hand (drop the table; its triggers go stale too).
	if _, err := db.Exec(`DROP TABLE blocks_fts`); err != nil {
		t.Fatal(err)
	}
	if ftsIndexPresent(db) {
		t.Fatal("drop failed")
	}
	if err := rebuildSearchIndex(db); err != nil {
		t.Fatal(err)
	}
	if !ftsIndexPresent(db) || ftsCount(t, db, "uniqueterm") != 1 {
		t.Fatal("reindex did not rebuild + backfill")
	}
	// Triggers were recreated: new writes index again.
	seedBlock(t, db, time.Now().Add(-30*time.Minute), "delta uniquetwo", "", "coding", "", "done")
	if ftsCount(t, db, "uniquetwo") != 1 {
		t.Fatal("triggers not restored by reindex")
	}
}

// TestSearchCallSiteParity: the CLI helper, the MCP tool, and the chat tool
// share one code path — identical hit sets for the same query.
func TestSearchCallSiteParity(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedBlock(t, db, time.Now().Add(-2*time.Hour), "parity one", "sharedterm alpha", "coding", "nvim", "done")
	seedBlock(t, db, time.Now().Add(-time.Hour), "parity two", "sharedterm beta", "coding", "emacs", "done")
	seedBlock(t, db, time.Now().Add(-30*time.Minute), "parity three", "other", "coding", "emacs", "done")

	cli, err := searchBlocks(db, "sharedterm")
	if err != nil || len(cli) != 2 {
		t.Fatalf("searchBlocks: %v %v", cli, err)
	}

	mcpRes, err := mcpCall(db, cfg, false, "search_journal", map[string]any{"query": "sharedterm"})
	if err != nil {
		t.Fatal(err)
	}
	mcpMatches := mcpRes.(map[string]any)["matches"].([]map[string]string)
	if len(mcpMatches) != len(cli) {
		t.Fatalf("mcp hit count %d != %d", len(mcpMatches), len(cli))
	}
	for i, m := range mcpMatches {
		if m["title"] != cli[i].Title {
			t.Fatalf("mcp[%d] = %q, want %q", i, m["title"], cli[i].Title)
		}
	}

	chatRes, err := executeTool(db, cfg, "searchJournal", map[string]any{"query": "sharedterm"})
	if err != nil {
		t.Fatal(err)
	}
	chatMatches := chatRes.(map[string]any)["matches"].([]Block)
	if len(chatMatches) != len(cli) {
		t.Fatalf("chat hit count %d != %d", len(chatMatches), len(cli))
	}
	for i, b := range chatMatches {
		if b.StartTs != cli[i].StartTs {
			t.Fatalf("chat[%d] start_ts %d != %d", i, b.StartTs, cli[i].StartTs)
		}
	}
}

func TestStandupFTS(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := saveStandupDraft(db, "2026-09-28", "shipped wobblegate fix", "- tasks", "", ""); err != nil {
		t.Fatal(err)
	}
	count := func(term string) int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(1) FROM standup_fts WHERE standup_fts MATCH ?`, term).Scan(&n); err != nil {
			t.Fatalf("standup MATCH %q: %v", term, err)
		}
		return n
	}
	if count("wobblegate") != 1 {
		t.Fatal("standup draft not indexed")
	}
	// upsert updates the index
	if err := saveStandupDraft(db, "2026-09-28", "other pivotnotes", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if count("wobblegate") != 0 || count("pivotnotes") != 1 {
		t.Fatal("standup update not reflected in index")
	}
	// delete removes it
	if _, err := db.Exec(`DELETE FROM standup_drafts WHERE date='2026-09-28'`); err != nil {
		t.Fatal(err)
	}
	if count("pivotnotes") != 0 {
		t.Fatal("deleted standup draft left an FTS ghost")
	}
}
