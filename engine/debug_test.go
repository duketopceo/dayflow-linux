package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendLogRotatesAcrossMidnight(t *testing.T) {
	testEnv(t)
	p := debugLogPath()
	if err := os.WriteFile(p, []byte("old line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// mtime yesterday evening — the 24h-ago check would NOT rotate this
	yesterday := time.Now().Add(-2 * time.Hour)
	if yesterday.Day() == time.Now().Day() {
		yesterday = yesterday.Add(-24 * time.Hour)
	}
	if err := os.Chtimes(p, yesterday, yesterday); err != nil {
		t.Fatal(err)
	}

	appendLog("new line")

	arch := filepath.Join(dataDir(), "debug-"+yesterday.Format("20060102")+".log")
	if _, err := os.Stat(arch); err != nil {
		t.Fatal("expected rotated archive for yesterday's log")
	}
	b, _ := os.ReadFile(p)
	if string(b) == "old line\n" {
		t.Fatal("current log still holds yesterday's line")
	}
}

func TestPruneLogArchives(t *testing.T) {
	testEnv(t)
	old := time.Now().Add(-15 * 24 * time.Hour)
	fresh := time.Now().Add(-2 * 24 * time.Hour)
	for _, ts := range []time.Time{old, fresh} {
		p := filepath.Join(dataDir(), "debug-"+ts.Format("20060102")+".log")
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, ts, ts)
	}
	pruneLogArchives()
	if _, err := os.Stat(filepath.Join(dataDir(), "debug-"+old.Format("20060102")+".log")); !os.IsNotExist(err) {
		t.Fatal("15-day archive should be pruned")
	}
	if _, err := os.Stat(filepath.Join(dataDir(), "debug-"+fresh.Format("20060102")+".log")); err != nil {
		t.Fatal("2-day archive should be kept")
	}
}

func TestAppendLogSanitizesControlChars(t *testing.T) {
	testEnv(t)
	appendLog("ui: first\nsecond\r\nthird" + string(rune(0)) + "tail")

	data, err := os.ReadFile(debugLogPath())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("forged lines: got %d, want 1: %q", len(lines), data)
	}
	if !strings.Contains(lines[0], "ui: first second third tail") {
		t.Fatalf("bad sanitized line: %q", lines[0])
	}
}

func TestSanitizeLogLine(t *testing.T) {
	if got := sanitizeLogLine("a\nb\rc\td"); got != "a b c d" {
		t.Fatalf("control chars: %q", got)
	}
	long := strings.Repeat("x", 600)
	if got := sanitizeLogLine(long); len([]rune(got)) != 501 {
		t.Fatalf("cap: %d runes", len([]rune(got)))
	}
}
