package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

// stubBackoff removes the real waits so retry tests stay fast.
func stubBackoff(t *testing.T) {
	t.Helper()
	orig := providerBackoff
	providerBackoff = func(int) time.Duration { return 0 }
	t.Cleanup(func() { providerBackoff = orig })
}

// testProvider builds a provider whose chat endpoint is the given test server.
// Kind "custom" requires auth, and the key is set inline so no keyring is used.
func testProvider(baseURL string) Provider {
	return Provider{
		ID:         "default",
		Kind:       "custom",
		APIBaseURL: baseURL,
		Model:      "test/model",
		APIKey:     "test-key",
		Enabled:    true,
	}
}

func okBody(content string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"content":%q}}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`, content)
}

func TestIsTransientProviderErr(t *testing.T) {
	if !isTransientProviderErr(transientf("boom %d", 1)) {
		t.Error("transientf error must be transient")
	}
	if isTransientProviderErr(errors.New("boom")) {
		t.Error("plain error must not be transient")
	}
	if !isTransientProviderErr(fmt.Errorf("wrapped: %w", transientf("boom"))) {
		t.Error("wrapped transient error must stay transient")
	}
	if isTransientProviderErr(nil) {
		t.Error("nil must not be transient")
	}
}

func TestCallProviderChatRetriesTransientThenSucceeds(t *testing.T) {
	stubBackoff(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"502"}}`))
			return
		}
		_, _ = w.Write([]byte(okBody("done")))
	}))
	defer srv.Close()

	got, pt, ct, err := callProviderChat(Config{SiteName: "t"}, testProvider(srv.URL), []orMessage{{Role: "user"}})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got != "done" {
		t.Fatalf("content = %q, want %q", got, "done")
	}
	if pt != 1 || ct != 2 {
		t.Fatalf("usage = %d/%d, want 1/2", pt, ct)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("server calls = %d, want 3", n)
	}
}

func TestCallProviderChatFailsFastOnAuthError(t *testing.T) {
	stubBackoff(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Missing Authentication header"}}`))
	}))
	defer srv.Close()

	_, _, _, err := callProviderChat(Config{SiteName: "t"}, testProvider(srv.URL), []orMessage{{Role: "user"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "api 401") {
		t.Fatalf("error = %q, want it to mention api 401", err)
	}
	if isTransientProviderErr(err) {
		t.Error("401 must not be retried")
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("server calls = %d, want 1 (no retry on 401)", n)
	}
}

func TestCallProviderChatRetriesEmptyBody(t *testing.T) {
	stubBackoff(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK) // 200 with nothing in it
	}))
	defer srv.Close()

	_, _, _, err := callProviderChat(Config{SiteName: "t"}, testProvider(srv.URL), []orMessage{{Role: "user"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "empty response body") {
		t.Fatalf("error = %q, want the empty-body message", err)
	}
	if n := atomic.LoadInt32(&calls); n != providerMaxAttempts {
		t.Fatalf("server calls = %d, want %d", n, providerMaxAttempts)
	}
}

func TestCallProviderChatRetriesTruncatedJSON(t *testing.T) {
	stubBackoff(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"half`)) // cut mid-stream
			return
		}
		_, _ = w.Write([]byte(okBody("recovered")))
	}))
	defer srv.Close()

	got, _, _, err := callProviderChat(Config{SiteName: "t"}, testProvider(srv.URL), []orMessage{{Role: "user"}})
	if err != nil {
		t.Fatalf("expected recovery after a truncated body, got %v", err)
	}
	if got != "recovered" {
		t.Fatalf("content = %q", got)
	}
}

// A provider with a custom base URL and no key used to send the request with no
// Authorization header, which OpenRouter answered with a bare 401.
func TestProviderChatRequiresKeyEvenWithCustomBaseURL(t *testing.T) {
	stubBackoff(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(okBody("should not happen")))
	}))
	defer srv.Close()

	p := testProvider(srv.URL)
	p.APIKey = ""
	_, _, _, err := callProviderChat(Config{SiteName: "t"}, p, []orMessage{{Role: "user"}})
	if err == nil || !strings.Contains(err.Error(), "no API key for provider") {
		t.Fatalf("error = %v, want the missing-key config error", err)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("server calls = %d, want 0 (must not egress without a key)", n)
	}
}

// RequestTimeoutSec replaces the old hard-coded 120s ceiling.
func TestCallProviderChatHonoursConfiguredTimeout(t *testing.T) {
	stubBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond)
		_, _ = w.Write([]byte(okBody("late")))
	}))
	defer srv.Close()

	start := time.Now()
	_, _, _, err := callProviderChat(Config{SiteName: "t", RequestTimeoutSec: 1}, testProvider(srv.URL), []orMessage{{Role: "user"}})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "api request failed") {
		t.Fatalf("error = %q, want a request failure", err)
	}
	if !isTransientProviderErr(err) {
		t.Error("a timeout must be retryable")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("took %s — timeout not applied", elapsed)
	}
}
