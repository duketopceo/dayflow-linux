package main

import (
	"bufio"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// portalBackend supervises the dayflow-portal helper: a long-lived child
// that owns the xdg-desktop-portal ScreenCast session and PipeWire stream.
//
// Contract (app/portal-capture): length-prefixed JPEG frames on stdout,
// "status <state>" and "token <restore_token>" lines on stderr, exit codes
// 0 clean / 2 denied / 3 stream-dead / 4 fatal.
//
// The supervisor holds the consent lifecycle: spawn only with
// capture_enabled, persist the rotating restore token in the meta table
// (NOT config.json — its mtime drives hot-reload), park on denial until a
// re-auth action, restart on stream death with a rapid-death bound.
type portalBackend struct {
	cfg    Config
	helper string // resolved lazily; tests pin it via DAYFLOW_PORTAL_HELPER

	mu     sync.Mutex
	cmd    *exec.Cmd
	done   chan struct{} // closed by wait() once the current cmd is reaped
	state  string        // "", "consent-needed", "streaming", "denied", "stream-dead", "parked"
	parked error         // sticky: set on denial/helper-missing/death bound

	// Death-bound parks are transient-infrastructure verdicts: they
	// auto-clear after parkedRetryAt so a bus/portal blip can't disable
	// capture until manual retry. Denial parks are persisted in meta and
	// only `capture retry` clears them.
	parkedRetryAt time.Time
	// A helper incarnation counts as a death when it exits (for any reason,
	// clean included) without ever delivering a frame — that bounds both
	// crash loops and exit-0 respawn storms while letting long-lived or
	// productive helpers reset the count on their own merits.
	consecDeaths int
	closed       bool

	frames  chan []byte   // cap 1; reader replaces, never blocks
	changed chan struct{} // cap 1; signals state transitions so Grab wakes

	token       string
	tokenLoaded bool
}

const (
	metaPortalToken  = "portal_restore_token"
	metaPortalDenied = "portal_consent_denied"

	// JPEG frames are a few hundred KB — 32MB is generous headroom while
	// still catching a corrupted length prefix long before 256MB.
	portalMaxFrameBytes = 32 << 20

	// portalMaxDeaths consecutive frameless exits parks the backend;
	// consecutive — not per-minute — because slow deaths (30s watchdog,
	// 10min consent timeout) space wider than any rolling window yet
	// still mean "will never work".
	portalMaxDeaths     = 3
	parkedRetryInterval = 5 * time.Minute
)

// The stderr status/exit-code contract with app/portal-capture. These are
// literals there (separate module) — keep both sides in sync.
const (
	portalStatusConsent = "consent-needed"
	portalStatusDenied  = "denied"
	portalExitDenied    = 2
)

// errDeniedParked is shared by the status-line and exit-code paths so the
// parked reason can't drift between the two detection sites.
var errDeniedParked = parkedError{"screen-capture consent denied — re-auth with `dayflow capture retry`"}

// parkedError marks intentionally-quiet states (consent pending, denial,
// helper absent): the capture loop logs the transition once and stays
// alive on heartbeat — never a per-tick capture_error streak.
type parkedError struct{ reason string }

func (e parkedError) Error() string { return e.reason }

// portalHelperPath resolves the helper binary: DAYFLOW_PORTAL_HELPER env
// override (dev/tests) → sibling of the engine binary (bundled layout) →
// PATH.
func portalHelperPath() string {
	if p := os.Getenv("DAYFLOW_PORTAL_HELPER"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "dayflow-portal"); fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("dayflow-portal"); err == nil {
		return p
	}
	return ""
}

func newPortalBackend(cfg Config) *portalBackend {
	return &portalBackend{cfg: cfg, frames: make(chan []byte, 1), changed: make(chan struct{}, 1)}
}

// poke wakes a blocked Grab on a state change or helper death.
func (b *portalBackend) poke() {
	select {
	case b.changed <- struct{}{}:
	default:
	}
}

func (b *portalBackend) Name() string { return "portal" }

// NeedsWaylandSocket: the portal session dies with the compositor, so the
// socket gate's quiet semantics apply — absent socket means intentional
// quiet, not a stall.
func (b *portalBackend) NeedsWaylandSocket() bool { return true }

func (b *portalBackend) Grab(db *sql.DB) ([]byte, error) {
	if !b.cfg.CaptureEnabled {
		return nil, parkedError{"capture not enabled — portal consent pending (run `dayflow config set capture_enabled true` or finish onboarding)"}
	}
	deadline := time.Now().Add(grabFrameTimeout)
	for {
		if err := b.ensure(db); err != nil {
			return nil, err
		}
		// Consent pending is a quiet wait, not an error: the picker stays
		// up across ticks and frames flow the moment the user approves —
		// the loop logs capture_paused once, not a per-tick streak.
		b.mu.Lock()
		consentPending := b.state == portalStatusConsent
		b.mu.Unlock()
		if consentPending {
			return nil, parkedError{"portal consent pending — approve the screen-share dialog"}
		}
		select {
		case f := <-b.frames:
			return f, nil
		case <-b.changed:
			// state transition or helper death — re-check before looping
			b.mu.Lock()
			pe := b.parked
			b.mu.Unlock()
			if pe != nil {
				return nil, pe
			}
		case <-time.After(time.Until(deadline)):
			// Alive but silent (wedged D-Bus, deadlocked PipeWire loop):
			// kill it so the next tick respawns through the normal death
			// bound instead of erroring every 30s forever.
			b.killHelper()
			return nil, fmt.Errorf("portal helper produced no frame in %s", grabFrameTimeout)
		}
		if time.Now().After(deadline) {
			b.killHelper()
			return nil, fmt.Errorf("portal helper produced no frame in %s", grabFrameTimeout)
		}
	}
}

// killHelper terminates the current helper, if any. Safe from any
// goroutine — the wait() reaper owns process accounting.
func (b *portalBackend) killHelper() {
	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}

// ensure spawns the helper when needed and enforces the restart policy:
// denial parks (persisted — survives config reloads and restarts until
// `capture retry`), stream death respawns, portalMaxDeaths consecutive
// deaths park with a timed auto-retry.
func (b *portalBackend) ensure(db *sql.DB) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return parkedError{"portal backend closed"}
	}
	if b.parked != nil {
		if !b.parkedRetryAt.IsZero() && time.Now().After(b.parkedRetryAt) {
			b.parked, b.parkedRetryAt, b.consecDeaths = nil, time.Time{}, 0
		} else {
			return b.parked
		}
	}
	// Denial survives backend re-creation (config reload): without the
	// meta marker any unrelated `config set` would re-fire the picker the
	// user already dismissed.
	if metaGet(db, metaPortalDenied) == "1" {
		b.parked = errDeniedParked
		return b.parked
	}
	select {
	case <-b.done:
	default:
		if b.cmd != nil {
			return nil // alive
		}
	}
	if b.helper == "" {
		if p := os.Getenv("DAYFLOW_PORTAL_HELPER"); p != "" {
			debugf(b.cfg, "portal helper override: %s", p)
		}
		b.helper = portalHelperPath()
		if b.helper == "" {
			b.parked = parkedError{"dayflow-portal helper not found — it must ship beside the dayflow binary or be on PATH"}
			return b.parked
		}
	}
	if !b.tokenLoaded {
		b.token = metaGet(db, metaPortalToken)
		b.tokenLoaded = true
	}
	if b.consecDeaths >= portalMaxDeaths {
		b.parked = parkedError{fmt.Sprintf("portal helper died %d consecutive times — parked for %s (fix the helper or `dayflow capture retry`)", b.consecDeaths, parkedRetryInterval)}
		b.parkedRetryAt = time.Now().Add(parkedRetryInterval)
		return b.parked
	}

	cmd := exec.Command(b.helper,
		"--jpeg-quality", strconv.Itoa(b.cfg.JPEGQuality),
		"--max-dim", strconv.Itoa(b.cfg.FrameMaxDim),
	)
	// The token goes through env, not argv — /proc/<pid>/cmdline is
	// world-readable; the environment needs same-uid ptrace to read.
	// The helper's env is an allowlist: the daemon's full environ carries
	// secrets the helper must never see.
	cmd.Env = portalHelperEnv(b.token)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		// Spawn failures count toward the death bound too — otherwise a
		// persistently unexecutable helper retries every tick forever.
		b.consecDeaths++
		return fmt.Errorf("spawn portal helper: %w", err)
	}
	b.cmd = cmd
	b.done = make(chan struct{})
	b.state = ""
	delivered := &atomic.Bool{}
	go b.readFrames(stdout, cmd, delivered)
	go b.readStatus(stderr, db, cmd)
	go b.wait(cmd, b.done, db, delivered)
	return nil
}

// portalHelperEnv is the minimal allowlist the helper needs: session bus,
// runtime dir, display identity, locale. Everything else — API keys,
// tokens, agent internals — stays out of the child's environment.
func portalHelperEnv(token string) []string {
	allow := []string{
		"HOME", "PATH", "LANG", "LC_ALL",
		"DBUS_SESSION_BUS_ADDRESS", "DBUS_STARTER_BUS_TYPE",
		"XDG_RUNTIME_DIR", "XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE",
		"WAYLAND_DISPLAY", "DISPLAY", "XAUTHORITY",
	}
	env := []string{"DAYFLOW_PORTAL_TOKEN=" + token}
	for _, k := range allow {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// readFrames decodes the helper's length-prefixed JPEG stream. cmd is the
// owning process — kills here must target it explicitly, never b.cmd,
// which may already point at a respawn.
func (b *portalBackend) readFrames(r io.Reader, cmd *exec.Cmd, delivered *atomic.Bool) {
	br := bufio.NewReaderSize(r, 1<<20)
	var hdr [4]byte
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n > portalMaxFrameBytes {
			debugf(b.cfg, "portal frame %d bytes exceeds cap — killing helper stream", n)
			// Leaving the helper alive would deadlock it: nothing drains
			// stdout, its next write blocks, and wait() never returns.
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			return
		}
		frame := make([]byte, n)
		if _, err := io.ReadFull(br, frame); err != nil {
			return
		}
		// latest wins: drop any queued frame (non-blocking — a racing
		// Grab may have drained it), then deliver this one.
		select {
		case <-b.frames:
		default:
		}
		b.frames <- frame
		delivered.Store(true) // this incarnation produced — its exit isn't a death
	}
}

// readStatus parses the helper's stderr protocol: "status <s>" updates the
// supervisor state, "token <t>" rotates the persisted restore token. A
// scanner error (line over the 1MB bound, torn pipe) kills the helper —
// silently losing status lines would strand a live-but-invisible helper.
func (b *portalBackend) readStatus(r io.Reader, db *sql.DB, cmd *exec.Cmd) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "status ") {
			b.setState(strings.TrimSpace(line[7:]), db)
		} else if strings.HasPrefix(line, "token ") {
			tok := strings.TrimSpace(line[6:])
			if len(tok) <= 4096 { // bound junk before it hits meta/disk
				b.mu.Lock()
				b.token = tok
				b.mu.Unlock()
				metaSet(db, metaPortalToken, tok)
			}
		} else {
			debugf(b.cfg, "portal helper: %s", line)
		}
	}
	if err := sc.Err(); err != nil {
		debugf(b.cfg, "portal helper stderr read failed: %v — killing", err)
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}
}

func (b *portalBackend) setState(s string, db *sql.DB) {
	b.mu.Lock()
	b.state = s
	if s == portalStatusDenied {
		b.parked = errDeniedParked
	}
	b.mu.Unlock()
	if s == portalStatusDenied {
		// Persist denial so a config reload / daemon restart can't
		// un-park and re-fire a dismissed picker. The rotating token is
		// dead weight now — the next consent flow issues a fresh one.
		metaSet(db, metaPortalDenied, "1")
		metaSet(db, metaPortalToken, "")
	}
	b.poke()
}

// wait is the sole reaper — it owns done's close and all death accounting
// (Close/oversize-kill only signal; they never call cmd.Wait). An
// incarnation that delivered a frame resets the count on exit; one that
// produced nothing counts toward the bound regardless of exit code — a
// frameless clean exit is still a failed spawn, just a polite one.
func (b *portalBackend) wait(cmd *exec.Cmd, done chan struct{}, db *sql.DB, delivered *atomic.Bool) {
	err := cmd.Wait()
	b.mu.Lock()
	defer b.mu.Unlock()
	defer close(done)
	if b.cmd == cmd {
		b.cmd = nil
	}
	if b.closed {
		return
	}
	if delivered.Load() {
		b.consecDeaths = 0
	} else {
		b.consecDeaths++
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == portalExitDenied {
		b.state = portalStatusDenied
		b.parked = errDeniedParked
		metaSet(db, metaPortalDenied, "1")
		metaSet(db, metaPortalToken, "")
	} else if err == nil {
		b.state = "stopped"
	} else if b.state != portalStatusDenied {
		b.state = "stream-dead"
	}
	debugf(b.cfg, "portal helper exited: %v", err)
	b.poke()
}

func (b *portalBackend) Close() error {
	b.mu.Lock()
	b.closed = true
	cmd, done := b.cmd, b.done
	b.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
	if done != nil {
		<-done // the reaper holds the lock while bookkeeping — wait for it
	}
	b.poke() // wake a Grab blocked on frames/changed
	return nil
}
