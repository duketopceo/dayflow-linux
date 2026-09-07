package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderForTaskFallback(t *testing.T) {
	cfg := defaultConfig()
	cfg.Providers = []Provider{
		{ID: "or1", Kind: "openrouter", APIKey: "k", Model: "m", Enabled: true},
		{ID: "local1", Kind: "local", APIBaseURL: "http://localhost:11434/v1", Model: "m", Enabled: false},
	}
	cfg.Routing = Routing{Primary: "or1", TaskProvider: map[string]string{"chat": "local1"}}

	// task override points at a disabled provider -> falls back to primary
	p, err := providerForTask(cfg, "chat")
	if err != nil || p.ID != "or1" {
		t.Fatalf("providerForTask chat = %+v, err=%v", p, err)
	}
	// explicit task override wins when enabled
	cfg.Providers[1].Enabled = true
	p, err = providerForTask(cfg, "chat")
	if err != nil || p.ID != "local1" {
		t.Fatalf("providerForTask chat = %+v, err=%v", p, err)
	}
	// unmapped task uses primary
	p, err = providerForTask(cfg, "review")
	if err != nil || p.ID != "or1" {
		t.Fatalf("providerForTask review = %+v, err=%v", p, err)
	}
	// secondary is used when primary is disabled
	cfg.Providers[0].Enabled = false
	cfg.Routing.Secondary = "local1"
	p, err = providerForTask(cfg, "review")
	if err != nil || p.ID != "local1" {
		t.Fatalf("providerForTask review secondary = %+v, err=%v", p, err)
	}
	// nothing enabled -> error
	cfg.Providers[1].Enabled = false
	if _, err = providerForTask(cfg, "review"); err == nil {
		t.Fatal("expected error when no provider is enabled")
	}
}

func TestLoadConfigMigratesLegacyKeys(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	os.WriteFile(cp, []byte(`{
		"provider": "custom",
		"model": "my-model",
		"api_base_url": "http://example.com",
		"openrouter_api_key": "sk-test"
	}`), 0o600)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("providers=%v", cfg.Providers)
	}
	p := cfg.Providers[0]
	if p.Kind != "custom" || p.Model != "my-model" || p.APIKey != "sk-test" || !p.Enabled {
		t.Fatalf("migrated provider=%+v", p)
	}
	if p.APIBaseURL != "http://example.com/v1" {
		t.Fatalf("api_base_url=%q", p.APIBaseURL)
	}
	if cfg.Routing.Primary != p.ID {
		t.Fatalf("routing.primary=%q", cfg.Routing.Primary)
	}
}

func TestCallProviderText(t *testing.T) {
	testEnv(t)
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req orRequest
		json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "hello there"}},
			},
			"usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 3},
		})
	}))
	defer srv.Close()

	cfg := defaultConfig()
	cfg.Providers = []Provider{
		{ID: "p1", Kind: "custom", APIBaseURL: srv.URL, APIKey: "sk-x", Model: "text-model", Enabled: true},
	}
	cfg.Routing.Primary = "p1"

	text, pt, ct, err := callProviderText(cfg, "review", "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello there" || pt != 12 || ct != 3 {
		t.Fatalf("text=%q pt=%d ct=%d", text, pt, ct)
	}
	if gotAuth != "Bearer sk-x" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotModel != "text-model" {
		t.Fatalf("model=%q", gotModel)
	}
}

func TestLocalProviderSendsNoAuth(t *testing.T) {
	testEnv(t)
	authed := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authed = r.Header.Get("Authorization") != ""
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}},
			},
		})
	}))
	defer srv.Close()

	cfg := defaultConfig()
	cfg.Providers = []Provider{
		{ID: "l1", Kind: "local", APIBaseURL: srv.URL, Model: "m", Enabled: true},
	}
	cfg.Routing.Primary = "l1"

	if _, _, _, err := callProviderText(cfg, "chat", "s", "u"); err != nil {
		t.Fatal(err)
	}
	if authed {
		t.Fatal("local provider received an Authorization header")
	}
}

func TestPromptOverrideUsed(t *testing.T) {
	cfg := defaultConfig()
	p := Provider{PromptOverrides: PromptOverrides{SummaryPrompt: "CUSTOM PROMPT"}}
	got := buildPromptForProvider(p, cfg)
	if got == buildSummarizePrompt(cfg) || got[:13] != "CUSTOM PROMPT" {
		t.Fatalf("override not used: %q", got[:60])
	}
	if p2 := (Provider{}); buildPromptForProvider(p2, cfg) != buildSummarizePrompt(cfg) {
		t.Fatal("default template not used without override")
	}
}
