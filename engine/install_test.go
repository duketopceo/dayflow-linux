package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnitOwned(t *testing.T) {
	dir := t.TempDir()
	body := "[Unit]\nDescription=test\n"
	expected := unitMarker + body

	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"marked body", write("marked.service", expected), true},
		{"legacy unmarked body", write("legacy.service", body), true},
		{"foreign content", write("foreign.service", "[Unit]\nDescription=mine\n"), false},
		{"missing file", filepath.Join(dir, "absent.service"), false},
		{"oversized", write("big.service", expected+strings.Repeat("x", 65<<10)), false},
	}

	// A symlink is never ours even when its target matches.
	target := write("target.service", expected)
	link := filepath.Join(dir, "link.service")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		name string
		path string
		want bool
	}{"symlink", link, false})

	for _, c := range cases {
		if got := unitOwned(c.path, expected); got != c.want {
			t.Errorf("%s: unitOwned = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWriteUnitAtomic(t *testing.T) {
	dir := t.TempDir()
	body := unitMarker + "[Unit]\nDescription=test\n"

	if err := writeUnitAtomic(dir, "a.service", body); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.service"))
	if err != nil || string(got) != body {
		t.Fatalf("wrote %q, err %v", got, err)
	}
	// No temp files left behind.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("leftover temp files: %v", entries)
	}

	// Replacing a planted symlink swaps the link, never follows it.
	victim := filepath.Join(dir, "victim.service")
	if err := os.WriteFile(victim, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "b.service")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	if err := writeUnitAtomic(dir, "b.service", body); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("link not replaced by regular file: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep me" {
		t.Fatal("symlink target was overwritten")
	}
}

func TestUnitSetMarked(t *testing.T) {
	for name, body := range unitSet() {
		if !strings.HasPrefix(body, unitMarker) {
			t.Errorf("%s: unit body missing ownership marker", name)
		}
	}
}
