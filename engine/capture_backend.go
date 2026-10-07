package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	if output == "auto" {
		// output=auto re-resolves the focused monitor every grab; a
		// dock/focus change can't pin capture to the start-time monitor.
		// "" means composite — no -o arg.
		output = autoOutput(db, cfg)
	}
	raw, err := grabFrame(grimArgv(cfg, output))
	if err != nil && cfg.Output == "auto" && output != "" {
		// resolve→exec race (output unplugged between `hyprctl monitors`
		// and `grim -o`, or a dock transition): one composite retry before
		// the failure counts.
		debugf(cfg, "capture: grim -o failed (%v); retrying composite", err)
		raw, err = grabFrame(grimArgv(cfg, ""))
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

// knownWlrootsDesktop wins over the non-wlroots list when both appear in a
// colon list — "sway:GNOME" is a common xdg-desktop-portal workaround and
// must not hard-error a working wlroots session.
var knownWlrootsDesktop = map[string]bool{
	"hyprland": true, "sway": true, "river": true, "wayfire": true,
	"labwc": true, "niri": true, "dwl": true, "wlroots": true,
}

func desktopHasToken(desktop string, set map[string]bool) bool {
	for _, tok := range strings.Split(strings.ToLower(desktop), ":") {
		if set[strings.TrimSpace(tok)] {
			return true
		}
	}
	return false
}

// waylandSocketPresent reports whether a wayland socket actually exists —
// either the WAYLAND_DISPLAY-named one or any wayland-* socket in
// XDG_RUNTIME_DIR. Env vars are claims, sockets are truth: the user-manager
// env keeps stale values across session-type switches, and unimported
// sessions lack the vars entirely.
func waylandSocketPresent() bool {
	isSocket := func(path string) bool {
		st, err := os.Stat(path)
		return err == nil && st.Mode()&os.ModeSocket != 0
	}
	rt := os.Getenv("XDG_RUNTIME_DIR")
	if disp := os.Getenv("WAYLAND_DISPLAY"); disp != "" {
		if filepath.IsAbs(disp) {
			if isSocket(disp) {
				return true
			}
		} else if rt != "" && isSocket(filepath.Join(rt, disp)) {
			return true
		}
	}
	if rt == "" {
		return false
	}
	matches, _ := filepath.Glob(filepath.Join(rt, "wayland-*"))
	for _, m := range matches {
		if isSocket(m) {
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
		if argv := strings.Fields(cfg.CaptureCommand); len(argv) > 0 {
			return &argvBackend{argv: argv}, nil
		}
		return nil, fmt.Errorf("capture_command is whitespace-only — fix it in %s", configPath())
	}
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	// WAYLAND_DISPLAY claims a Wayland session; it only counts when its
	// socket exists (the user-manager env keeps it stale after switching
	// to X11) or when there is no competing DISPLAY (boot race — the
	// socket may appear seconds after the daemon starts).
	wayland := os.Getenv("WAYLAND_DISPLAY") != "" &&
		(waylandSocketPresent() || os.Getenv("DISPLAY") == "")
	if wayland {
		if !desktopHasToken(desktop, knownNonWlrootsDesktop) || desktopHasToken(desktop, knownWlrootsDesktop) {
			if _, err := exec.LookPath("grim"); err == nil {
				return &grimBackend{cfg: cfg}, nil
			}
			return nil, fmt.Errorf("no capture backend for Wayland session (XDG_CURRENT_DESKTOP=%q) — install grim (wlroots compositors) or set capture_command", desktop)
		}
		return nil, fmt.Errorf("no capture backend for Wayland session (XDG_CURRENT_DESKTOP=%q) — this compositor needs the portal backend (pending); set capture_command as a stopgap", desktop)
	}
	if os.Getenv("DISPLAY") != "" {
		if waylandSocketPresent() {
			return nil, fmt.Errorf("a Wayland socket exists but WAYLAND_DISPLAY was not inherited — import it (systemctl --user import-environment WAYLAND_DISPLAY) rather than capturing the XWayland root")
		}
		return &x11Backend{jpegQuality: cfg.JPEGQuality, frameMaxDim: cfg.FrameMaxDim}, nil
	}
	return nil, fmt.Errorf("no graphical session (WAYLAND_DISPLAY and DISPLAY unset) — set capture_command in %s", configPath())
}
