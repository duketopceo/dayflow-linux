package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakePortalHelper writes a script emulating the helper contract: status
// and token lines on stderr, length-prefixed JPEG frames on stdout, and a
// configurable exit mode. MODE values: "stream" (frames forever), "deny"
// (status denied + exit 2), "die" (exit 3 after one frame), "park" (exit 4).
func fakePortalHelper(t *testing.T, fixture, mode string) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
mode="` + mode + `"
case "$mode" in
deny) echo "status denied" >&2; exit 2 ;;
park) echo "status parked" >&2; exit 4 ;;
consent) echo "status consent-needed" >&2; sleep 300 ;;
esac
echo "token fake-tok-9" >&2
echo "status streaming" >&2
` + "python3 - \"" + fixture + `" "$mode" <<'PY'
import struct, sys, time
frame = open(sys.argv[1], "rb").read()
sys.stdout.buffer.write(struct.pack(">I", len(frame)) + frame)
sys.stdout.buffer.flush()
if sys.argv[2] == "die":
    print("status stream-dead", file=sys.stderr)
    sys.exit(3)
while True:
    time.sleep(0.02)
    sys.stdout.buffer.write(struct.pack(">I", len(frame)) + frame)
    sys.stdout.buffer.flush()
PY
`
	p := filepath.Join(dir, "dayflow-portal")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPortalBackendDisabledGate(t *testing.T) {
	cfg := testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b := newPortalBackend(cfg) // CaptureEnabled false by default
	if _, err := b.Grab(db); err == nil {
		t.Fatal("Grab should refuse when capture_enabled is unset")
	} else {
		var pe parkedError
		if !errors.As(err, &pe) {
			t.Fatalf("expected parkedError, got %v", err)
		}
	}
	// The gate must refuse before even looking for a helper binary.
	if b.helper != "" || b.cmd != nil {
		t.Fatal("disabled gate spawned or resolved a helper")
	}
}

func TestPortalBackendStreamsFrames(t *testing.T) {
	cfg := testEnv(t)
	cfg.CaptureEnabled = true
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("DAYFLOW_PORTAL_HELPER", fakePortalHelper(t, frameFixture(t), "stream"))
	b := newPortalBackend(cfg)
	defer b.Close()
	frame, err := b.Grab(db)
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}
	if len(frame) < 100 || frame[0] != 0xFF || frame[1] != 0xD8 {
		t.Fatalf("frame is not a JPEG (%d bytes)", len(frame))
	}
	// The helper's token line must land in meta for silent re-Start.
	deadline := time.Now().Add(3 * time.Second)
	for metaGet(db, metaPortalToken) == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := metaGet(db, metaPortalToken); got != "fake-tok-9" {
		t.Fatalf("restore token persisted = %q", got)
	}
}

func TestPortalBackendDenialParks(t *testing.T) {
	cfg := testEnv(t)
	cfg.CaptureEnabled = true
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("DAYFLOW_PORTAL_HELPER", fakePortalHelper(t, frameFixture(t), "deny"))
	b := newPortalBackend(cfg)
	defer b.Close()
	_, err = b.Grab(db)
	var pe parkedError
	if !errors.As(err, &pe) {
		t.Fatalf("denied consent should park, got %v", err)
	}
	if !strings.Contains(pe.reason, "denied") {
		t.Fatalf("parked reason should mention denial: %s", pe.reason)
	}
	// Parked is sticky — a second Grab must not respawn the helper.
	if _, err := b.Grab(db); !errors.As(err, &pe) {
		t.Fatalf("second Grab should stay parked, got %v", err)
	}
}

// Consent pending must not surface as an error streak: Grab returns a
// quiet parkedError while the helper waits on the picker, and the helper
// stays alive so approval mid-wait resumes capture.
func TestPortalBackendConsentPendingIsQuiet(t *testing.T) {
	cfg := testEnv(t)
	cfg.CaptureEnabled = true
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("DAYFLOW_PORTAL_HELPER", fakePortalHelper(t, frameFixture(t), "consent"))
	b := newPortalBackend(cfg)
	defer b.Close()
	var pe parkedError
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, err := b.Grab(db)
		if errors.As(err, &pe) && strings.Contains(pe.reason, "consent pending") {
			// Non-sticky: helper must still be alive waiting on the picker.
			if b.cmd == nil || b.cmd.ProcessState != nil {
				t.Fatal("consent-pending should keep the helper alive")
			}
			return
		}
	}
	t.Fatal("consent-needed should yield a quiet parkedError, got no such state")
}

func TestPortalBackendRapidDeathParks(t *testing.T) {
	cfg := testEnv(t)
	cfg.CaptureEnabled = true
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("DAYFLOW_PORTAL_HELPER", fakePortalHelper(t, frameFixture(t), "die"))
	b := newPortalBackend(cfg)
	defer b.Close()
	var pe parkedError
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, err := b.Grab(db)
		if errors.As(err, &pe) && strings.Contains(pe.reason, "3 times") {
			return // parked on the rapid-death bound — correct
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("helper dying every spawn should park after 3 rapid deaths")
}
