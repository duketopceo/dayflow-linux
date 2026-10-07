package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// captureBackend abstracts where frames come from. Grim/capture_command are
// per-shot argv adapters over grabFrame; portal (supervised helper) and X11
// (persistent connection) are separate impls. Grab returns JPEG/PNG bytes;
// downstream dedup/store is backend-neutral.
type captureBackend interface {
	// Name identifies the backend for daemon_start logging and doctor.
	Name() string
	// Grab samples the screen once. db is available for backends that log
	// events during resolution (grim's autoOutput); most ignore it.
	Grab(db *sql.DB) ([]byte, error)
	// NeedsWaylandSocket reports whether the backend dies without a
	// wayland socket — gates the daemon's quiet-pause path. Only grim
	// requires it; portal rides D-Bus, X11 and custom commands neither.
	NeedsWaylandSocket() bool
	// Close releases persistent resources (x11 conn, portal helper).
	Close() error
}

// argvBackend runs a fixed command line per grab — capture_command and any
// argv-shaped fallback.
type argvBackend struct{ argv []string }

func (b *argvBackend) Name() string                   { return strings.Join(b.argv, " ") }
func (b *argvBackend) Grab(_ *sql.DB) ([]byte, error) { return grabFrame(b.argv) }
func (b *argvBackend) NeedsWaylandSocket() bool       { return false }
func (b *argvBackend) Close() error                   { return nil }

// grimBackend wraps grim with output=auto's per-tick focused-output
// resolution and the resolve→exec-race composite retry, previously inlined
// in captureOnce.
type grimBackend struct{ cfg Config }

func (b *grimBackend) Name() string             { return "grim" }
func (b *grimBackend) NeedsWaylandSocket() bool { return true }
func (b *grimBackend) Close() error             { return nil }

func (b *grimBackend) Grab(db *sql.DB) ([]byte, error) {
	cfg := b.cfg
	output := cfg.Output
	if output != "auto" {
		return grabFrame(grimArgv(cfg, output))
	}
	// output=auto re-resolves the focused monitor every grab; a dock/focus
	// change can't pin capture to the start-time monitor. "" means
	// composite — no -o arg.
	var composite []string
	args := grimArgv(cfg, "")
	if name := autoOutput(db, cfg); name != "" {
		composite = args
		args = grimArgv(cfg, name)
	}
	raw, err := grabFrame(args)
	if err != nil && composite != nil {
		// resolve→exec race (output unplugged between `hyprctl monitors`
		// and `grim -o`, or a dock transition): one composite retry before
		// the failure counts.
		debugf(cfg, "capture: grim -o failed (%v); retrying composite", err)
		raw, err = grabFrame(composite)
	}
	return raw, err
}

// knownNonWlrootsDesktop tokens where grim can't work — wlr-screencopy
// doesn't exist there, so grim-in-PATH on GNOME/KDE is a trap. Detection
// uses XDG_CURRENT_DESKTOP (colon-separated, e.g. "ubuntu:GNOME").
var knownNonWlrootsDesktop = map[string]bool{
	"gnome": true, "kde": true, "plasma": true, "gnome-wayland": true,
	"cinnamon": true, "mate": true, "pantheon": true, "budgie": true,
	"deepin": true,
}

func desktopIsKnownNonWlroots(desktop string) bool {
	for _, tok := range strings.Split(strings.ToLower(desktop), ":") {
		if knownNonWlrootsDesktop[strings.TrimSpace(tok)] {
			return true
		}
	}
	return false
}

// resolveCaptureBackend picks the frame source for this session. Order per
// the standalone-app plan (KTD2): capture_command always wins; on Wayland
// the compositor orders attempts — known non-wlroots desktops skip grim
// (it's installable there but can't work) while wlroots-family and
// unrecognized desktops try grim first; a portal/PipeWire backend slots in
// before the error once it exists. Without WAYLAND_DISPLAY an X11 session
// falls to the pure-Go X11 backend.
func resolveCaptureBackend(cfg Config) (captureBackend, error) {
	if cfg.CaptureCommand != "" {
		return &argvBackend{argv: strings.Fields(cfg.CaptureCommand)}, nil
	}
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if !desktopIsKnownNonWlroots(desktop) {
			if _, err := exec.LookPath("grim"); err == nil {
				return &grimBackend{cfg: cfg}, nil
			}
		}
		hint := "install grim (wlroots compositors) or set capture_command"
		if desktopIsKnownNonWlroots(desktop) {
			hint = "this compositor needs the portal backend (pending); set capture_command as a stopgap"
		}
		return nil, fmt.Errorf("no capture backend for Wayland session (XDG_CURRENT_DESKTOP=%q) — %s", desktop, hint)
	}
	if os.Getenv("DISPLAY") != "" {
		return newX11Backend(cfg), nil
	}
	return nil, fmt.Errorf("no graphical session (WAYLAND_DISPLAY and DISPLAY unset) — set capture_command in %s", configPath())
}
