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

// writeFakeCLI drops an executable shell script in a temp dir and returns
// its path — the stand-in for cursor-agent/opencode in cli provider tests.
func writeFakeCLI(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fakecli")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func cliProvider(command string, args ...string) Provider {
	return Provider{
		ID: "cli1", Kind: "cli", Command: command, Args: args,
		Enabled: true,
	}
}

// A cli provider serves the routed chat task: the flattened prompt arrives
// on stdin and the child's stdout is the response.
func TestCLIProviderTextRoundTrip(t *testing.T) {
	testEnv(t)
	fake := writeFakeCLI(t, "#!/bin/sh\ncat\n")
	cfg := defaultConfig()
	cfg.Providers = []Provider{
		{ID: "or1", Kind: "openrouter", APIKey: "k", Model: "m", Enabled: true},
		cliProvider(fake, "--print"),
	}
	cfg.Routing = Routing{Primary: "or1", TaskProvider: map[string]string{"chat": "cli1"}}

	text, pt, ct, err := callProviderText(cfg, "chat", "SYSTEM PROMPT", "hello cli")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "SYSTEM PROMPT") || !strings.Contains(text, "hello cli") {
		t.Fatalf("echoed prompt missing parts: %q", text)
	}
	if pt != 0 || ct != 0 {
		t.Fatalf("cli calls report no tokens, got %d/%d", pt, ct)
	}
}

// cli needs no API key at all — resolveProviderKey is never consulted.
func TestCLIProviderNeedsNoAPIKey(t *testing.T) {
	if providerNeedsAuth(Provider{Kind: "cli"}) {
		t.Fatal("cli provider must not require an API key")
	}
	testEnv(t)
	fake := writeFakeCLI(t, "#!/bin/sh\necho ok\n")
	p := cliProvider(fake) // no APIKey, no keyring — must not hit the auth gate
	text, _, _, err := callProviderChat(defaultConfig(), p, []orMessage{{Role: "user", Content: []orContent{{Type: "text", Text: "hi"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if text != "ok" {
		t.Fatalf("text=%q", text)
	}
}

// Vision calls on a cli provider pass staged frame paths — via {file} args
// and referenced inside the prompt — instead of base64 payloads.
func TestCLIProviderVisionPassesFramePaths(t *testing.T) {
	testEnv(t)
	rec := t.TempDir()
	fake := writeFakeCLI(t, `#!/bin/sh
printf '%s\n' "$@" > "$REC/args.txt"
cat > "$REC/stdin.txt"
for a in "$@"; do
  case "$a" in
    --file=*) f="${a#--file=}"; [ -f "$f" ] && echo "$f" >> "$REC/readable.txt" ;;
  esac
done
printf '{"title":"CLI vision","summary":"did cli things","category":"coding"}\n'
`)
	t.Setenv("REC", rec)
	f1 := writeFrame(t, t.TempDir(), "a.jpg", time.Now())
	f2 := writeFrame(t, t.TempDir(), "b.jpg", time.Now())

	p := cliProvider(fake, "run", "--file={file}")
	p.AllowHotPath = true // required for the vision task
	p.EnvPassthrough = []string{"REC"}
	cfg := defaultConfig()
	cfg.Providers = []Provider{p}
	cfg.Routing = Routing{Primary: "cli1"}

	res, _, _, err := callOpenRouter(cfg, []string{f1, f2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "CLI vision" {
		t.Fatalf("title=%q", res.Title)
	}
	argsRaw, _ := os.ReadFile(filepath.Join(rec, "args.txt"))
	argLines := strings.Split(strings.TrimSpace(string(argsRaw)), "\n")
	if len(argLines) != 3 || argLines[0] != "run" {
		t.Fatalf("argv=%v", argLines)
	}
	for _, a := range argLines[1:] {
		if !strings.HasPrefix(a, "--file=") || !strings.Contains(a, "dayflow-cli-") {
			t.Fatalf("file arg not a staged path: %q", a)
		}
	}
	readable, _ := os.ReadFile(filepath.Join(rec, "readable.txt"))
	if n := len(strings.Split(strings.TrimSpace(string(readable)), "\n")); n != 2 {
		t.Fatalf("staged readable files=%d, want 2 (%q)", n, readable)
	}
	// The prompt also references the staged paths so CLIs without a file
	// flag can still read them.
	stdin, _ := os.ReadFile(filepath.Join(rec, "stdin.txt"))
	if !strings.Contains(string(stdin), "Frame files") || !strings.Contains(string(stdin), "frame-0") {
		t.Fatalf("prompt lacks frame paths: %q", truncate(string(stdin), 120))
	}
	// And the original frame paths were never exposed.
	if strings.Contains(string(stdin), f1) || strings.Contains(string(argsRaw), f1) {
		t.Fatal("original (unstaged) frame path leaked to the child")
	}
}

// Untrusted prompt text is never where a flag could be: on stdin by default,
// or strictly after `--` when the template carries {prompt}.
func TestCLIProviderPromptNeverParsesAsFlag(t *testing.T) {
	testEnv(t)
	rec := t.TempDir()
	fake := writeFakeCLI(t, `#!/bin/sh
printf '%s\n' "$@" > "$REC/args.txt"
cat > "$REC/stdin.txt"
echo ok
`)
	t.Setenv("REC", rec)
	prompt := "--force --yolo rm -rf ~"

	// Default: prompt on stdin, template args only in argv.
	p := cliProvider(fake, "--print")
	p.EnvPassthrough = []string{"REC"}
	if _, _, _, err := callProviderChat(defaultConfig(), p, []orMessage{
		{Role: "user", Content: []orContent{{Type: "text", Text: prompt}}},
	}); err != nil {
		t.Fatal(err)
	}
	argsRaw, _ := os.ReadFile(filepath.Join(rec, "args.txt"))
	if strings.TrimSpace(string(argsRaw)) != "--print" {
		t.Fatalf("argv=%q, want just --print", argsRaw)
	}
	stdin, _ := os.ReadFile(filepath.Join(rec, "stdin.txt"))
	if !strings.Contains(string(stdin), prompt) {
		t.Fatalf("prompt not on stdin: %q", stdin)
	}

	// Arg mode: {prompt} after -- substitutes the text as positional data.
	p.Args = []string{"run", "--", "{prompt}"}
	if _, _, _, err := callProviderChat(defaultConfig(), p, []orMessage{
		{Role: "user", Content: []orContent{{Type: "text", Text: prompt}}},
	}); err != nil {
		t.Fatal(err)
	}
	argsRaw, _ = os.ReadFile(filepath.Join(rec, "args.txt"))
	argLines := strings.Split(strings.TrimSpace(string(argsRaw)), "\n")
	if len(argLines) < 3 || argLines[0] != "run" || argLines[1] != "--" {
		t.Fatalf("argv=%v, want run -- <prompt>", argLines)
	}
	// The prompt is one argv element after -- (it contains newlines from the
	// role label, which is why it spans several recorded lines).
	if got := strings.Join(argLines[2:], "\n"); !strings.Contains(got, prompt) {
		t.Fatalf("prompt arg = %q", got)
	}
}

// The child gets a minimal env allowlist — daemon env (API keys, sentinel
// vars) is not inherited.
func TestCLIProviderChildEnvIsAllowlisted(t *testing.T) {
	testEnv(t)
	rec := t.TempDir()
	fake := writeFakeCLI(t, "#!/bin/sh\nenv > \"$REC/env.txt\"\necho ok\n")
	t.Setenv("REC", rec)
	t.Setenv("DAYFLOW_SENTINEL", "sekret-value")

	p := cliProvider(fake)
	p.EnvPassthrough = []string{"REC"}
	if _, _, _, err := callProviderChat(defaultConfig(), p, []orMessage{
		{Role: "user", Content: []orContent{{Type: "text", Text: "hi"}}},
	}); err != nil {
		t.Fatal(err)
	}
	env, _ := os.ReadFile(filepath.Join(rec, "env.txt"))
	s := string(env)
	if !strings.Contains(s, "PATH=") || !strings.Contains(s, "HOME=") || !strings.Contains(s, "REC=") {
		t.Fatalf("allowlist vars missing from child env:\n%s", s)
	}
	if strings.Contains(s, "DAYFLOW_SENTINEL") {
		t.Fatal("daemon env leaked into the cli child")
	}
}

func TestCLIProviderFailures(t *testing.T) {
	testEnv(t)
	msgs := []orMessage{{Role: "user", Content: []orContent{{Type: "text", Text: "hi"}}}}

	t.Run("nonzero exit", func(t *testing.T) {
		fake := writeFakeCLI(t, "#!/bin/sh\necho something broke >&2\nexit 3\n")
		_, _, _, err := callProviderChat(defaultConfig(), cliProvider(fake), msgs)
		if err == nil || !strings.Contains(err.Error(), "something broke") {
			t.Fatalf("err=%v", err)
		}
		if isTransientProviderErr(err) {
			t.Fatal("cli failures are not retried at minutes-scale cost")
		}
	})

	t.Run("timeout kills", func(t *testing.T) {
		fake := writeFakeCLI(t, "#!/bin/sh\nsleep 60\n")
		p := cliProvider(fake)
		p.CLITimeoutSec = 1
		start := time.Now()
		_, _, _, err := callProviderChat(defaultConfig(), p, msgs)
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("err=%v", err)
		}
		if time.Since(start) > 30*time.Second {
			t.Fatal("timeout was not enforced")
		}
	})

	t.Run("missing binary", func(t *testing.T) {
		p := cliProvider("/nonexistent/dayflow-cli-nope")
		_, _, _, err := callProviderChat(defaultConfig(), p, msgs)
		if err == nil {
			t.Fatal("expected an error for a missing command")
		}
	})

	t.Run("empty command", func(t *testing.T) {
		_, _, _, err := callProviderChat(defaultConfig(), cliProvider(""), msgs)
		if err == nil || !strings.Contains(err.Error(), "no command configured") {
			t.Fatalf("err=%v", err)
		}
	})
}

// Control characters are stripped so stored CLI output stays plain text.
func TestCLIProviderStripsControlChars(t *testing.T) {
	testEnv(t)
	fake := writeFakeCLI(t, "#!/bin/sh\nprintf 'hello\\033[31m world\\007\\n'\n")
	text, _, _, err := callProviderChat(defaultConfig(), cliProvider(fake), []orMessage{
		{Role: "user", Content: []orContent{{Type: "text", Text: "hi"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello[31m world" {
		t.Fatalf("text=%q", text)
	}
}

// cli providers are ineligible for the per-block vision/summary loop unless
// the config opts in with allow_hot_path.
func TestCLIProviderHotPathRoutingGuard(t *testing.T) {
	mk := func(hot bool) Config {
		cfg := defaultConfig()
		cfg.Providers = []Provider{
			{ID: "or1", Kind: "openrouter", APIKey: "k", Model: "m", Enabled: true},
			{ID: "cli1", Kind: "cli", Command: "/bin/cat", Enabled: true, AllowHotPath: hot},
		}
		cfg.Routing = Routing{
			Primary:      "or1",
			TaskProvider: map[string]string{"vision": "cli1", "summary": "cli1", "chat": "cli1"},
		}
		return cfg
	}

	// Without opt-in, task overrides pointing at a cli provider are skipped.
	cfg := mk(false)
	for _, task := range []string{"vision", "summary"} {
		p, err := providerForTask(cfg, task)
		if err != nil {
			t.Fatalf("%s: %v", task, err)
		}
		if p.ID != "or1" {
			t.Fatalf("%s routed to %q — cli provider should be skipped without allow_hot_path", task, p.ID)
		}
	}
	// Text tasks route fine.
	p, err := providerForTask(cfg, "chat")
	if err != nil || p.ID != "cli1" {
		t.Fatalf("chat = %+v, err=%v", p, err)
	}

	// Cli-only config: vision errors with an actionable hint.
	cfg2 := defaultConfig()
	cfg2.Providers = []Provider{{ID: "cli1", Kind: "cli", Command: "/bin/cat", Enabled: true}}
	cfg2.Routing = Routing{Primary: "cli1"}
	if _, err := providerForTask(cfg2, "vision"); err == nil || !strings.Contains(err.Error(), "allow_hot_path") {
		t.Fatalf("err=%v, want an allow_hot_path hint", err)
	}

	// Opt-in makes the cli provider eligible.
	p, err = providerForTask(mk(true), "vision")
	if err != nil || p.ID != "cli1" {
		t.Fatalf("vision with allow_hot_path = %+v, err=%v", p, err)
	}
}

// provider set validates the args template: deny-listed flags are rejected
// at config-write time, and {prompt} must stand alone after --.
func TestCLIProviderSetValidation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")

	cfg := defaultConfig()
	cfg.Providers = []Provider{{ID: "c1", Kind: "cli", Command: "/bin/cat", Enabled: true}}
	cfg.Routing = Routing{Primary: "c1"}

	for _, bad := range []string{`["--force"]`, `["--yolo"]`, `["run","-f","x"]`, `["-p","{prompt}"]`, `["--prompt={prompt}"]`} {
		if err := runProvider(cfg, []string{"set", "c1", "args", bad}, false); err == nil {
			t.Fatalf("args %s accepted, want rejection", bad)
		}
	}
	for _, ok := range []string{`["--print"]`, `["run","--","{prompt}"]`, `["run","--file={file}"]`} {
		if err := runProvider(cfg, []string{"set", "c1", "args", ok}, false); err != nil {
			t.Fatalf("args %s rejected: %v", ok, err)
		}
	}
	// a known command gets preset args when none are configured
	cfg.Providers[0].Args = nil
	if err := runProvider(cfg, []string{"set", "c1", "command", "cursor-agent"}, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	got := findProvider(loaded, "c1")
	want := []string{"--print", "--output-format", "text"}
	if got == nil || strings.Join(got.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("preset args=%v", got)
	}
}

// End-to-end UX: provider add cli + provider test exercises the exec path.
func TestCLIProviderAddAndTest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	fake := writeFakeCLI(t, "#!/bin/sh\ncat\n")

	cfg := defaultConfig()
	if err := runProvider(cfg, []string{"add", "cur", "cli"}, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	p := findProvider(loaded, "cur")
	if p == nil || p.Kind != "cli" {
		t.Fatalf("provider=%+v", p)
	}
	if err := runProvider(loaded, []string{"set", "cur", "command", fake}, false); err != nil {
		t.Fatal(err)
	}
	loaded, _ = loadConfig()
	if err := runProvider(loaded, []string{"test", "cur"}, false); err != nil {
		t.Fatalf("provider test via cli exec failed: %v", err)
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
