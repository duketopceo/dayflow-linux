package main

import (
	"database/sql"
	"fmt"
	"strconv"
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

// ftsIndexedDocs counts real index entries via the _docsize shadow table
// (one row per indexed document — a bare SELECT from blocks_fts scans the
// *content* table and can never see duplicates) and asserts the index
// passes FTS5's integrity-check.
func ftsIndexedDocs(t *testing.T, db *sql.DB) int {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO blocks_fts(blocks_fts) VALUES('integrity-check')`); err != nil {
		t.Fatalf("fts integrity-check: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM blocks_fts_docsize`).Scan(&n); err != nil {
		t.Fatalf("docsize count: %v", err)
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
// openDB — the index is derived state. The DB opens without it (v4 stays
// unstamped so a later open retries), an event is logged, and search falls
// back to LIKE. Later derived-index migrations (v5) still apply.
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
	if v := dbSchemaVersion(db); v != schemaVersion {
		t.Fatalf("schema version = %d, want %d (v5 cleanup still stamps)", v, schemaVersion)
	}
	var stamped int
	db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version=?`, schemaVersionFTS).Scan(&stamped)
	if stamped != 0 {
		t.Fatal("v4 stamped despite the failed migration")
	}
	// A squatter table is not an index — the LIKE path must not pay a
	// failing MATCH + phrase retry first.
	if ftsIndexPresent(db) {
		t.Fatal("plain-table squatter counted as the FTS index")
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

// TestFTSMigrationBackoff: a persistent v4 failure must not re-run the
// CREATE+backfill on every open — the fts_migration_failed_at meta marker
// backs retries off for ftsRetryBackoff, a stale marker retries, and
// `search --reindex` clears the marker and repairs the squatter.
func TestFTSMigrationBackoff(t *testing.T) {
	testEnv(t)
	raw := writePreFTSDB(t)
	if _, err := raw.Exec(`INSERT INTO blocks(start_ts,end_ts,title,summary,category,app,status,created_at)
	  VALUES(1000,1900,'backoff alpha block','','coding','nvim','done',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE blocks_fts(x TEXT)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	ftsErrCount := func(db *sql.DB) int {
		var n int
		db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='fts_error'`).Scan(&n)
		return n
	}

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	if ftsErrCount(db) != 1 {
		t.Fatalf("first open should log one fts_error, got %d", ftsErrCount(db))
	}
	if metaGet(db, metaFTSFailedAt) == "" {
		t.Fatal("failed migration did not record the backoff marker")
	}
	db.Close()

	// A fresh open inside the backoff window must not retry — still one
	// fts_error, no second CREATE+backfill pass.
	db, err = openDB()
	if err != nil {
		t.Fatal(err)
	}
	if n := ftsErrCount(db); n != 1 {
		t.Fatalf("backoff open retried the migration (fts_error count=%d)", n)
	}
	// A stale marker retries on the next open.
	metaSet(db, metaFTSFailedAt, strconv.FormatInt(time.Now().Add(-2*ftsRetryBackoff).Unix(), 10))
	db.Close()
	db, err = openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n := ftsErrCount(db); n != 2 {
		t.Fatalf("stale marker should retry once (fts_error count=%d)", n)
	}

	// --reindex clears the marker and repairs in place: it drops the
	// squatter, builds the real index, and stamps v4.
	if err := rebuildSearchIndex(db); err != nil {
		t.Fatal(err)
	}
	if metaGet(db, metaFTSFailedAt) != "" {
		t.Fatal("reindex did not clear the backoff marker")
	}
	if !ftsIndexPresent(db) {
		t.Fatal("reindex did not replace the squatter with a real index")
	}
	var stamped int
	db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version=?`, schemaVersionFTS).Scan(&stamped)
	if stamped != 1 {
		t.Fatal("reindex did not stamp v4")
	}
	blocks, err := searchBlocks(db, "alpha")
	if err != nil || len(blocks) != 1 {
		t.Fatalf("repaired index search: %v %v", blocks, err)
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
	// The pre-edit term must not resurface through the LIKE floor: the raw
	// column still says "Original planning doc", but the effective text is
	// the renamed title, so a search for the old term finds nothing.
	if blocks, err := searchBlocks(db, "planning"); err != nil || len(blocks) != 0 {
		t.Fatalf("pre-edit term surfaced via LIKE floor: %v %v", blocks, err)
	}
}

// TestSearchBlocksSubstringFloorMerged: FTS matches exact tokens only, so
// a search for `plan` must still find `planning` titles — the LIKE
// substring floor runs alongside FTS and its extra hits merge in rather
// than being skipped whenever FTS returns anything.
func TestSearchBlocksSubstringFloorMerged(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)
	seedBlock(t, db, start, "plan review meeting", "", "work", "docs", "done")
	seedBlock(t, db, start.Add(time.Hour), "planning retro notes", "", "work", "nvim", "done")
	if ftsCount(t, db, "plan") != 1 {
		t.Fatal("seed not indexed as expected")
	}
	blocks, err := searchBlocks(db, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("substring floor not merged with FTS hits: %v", blocks)
	}
	titles := map[string]bool{}
	for _, b := range blocks {
		titles[b.Title] = true
	}
	if !titles["plan review meeting"] || !titles["planning retro notes"] {
		t.Fatalf("merged results lost a hit: %v", titles)
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

// TestFTSUpdateTriggerWhenClause: the blocks_fts_au WHEN clause skips
// updates that can't change the indexed text (attempts, triaged, status
// churn during summarize passes) — the row stays indexed, untouched. An
// update that does change an indexed column still reindexes.
func TestFTSUpdateTriggerWhenClause(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Now().Add(-time.Hour).Truncate(time.Minute)
	seedBlock(t, db, start, "whenclause uniqterm block", "s", "coding", "nvim", "done")
	if ftsCount(t, db, "uniqterm") != 1 {
		t.Fatal("seed not indexed")
	}

	// Non-indexed-column churn must leave the FTS row alone.
	for _, stmt := range []string{
		`UPDATE blocks SET attempts=3, triaged=1 WHERE start_ts=?`,
		`UPDATE blocks SET status='dead' WHERE start_ts=?`,
		`UPDATE blocks SET category_confidence=0.5, quality_confidence=0.5 WHERE start_ts=?`,
	} {
		if _, err := db.Exec(stmt, start.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	if ftsCount(t, db, "uniqterm") != 1 {
		t.Fatal("non-indexed UPDATE disturbed the FTS row")
	}
	if n := ftsIndexedDocs(t, db); n != 1 {
		t.Fatalf("index entries=%d — non-indexed UPDATE duplicated or dropped the entry", n)
	}

	// An indexed-column update still reindexes.
	if _, err := db.Exec(`UPDATE blocks SET title='whenclause renamed zztop' WHERE start_ts=?`, start.Unix()); err != nil {
		t.Fatal(err)
	}
	if ftsCount(t, db, "uniqterm") != 0 || ftsCount(t, db, "zztop") != 1 {
		t.Fatal("indexed-column UPDATE did not reindex")
	}
}

// TestSearchBlocksScrubDeletesEdits is the privacy regression test at the
// edit overlay: scrub must remove the block's block_edits too — their
// old_value/new_value retain the scrubbed text — and the delete ordering
// (blocks first, edits second) is what lets the blocks_fts_ad trigger
// un-index the *effective* text rather than the raw columns.
func TestSearchBlocksScrubDeletesEdits(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Now().Add(-time.Hour).Truncate(time.Minute)
	seedBlock(t, db, start, "acmecorp contract review", "sensitive", "work", "docs", "done")

	// The edit changes the indexed text — after the edit the index holds
	// "zztop", not the raw title.
	if err := saveBlockEdit(db, cfg, start.Unix(), "title", "zztop renamed"); err != nil {
		t.Fatal(err)
	}
	if ftsCount(t, db, "zztop") != 1 || ftsCount(t, db, "acmecorp") != 0 {
		t.Fatal("edit overlay not reflected in index")
	}

	// Scrub still matches the raw title and must leave no ghost of either
	// the raw or the edited text.
	if n, err := deleteBlocksLike(db, "acmecorp"); err != nil || n != 1 {
		t.Fatalf("scrub deleted %d err=%v", n, err)
	}
	if ftsCount(t, db, "zztop") != 0 {
		t.Fatal("edited text ghosted in the index — edits were deleted before the unindex read them")
	}
	if ftsCount(t, db, "acmecorp") != 0 {
		t.Fatal("scrubbed term still indexed")
	}
	var edits int
	if err := db.QueryRow(`SELECT COUNT(1) FROM block_edits WHERE start_ts=?`, start.Unix()).Scan(&edits); err != nil {
		t.Fatal(err)
	}
	if edits != 0 {
		t.Fatalf("scrub left %d block_edits rows retaining the scrubbed text", edits)
	}
	if blocks, err := searchBlocks(db, "zztop"); err != nil || len(blocks) != 0 {
		t.Fatalf("edited ghost still searchable: %v %v", blocks, err)
	}
}

// TestSearchBlocksScrubMatchesEditedText: the scrub predicate must cover
// the edit overlay too. A term that exists only in block_edits.new_value —
// the text the user actually sees and the index actually holds — must scrub
// its block, not report zero deletions while the term stays visible and
// searchable.
func TestSearchBlocksScrubMatchesEditedText(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Now().Add(-time.Hour).Truncate(time.Minute)
	seedBlock(t, db, start, "innocuous title", "", "work", "docs", "done")

	// The edit introduces the sensitive term: it exists only in
	// block_edits.new_value and the FTS index, not in the raw columns.
	if err := saveBlockEdit(db, cfg, start.Unix(), "title", "acmecorp contract"); err != nil {
		t.Fatal(err)
	}
	if ftsCount(t, db, "acmecorp") != 1 {
		t.Fatal("edited term not indexed")
	}

	// Scrub must find it through the edit overlay and delete the block.
	if n, err := deleteBlocksLike(db, "acmecorp"); err != nil || n != 1 {
		t.Fatalf("scrub of edited text deleted %d err=%v", n, err)
	}
	if ftsCount(t, db, "acmecorp") != 0 {
		t.Fatal("scrubbed edited term still indexed")
	}
	if blocks, err := searchBlocks(db, "acmecorp"); err != nil || len(blocks) != 0 {
		t.Fatalf("scrubbed edited term still searchable: %v %v", blocks, err)
	}
}

// TestRetryDeletesBlockEdits: an edit can exist on a failed block (the edit
// path doesn't check status), and retry deletes those rows — otherwise the
// orphaned overlay would re-apply to the re-summarized block at the same
// start_ts.
func TestRetryDeletesBlockEdits(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Now().Add(-time.Hour).Truncate(time.Minute)
	seedBlock(t, db, start, "retryedit uniqterm", "", "", "", "failed")
	if err := saveBlockEdit(db, cfg, start.Unix(), "title", "user override zztop"); err != nil {
		t.Fatal(err)
	}
	if _, err := resetFailedBlocks(db); err != nil {
		t.Fatal(err)
	}
	var edits int
	if err := db.QueryRow(`SELECT COUNT(1) FROM block_edits WHERE start_ts=?`, start.Unix()).Scan(&edits); err != nil {
		t.Fatal(err)
	}
	if edits != 0 {
		t.Fatalf("retry left %d orphaned block_edits rows", edits)
	}
	if ftsCount(t, db, "uniqterm") != 0 || ftsCount(t, db, "zztop") != 0 {
		t.Fatal("retry left an FTS ghost")
	}
}

// TestUsageProviderBucketsCLI: summarize logs api_calls.model as
// 'cli:<command>' for a cli vision provider — those rows belong in a 'cli'
// provider bucket, not folded under 'openrouter'.
func TestUsageProviderBucketsCLI(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	seedAPICall(t, db, now, "cli:/usr/bin/mysummarizer", 100, 10, 50, "ok")
	seedAPICall(t, db, now, "openai/gpt-5", 200, 20, 100, "ok")
	seedLLMCall(t, db, now, "chat", "mylocal", "m1", 50, 5, 10, "ok")

	sum, err := usageSummaryWindow(db, 0, defaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	byProvider := sum["breakdown"].(map[string]any)["by_provider"].(map[string]usageRow)
	if byProvider["cli"].Calls != 1 {
		t.Fatalf("cli: model should bucket under 'cli': %+v", byProvider)
	}
	if byProvider["openrouter"].Calls != 1 {
		t.Fatalf("non-cli api_call should bucket under 'openrouter': %+v", byProvider)
	}
	if byProvider["mylocal"].Calls != 1 {
		t.Fatalf("llm_calls provider grouping: %+v", byProvider)
	}
}

// TestUsageDaysFlag: a bare or invalid --days must error instead of silently
// reporting the full history.
func TestUsageDaysFlag(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		want    int
		wantErr bool
	}{
		{[]string{"--json"}, 0, false},
		{[]string{"--days", "7"}, 7, false},
		{[]string{"--days=3"}, 3, false},
		{[]string{"--days"}, 0, true},
		{[]string{"--days="}, 0, true},
		{[]string{"--days", "abc"}, 0, true},
		{[]string{"--days", "0"}, 0, true},
		{[]string{"--days", "-2"}, 0, true},
		{[]string{"--days", "--json"}, 0, true}, // flag in value position
		{[]string{"--", "--days"}, 0, false},    // after -- it's positional
	} {
		got, err := usageDays(tc.args)
		if (err != nil) != tc.wantErr {
			t.Fatalf("usageDays(%v): err=%v wantErr=%v", tc.args, err, tc.wantErr)
		}
		if err == nil && got != tc.want {
			t.Fatalf("usageDays(%v) = %d, want %d", tc.args, got, tc.want)
		}
	}
}

// TestSearchReindexChunked exercises the bounded-commit backfill: with the
// batch size forced small, a rebuild over several blocks still indexes them
// all exactly once.
func TestSearchReindexChunked(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := reindexBatchRows
	reindexBatchRows = 2
	t.Cleanup(func() { reindexBatchRows = old })

	for i := 0; i < 5; i++ {
		seedBlock(t, db, time.Now().Add(-time.Duration(i+1)*time.Hour),
			fmt.Sprintf("chunked block term%d", i), "", "coding", "", "done")
	}
	if err := rebuildSearchIndex(db); err != nil {
		t.Fatal(err)
	}
	if n := ftsIndexedDocs(t, db); n != 5 {
		t.Fatalf("index entries=%d — chunked backfill missed or duplicated rows", n)
	}
	for i := 0; i < 5; i++ {
		if ftsCount(t, db, fmt.Sprintf("term%d", i)) != 1 {
			t.Fatalf("term%d not indexed", i)
		}
	}
}
