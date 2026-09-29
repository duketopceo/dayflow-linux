package main

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProbeEndpoint(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer up.Close()

	if !probeEndpoint(up.URL + "/v1") {
		t.Fatal("expected probe to succeed against live endpoint")
	}
	if probeEndpoint(up.URL + "/wrong") {
		t.Fatal("404 endpoint must not count as detected")
	}

	dead := httptest.NewServer(nil)
	url := dead.URL
	dead.Close()
	start := time.Now()
	if probeEndpoint(url) {
		t.Fatal("expected probe to fail against dead endpoint")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("dead-endpoint probe should fail fast")
	}
}

func TestCollectDoctorChecks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("DAYFLOW_CONFIG", dir+"/config.json")

	checks, _ := collectDoctorChecks(Config{Model: "google/gemma-4-31b-it"}, false)
	if len(checks) == 0 {
		t.Fatal("expected at least one check")
	}
	for _, c := range checks {
		if c.Name == "" {
			t.Fatal("check missing name")
		}
		switch c.Status {
		case "ok", "warn", "info", "fail":
		default:
			t.Fatalf("check %q has invalid status %q", c.Name, c.Status)
		}
	}
	// JSON shape must round-trip for panel consumption.
	b, err := json.Marshal(checks)
	if err != nil {
		t.Fatal(err)
	}
	var back []map[string]interface{}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal("doctor checks must marshal to JSON")
	}
}

func findCheck(checks []doctorCheck, name string) *doctorCheck {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

func TestDoctorOutputAutoWarn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("DAYFLOW_CONFIG", dir+"/config.json")

	// output:"auto" + capture_command → the setting is silently dead; warn.
	checks, _ := collectDoctorChecks(Config{
		Model: "google/gemma-4-31b-it", Output: "auto", CaptureCommand: "mygrabber",
	}, false)
	c := findCheck(checks, "output auto")
	if c == nil || c.Status != "warn" {
		t.Fatalf("expected warn for auto+capture_command, got %+v", c)
	}

	// auto without capture_command → no warning.
	checks, _ = collectDoctorChecks(Config{Model: "google/gemma-4-31b-it", Output: "auto"}, false)
	if c := findCheck(checks, "output auto"); c != nil {
		t.Fatalf("auto without capture_command must not warn, got %+v", c)
	}

	// capture_command with an explicit output → no warning.
	checks, _ = collectDoctorChecks(Config{
		Model: "google/gemma-4-31b-it", Output: "DP-3", CaptureCommand: "mygrabber",
	}, false)
	if c := findCheck(checks, "output auto"); c != nil {
		t.Fatalf("explicit output must not warn, got %+v", c)
	}
}

func TestNotificationBusReachable(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	if notificationBusReachable() {
		t.Fatal("no bus env at all must be unreachable")
	}

	// An explicit address passes without touching the filesystem.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent")
	if !notificationBusReachable() {
		t.Fatal("explicit DBUS_SESSION_BUS_ADDRESS should pass")
	}

	// GLib fallback: a socket at $XDG_RUNTIME_DIR/bus.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	if notificationBusReachable() {
		t.Fatal("runtime dir without a bus socket must be unreachable")
	}
	// A regular file named "bus" is not a socket.
	if err := os.WriteFile(filepath.Join(rt, "bus"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if notificationBusReachable() {
		t.Fatal("a regular file named bus is not a socket")
	}
	os.Remove(filepath.Join(rt, "bus"))
	ln, err := net.Listen("unix", filepath.Join(rt, "bus"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !notificationBusReachable() {
		t.Fatal("$XDG_RUNTIME_DIR/bus socket should pass")
	}
}

func writePluginManifest(t *testing.T, cfgHome, ver string) string {
	t.Helper()
	dir := filepath.Join(cfgHome, "omarchy", "plugins", "io.github.duketopceo.dayflow")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(p, []byte(`{"version":"`+ver+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDoctorPluginManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("DAYFLOW_CONFIG", dir+"/config.json")
	cfgHome := filepath.Join(dir, "config-home")
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	cfg := Config{Model: "google/gemma-4-31b-it"}

	// Absent manifest → info "engine-only install", never a failure.
	checks, _ := collectDoctorChecks(cfg, false)
	c := findCheck(checks, "plugin manifest")
	if c == nil || c.Status != "info" {
		t.Fatalf("absent manifest should be info, got %+v", c)
	}

	// Matching manifest → ok.
	writePluginManifest(t, cfgHome, version)
	checks, failMatch := collectDoctorChecks(cfg, false)
	c = findCheck(checks, "plugin manifest")
	if c == nil || c.Status != "ok" {
		t.Fatalf("matching manifest should be ok, got %+v", c)
	}

	// Mismatch → fail, detail names the manifest path.
	mPath := writePluginManifest(t, cfgHome, "0.0.0")
	checks, failMismatch := collectDoctorChecks(cfg, false)
	c = findCheck(checks, "plugin manifest")
	if c == nil || c.Status != "fail" {
		t.Fatalf("mismatched manifest should fail, got %+v", c)
	}
	if !strings.Contains(c.Detail, mPath) || !strings.Contains(c.Detail, "0.0.0") {
		t.Fatalf("fail detail should name manifest path and version: %q", c.Detail)
	}
	if failMismatch != failMatch+1 {
		t.Fatalf("manifest mismatch should add exactly one failure: %d → %d", failMatch, failMismatch)
	}

	// Unparseable manifest → warn (corrupt install, not a clean match or gap).
	if err := os.WriteFile(mPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	checks, _ = collectDoctorChecks(cfg, false)
	c = findCheck(checks, "plugin manifest")
	if c == nil || c.Status != "warn" {
		t.Fatalf("unparseable manifest should warn, got %+v", c)
	}

	// The helper itself reports the corrupt manifest's path with its error.
	if p, v, err := pluginManifestVersion(); err == nil || p == "" {
		t.Fatalf("corrupt manifest should error with its path, got %q %q %v", p, v, err)
	}
}

func TestDoctorSchemaVersionDetail(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("DAYFLOW_CONFIG", dir+"/config.json")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config-home"))

	// Create the db so peekSchemaVersion sees a real schema_migrations row.
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	checks, _ := collectDoctorChecks(Config{Model: "google/gemma-4-31b-it"}, false)
	c := findCheck(checks, "schema version")
	if c == nil || c.Status != "ok" {
		t.Fatalf("fresh db should pass schema version, got %+v", c)
	}
	want := "database v" + strconv.Itoa(schemaVersion)
	if !strings.Contains(c.Detail, want) {
		t.Fatalf("schema version detail should report current vs binary, got %q", c.Detail)
	}
}

func TestAgentStoresDetected(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude-proj")
	if err := os.MkdirAll(claude, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAYFLOW_CLAUDE_DIR", claude)
	t.Setenv("DAYFLOW_CODEX_DIR", filepath.Join(dir, "no-codex"))
	t.Setenv("DAYFLOW_OPENCODE_DB", filepath.Join(dir, "no-opencode.db"))
	t.Setenv("DAYFLOW_DEVIN_DIR", filepath.Join(dir, "no-devin"))
	t.Setenv("DAYFLOW_CURSOR_DB", filepath.Join(dir, "no-cursor.vscdb"))

	got := agentStoresDetected()
	if !got["claude"] {
		t.Fatal("existing claude dir not detected")
	}
	for _, name := range []string{"codex", "opencode", "devin", "cursor"} {
		if got[name] {
			t.Fatalf("absent %s store reported present", name)
		}
	}
	names := detectedStoreNames()
	if len(names) != 1 || names[0] != "claude" {
		t.Fatalf("expected [claude], got %v", names)
	}

	// Nothing exists → nothing detected (setup stays silent on recaps).
	t.Setenv("DAYFLOW_CLAUDE_DIR", filepath.Join(dir, "no-claude"))
	if got := agentStoresDetected(); len(detectedStoreNames()) != 0 {
		t.Fatalf("all-absent must detect nothing, got %v", got)
	}
}

func TestDetectResultAgentsField(t *testing.T) {
	// The detect JSON payload must carry the agents map — Onboarding.qml's
	// recaps step gates on root.detected.agents.
	b, err := json.Marshal(detectResult{Agents: map[string]bool{"claude": true}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	agents, ok := m["agents"].(map[string]interface{})
	if !ok {
		t.Fatalf("detect JSON must carry an agents object, got %s", b)
	}
	if agents["claude"] != true {
		t.Fatalf("agents map lost its values: %v", agents)
	}
}

func TestPromptYesDefaultsNo(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},    // EOF / non-interactive stdin
		{"\n", false},  // empty answer
		{"n\n", false}, // explicit no
		{"yes please\n", false},
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"  y  \n", true},
	}
	for _, c := range cases {
		got := promptYes(bufio.NewReader(strings.NewReader(c.in)))
		if got != c.want {
			t.Errorf("promptYes(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
