package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The classic-UI restore (91ee9fe baseline) carries forward fixes that shipped
// inside UI files after the pin. Without these carries a verbatim restore
// silently reintroduces the original bugs: a stale panel snapshot clobbering
// provider prompt overrides (#52), and the agents pane killed mid-briefing
// by a 75s watchdog (#43). This test pins the carries so a future restore
// can't drop them without a red suite.
func TestClassicUICarryForwards(t *testing.T) {
	root := filepath.Join("..")

	panel, err := os.ReadFile(filepath.Join(root, "Panel.qml"))
	if err != nil {
		t.Fatalf("Panel.qml: %v", err)
	}
	for _, want := range []string{"delete patch.providers", "delete patch.routing"} {
		if !strings.Contains(string(panel), want) {
			t.Errorf("Panel.qml missing #52 carry %q — stale snapshot would clobber provider edits", want)
		}
	}

	full, err := os.ReadFile(filepath.Join(root, "FullView.qml"))
	if err != nil {
		t.Fatalf("FullView.qml: %v", err)
	}
	if !strings.Contains(string(full), "interval: 300000") {
		t.Errorf("FullView.qml missing #43 carry (300s agents watchdog)")
	}
}
