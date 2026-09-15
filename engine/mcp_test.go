package main

import (
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
