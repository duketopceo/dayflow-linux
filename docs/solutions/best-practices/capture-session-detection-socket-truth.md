---
title: Detect display sessions by sockets, not env vars — and persist backend capabilities for oneshot consumers
date: 2026-10-06
module: engine (capture_backend, capture_x11, notify, install)
problem_type: best_practice
component: capture_pipeline
severity: high
tags:
  - "wayland"
  - "x11"
  - "systemd"
  - "session-detection"
  - "capture-backend"
  - "stall-detector"
---

# Detect display sessions by sockets, not env vars

## Context

The U1 capture-backend seam (`resolveCaptureBackend`) selects grim / x11 /
`capture_command` per session. A ten-lens review found that every naive
env-var check in this space produces a silent-wrong-state failure, and these
rules are exactly what U2 (portal/PipeWire backend) will need again.

## Env vars are claims; sockets are truth

- `systemctl --user` manager env **retains stale `WAYLAND_DISPLAY`/`DISPLAY`
  across session-type switches**. `WAYLAND_DISPLAY` set + dead socket +
  `DISPLAY` set is an X11 session — selecting grim parks the daemon in
  quiet-pause forever (heartbeat stays fresh, detector stays quiet).
- Conversely `WAYLAND_DISPLAY` unset + `DISPLAY` set + a live
  `wayland-*` socket in `$XDG_RUNTIME_DIR` is a Wayland session whose env
  wasn't imported — selecting x11 captures the **XWayland root** (black
  frames) instead of the desktop.
- Boot race is the counter-case: socket absent + `WAYLAND_DISPLAY` set +
  no `DISPLAY` is early boot, not a stale var — keep the Wayland path.
- `XDG_CURRENT_DESKTOP` is a colon list and users embed tokens for portal
  workarounds (`sway:GNOME`). Any-token blacklisting hard-errors working
  wlroots sessions — a wlroots whitelist must win over the blacklist.
- Test truth needs a **real unix socket** (`net.Listen("unix", ...)` in a
  stubbed `XDG_RUNTIME_DIR`) — `os.Stat` + `ModeSocket` distinguishes it
  from compositor `.lock` files.

## Persist resolved capability for oneshot consumers

`dayflow-summarize.service` runs the stall check in a oneshot with no
session env (`PassEnvironment` can only pass what the manager imported —
which is the broken thing being detected). The daemon persists the resolved
backend's `NeedsWaylandSocket()` as a meta row at start and each successful
reload; the oneshot reads the row instead of re-deriving detection. Any
future backend must declare this via the interface, not re-signature env.

## Daemon lifecycle traps

- `defer backend.Close()` binds the **interface value at registration** —
  after a config-reload swap it closes the dead backend and leaks the live
  one. Use `defer func() { backend.Close() }()`.
- A backend-resolve failure before `logEvent("daemon_start", ...)` is
  invisible to the stall detector (no heartbeats, no events, Restart=always
  burns start-limit in silence). Log the failure event first, then return.
- `xgb`'s `Reply()` has **no deadline** — an unresponsive X server wedges
  the single-threaded capture loop (SIGTERM can't help mid-syscall). Run
  round-trips in a goroutine, select on `grabFrameTimeout`, and drop the
  conn on timeout so the next tick reconnects.
- `xproto.Setup(conn)` re-parses **cached** connection setup bytes — it is
  not a live query. Root geometry must come from `GetGeometry` per grab or
  a resized root silently crops.
- X11 scanlines pad per-row to `ScanlinePad` bits — never infer bpp from
  `len(data)/(w*h)`; read the `PixmapFormats` entry and guard `pad > 0`
  before modulo (server can send 0).

## When this applies

U2's portal backend, any future capture backend, and any `systemd --user`
unit that gates behavior on session type. Doctor checks also run in caller
env — they can report "ok" while the service env differs; keep that gap
named rather than fixed by inspection.
