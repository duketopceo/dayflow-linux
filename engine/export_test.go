package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteExportFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "exports", "week.md")
	if err := writeExportFile(out, []byte("# hello\n")); err != nil {
		t.Fatalf("writeExportFile: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "# hello\n" {
		t.Fatalf("content mismatch: %q", got)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("export file mode = %o, want 600", fi.Mode().Perm())
	}
	// parent dir created 0700 (export carries journal text)
	di, err := os.Stat(filepath.Join(dir, "exports"))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("exports dir mode = %o, want 700", di.Mode().Perm())
	}
	// no stray tmp file
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file left behind")
	}
}

func TestWriteExportFileBadPath(t *testing.T) {
	// parent under a file (not a dir) must fail cleanly
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(blocker, "week.md")
	if err := writeExportFile(out, []byte("x")); err == nil {
		t.Fatal("expected error writing under a non-directory path")
	}
	// nothing left behind
	matches, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(matches) != 1 || matches[0] != blocker {
		t.Fatalf("unexpected leftover files: %v", matches)
	}
}

func TestWriteExportFileNoPartialOnReadOnlyDir(t *testing.T) {
	if syscall.Getuid() == 0 {
		t.Skip("running as root; permission checks are bypassed")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.MkdirAll(ro, 0o700); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(ro, "week.md")
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ro, 0o700)
	if err := writeExportFile(out, []byte("x")); err == nil {
		t.Fatal("expected error writing into read-only dir")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("partial target file exists after failed write")
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("tmp file left behind after failed write")
	}
}
