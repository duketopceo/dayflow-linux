package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestConfig drops a config.json containing secrets and points
// DAYFLOW_CONFIG at it.
func writeTestConfig(t *testing.T) {
	t.Helper()
	p := filepath.Join(dataDir(), "config.json")
	cfg := `{"provider":"openrouter","openrouter_api_key":"sk-secret-123",
"providers":[{"id":"p1","kind":"openrouter","api_key":"sk-provider-456"}]}`
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAYFLOW_CONFIG", p)
}

func TestBackupSnapshot(t *testing.T) {
	cfg := testEnv(t)
	writeTestConfig(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ts := time.Now().Add(-time.Hour)
	markSummarized(t, db, cfg, ts)
	framePath := addFrameFile(t, db, ts, 10)

	dest := filepath.Join(t.TempDir(), "backups")
	dir, err := runBackup(db, cfg, dest, true)
	if err != nil {
		t.Fatal(err)
	}

	// snapshot db: opens clean, integrity ok, has our block
	bdb, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "dayflow.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer bdb.Close()
	var qc string
	if err := bdb.QueryRow(`PRAGMA quick_check`).Scan(&qc); err != nil || qc != "ok" {
		t.Fatalf("backup db quick_check = %q, err %v", qc, err)
	}
	var blocks int
	if err := bdb.QueryRow(`SELECT COUNT(1) FROM blocks`).Scan(&blocks); err != nil || blocks != 1 {
		t.Fatalf("backup db blocks = %d, err %v", blocks, err)
	}
	var sv int
	if err := bdb.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&sv); err != nil || sv != schemaVersion {
		t.Fatalf("backup schema version = %d, err %v", sv, err)
	}

	// manifest
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m backupManifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != schemaVersion || m.EngineVersion != version {
		t.Fatalf("manifest = %+v", m)
	}

	// config is backed up but secrets are redacted
	cb, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cb), "sk-secret-123") || strings.Contains(string(cb), "sk-provider-456") {
		t.Fatal("backup config leaked an API key")
	}

	// frames copied
	if _, err := os.Stat(filepath.Join(dir, "frames", ts.Format("2006-01-02"), filepath.Base(framePath))); err != nil {
		t.Fatalf("frame missing from backup: %v", err)
	}
}

func TestBackupNoFrames(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := time.Now().Add(-time.Hour)
	markSummarized(t, db, cfg, ts)
	addFrameFile(t, db, ts, 10)

	dir, err := runBackup(db, cfg, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "frames")); !os.IsNotExist(err) {
		t.Fatal("frames dir present despite --no-frames")
	}
}

func TestBackupPrunesOld(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dest := t.TempDir()
	for i := 0; i < backupKeep+3; i++ {
		d := filepath.Join(dest, fmt.Sprintf("dayflow-2020010%d-120000", i))
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Unrelated entries sharing the prefix are never pruned.
	stray := filepath.Join(dest, "dayflow-notes")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runBackup(db, cfg, dest, false); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dest, "dayflow-????????-??????"))
	if len(matches) > backupKeep {
		t.Fatalf("%d backups kept, want <= %d", len(matches), backupKeep)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("unrelated dayflow-* entry was pruned: %v", err)
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	markSummarized(t, db, cfg, time.Now().Add(-time.Hour))
	db.Close()

	dest := filepath.Join(t.TempDir(), "bk")
	db2, _ := openDB()
	dir, err := runBackup(db2, cfg, dest, true)
	db2.Close()
	if err != nil {
		t.Fatal(err)
	}

	// point at a fresh empty data dir and restore
	newData := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", newData)
	if err := runRestore(dir, false); err != nil {
		t.Fatal(err)
	}
	rdb, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	var blocks int
	if err := rdb.QueryRow(`SELECT COUNT(1) FROM blocks`).Scan(&blocks); err != nil || blocks != 1 {
		t.Fatalf("restored blocks = %d, err %v", blocks, err)
	}
	fi, err := os.Stat(filepath.Join(newData, "dayflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("restored db mode = %o, want 600", fi.Mode().Perm())
	}
}

func TestRestoreRefusesNonEmpty(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := runBackup(db, cfg, t.TempDir(), false)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	// data dir already has a live db — restore must refuse without --force
	if err := runRestore(dir, false); err == nil {
		t.Fatal("restore overwrote live data without --force")
	}
	if err := runRestore(dir, true); err != nil {
		t.Fatalf("restore --force failed: %v", err)
	}
}

func TestBackupVerifyDetectsCorruption(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := runBackup(db, cfg, t.TempDir(), false)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dayflow.db"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backupVerify(dir); err == nil {
		t.Fatal("verify accepted a corrupt backup")
	}
}

func TestRestoreRejectsNewerSchema(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := runBackup(db, cfg, t.TempDir(), false)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	// rewrite the manifest as a newer schema
	mp := filepath.Join(dir, "manifest.json")
	b, _ := os.ReadFile(mp)
	var m backupManifest
	json.Unmarshal(b, &m)
	m.SchemaVersion = schemaVersion + 99
	nb, _ := json.Marshal(m)
	if err := os.WriteFile(mp, nb, 0o600); err != nil {
		t.Fatal(err)
	}

	newData := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", newData)
	if err := runRestore(dir, false); err == nil {
		t.Fatal("restore accepted a backup from a newer schema")
	}
}
