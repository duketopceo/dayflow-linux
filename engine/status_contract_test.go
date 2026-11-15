package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// statusJSON runs printStatus(cfg, true) and returns the decoded object.
func statusJSON(t *testing.T, cfg Config) map[string]any {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	printStatus(cfg, true)
	os.Stdout = old
	w.Close()
	out, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("status output is not JSON: %v\n%s", err, out)
	}
	return m
}

// The app's service strip and the bar widget both read these keys; docs/
// service-contract.md lists them. Removing or retyping one breaks both UIs.
func TestStatusJSONServiceContractKeys(t *testing.T) {
	cfg := testEnv(t)
	m := statusJSON(t, cfg)
	for _, k := range []string{
		"paused", "capture_state", "last_frame", "last_frame_ts", "frames_today",
		"blocks_done", "blocks_pending", "configured", "version",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("status --json is missing contract key %q", k)
		}
	}
	if ts, _ := m["last_frame_ts"].(float64); ts != 0 {
		t.Errorf("empty journal: last_frame_ts = %v, want 0", ts)
	}
}

func TestStatusJSONLastFrameTS(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().Add(-90 * time.Second)
	if err := insertFrame(db, want, filepath.Join(t.TempDir(), "f.jpg")); err != nil {
		t.Fatal(err)
	}
	db.Close()
	m := statusJSON(t, cfg)
	if got, _ := m["last_frame_ts"].(float64); int64(got) != want.Unix() {
		t.Errorf("last_frame_ts = %v, want %d", got, want.Unix())
	}
}
