package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// This file owns the FTS5 search index (schema v4, cleaned up by v5) and the
// single block-search entry point shared by `dayflow search`, the MCP
// search_journal tool, and the chat searchJournal tool.
//
// Index design (plan R3/KTD4):
//
//   - blocks_fts is an external-content FTS5 table over blocks (rowid =
//     start_ts, blocks.start_ts is INTEGER PRIMARY KEY). Only the inverted
//     index is stored; column values stay in blocks. The indexed text is the
//     *effective* text: latest block_edits new_value per field overlaid on the
//     raw columns, mirroring applyEdits' newest-wins rule.
//   - Maintenance is by triggers. blocks gets AFTER INSERT/UPDATE/DELETE. The
//     update trigger's WHEN clause limits it to changes that can alter the
//     indexed text — attempts/triaged/status churn during summarize passes
//     doesn't pay a delete+reindex per row. The delete trigger is
//     load-bearing — deleteBlocksLike (scrub), resetFailedBlocks (retry),
//     and pruneOldestBlocks (cap eviction) all DELETE blocks rows and must
//     not leave searchable ghosts. Its second statement also deletes the
//     block's block_edits rows (after the unindex, which needs them to
//     compute the effective OLD text): a dead block's edit overlay is
//     orphaned data that could re-apply to a re-summarized block with the
//     same start_ts.
//   - block_edits gets BEFORE INSERT (unindex the pre-edit effective text —
//     FTS5's 'delete' command requires the exact values that were indexed, and
//     in a BEFORE trigger the pending edit is not yet visible, so the same
//     effective-value expression yields the old text) and AFTER INSERT
//     (reindex the post-edit effective text). An AFTER UPDATE on blocks cannot
//     serve this: edits never touch the blocks row, so no blocks trigger ever
//     fires for them. The WHEN clause skips 'productive' edits (not indexed).
//   - External-content 'delete' is also why the blocks AFTER UPDATE/DELETE
//     trigger supplies OLD.* base columns rather than selecting from blocks:
//     at that point the content row is already changed/gone, and the index
//     holds the OLD effective text.
//   - The index migrations run outside the linear chain in
//     applyDerivedIndexMigrations so a failure degrades openDB to the LIKE
//     path instead of wedging it, and a failed attempt backs off via the
//     meta fts_migration_failed_at marker instead of re-running
//     CREATE+backfill on every command. `search --reindex` clears the marker
//     and retries on demand.
//   - v5 removed standup_fts: it was trigger-maintained but never queried
//     (no MATCH path read it), and its implicit-rowid external-content index
//     could drift on VACUUM because standup_drafts is keyed by a TEXT date,
//     not an INTEGER PRIMARY KEY. The drafts table itself is untouched.

// schemaVersionFTS is the migration that builds the FTS index;
// schemaVersionFTSCleanup removes the v4-era standup_fts index and refreshes
// the trigger definitions on already-migrated databases. Both run through
// applyDerivedIndexMigrations — a failure of either is non-fatal (derived
// index state only) and backs off via metaFTSFailedAt.
const schemaVersionFTS = 4
const schemaVersionFTSCleanup = 5

// metaFTSFailedAt records (unix seconds) when a derived-index migration last
// failed; retries are skipped while the marker is younger than
// ftsRetryBackoff. `search --reindex` deletes it to force an immediate retry.
const metaFTSFailedAt = "fts_migration_failed_at"
const ftsRetryBackoff = time.Hour

// reindexBatchRows bounds each backfill commit during rebuildSearchIndex so
// one rebuild never holds the write lock across the whole journal — a long
// lock would starve the capture daemon into busy-timeout frame drops. A var
// so tests can exercise multi-batch rebuilds on a handful of rows.
var reindexBatchRows = 500

// ftsEffExpr renders the effective (post-block_edits overlay) value of the
// blocks column col as seen through trigger reference rel (OLD, NEW) or a
// table alias (b). Newest edit wins, ordered exactly like editsForRange.
func ftsEffExpr(rel, col string) string {
	return fmt.Sprintf(`COALESCE((SELECT e.new_value FROM block_edits e
	  WHERE e.start_ts = %[1]s.start_ts AND e.field = '%[2]s'
	  ORDER BY e.edited_at DESC, e.id DESC LIMIT 1), %[1]s.%[2]s)`, rel, col)
}

// ftsIndexInsertStmt indexes the effective text of every blocks row matching
// pred (a WHERE-clause predicate over the blocks alias b). The SELECT form
// lets one statement serve blocks triggers (pred = "b.start_ts = NEW.start_ts"),
// block_edits triggers, and the initial backfill (pred = "1").
func ftsIndexInsertStmt(pred string) string {
	return fmt.Sprintf(`INSERT INTO blocks_fts(rowid, title, summary, category, app)
	  SELECT b.start_ts, %s, %s, %s, b.app
	  FROM blocks b WHERE %s`,
		ftsEffExpr("b", "title"), ftsEffExpr("b", "summary"), ftsEffExpr("b", "category"), pred)
}

// ftsIndexDeleteStmt un-indexes the effective text of every blocks row
// matching pred, reading the base columns from the blocks row itself. Only
// valid while the content row is unchanged — i.e. the block_edits BEFORE
// INSERT trigger. For blocks UPDATE/DELETE the row is already gone or
// updated; ftsIndexDeleteOldStmt must be used instead.
func ftsIndexDeleteStmt(pred string) string {
	return fmt.Sprintf(`INSERT INTO blocks_fts(blocks_fts, rowid, title, summary, category, app)
	  SELECT 'delete', b.start_ts, %s, %s, %s, b.app
	  FROM blocks b WHERE %s`,
		ftsEffExpr("b", "title"), ftsEffExpr("b", "summary"), ftsEffExpr("b", "category"), pred)
}

// ftsIndexDeleteOldStmt un-indexes the effective OLD row — used by the blocks
// AFTER UPDATE and AFTER DELETE triggers, where the content row no longer
// holds the indexed base values.
func ftsIndexDeleteOldStmt() string {
	return fmt.Sprintf(`INSERT INTO blocks_fts(blocks_fts, rowid, title, summary, category, app)
	  VALUES('delete', OLD.start_ts, %s, %s, %s, OLD.app)`,
		ftsEffExpr("OLD", "title"), ftsEffExpr("OLD", "summary"), ftsEffExpr("OLD", "category"))
}

// ftsBlocksTableDDL creates the external-content index over blocks.
const ftsBlocksTableDDL = `CREATE VIRTUAL TABLE IF NOT EXISTS blocks_fts USING fts5(
  title, summary, category, app,
  content='blocks', content_rowid='start_ts'
);
`

// ftsTriggersDDL maintains blocks_fts. Two subtleties beyond the plain
// delete+reindex pattern: the AFTER UPDATE WHEN clause skips updates that
// can't change indexed text (attempts, triaged, status flips during
// summarize passes), and the AFTER DELETE body removes the dead block's
// block_edits AFTER the unindex statement — the unindex reads those edits to
// compute the effective OLD text, but they must not outlive their block.
var ftsTriggersDDL = `
CREATE TRIGGER IF NOT EXISTS blocks_fts_ai AFTER INSERT ON blocks BEGIN
` + ftsIndexInsertStmt("b.start_ts = NEW.start_ts") + `;
END;
CREATE TRIGGER IF NOT EXISTS blocks_fts_au AFTER UPDATE ON blocks
WHEN OLD.title IS NOT NEW.title OR OLD.summary IS NOT NEW.summary
  OR OLD.category IS NOT NEW.category OR OLD.app IS NOT NEW.app
BEGIN
` + ftsIndexDeleteOldStmt() + `;
` + ftsIndexInsertStmt("b.start_ts = NEW.start_ts") + `;
END;
CREATE TRIGGER IF NOT EXISTS blocks_fts_ad AFTER DELETE ON blocks BEGIN
` + ftsIndexDeleteOldStmt() + `;
  DELETE FROM block_edits WHERE start_ts = OLD.start_ts;
END;

CREATE TRIGGER IF NOT EXISTS block_edits_fts_bi BEFORE INSERT ON block_edits
WHEN NEW.field IN ('title','summary','category') BEGIN
` + ftsIndexDeleteStmt("b.start_ts = NEW.start_ts") + `;
END;
CREATE TRIGGER IF NOT EXISTS block_edits_fts_ai AFTER INSERT ON block_edits
WHEN NEW.field IN ('title','summary','category') BEGIN
` + ftsIndexInsertStmt("b.start_ts = NEW.start_ts") + `;
END;
`

// schemaV4 is migration v4's object DDL — the index table plus its
// maintenance triggers. The backfill runs as a separate statement inside the
// same migration transaction (see applyMigration) so a killed mid-backfill
// can never leave a stamped-but-empty index.
var schemaV4 = ftsBlocksTableDDL + ftsTriggersDDL

// ftsBackfillStmt populates the index during the v4 migration. The NOT IN
// guard against the _docsize shadow table makes it idempotent: a retry over
// an index a `search --reindex` already (partially) filled inserts only the
// missing rows instead of double-indexing — external-content FTS5 doesn't
// dedupe rowids. (The guard must read the docsize shadow: a bare
// `SELECT rowid FROM blocks_fts` scans the *content* table, not the index.)
var ftsBackfillStmt = ftsIndexInsertStmt(
	"b.start_ts NOT IN (SELECT rowid FROM blocks_fts_docsize)")

// schemaV5 is migration v5's object DDL: drop the dead standup_fts index and
// its triggers (the drafts table stays — it is the live standup store).
const schemaV5 = `
DROP TRIGGER IF EXISTS standup_fts_ai;
DROP TRIGGER IF EXISTS standup_fts_au;
DROP TRIGGER IF EXISTS standup_fts_ad;
DROP TABLE IF EXISTS standup_fts;
`

// schemaV5TriggerRefresh re-creates the blocks/block_edits index triggers
// with their current definitions — CREATE TRIGGER has no OR REPLACE, and
// IF NOT EXISTS alone would leave v4-era trigger bodies (no UPDATE WHEN
// clause, no block_edits cascade) in place on already-migrated databases.
// applyMigration runs it only when blocks_fts is really present: pointing
// these triggers at a missing or squatted-on table would fail every write.
var schemaV5TriggerRefresh = `
DROP TRIGGER IF EXISTS blocks_fts_ai;
DROP TRIGGER IF EXISTS blocks_fts_au;
DROP TRIGGER IF EXISTS blocks_fts_ad;
DROP TRIGGER IF EXISTS block_edits_fts_bi;
DROP TRIGGER IF EXISTS block_edits_fts_ai;
` + ftsTriggersDDL

// ftsIndexPresentSQL matches only a real FTS5 table — a plain table squatting
// on the blocks_fts name must not count (every search would pay a failing
// MATCH + phrase retry before falling back to LIKE).
const ftsIndexPresentSQL = `SELECT COUNT(1) FROM sqlite_master
  WHERE type='table' AND name='blocks_fts' AND LOWER(sql) LIKE '%using fts5%'`

// ftsIndexPresent reports whether the blocks_fts index exists (false on a
// pre-v4 database, after a degraded migration, under a read-only open of an
// unmigrated DB, or on a squatter table — all of which take the LIKE path).
func ftsIndexPresent(db *sql.DB) bool {
	var n int
	err := db.QueryRow(ftsIndexPresentSQL).Scan(&n)
	return err == nil && n == 1
}

func ftsIndexPresentTx(tx *sql.Tx) bool {
	var n int
	err := tx.QueryRow(ftsIndexPresentSQL).Scan(&n)
	return err == nil && n == 1
}

// applyDerivedIndexMigrations applies the schema versions that only build or
// tear down the derived FTS index (v4 build, v5 standup_fts cleanup). They
// run outside the linear migration chain so a failure logs an fts_error
// event and degrades to the LIKE path instead of wedging openDB — and so
// retry bookkeeping can't ride on MAX(version), which a later migration may
// already have stamped. A failed attempt records metaFTSFailedAt and opens
// inside ftsRetryBackoff skip the retry rather than re-running
// CREATE+backfill on every `dayflow status`/MCP connect; `search --reindex`
// clears the marker to retry immediately.
//
// The cleanup runs before the build so that on a database where the build
// keeps failing, the cleanup still gets applied and stamped — a persistent
// v4 failure must not hold v5's retry hostage behind v4's backoff marker.
func applyDerivedIndexMigrations(db *sql.DB) {
	backoff := false
	if ts, err := strconv.ParseInt(metaGet(db, metaFTSFailedAt), 10, 64); err == nil &&
		ts > 0 && time.Since(time.Unix(ts, 0)) < ftsRetryBackoff {
		backoff = true
	}
	for _, v := range []int{schemaVersionFTSCleanup, schemaVersionFTS} {
		var applied int
		if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version=?`, v).Scan(&applied); err != nil {
			continue
		}
		if applied > 0 || backoff {
			continue
		}
		if err := applyMigration(db, v); err != nil {
			logEvent(db, "fts_error", fmt.Sprintf("search index migration v%d failed: %v", v, err))
			metaSet(db, metaFTSFailedAt, strconv.FormatInt(time.Now().Unix(), 10))
			backoff = true // a fresh failure backs the rest of this pass off too
		}
	}
}

// rebuildSearchIndex drops and rebuilds blocks_fts from current content
// (`dayflow search --reindex`) — the repair path for a stale, missing, or
// squatted-on index. It also clears the fts_migration_failed_at backoff
// marker so an explicit reindex retries a failed v4 immediately, and stamps
// the v4 migration row so the next open doesn't re-run it.
//
// The rebuild runs in three lock windows instead of one transaction so the
// capture daemon isn't starved by a full-table write lock:
//  1. drop triggers + index, create the empty index (short tx);
//  2. chunked backfill — reindexBatchRows rows per INSERT..SELECT, each its
//     own implicit commit;
//  3. recreate triggers + stamp v4 (short tx).
//
// Trade-off: triggers are down for the duration of step 2, so a block or
// edit written mid-rebuild is indexed only if the backfill hasn't passed
// its start_ts yet — the index stays slightly stale until that row's next
// update (or the next --reindex). A daemon-liveness warning was considered
// and dropped: once each lock window is bounded to one batch, the
// contention itself is fixed, not just reported. (Rebuild deliberately does
// NOT use `INSERT INTO blocks_fts(blocks_fts) VALUES('rebuild')` — that
// re-indexes the raw blocks columns, bypassing the block_edits effective-
// text overlay the triggers maintain.)
func rebuildSearchIndex(db *sql.DB) error {
	if _, err := db.Exec(`DELETE FROM meta WHERE k=?`, metaFTSFailedAt); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, stmt := range []string{
		`DROP TRIGGER IF EXISTS blocks_fts_ai`,
		`DROP TRIGGER IF EXISTS blocks_fts_au`,
		`DROP TRIGGER IF EXISTS blocks_fts_ad`,
		`DROP TRIGGER IF EXISTS block_edits_fts_bi`,
		`DROP TRIGGER IF EXISTS block_edits_fts_ai`,
		// Dead v4-era standup index objects; IF EXISTS makes these no-ops
		// once v5 has run.
		`DROP TRIGGER IF EXISTS standup_fts_ai`,
		`DROP TRIGGER IF EXISTS standup_fts_au`,
		`DROP TRIGGER IF EXISTS standup_fts_ad`,
		`DROP TABLE IF EXISTS blocks_fts`,
		`DROP TABLE IF EXISTS standup_fts`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(ftsBlocksTableDDL); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Chunked backfill: every INSERT..SELECT is its own transaction, so the
	// daemon's writes interleave between batches instead of timing out
	// behind one rebuild-length lock.
	last := int64(-1) // start_ts values are unix seconds
	for {
		var hi int64
		err := db.QueryRow(`SELECT start_ts FROM blocks WHERE start_ts > ?
		  ORDER BY start_ts LIMIT 1 OFFSET ?`, last, reindexBatchRows-1).Scan(&hi)
		if err == sql.ErrNoRows {
			// Final partial batch: everything above last.
			if _, err := db.Exec(ftsIndexInsertStmt("b.start_ts > ?"), last); err != nil {
				return err
			}
			break
		}
		if err != nil {
			return err
		}
		if _, err := db.Exec(ftsIndexInsertStmt("b.start_ts > ? AND b.start_ts <= ?"), last, hi); err != nil {
			return err
		}
		last = hi
	}

	tx, err = db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ftsTriggersDDL); err != nil {
		return err
	}
	// The rebuild produces exactly the state v4 stamps — record it so the
	// next open doesn't re-run the migration over a populated index.
	if _, err := tx.Exec(`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
		schemaVersionFTS, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// searchBlockCols is the column list both search paths scan into a Block.
const searchBlockCols = `b.start_ts, b.end_ts, b.title, b.summary, b.category, b.app,
  b.activities, b.productive, b.category_confidence, b.quality_confidence, b.same_as_prev`

const searchBlockLimit = 50

// scanSearchRows materializes Block rows for searchBlockCols selects.
func scanSearchRows(rows *sql.Rows) ([]Block, error) {
	defer rows.Close()
	var out []Block
	for rows.Next() {
		var b Block
		var s, e int64
		var acts string
		var prod sql.NullBool
		var conf, qual sql.NullFloat64
		var sap sql.NullBool
		if err := rows.Scan(&s, &e, &b.Title, &b.Summary, &b.Category, &b.App, &acts, &prod,
			&conf, &qual, &sap); err != nil {
			return nil, err
		}
		if prod.Valid {
			b.Productive = &prod.Bool
		}
		if conf.Valid {
			b.CategoryConfidence = &conf.Float64
		}
		if qual.Valid {
			b.QualityConfidence = &qual.Float64
		}
		if sap.Valid {
			b.SameAsPrev = &sap.Bool
		}
		if acts != "" {
			json.Unmarshal([]byte(acts), &b.Activities)
		}
		b.LowConfidence = blockLowConfidence(b)
		b.Start = time.Unix(s, 0).Local()
		b.End = time.Unix(e, 0).Local()
		b.StartTs = s
		b.EndTs = e
		b.StartStr = b.Start.Format("3:04 PM")
		b.EndStr = b.End.Format("3:04 PM")
		b.AppName = appDisplayName(b.App)
		out = append(out, b)
	}
	return out, rows.Err()
}

// ftsMatchBlocks runs an FTS5 MATCH query (raw FTS5 syntax, so callers get
// AND/OR/NEAR/prefix operators for free) and returns the matching done blocks
// ranked by relevance.
func ftsMatchBlocks(db *sql.DB, query string) ([]Block, error) {
	rows, err := db.Query(`SELECT `+searchBlockCols+`
	  FROM blocks_fts JOIN blocks b ON b.start_ts = blocks_fts.rowid
	  WHERE blocks_fts MATCH ? AND b.status = 'done'
	  ORDER BY blocks_fts.rank, b.start_ts DESC LIMIT ?`, query, searchBlockLimit)
	if err != nil {
		return nil, err
	}
	return scanSearchRows(rows)
}

// ftsTerms rewrites free text into a safe FTS5 phrase query: runs of
// letters/digits, double-quoted. Returns "" when the text has no usable terms.
func ftsTerms(q string) string {
	var terms []string
	for _, f := range strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		terms = append(terms, f)
	}
	if len(terms) == 0 {
		return ""
	}
	return `"` + strings.Join(terms, " ") + `"`
}

// searchBlocksLike is the pre-FTS substring search and the fallback path.
func searchBlocksLike(db *sql.DB, query string) ([]Block, error) {
	like := "%" + query + "%"
	rows, err := db.Query(`SELECT `+searchBlockCols+` FROM blocks b
	  WHERE b.status='done' AND (b.title LIKE ? OR b.summary LIKE ? OR b.category LIKE ? OR b.app LIKE ?)
	  ORDER BY b.start_ts DESC LIMIT ?`, like, like, like, like, searchBlockLimit)
	if err != nil {
		return nil, err
	}
	return scanSearchRows(rows)
}

// searchBlocks is the one block-search path used by `dayflow search`, MCP
// search_journal, and chat's searchJournal tool.
//
// It prefers the FTS index; it falls back to LIKE when the index is absent
// (pre-v4 or read-only DB, degraded migration), when MATCH rejects the query
// (FTS5 syntax such as ':', '(' or unbalanced quotes errors where LIKE never
// did — the query is first retried as a quoted phrase), or when MATCH simply
// returns nothing (empty index, or substring text that never tokenizes to an
// exact term — the LIKE floor keeps pre-FTS substring semantics).
func searchBlocks(db *sql.DB, query string) ([]Block, error) {
	var blocks []Block
	var err error
	if ftsIndexPresent(db) && strings.TrimSpace(query) != "" {
		blocks, err = ftsMatchBlocks(db, query)
		if err != nil {
			if q := ftsTerms(query); q != "" {
				blocks, err = ftsMatchBlocks(db, q)
			}
		}
		if err == nil && len(blocks) > 0 {
			return applyEditsToHits(db, blocks)
		}
	}
	blocks, err = searchBlocksLike(db, query)
	if err != nil {
		return nil, err
	}
	return applyEditsToHits(db, blocks)
}

// applyEditsToHits overlays block_edits on the result set so search output
// shows the same effective text that was indexed.
func applyEditsToHits(db *sql.DB, blocks []Block) ([]Block, error) {
	if len(blocks) == 0 {
		return blocks, nil
	}
	lo, hi := blocks[0].StartTs, blocks[0].StartTs
	for _, b := range blocks[1:] {
		if b.StartTs < lo {
			lo = b.StartTs
		}
		if b.StartTs > hi {
			hi = b.StartTs
		}
	}
	return applyBlockEdits(db, blocks, lo, hi+1)
}

// parseSearchArgs splits `dayflow search` argv into the query text and the
// --reindex flag. Flag parsing lives here (inside the command's own argument
// handling) so `search --reindex` is not mistaken for a query; positionalArgs
// already drops "" and flags, so `search ""` and bare `search` yield "".
func parseSearchArgs(args []string) (query string, reindex bool) {
	return strings.Join(positionalArgs(args), " "), hasFlag(args, "--reindex")
}
