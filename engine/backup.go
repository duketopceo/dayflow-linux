package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// backupKeep is how many timestamped backups are retained under the dest dir.
const backupKeep = 7

type backupManifest struct {
	EngineVersion string `json:"engine_version"`
	SchemaVersion int    `json:"schema_version"`
	CreatedAt     int64  `json:"created_at"`
	Frames        bool   `json:"frames_included"`
	Blocks        int    `json:"blocks"`
	FramesCount   int    `json:"frames_count"`
}

func defaultBackupDir() string {
	if d := os.Getenv("DAYFLOW_BACKUP_DIR"); d != "" {
		return d
	}
	// sibling of the data dir so snapshots don't count against max_storage_mb
	return filepath.Join(filepath.Dir(dataDir()), "dayflow-backups")
}

// runBackup writes a consistent snapshot into dest/dayflow-<timestamp>/:
// a VACUUM INTO database copy (safe while WAL is active), a secret-redacted
// config.json, retained frames (hardlinked when possible), and a manifest.
func runBackup(db *sql.DB, cfg Config, dest string, includeFrames bool) (string, error) {
	if dest == "" {
		dest = defaultBackupDir()
	}
	final := filepath.Join(dest, "dayflow-"+time.Now().Format("20060102-150405"))
	tmp := final + ".tmp"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return "", err
	}

	// consistent snapshot of the live WAL database
	dbDst := filepath.Join(tmp, "dayflow.db")
	if _, err := db.Exec(fmt.Sprintf("VACUUM INTO '%s'", strings.ReplaceAll(dbDst, "'", "''"))); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("snapshot db: %w", err)
	}
	os.Chmod(dbDst, 0o600)

	// config with secrets redacted — a backup should not smuggle API keys
	if b, err := os.ReadFile(configPath()); err == nil {
		var m map[string]json.RawMessage
		if json.Unmarshal(b, &m) == nil {
			redactSecrets(m)
			if rb, err := json.MarshalIndent(m, "", "  "); err == nil {
				os.WriteFile(filepath.Join(tmp, "config.json"), rb, 0o600)
			}
		}
	}

	framesCopied := 0
	if _, err := os.Stat(framesDir()); includeFrames && err == nil {
		walkErr := filepath.Walk(framesDir(), func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !fi.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(framesDir(), p)
			if err != nil {
				return err
			}
			d := filepath.Join(tmp, "frames", rel)
			if err := os.MkdirAll(filepath.Dir(d), 0o700); err != nil {
				return err
			}
			if err := os.Link(p, d); err != nil {
				if err := copyFile(p, d); err != nil {
					return err
				}
			}
			os.Chmod(d, 0o600)
			framesCopied++
			return nil
		})
		if walkErr != nil {
			os.RemoveAll(tmp)
			return "", fmt.Errorf("copy frames: %w", walkErr)
		}
	}

	var blocks, fcount int
	db.QueryRow(`SELECT COUNT(1) FROM blocks`).Scan(&blocks)
	db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&fcount)
	m := backupManifest{
		EngineVersion: version, SchemaVersion: schemaVersion,
		CreatedAt: time.Now().Unix(), Frames: includeFrames,
		Blocks: blocks, FramesCount: fcount,
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), mb, 0o600); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}

	if err := os.Rename(tmp, final); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	logEvent(db, "backup", fmt.Sprintf("%s (%d frames)", final, framesCopied))
	pruneBackups(dest)
	return final, nil
}

// redactSecrets replaces every credential-looking value in a parsed config
// tree with a placeholder, recursing into nested objects and arrays.
func redactSecrets(m map[string]json.RawMessage) {
	for k, v := range m {
		lk := strings.ToLower(k)
		if lk == "api_key" || strings.HasSuffix(lk, "_api_key") ||
			lk == "token" || strings.Contains(lk, "secret") || strings.Contains(lk, "password") {
			m[k] = json.RawMessage(`"***redacted***"`)
			continue
		}
		if obj := map[string]json.RawMessage(nil); json.Unmarshal(v, &obj) == nil && obj != nil {
			redactSecrets(obj)
			if nb, err := json.Marshal(obj); err == nil {
				m[k] = nb
			}
			continue
		}
		var arr []json.RawMessage
		if json.Unmarshal(v, &arr) == nil && arr != nil {
			for i, e := range arr {
				var o map[string]json.RawMessage
				if json.Unmarshal(e, &o) == nil && o != nil {
					redactSecrets(o)
					if nb, err := json.Marshal(o); err == nil {
						arr[i] = nb
					}
				}
			}
			if nb, err := json.Marshal(arr); err == nil {
				m[k] = nb
			}
		}
	}
}

// pruneBackups removes all but the newest backupKeep snapshots under dest.
// Only timestamped snapshot dirs are touched — a user-chosen dest may hold
// unrelated entries that happen to start with "dayflow-".
func pruneBackups(dest string) {
	matches, _ := filepath.Glob(filepath.Join(dest, "dayflow-????????-??????"))
	var dirs []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.IsDir() {
			dirs = append(dirs, m)
		}
	}
	sort.Strings(dirs) // timestamps sort chronologically
	for _, d := range dirs[:max(0, len(dirs)-backupKeep)] {
		os.RemoveAll(d)
	}
}

// backupVerify checks a backup dir is structurally sound: manifest parses and
// is schema-compatible, and the db snapshot passes a quick integrity check.
func backupVerify(dir string) (backupManifest, error) {
	var m backupManifest
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return m, fmt.Errorf("no manifest.json: %w", err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("bad manifest: %w", err)
	}
	if m.SchemaVersion > schemaVersion {
		return m, fmt.Errorf("backup schema v%d is newer than this binary supports (v%d)",
			m.SchemaVersion, schemaVersion)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "dayflow.db")+"?mode=ro")
	if err != nil {
		return m, err
	}
	defer db.Close()
	var qc string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&qc); err != nil {
		return m, fmt.Errorf("backup db unreadable: %w", err)
	}
	if qc != "ok" {
		return m, fmt.Errorf("backup db integrity check failed: %s", qc)
	}
	return m, nil
}

// runRestore copies a verified backup into the data dir. It refuses to
// overwrite an existing database without force. Stop dayflow-capture and
// dayflow-summarize before restoring.
func runRestore(dir string, force bool) error {
	m, err := backupVerify(dir)
	if err != nil {
		return err
	}
	// Restoring over a live capture daemon corrupts both the snapshot and
	// the running process's open database handle. The systemd service only
	// writes the default data dir, so scoped restores (DAYFLOW_DATA_DIR,
	// tests) skip this check.
	if os.Getenv("DAYFLOW_DATA_DIR") == "" &&
		exec.Command("systemctl", "--user", "is-active", "--quiet",
			"dayflow-capture.service").Run() == nil {
		return fmt.Errorf("dayflow-capture is running — stop it first: systemctl --user stop dayflow-capture")
	}
	if _, err := os.Stat(dbPath()); err == nil && !force {
		return fmt.Errorf("data dir already has a database — pass --force to overwrite")
	}
	if err := os.MkdirAll(dataDir(), 0o700); err != nil {
		return err
	}
	// copy to a sibling temp file first: a partial copy must never leave a
	// truncated dayflow.db behind
	tmpDB := dbPath() + ".restore-tmp"
	if err := copyFile(filepath.Join(dir, "dayflow.db"), tmpDB); err != nil {
		os.Remove(tmpDB)
		return fmt.Errorf("restore db: %w", err)
	}
	// remove a stale wal/shm from a previous db so the snapshot is authoritative
	for _, ext := range []string{"-wal", "-shm"} {
		os.Remove(dbPath() + ext)
	}
	if err := os.Rename(tmpDB, dbPath()); err != nil {
		os.Remove(tmpDB)
		return fmt.Errorf("restore db: %w", err)
	}
	os.Chmod(dbPath(), 0o600)

	if m.Frames {
		src := filepath.Join(dir, "frames")
		err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
			if err != nil || !fi.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return nil
			}
			d := filepath.Join(framesDir(), rel)
			if err := os.MkdirAll(filepath.Dir(d), 0o700); err != nil {
				return nil
			}
			if err := copyFile(p, d); err != nil {
				return err
			}
			os.Chmod(d, 0o600)
			return nil
		})
		if err != nil {
			return fmt.Errorf("restore frames: %w", err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
