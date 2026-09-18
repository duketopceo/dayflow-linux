package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestMCPReadOnlyBlocksChat(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := mcpCall(db, cfg, true, "chat", map[string]any{"message": "hi"}); err == nil ||
		!strings.Contains(err.Error(), "read-only") {
		t.Fatalf("chat not blocked in read-only mode: %v", err)
	}
	if _, err := mcpCall(db, cfg, true, "get_status", map[string]any{}); err != nil {
		t.Fatalf("read-only tool failed: %v", err)
	}
}

func TestMCPToolListFiltersReadOnly(t *testing.T) {
	for _, tool := range mcpToolList(true) {
		if tool["name"] == "chat" {
			t.Fatal("read-only tool list exposes chat")
		}
	}
	found := false
	for _, tool := range mcpToolList(false) {
		if tool["name"] == "chat" {
			found = true
		}
	}
	if !found {
		t.Fatal("chat missing from full tool list")
	}
}

func TestMCPReadToolsAreReadOnlyByConstruction(t *testing.T) {
	// Every advertised tool except chat must be a pure read — guard against
	// a future mutating tool being added without joining mcpMutating.
	for _, tool := range mcpTools {
		name := tool["name"].(string)
		if name != "chat" && mcpMutating[name] {
			t.Fatalf("tool %q marked mutating but is not chat", name)
		}
	}
}

func TestMCPUnknownTool(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := mcpCall(db, cfg, false, "nope", nil); err == nil {
		t.Fatal("unknown tool did not error")
	}
}

func TestReadMCPLine(t *testing.T) {
	t.Run("normal lines", func(t *testing.T) {
		br := bufio.NewReader(strings.NewReader(`{"a":1}` + "\n" + `{"b":2}` + "\n"))
		l1, err := readMCPLine(br)
		if err != nil || string(l1) != `{"a":1}`+"\n" {
			t.Fatalf("line1: %q %v", l1, err)
		}
		l2, err := readMCPLine(br)
		if err != nil || string(l2) != `{"b":2}`+"\n" {
			t.Fatalf("line2: %q %v", l2, err)
		}
		if _, err := readMCPLine(br); err != io.EOF {
			t.Fatalf("expected io.EOF, got %v", err)
		}
	})

	t.Run("final line without newline", func(t *testing.T) {
		br := bufio.NewReader(strings.NewReader(`{"x":true}`))
		l, err := readMCPLine(br)
		if err != nil || string(l) != `{"x":true}` {
			t.Fatalf("line: %q %v", l, err)
		}
		if _, err := readMCPLine(br); err != io.EOF {
			t.Fatalf("expected io.EOF, got %v", err)
		}
	})

	t.Run("oversized line errors and reader resyncs", func(t *testing.T) {
		big := strings.Repeat("x", mcpMaxRequestBytes+10)
		br := bufio.NewReaderSize(strings.NewReader(big+"\n"+`{"ok":1}`+"\n"), 64)
		if _, err := readMCPLine(br); err != errLineTooLarge {
			t.Fatalf("expected errLineTooLarge, got %v", err)
		}
		l, err := readMCPLine(br)
		if err != nil || string(l) != `{"ok":1}`+"\n" {
			t.Fatalf("resync failed: %q %v", l, err)
		}
	})

	t.Run("oversized without newline is drained", func(t *testing.T) {
		br := bufio.NewReaderSize(strings.NewReader(strings.Repeat("y", mcpMaxRequestBytes+10)), 64)
		if _, err := readMCPLine(br); err != errLineTooLarge {
			t.Fatalf("expected errLineTooLarge, got %v", err)
		}
	})

	t.Run("blank lines pass through", func(t *testing.T) {
		br := bufio.NewReader(strings.NewReader("\n\n{}\n"))
		l, err := readMCPLine(br)
		if err != nil || string(l) != "\n" {
			t.Fatalf("blank line: %q %v", l, err)
		}
	})
}
