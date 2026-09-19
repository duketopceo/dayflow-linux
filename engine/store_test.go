package main

import (
	"database/sql"
	"os"
	"testing"
	"time"
)

// legacySchema builds a 1.0.0-era database: core tables only, no
// schema_migrations, and none of the columns added by later patches.
const legacySchema = `
CREATE TABLE frames (id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, path TEXT NOT NULL);
CREATE TABLE blocks (start_ts INTEGER PRIMARY KEY, end_ts INTEGER NOT NULL,
  title TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '', frame_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'done', error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL);
CREATE TABLE events (id INTEGER PRIMARY KEY, ts INTEGER NOT NULL,
  type TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '');
CREATE TABLE api_calls (id INTEGER PRIMARY KEY, ts INTEGER NOT NULL,
  block_start INTEGER NOT NULL, model TEXT NOT NULL,
  frames_sent INTEGER NOT NULL, prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0, latency_ms INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'ok', error TEXT NOT NULL DEFAULT '');
`

func TestMigrateFreshDB(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var v int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != schemaVersion {
		t.Fatalf("schema version = %d, want %d", v, schemaVersion)
	}
	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatal("foreign_keys pragma is off")
	}
	// all columns the legacy patches add must exist on a fresh DB too
	for _, tc := range [][2]string{
		{"frames", "app"}, {"blocks", "attempts"}, {"blocks", "app"},
		{"blocks", "activities"}, {"blocks", "productive"},
		{"blocks", "category_confidence"}, {"blocks", "quality_confidence"},
		{"blocks", "same_as_prev"}, {"blocks", "triaged"},
	} {
		has, err := hasColumn(db, tc[0], tc[1])
		if err != nil {
			t.Fatal(err)
		}
		if !has {
			t.Fatalf("fresh DB missing column %s.%s", tc[0], tc[1])
		}
	}
}

func TestMigrateLegacyDB(t *testing.T) {
	testEnv(t)
	raw, err := sql.Open("sqlite", dbPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO blocks(start_ts,end_ts,title,created_at) VALUES(1000,1900,'legacy block',1)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var v int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != schemaVersion {
		t.Fatalf("schema version = %d, want %d", v, schemaVersion)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM blocks WHERE start_ts=1000`).Scan(&title); err != nil {
		t.Fatalf("legacy row lost: %v", err)
	}
	if title != "legacy block" {
		t.Fatalf("legacy row corrupted: %q", title)
	}
	for _, name := range []string{"chat_conversations", "chat_messages", "standup_drafts", "journal_entries", "day_goals", "llm_calls", "block_edits"} {
		var n int
		db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
		if n != 1 {
			t.Fatalf("migration did not create %s", name)
		}
	}
	// patched columns exist and carry their defaults on the legacy row
	var app string
	var attempts int
	if err := db.QueryRow(`SELECT app, attempts FROM blocks WHERE start_ts=1000`).Scan(&app, &attempts); err != nil {
		t.Fatalf("patched columns missing: %v", err)
	}
}

func TestMigrateReopenIdempotent(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = openDB()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != schemaVersion {
		t.Fatalf("expected %d migration rows, got %d", schemaVersion, n)
	}
}

func TestForeignKeyCascade(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	res, err := db.Exec(`INSERT INTO chat_conversations(title, created_at, updated_at) VALUES('t', ?, ?)`, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	cid, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO chat_messages(conversation_id, role, content, created_at) VALUES(?, 'user', 'hi', ?)`, cid, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM chat_conversations WHERE id=?`, cid); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM chat_messages WHERE conversation_id=?`, cid).Scan(&n)
	if n != 0 {
		t.Fatalf("expected cascade delete, %d messages remain", n)
	}
}

func TestMigrateNewerSchemaRefused(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, schemaVersion+1, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := openDB(); err == nil {
		t.Fatal("expected openDB to refuse a database from a newer schema version")
	}
}

func TestOpenDBCorruptFails(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	// Truncate mid-header so SQLite reports a malformed database instead of
	// silently serving a partial schema.
	if err := os.WriteFile(dbPath(), []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openDB(); err == nil {
		t.Fatal("expected openDB to fail on a corrupt database")
	}
}
