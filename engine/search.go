package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// This file owns the FTS5 search index (schema v4) and the single block-search
// entry point shared by `dayflow search`, the MCP search_journal tool, and the
// chat searchJournal tool.
//
// Index design (plan R3/KTD4):
//
//   - blocks_fts is an external-content FTS5 table over blocks (rowid =
//     start_ts, blocks.start_ts is INTEGER PRIMARY KEY). Only the inverted
//     index is stored; column values stay in blocks. The indexed text is the
//     *effective* text: latest block_edits new_value per field overlaid on the
//     raw columns, mirroring applyEdits' newest-wins rule.
//   - standup_fts is an external-content table over standup_drafts (implicit
//     rowid; journal_entries is vestigial and deliberately not indexed).
//   - Maintenance is by triggers. blocks gets AFTER INSERT/UPDATE/DELETE; the
//     delete trigger is load-bearing — deleteBlocksLike (scrub),
//     resetFailedBlocks (retry), and pruneOldestBlocks (cap eviction) all
//     DELETE blocks rows and must not leave searchable ghosts.
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
//   - The whole v4 migration (virtual tables + triggers + backfill + stamp)
//     runs in one transaction, so a killed mid-backfill can never leave a
//     stamped-but-empty index. If it fails anyway, migrate() logs an event and
//     opens without the index; searchBlocks falls back to LIKE.

// schemaVersionFTS is the migration that adds the FTS index. migrate() treats
// a failure of this version as non-fatal (derived index only).
const schemaVersionFTS = 4

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

// schemaV4 builds the FTS index objects and backfills them. It runs inside
// the migration transaction (and inside rebuildSearchIndex), so it never
// leaves a partial index.
var schemaV4 = `
CREATE VIRTUAL TABLE IF NOT EXISTS blocks_fts USING fts5(
  title, summary, category, app,
  content='blocks', content_rowid='start_ts'
);
CREATE VIRTUAL TABLE IF NOT EXISTS standup_fts USING fts5(
  highlights, tasks, blockers, priorities,
  content='standup_drafts'
);

CREATE TRIGGER IF NOT EXISTS blocks_fts_ai AFTER INSERT ON blocks BEGIN
` + ftsIndexInsertStmt("b.start_ts = NEW.start_ts") + `;
END;
CREATE TRIGGER IF NOT EXISTS blocks_fts_au AFTER UPDATE ON blocks BEGIN
` + ftsIndexDeleteOldStmt() + `;
` + ftsIndexInsertStmt("b.start_ts = NEW.start_ts") + `;
END;
CREATE TRIGGER IF NOT EXISTS blocks_fts_ad AFTER DELETE ON blocks BEGIN
` + ftsIndexDeleteOldStmt() + `;
END;

CREATE TRIGGER IF NOT EXISTS block_edits_fts_bi BEFORE INSERT ON block_edits
WHEN NEW.field IN ('title','summary','category') BEGIN
` + ftsIndexDeleteStmt("b.start_ts = NEW.start_ts") + `;
END;
CREATE TRIGGER IF NOT EXISTS block_edits_fts_ai AFTER INSERT ON block_edits
WHEN NEW.field IN ('title','summary','category') BEGIN
` + ftsIndexInsertStmt("b.start_ts = NEW.start_ts") + `;
END;

CREATE TRIGGER IF NOT EXISTS standup_fts_ai AFTER INSERT ON standup_drafts BEGIN
  INSERT INTO standup_fts(rowid, highlights, tasks, blockers, priorities)
    VALUES(NEW.rowid, NEW.highlights, NEW.tasks, NEW.blockers, NEW.priorities);
END;
CREATE TRIGGER IF NOT EXISTS standup_fts_au AFTER UPDATE ON standup_drafts BEGIN
  INSERT INTO standup_fts(standup_fts, rowid, highlights, tasks, blockers, priorities)
    VALUES('delete', OLD.rowid, OLD.highlights, OLD.tasks, OLD.blockers, OLD.priorities);
  INSERT INTO standup_fts(rowid, highlights, tasks, blockers, priorities)
    VALUES(NEW.rowid, NEW.highlights, NEW.tasks, NEW.blockers, NEW.priorities);
END;
CREATE TRIGGER IF NOT EXISTS standup_fts_ad AFTER DELETE ON standup_drafts BEGIN
  INSERT INTO standup_fts(standup_fts, rowid, highlights, tasks, blockers, priorities)
    VALUES('delete', OLD.rowid, OLD.highlights, OLD.tasks, OLD.blockers, OLD.priorities);
END;

` + ftsIndexInsertStmt("1") + `;
INSERT INTO standup_fts(rowid, highlights, tasks, blockers, priorities)
  SELECT rowid, highlights, tasks, blockers, priorities FROM standup_drafts;
`

// ftsIndexPresent reports whether the blocks_fts index exists (false on a
// pre-v4 database, after a degraded migration, or under a read-only open of
// an unmigrated DB — all of which take the LIKE path).
func ftsIndexPresent(db *sql.DB) bool {
	var n int
	err := db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='blocks_fts'`).Scan(&n)
	return err == nil && n == 1
}

// rebuildSearchIndex drops and rebuilds both FTS tables from current content
// (`dayflow search --reindex`). Triggers are dropped too — they live on
// blocks/block_edits/standup_drafts, so dropping the FTS tables alone would
// leave triggers that fail on the next write; schemaV4 recreates everything.
func rebuildSearchIndex(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DROP TRIGGER IF EXISTS blocks_fts_ai`,
		`DROP TRIGGER IF EXISTS blocks_fts_au`,
		`DROP TRIGGER IF EXISTS blocks_fts_ad`,
		`DROP TRIGGER IF EXISTS block_edits_fts_bi`,
		`DROP TRIGGER IF EXISTS block_edits_fts_ai`,
		`DROP TRIGGER IF EXISTS standup_fts_ai`,
		`DROP TRIGGER IF EXISTS standup_fts_au`,
		`DROP TRIGGER IF EXISTS standup_fts_ad`,
		`DROP TABLE IF EXISTS blocks_fts`,
		`DROP TABLE IF EXISTS standup_fts`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(schemaV4); err != nil {
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
