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
	helper string // resolved lazily; tests may pin via newPortalBackend

	mu     sync.Mutex
	cmd    *exec.Cmd
	state  string // "", "consent-needed", "streaming", "denied", "stream-dead", "parked"
	parked error  // sticky: set on denial/helper-missing/rapid-death bound
	deaths []time.Time
	closed bool

	frames  chan []byte   // cap 1; reader replaces, never blocks
	changed chan struct{} // cap 1; signals state transitions so Grab wakes

	token       string
	tokenLoaded bool
}

const metaPortalToken = "portal_restore_token"

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
		consentPending := b.state == "consent-needed"
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
			return nil, fmt.Errorf("portal helper produced no frame in %s", grabFrameTimeout)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("portal helper produced no frame in %s", grabFrameTimeout)
		}
	}
}

// ensure spawns the helper when needed and enforces the restart policy:
// denial parks, stream death respawns, ≥3 deaths in 60s park the backend
// until a config reload re-resolves it.
func (b *portalBackend) ensure(db *sql.DB) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.parked != nil {
		return b.parked
	}
	if b.cmd != nil && b.cmd.ProcessState == nil {
		return nil // alive
	}
	if b.helper == "" {
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
	// Rapid-death bound: N respawns inside a minute → park.
	now := time.Now()
	keep := b.deaths[:0]
	for _, d := range b.deaths {
		if now.Sub(d) < time.Minute {
			keep = append(keep, d)
		}
	}
	b.deaths = keep
	if len(b.deaths) >= 3 {
		b.parked = parkedError{"portal helper died 3 times in a minute — parked; fix the helper or `dayflow capture retry`"}
		return b.parked
	}

	args := []string{
		"--jpeg-quality", strconv.Itoa(b.cfg.JPEGQuality),
		"--max-dim", strconv.Itoa(b.cfg.FrameMaxDim),
	}
	if b.token != "" {
		args = append(args, "--token", b.token)
	}
	cmd := exec.Command(b.helper, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn portal helper: %w", err)
	}
	b.cmd = cmd
	b.state = ""
	go b.readFrames(stdout)
	go b.readStatus(stderr, db)
	go b.wait(cmd)
	return nil
}

func (b *portalBackend) readFrames(r io.Reader) {
	br := bufio.NewReaderSize(r, 1<<20)
	var hdr [4]byte
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n > x11MaxFrameBytes {
			debugf(b.cfg, "portal frame %d bytes exceeds cap — dropping helper stream", n)
			return
		}
		frame := make([]byte, n)
		if _, err := io.ReadFull(br, frame); err != nil {
			return
		}
		select {
		case b.frames <- frame:
		default:
			// latest wins: drop the queued frame, deliver this one
			<-b.frames
			b.frames <- frame
		}
	}
}

// readStatus parses the helper's stderr protocol: "status <s>" updates the
// supervisor state, "token <t>" rotates the persisted restore token.
func (b *portalBackend) readStatus(r io.Reader, db *sql.DB) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "status ") {
			b.setState(strings.TrimSpace(line[7:]))
		} else if strings.HasPrefix(line, "token ") {
			tok := strings.TrimSpace(line[6:])
			b.mu.Lock()
			b.token = tok
			b.mu.Unlock()
			metaSet(db, metaPortalToken, tok)
		} else {
			debugf(b.cfg, "portal helper: %s", line)
		}
	}
}

func (b *portalBackend) setState(s string) {
	b.mu.Lock()
	b.state = s
	if s == "denied" {
		b.parked = parkedError{"screen-capture consent denied — re-auth with `dayflow capture retry`"}
	}
	b.mu.Unlock()
	b.poke()
}

// wait observes process exit: clean stops leave state alone, death records
// the timestamp for the rapid-death bound. Denial may arrive via exit code
// before the status line is parsed.
func (b *portalBackend) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.deaths = append(b.deaths, time.Now())
	if b.cmd == cmd {
		b.cmd = nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 2 {
		b.state = "denied"
		b.parked = parkedError{"screen-capture consent denied — re-auth with `dayflow capture retry`"}
	} else if b.state != "denied" {
		b.state = "stream-dead"
	}
	debugf(b.cfg, "portal helper exited: %v", err)
	b.poke()
}

func (b *portalBackend) Close() error {
	b.mu.Lock()
	b.closed = true
	cmd := b.cmd
	b.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
	}
	return nil
}
