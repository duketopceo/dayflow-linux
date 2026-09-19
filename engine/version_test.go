package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The release version is repeated in three places by design (manifest for the
// plugin loader, the Go const for the binary, pluginVersion in Panel.qml for
// drift warnings). This test is the consistency check so a bump can't land in
// only two of the three.
func TestVersionConsistency(t *testing.T) {
	root := filepath.Join("..")

	mf, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(mf, &m); err != nil {
		t.Fatalf("manifest.json parse: %v", err)
	}
	if m.Version != version {
		t.Errorf("manifest.json version %q != engine version %q", m.Version, version)
	}

	panel, err := os.ReadFile(filepath.Join(root, "Panel.qml"))
	if err != nil {
		t.Fatalf("Panel.qml: %v", err)
	}
	re := regexp.MustCompile(`pluginVersion:\s*"([^"]+)"`)
	mm := re.FindSubmatch(panel)
	if mm == nil {
		t.Fatal("Panel.qml: pluginVersion not found")
	}
	if string(mm[1]) != version {
		t.Errorf("Panel.qml pluginVersion %q != engine version %q", mm[1], version)
	}
}
