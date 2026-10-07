# Requirements: Dayflow standalone desktop app — broad Linux distribution

**Created:** 2026-10-06
**Status:** settled — ready for planning
**Origin:** user request — "a large linux ecosystem compatible desktop app available for download. snap store debian and so on… start with ubuntu, then fedora, redhat, arch, one at a time fully built and tested"
**Grounding:** `/tmp/compound-engineering/ce-brainstorm/u4-standalone/grounding.md`

## Problem frame

Dayflow is an Omarchy/Quickshell shell plugin over a portable Go engine. The
engine is ~80% distro-agnostic today (static binary, `capture_command` escape
hatch, omaseal optional, systemd --user units, no package-manager coupling in
`install.sh`), but three couplings block a distributable desktop app:

1. **UI host** — 22 QML files root in Omarchy shell types (`BarWidget`,
   `Panel`, `KeyboardPanel`, `FloatingWindow`) and ~926 `Color.`/`Style.`
   token references; all engine I/O goes through `Quickshell.Io.Process`.
2. **Capture** — `grim` is the only built-in backend and is wlroots-only;
   Ubuntu/Fedora/RHEL default to GNOME Wayland where it cannot work.
3. **Secrets** — `dayflow key` requires omaseal (Luke-specific); generic
   distro users get file/env fallback only.

Distribution itself is greenfield: no `.desktop` file, no tray entry, no
distro packaging anywhere — release ships bare binaries + `install.sh`.

## Key decisions (user-settled)

- **App shape:** Qt6/QML window + StatusNotifier tray icon — standalone Qt
  runtime, **no Quickshell dependency**. Existing tab/component QML is reused
  under a shim host that provides the `Color`/`Style` singletons and a
  `Process` equivalent; a small C++ host binary embeds the QML. The Omarchy
  plugin remains a separate shipping surface on the same engine.
- **Feature parity:** core first — Today view + Settings + tray in v1;
  Standup, Chat, Week follow in point releases once the distro pipeline is
  proven.
- **Display servers:** Wayland (portal + PipeWire) plus X11 fallback. `grim`
  stays as the wlroots fast path — existing Omarchy installs regress nothing.
- **Secrets:** engine gains a Secret Service backend (gnome-keyring/KWallet
  over D-Bus — godbus is pure-Go) alongside omaseal; file/env fallback stays.
- **Distro order:** Ubuntu first (snap + deb), then Fedora (rpm), then
  RHEL, then Arch — **one at a time, each fully built and tested before the
  next starts**.
- **Test fleet:** `ssh cluster1`–`cluster4` = Ubuntu verification nodes;
  `ssh node3` = Fedora.
- **Distribution:** public — Snap Store listing + GitHub Releases assets
  (deb/rpm downloadable); snap is the canonical auto-update channel.

## Requirements

**R1 — Standalone Qt6/QML host.** `dayflow` ships (or a companion
`dayflow-ui` binary embeds) a Qt6 host providing: the application window, a
StatusNotifierItem tray (recording state, open/quit), a `Process`-equivalent
QML element matching `Quickshell.Io.Process` semantics used by the tabs, and
`Color`/`Style`-compatible singletons driven by a built-in fallback theme
(the Omarchy token values as defaults, no `qs.*` imports at runtime).
Reused QML lives under `app/` or equivalent so plugin files stay untouched.

**R2 — Portal+PipeWire capture backend.** New engine capture backend for
GNOME/KDE/generic Wayland: `org.freedesktop.portal.ScreenCast` session +
PipeWire stream → JPEG frames at the existing cadence. First run shows the
portal consent dialog (once per login; restore token where supported); the
compositor's persistent recording indicator is expected and welcomed — this
app *is* a screen recorder. Backend auto-detect order: `capture_command`
override → grim (wlroots) → portal/PipeWire → X11 → clear error.

**R3 — X11 fallback backend.** Frame grab via XGetImage-class capture (or
`import`-equivalent) when `WAYLAND_DISPLAY` is absent. RHEL/older-Ubuntu/X11
sessions work without extra config.

**R4 — Secret Service keyring.** `dayflow key set|get|del|status` gains a
Secret Service backend used when omaseal is absent; provider key resolution
tries omaseal → Secret Service → config/env. `openrouter_api_key` edits from
Settings land in Secret Service on generic Linux.

**R5 — Desktop integration.** `.desktop` launcher entry, XDG autostart
(option), icon, and tray are installed by packages; `systemd --user` units
remain the capture/timers runtime. `wl-copy` gains `xclip`/`xsel` fallback;
`notify-send` already degrades cleanly.

**R6 — Ubuntu packaging first.** Snap Store package (strict confinement or
documented classic/portal justification) + `.deb` built in CI and published
to GitHub Releases. Installed-and-working is verified on cluster1–4 (clean
Ubuntu GNOME Wayland) — install → consent → capture → timeline shows frames
→ tray visible.

**R7 — Sequential distro expansion.** Fedora rpm verified on node3 next;
then RHEL rpm; then Arch (AUR or pacman repo). Each distro gets its own
packaging + verification pass — no "should work" claims without a real
install on the target distro.

**R8 — Update path.** Snap handles auto-update; deb/rpm surface the existing
version-skew notice pointing at the GitHub Release download.

## Non-goals

- Flatpak/Flathub (revisit after rpm proves out — Fedora's native store)
- Windows/macOS ports; moving the UI off QML; Electron or web views
- Rearchitecting the engine beyond the capture-backend and keyring seams
- Omarchy plugin changes (it stays the shell-native surface)
- Standup/Chat/Week in the standalone v1 (point releases)
- AUR maintenance automation beyond publishing the pkgbuild

## Constraints

- **Publicly distributed screen recorder** — privacy posture is load-bearing:
  local-first storage, explicit portal consent, visible recording indicator,
  no telemetry. Snap store review will scrutinize the interface requests.
- Portal capture shows a consent dialog per login session (persist tokens
  reduce repeat prompts); onboarding copy must set that expectation — it is
  not a bug to be hidden.
- Cgo enters the build for PipeWire (or a small C helper is shipped); the
  main binary stays static where possible — snap bundles the rest.
- The U3 polish plan (shared components, dense Settings) lands first and is
  the component source the standalone host reuses — sequencing dependency.
- Distro matrix for v1 claims: Ubuntu GNOME Wayland + Ubuntu X11 only.

## Success criteria

- Clean Ubuntu install on a cluster node: `snap install dayflow` or
  `apt install ./dayflow.deb` → window opens from launcher, consent dialog
  explained, frames land in timeline within a minute, Settings saves an API
  key to Secret Service.
- Same machine on X11 session: capture works with no config change.
- Fedora node3: rpm installs, captures (Wayland), passes the same smoke.
- Omarchy plugin install unaffected (grim path unchanged, units unchanged).

## Deferred to planning

- Exact Qt6 host layout (single binary vs `dayflow-ui` companion), Cgo vs
  helper for PipeWire, snap confinement mode choice, tray icon set,
  .deb/rpm/aap maintainer scripts, CI matrix layout, restore-token UX.
