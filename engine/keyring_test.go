package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeOmaSeal puts a stub `omaseal` on PATH backed by plain files in dir.
func fakeOmaSeal(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	if err := os.MkdirAll(store, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
cmd="$1"; shift
case "$cmd" in
  get) cat "` + store + `/$2" 2>/dev/null || exit 1 ;;
  set) cat > "` + store + `/$2" ;;
  del) rm -f "` + store + `/$2" ;;
  list) ls "` + store + `" ;;
  ping) exit 0 ;;
esac
`
	bin := filepath.Join(dir, "omaseal")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return store
}

func TestKeyringSetGetDel(t *testing.T) {
	store := fakeOmaSeal(t)
	if !keyringAvailable() {
		t.Fatal("fake omaseal not detected on PATH")
	}
	if err := keyringSet("openrouter", "sk-test-123"); err != nil {
		t.Fatalf("keyringSet: %v", err)
	}
	got, err := keyringGet("openrouter")
	if err != nil || got != "sk-test-123" {
		t.Fatalf("keyringGet = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(store, "openrouter")); err != nil {
		t.Fatalf("store file missing: %v", err)
	}
	if err := keyringDel("openrouter"); err != nil {
		t.Fatalf("keyringDel: %v", err)
	}
	if _, err := keyringGet("openrouter"); err == nil {
		t.Fatal("keyringGet after del should fail")
	}
}

func TestKeyringMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if keyringAvailable() {
		t.Fatal("keyringAvailable true without omaseal")
	}
	if _, err := keyringGet("openrouter"); err == nil {
		t.Fatal("keyringGet should error without omaseal")
	}
}

func TestResolveProviderKeyFallsBackToKeyring(t *testing.T) {
	fakeOmaSeal(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))
	t.Setenv("DAYFLOW_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("OPENROUTER_API_KEY", "")
	writeConfig(Config{
		Providers: []Provider{{
			ID: "default", Name: "OpenRouter", Kind: "openrouter",
			Model: "m", APIBaseURL: "https://openrouter.ai/api/v1",
		}},
		Routing: Routing{Primary: "default"},
	})
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.OpenRouterAPIKey; got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}
	// Seed the fake keyring, then reload — key should resolve via omaseal.
	if err := keyringSet("openrouter", "sk-ring-9"); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenRouterAPIKey != "sk-ring-9" {
		t.Fatalf("keyring fallback not applied, got %q", cfg.OpenRouterAPIKey)
	}
	p := cfg.Providers[0]
	if got := resolveProviderKey(p); got != "sk-ring-9" {
		t.Fatalf("resolveProviderKey = %q", got)
	}
}
