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
// (status denied + exit 2), "die" (stream-dead + exit 3, no frame),
// "park" (exit 4), "consent" (status consent-needed, then idle — picker
// waiting), "consent-then-stream" (consent pending, then frames),
// "clean" (exit 0, no frame), "huge" (oversized length prefix).
// Each spawn appends its DAYFLOW_PORTAL_TOKEN env value to tokens.log
// beside the script — the supervisor passes tokens via env, never argv.
func fakePortalHelper(t *testing.T, fixture, mode string) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
mode="` + mode + `"
echo "$DAYFLOW_PORTAL_TOKEN" >> "$(dirname "$0")/tokens.log"
case "$mode" in
deny) echo "status denied" >&2; exit 2 ;;
park) echo "status parked" >&2; exit 4 ;;
consent) echo "status consent-needed" >&2; sleep 300 ;;
consent-then-stream) echo "status consent-needed" >&2 ;;
clean) exit 0 ;;
huge)
	` + "python3 - <<'PY'\nimport struct, sys\nsys.stdout.buffer.write(struct.pack(\">I\", 64 << 20))\nsys.stdout.buffer.flush()\nPY" + `
	sleep 60 ;;
die) echo "status stream-dead" >&2; exit 3 ;;
esac
echo "token fake-tok-9" >&2
[ "$mode" = "consent-then-stream" ] && sleep 1
echo "status streaming" >&2
` + "python3 - \"" + fixture + `" <<'PY'
import struct, sys, time
frame = open(sys.argv[1], "rb").read()
while True:
    sys.stdout.buffer.write(struct.pack(">I", len(frame)) + frame)
    sys.stdout.buffer.flush()
    time.sleep(0.02)
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
		if errors.As(err, &pe) && strings.Contains(pe.reason, "consecutive") {
			// Frameless deaths park regardless of spacing — but the park
			// is transient-infra, so it must carry an auto-retry time.
			b.mu.Lock()
			retry := b.parkedRetryAt
			b.mu.Unlock()
			if retry.IsZero() {
				t.Fatal("death-bound park must schedule an auto-retry")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("helper dying every spawn should park after 3 consecutive deaths")
}

// Denial persists in meta — a config reload (which re-creates the backend)
// must not un-park and re-fire a dismissed picker; only clearing the
// marker (what `capture retry` does) unblocks it.
func TestPortalBackendDenialSurvivesReResolve(t *testing.T) {
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
	var pe parkedError
	if _, err := b.Grab(db); !errors.As(err, &pe) {
		t.Fatalf("denial should park, got %v", err)
	}
	if metaGet(db, metaPortalDenied) != "1" {
		t.Fatal("denial must persist to meta")
	}
	// Simulated reload: a fresh backend instance is parked before spawn.
	b2 := newPortalBackend(cfg)
	defer b2.Close()
	if _, err := b2.Grab(db); !errors.As(err, &pe) {
		t.Fatalf("fresh backend should inherit persisted denial, got %v", err)
	}
	if b2.helper != "" {
		t.Fatal("persisted denial must park before helper resolution")
	}
	// `capture retry` clears the marker; the next backend spawns again.
	metaSet(db, metaPortalDenied, "")
	b3 := newPortalBackend(cfg)
	defer b3.Close()
	b3.Grab(db) // denial helper exits again — spawn is what matters
	if b3.helper == "" {
		t.Fatal("cleared marker should allow a fresh spawn")
	}
}

// A helper that exits 0 without ever producing a frame is still a failed
// spawn — three consecutive frameless exits must park even when each exit
// was clean (otherwise an exit-0 loop respawns forever).
func TestPortalBackendCleanExitCountsTowardBound(t *testing.T) {
	cfg := testEnv(t)
	cfg.CaptureEnabled = true
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("DAYFLOW_PORTAL_HELPER", fakePortalHelper(t, frameFixture(t), "clean"))
	b := newPortalBackend(cfg)
	defer b.Close()
	var pe parkedError
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, err := b.Grab(db)
		if errors.As(err, &pe) && strings.Contains(pe.reason, "consecutive") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("three consecutive frameless exits should park the backend")
}

// An oversized length prefix must kill the helper (drain deadlock
// protection) and respawn through the death bound — not wedge Grab.
func TestPortalBackendOversizedFrameKills(t *testing.T) {
	cfg := testEnv(t)
	cfg.CaptureEnabled = true
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("DAYFLOW_PORTAL_HELPER", fakePortalHelper(t, frameFixture(t), "huge"))
	b := newPortalBackend(cfg)
	defer b.Close()
	// "huge" writes a 64MB length prefix then idles — the supervisor must
	// kill it (each incarnation dies frameless → parks after 3).
	var pe parkedError
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		_, err := b.Grab(db)
		if errors.As(err, &pe) && strings.Contains(pe.reason, "consecutive") {
			return
		}
		if err != nil {
			var p parkedError
			if !errors.As(err, &p) {
				// no-frame deadline or death poke — keep polling
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("oversized frames should kill helper incarnations until parked")
}

// Consent → streaming transition: the consent status quiets Grab while
// the picker is up, then frames flow when the helper starts streaming.
func TestPortalBackendConsentThenStream(t *testing.T) {
	cfg := testEnv(t)
	cfg.CaptureEnabled = true
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Setenv("DAYFLOW_PORTAL_HELPER", fakePortalHelper(t, frameFixture(t), "consent-then-stream"))
	b := newPortalBackend(cfg)
	defer b.Close()
	// Grab returns the quiet parkedError while consent is pending; the
	// next tick (helper now streaming) must deliver a real frame — the
	// parked state is not sticky.
	var pe parkedError
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		frame, err := b.Grab(db)
		if err == nil && len(frame) >= 100 {
			return
		}
		if err != nil && !(errors.As(err, &pe) && strings.Contains(pe.reason, "consent pending")) {
			t.Fatalf("unexpected Grab error: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("consent→streaming never delivered a frame")
}

// The restore token goes to the helper via env, and a respawn after a
// rotation must present the persisted (newer) token.
func TestPortalBackendTokenViaEnvAndRotation(t *testing.T) {
	cfg := testEnv(t)
	cfg.CaptureEnabled = true
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	helper := fakePortalHelper(t, frameFixture(t), "stream")
	t.Setenv("DAYFLOW_PORTAL_HELPER", helper)
	b := newPortalBackend(cfg)
	defer b.Close()
	if _, err := b.Grab(db); err != nil {
		t.Fatalf("Grab: %v", err)
	}
	toklog := filepath.Join(filepath.Dir(helper), "tokens.log")
	deadline := time.Now().Add(3 * time.Second)
	for metaGet(db, metaPortalToken) == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	// A fresh backend on the same db must hand the helper the rotated
	// token via env — not argv.
	b.Close()
	b2 := newPortalBackend(cfg)
	defer b2.Close()
	if _, err := b2.Grab(db); err != nil {
		t.Fatalf("respawn Grab: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(toklog); err == nil && strings.Contains(string(b), "fake-tok-9") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("respawned helper did not receive the persisted token via env")
}
