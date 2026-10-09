---
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Plan: Dayflow standalone desktop app — Ubuntu first, then Fedora

**Created:** 2026-10-06
**Origin:** `docs/brainstorms/2026-10-06-standalone-desktop-app-requirements.md`
**Related:** `docs/plans/2026-10-05-002-feat-classic-ui-popup-polish-plan.md`
(popup polish — sequencing relationship in KTD10), grounding dossier at
`/tmp/compound-engineering/ce-brainstorm/u4-standalone/grounding.md`.

## Goal Capsule

**Objective:** a Linux user on stock Ubuntu (GNOME Wayland or X11) can install
Dayflow from the Snap Store or a `.deb`, open it, grant screen access once, and
get a working recorder + timeline + Settings + tray — no Omarchy, no
Quickshell, no manual config. Fedora follows as an rpm verified on a real
install before the next distro starts.

**Means:** keep the static Go engine; add capture-backend + keyring seams,
a cgo-confined PipeWire portal helper, a `dayflow-ui` Qt6/QML host embedding
reused tab QML, and nfpm/snapcraft packaging — sequential per-distro gates
(KTD2, KTD3, KTD5, KTD8).

**Authority hierarchy:** the origin requirements doc governs product scope
(R1–R8, non-goals); this plan governs implementation shape; code review may
correct implementation, not product decisions. **Stop conditions:** strict
confinement proves impossible without privileged plugs on Ubuntu's actual
snapd → surface before switching to classic; portal persist unsupported on
the target Ubuntu release → surface before shipping a consent-every-login
experience.

---

## Product Contract

### Problem frame

Dayflow is an Omarchy shell plugin over a portable Go engine. The engine is
~80% distro-agnostic already, but the UI roots in Omarchy shell types, capture
is wlroots-only, and `dayflow key` requires Luke's omaseal — so the product
can't be distributed. Prior plans ruled out a standalone GUI as a
*replacement* for the plugin; this plan adds it as a separate shipping
surface — the reversal is additive, not contradictory (see Sources).

### Requirements (condensed from origin; the origin text governs)

- **R1 — Standalone Qt6/QML host:** window + StatusNotifier tray, no
  Quickshell; shim provides `Color`/`Style` singletons and a `Process`
  equivalent; tab QML reused.
- **R2 — Portal+PipeWire capture backend** for GNOME/KDE/generic Wayland,
  consent dialog once, recording indicator welcomed. Detect order:
  `capture_command` → grim → portal/PipeWire → X11 → clear error.
- **R3 — X11 fallback** frame-grab when `WAYLAND_DISPLAY` is absent.
- **R4 — Secret Service keyring** backend alongside omaseal; file/env
  fallback stays.
- **R5 — Desktop integration:** `.desktop`, icon, tray, autostart option;
  `systemd --user` units remain the runtime for unconfined installs;
  `wl-copy` gains X11 fallbacks.
- **R6 — Ubuntu packaging first:** snap + `.deb` in CI → GitHub Releases;
  verified working on cluster1–4 (install → consent → frames → tray).
- **R7 — Sequential distro expansion:** Fedora rpm verified on node3, then
  RHEL, then Arch — each fully built and tested before the next.
- **R8 — Update path:** snap auto-updates; deb/rpm get the version-skew
  notice pointing at GitHub Releases.

### Key decisions (settled upstream — provenance only)

- **Qt6/QML window + SNI tray, no Quickshell** (governs R1) — reuses all
  existing tab QML under a shim host.
- **Core-first v1: Today + Settings + tray** (governs R1 scope) — Standup,
  Chat, Week follow in point releases.
- **Wayland + X11 fallback** (governs R2/R3) — grim stays the wlroots fast
  path; no Omarchy regression.
- **Secret Service backend** (governs R4) — omaseal retained where present.
- **Public distribution, sequential per-distro gates** (governs R6–R8) —
  Snap Store + GitHub Releases; cluster nodes are the test fleet.

### Success criteria (from origin)

- Clean Ubuntu node: snap or deb install → window opens → consent explained
  and granted → frames in timeline within a minute → API key lands in Secret
  Service.
- Same machine on X11 session: capture works, no config change.
- Fedora node3: rpm installs, captures on Wayland, same smoke.
- Omarchy plugin install unaffected (grim path, units unchanged).

### Scope boundaries

**Deferred to follow-up work:** Standup/Chat/Week tabs in the standalone UI;
Flatpak/Flathub (easiest portal host — revisit after rpm proves out, but the
user's order is snap+deb first); RHEL and Arch packaging (each gets its own
plan once Fedora's rpm gate passes — RHEL Qt6/EPEL availability is unverified);
`ignore_apps` active-window detection on non-wlroots compositors (no source
exists on GNOME — field degrades, doctor warns, and the ported UI marks
dependent controls inert per U5); arm64 package artifacts (amd64 v1;
arm64 when a real target node exists — the only aarch64 machine here runs
the plugin lane, which packaging doesn't affect); AUR automation beyond a
maintained `dayflow-bin` PKGBUILD; the `dayflow` UI remaining tabs' full
command surface where reused QML references future features.

**Non-goals:** Windows/macOS, Electron/web views, engine rearchitecture beyond
the two seams (capture backend, keyring backend), Omarchy plugin changes,
telemetry, classic snap confinement.

---

## Planning Contract

### Key technical decisions

- **KTD1 — `dayflow-ui` companion binary (C++/Qt6).** QML embeds in the binary
  via `qt_add_qml_module()` — no filesystem QML paths (AppArmor-friendly).
  Build needs `qt6-base-dev`, `qt6-declarative-dev`, Qt6::Quick/Widgets/DBus.
  `setDesktopFileName("dayflow")` early — drives Wayland app_id, portal
  prompt attribution, and SNI identity. The engine stays a pure-Go static
  binary exec'd by the UI.
- **KTD2 — `captureBackend` interface.** `Grab(ctx) ([]byte, error)` +
  `Close()`, resolved once at daemon start/reload (the `resolveCaptureCommand`
  seam at `engine/capture.go`). grim/custom-command become a per-shot adapter
  over the existing `grabFrame`; portal and X11 are new impls.
  `XDG_CURRENT_DESKTOP` **orders** backend attempts, not selects one — an
  init/exec failure falls through to the next candidate, and an
  unrecognized Wayland desktop tries grim before portal, preserving the
  coverage `exec.LookPath` provided today (niri/river/wayfire/labwc and
  colon-joined values like `sway:wlroots` all keep working).
  `waylandReachable()` gates capture idling for all session-bound backends
  — grim needs the socket, and the portal session dies with the
  compositor too, so portal reports the same quiet semantics (implemented
  as `NeedsWaylandSocket()` on the backend; X11/custom commands are
  ungated). Fixes `install.go`'s hardcoded `WAYLAND_DISPLAY=wayland-1`
  (GNOME uses `wayland-0`) — the fallback env line goes, and the unit's
  `PassEnvironment` gains `DISPLAY` and `XAUTHORITY` so the X11 backend is
  actually reachable under the shipped units (it is a whitelist; GDM Xorg
  sessions export both to the manager env).
- **KTD3 — Portal capture = supervised helper binary.** `dayflow-portal`
  (C++/Qt or Go+cgo — cgo confined to this artifact, `dayflow` stays
  `CGO_ENABLED=0`): owns the ScreenCast session lifecycle
  (CreateSession → SelectSources `types:MONITOR multiple:true cursor_mode:2
  persist_mode:2` → Start → `OpenPipeWireRemote` → `pw_context_connect_fd`
  on the passed fd — **never** the ambient pipewire socket, which is what
  makes strict snap viable). Frames negotiated ~1fps `SPA_DATA_MemFd` BGRA,
  per-monitor streams stitched horizontally to preserve grim's composite
  semantics, JPEG-encoded, written length-prefixed to stdout; engine spawns
  it as a long-lived child and reads frames per tick. Crib implementations:
  `go2tv.app/screencast`, `marang/robotgo/screen/portal`. Consent denial /
  portal absent / stream death → distinct `captureState` values surfaced to
  tray/UI; denial parks with a re-auth affordance, never re-prompts in a
  loop. **Portal-helper spawn is gated on a `capture_enabled` engine flag
  written only by the onboarding consent step** — an autostarted daemon
  (snapd, systemd preset, or a bare `dayflow daemon`) idles with zero
  portal traffic until that flag exists, so no install path can fire an
  unattributed consent prompt. Restore token (rotated each `Start`) lives
  in a state file under the data dir or a DB meta row — **not**
  config.json, whose mtime drives the daemon's hot-reload and would churn
  the helper on every rotation. Low-sensitivity and revocable; enables
  consent-once-across-logins on xdp-gnome with ScreenCast restoration
  (≈45+; verify the shipped version on cluster1), xdp-kde ≥5.25,
  xdph ≥1.3.5. Stream teardown+reattach on PipeWire error/suspend;
  monitor hotplug handled by re-Start with token.
- **KTD4 — Keyring backend interface.** `type keyringBackend { Get/Set/Del }`:
  omaseal exec backend (existing) → `zalando/go-keyring` Secret Service
  (pure Go, godbus — `collection/login` via `aliases/default`, works on
  gnome-keyring + KWallet) → file/env. Inside the snap, Secret Service is
  unreachable without the reviewed `password-manager-service` plug — so the
  snap uses `org.freedesktop.portal.Secret` (auto-connected via `desktop`
  plug; `rymdport/portal` implements it in Go) to derive an app-scoped
  master key encrypting a local store file. Backend select orders
  omaseal → Secret Service → file/env, but the **lookup chain preserves
  shipped precedence**: explicit `api_key`/`openrouter_api_key`/env/
  keys.json win first and keyring backends are consulted only when no
  explicit key is set — a stale Secret Service entry must never shadow a
  deliberately-set config key. `dayflow key status` reports the active
  backend; account enumeration degrades to backend-name-only off omaseal
  (Secret Service can't list — accepted). Backend absent → file
  fallback + warning.
- **KTD5 — Bundled-engine resolution.** The app execs the engine bundled
  beside `dayflow-ui` (`applicationDirPath()`-relative), not PATH —
  `DAYFLOW_ENGINE` env overrides for dev. This kills the PATH-hijack skew
  class and repurposes `InstallPrompt.qml`: bundled engine means
  "engine missing" is impossible. The plugin-vs-engine skew check keys on
  `manifest.json` which doesn't exist in the app — app shows engine version
  from `dayflow --version`, so *component* skew is impossible. That closes
  only the engine-vs-UI axis; R8's deb/rpm half needs the different
  installed-vs-latest check — the app reuses the skew-banner affordance as
  an update-available notice: a once-daily GitHub `/releases/latest` tag
  comparison (the endpoint `install.sh` already resolves), dismissible via
  the existing `skewDismissedFor` pattern, opt-out-able in Settings, and
  disabled inside the snap (snapd owns updates there).
- **KTD6 — Strict snap, no privileged plugs.** base `core24`, extension
  `kde-neon-6` (Qt6 runtime via `kf6-core24` content snap) — fallback:
  staged `qt6` stage-packages if extension weight is unacceptable. Plugs:
  `desktop desktop-legacy wayland x11 opengl unity7 network home gsettings`
  — no `pipewire` (fd-connect), no `screencast-legacy`, no
  `password-manager-service` (Secret portal instead). Stage
  `libpipewire-0.3-0` **and** `libspa-0.2-modules` (or verify the
  kde-neon-6 content snap provides them and set `SPA_PLUGIN_DIR`
  accordingly) — the PipeWire client cannot negotiate buffers without
  the SPA modules. Data dirs redirect via `DAYFLOW_DATA_DIR`/
  `DAYFLOW_CONFIG` env to `SNAP_USER_COMMON` — the snap journal is
  **separate** from deb/plugin installs (documented; env override seam
  already exists per `docs/agent-contract.md`). **Capture is owned by an
  in-app supervisor**: `dayflow-ui` spawns and watches the capture child
  whenever the app session runs. The `daemon-scope: user` snap service
  is demoted to an opportunistic alternative — it is gated behind
  snapd's `experimental.user-daemons` host flag (experimental since
  snapd 2.46), off by default everywhere and unrequestable by a store
  snap. Launch-at-login comes from `org.freedesktop.portal.Background`
  plus the autostart entry that portal grants. **Product consequence,
  accepted:** in the snap lane capture is session-bound — it runs while
  the app runs (including headless background mode), not as an
  always-on system service; the UI surfaces "capture runs while Dayflow
  is running" honestly rather than pretending parity with the units
  lane. The `capture_command` escape hatch is inert in the snap (can't
  exec host binaries).
- **KTD7 — nfpm for deb/rpm.** One `nfpm.yaml` produces `.deb` and `.rpm`:
  `usr/bin/dayflow{,-ui}`, `/usr/lib/systemd/user/` units (never enabled
  from root maintainer scripts — per-user state is untouchable there), the
  `.desktop` file, hicolor icons, AppStream metainfo (GNOME Software /
  Discover storefronts). First run calls `dayflow install` (existing
  onboarding path) — capture enablement is gated behind the onboarding step
  that explains the portal consent dialog *before* the daemon can trigger
  it. `Depends` on qt6 runtime + QML module packages
  (`qml6-module-qtquick`, `qml6-module-qtquick-window`, `qt6-wayland`,
  `qml6-module-qt-labs-platform` if the labs tray type is used) +
  `libpipewire-0.3-0` + `libspa-0.2-modules` — Ubuntu splits QML imports
  into `qml6-module-*` packages, so a lib-only Depends yields a blank
  window, and without `qt6-wayland` the app falls back to XWayland on the
  primary target session. postinst only runs
  `update-desktop-database`/icon-cache guards.
- **KTD8 — Per-distro gate = scripted smoke on the ssh fleet.** Ubuntu done
  when: deb + snap both built in CI; `scripts/test-standalone.sh` passes on
  ≥1 cluster node per session type (GNOME Wayland + X11) — install →
  launch → consent copy → capture running → `status --json` shows frames →
  tray visible → Settings key save → uninstall clean. "cluster1–4" in R6
  names the verification **fleet**, not a required set — the gate is one
  node per session type, not a pass on all four. The tray check presumes
  an SNI host (Ubuntu ships appindicator enabled; on a node without one
  the window-only path is the expected behavior, not a failure). Snap
  Store submission
  (name registration, description, review) proceeds in parallel — human
  review latency is tracked, not gating. Fedora = same smoke via rpm on
  node3 (note: stock Fedora GNOME has no SNI host — window-only is the
  accepted fallback there).
- **KTD9 — Single version contract extended.** `dayflow-ui` is stamped with
  `-DVERSION` from the release tag; `release.yml` asserts tag == engine
  const == manifest == app version — the version-truth trio becomes a
  quartet. `version_test.go`-style pin covers the app manifest.
- **KTD10 — QML port targets the carried tree, softened popup-polish
  dependency.** The
  shim reproduces the singleton interface (`Color`/`Style`/`Process`), not
  file contents — so host work proceeds in parallel with the popup-polish
  plan; reusing its shared components (`PanelCard`/`PanelButton`/…) later
  is a bonus
  normalization, not a blocker. Reused QML must come from the post-restore
  carried tree — three invisible carries pinned by `panel_carry_test.go`
  (see `docs/solutions/best-practices/restores-must-carry-forward-fixes-inside-reverted-files.md`).
- **KTD11 — `Process` shim contract is exact.** Reproduce
  `Quickshell.Io.Process`/`StdioCollector` semantics verbatim: `command`
  assignable while stopped, `stdinEnabled` + `write()`, collectors with
  `waitForEnd` + `onStreamFinished(text)`, `onStarted`, `onExited(code)`,
  `onRunningChanged` — and critically `FailedToStart` emits *neither*
  exited nor streamFinished (callers' `didStart` pattern depends on it;
  contract-exact per the local-decisions-endpoint lesson). Six
  `["bash","-c","dayflow … | wl-copy"]` call sites (five in Panel.qml,
  one in TodayTab.qml) get a dedicated clipboard path (Qt clipboard API /
  `xclip`/`xsel` fallback), not a shell shim.
  `Quickshell.iconPath()` → `QIcon::fromTheme`.
- **KTD12 — Consent/onboarding ordering.** First-run capture enablement is
  explicit and sequenced *after* the copy explaining the system consent
  dialog — no systemd-started daemon may fire an unattributed portal prompt.
  Consent copy is **session-aware**: portal sessions get the
  dialog-expectation copy; X11 sessions get copy stating no system prompt
  will appear and naming the app's own indicator as the recording
  guarantee. On X11 (no compositor indicator) the app shows its own
  persistent recording affordance (tray icon state + window badge) — and
  that affordance is a **capture precondition**, not decoration: the
  daemon auto-pauses when no `dayflow-ui` indicator host is alive
  (presence heartbeat) and resumes on return, because the
  privacy-posture claim can't depend on compositor UI that doesn't
  exist or a window the user may close. Denial re-auth routes back
  through the same explanation before retry. Tray state map (U4):
  recording / paused / locked / stalled / consent-needed /
  denied-parked / stream-error each get a distinct icon + menu label,
  plus a "Grant screen access" item in the parked state. When no SNI
  host exists (stock Fedora GNOME), closing the window **quits**
  `dayflow-ui` while the daemon keeps capturing — no hide-to-nowhere;
  the app is single-instance so relaunching raises the window.

### High-level technical design

```text
Component topology (standalone install)
─────────────────────────────────────────────────────────────
 dayflow-ui (Qt6/C++, QML embedded via qt_add_qml_module)
   │  spawns                          │  registers
   ▼                                  ▼
 dayflow (Go, static) ──supervises──▶ dayflow-portal (cgo)
   │  exec argv + stdout JSON            │  D-Bus: ScreenCast portal
   │  same protocol as Quickshell.Io     │  pw_context_connect_fd(portal fd)
   │  Process contract today             ▼
   ▼                                PipeWire stream ~1fps → stitched JPEGs
 systemd --user units (deb/rpm)  or   in-app supervisor (snap lane)
   │
   ├─ keyring: omaseal → Secret Service (godbus) → file/env
   └─ snap lane: org.freedesktop.portal.Secret instead

Capture backend resolution (KTD2)
─────────────────────────────────
 capture_command → wlroots desktop? → grim adapter
                 → Wayland session bus? → portal helper
                 → else → X11 backend → clear error state
                 (grim LookPath no longer decides; XDG_CURRENT_DESKTOP does)

Portal consent lifecycle (KTD3)
───────────────────────────────
 first run → app explains dialog → user enables capture →
   helper CreateSession/SelectSources(persist_mode:2) →
   consent dialog ONCE → stream up ──▶ frames flow
                       ├── denial → parked "needs consent" state (no loop)
                       ├── stream death/suspend → teardown → re-Start with
                       │   rotated restore_token (no dialog on modern xdp)
                       └── portal/backend absent → error state (X11
                           fallback only when WAYLAND_DISPLAY is absent,
                           per U1 — a Wayland session with no working
                           portal has no further backend)
```

### Assumptions

- The in-app supervisor is the primary snap capture path (KTD6);
  `daemon-scope: user` stays opportunistic because it requires snapd's
  `experimental.user-daemons` host flag, off by default. Whether the
  Background portal reliably grants autostart/run-in-background under
  strict confinement on Ubuntu's shipped xdg-desktop-portal is
  verified on cluster1 — if not, the snap lane can never offer
  always-on-adjacent capture and that limitation ships documented.
- **v1 support floor is Ubuntu 24.04 LTS** (ships xdp-gnome 46 with
  ScreenCast restoration; Ubuntu 22.04's xdp-gnome 42 predates it and
  would re-prompt every login — 22.04 is out of the v1 matrix, not a
  silent degradation). `persist_mode=2` behaves on the cluster's shipped
  xdp — verified early at U2/U8, since the stop condition covers a
  missing-persist surprise.
- Qt6 Quick is packaged for Ubuntu 24.04+ and Fedora 40+ from native
  archives (EPEL/RHEL is *not* assumed — that's why RHEL is a later phase).

### Open questions (deferred, non-blocking)

- `multiple:true` stream stitch vs per-monitor frame attribution — v1
  stitches to one composite (grim parity); attribution is a schema question
  for later.
- Whether the portal helper is C++/Qt or Go+cgo — pick at implementation;
  both confine cgo to the helper (C++/Qt reuses the dayflow-ui toolchain;
  Go+cgo reuses robotgo cribs).
- Fedora SNI absence — window-only accepted for v1; KStatusNotifierItem vs
  Qt.labs.platform SystemTrayIcon decided at implementation.

---

## Implementation Units

| U-ID | Title | Key files | Depends on |
|---|---|---|---|
| U1 | Capture-backend seam + X11 + detect fixes | engine/capture.go, engine/install.go, engine/capture_x11.go | — |
| U2 | Portal+PipeWire capture helper | app/portal-capture/, engine/capture_portal.go | U1 |
| U3 | Keyring backends (Secret Service + Secret portal) | engine/keyring*.go | — |
| U4 | Qt6 host scaffold + Process/theme shims | app/ui/ (CMake, main.cpp, Process.qml-el, Color/Style singletons) | — |
| U5 | QML port: Today + Settings + tray window | app/ui/qml/, reused tab QML, Onboarding adaptation | U4, U1 |
| U6 | Desktop integration assets | app/packaging/ (.desktop, icons, metainfo, autostart) | U5 |
| U7 | nfpm deb + rpm in release CI | app/packaging/nfpm.yaml, .github/workflows/release.yml | U1–U6 |
| U8 | Snapcraft strict snap + store submission | snap/snapcraft.yaml, store listing assets | U2, U3, U5, U6 |
| U9 | Cluster verification + Fedora rpm gate | scripts/test-standalone.sh, ssh node runs | U7, U8 |

### U1. Capture-backend seam, X11 backend, detect fixes

**Goal:** `captureBackend { Grab, Close }` exists; grim+`capture_command` are
adapters; X11 backend works; detection is compositor-keyed.

**Requirements:** R2 (order), R3, R5 (partial). **Dependencies:** none.

**Files:** `engine/capture.go` (extract `grabFrame`/`captureOnce` backend
seam), `engine/capture_grim.go` (grim/capture_command adapter),
`engine/capture_x11.go` (new), `engine/install.go` (drop hardcoded
`WAYLAND_DISPLAY=wayland-1`; add `DISPLAY` and `XAUTHORITY` to
`PassEnvironment` so the X11 backend is reachable under the shipped
units — it is a whitelist, and without them detection can never select
X11 from a packaged install), `engine/capture_test.go`, `engine/go.mod`.

**Approach:** per KTD2. X11 backend is pure Go — crib the X11 wire-protocol
capture from `go-freedesktop/screencast` (CGO_ENABLED=0 preserved). Root
window capture per tick; detection requires `WAYLAND_DISPLAY` unset AND
`DISPLAY` set. `activeWindowClass()`/`output=auto` stay hyprctl-only and
degrade silently on non-wlroots (doctor already warns).

**Test scenarios:**
- `captureBackend` fake returning fixture JPEG → `captureOnce` writes frame +
  dedup events (pattern from `capture_test.go:452-667`).
- Detect order: `XDG_CURRENT_DESKTOP=GNOME` + grim on PATH → grim NOT
  selected (portal or error), `Hyprland` + grim → grim selected.
- X11 backend: `DISPLAY` set + no `WAYLAND_DISPLAY` → x11 selected; grabs
  non-empty buffer (unit-test the XGetImage encoding path against a fixture
  or xvfb when available; CI can run xvfb).
- `WAYLAND_DISPLAY=wayland-1` no longer hardcoded in generated unit.

**Verification:** `go test ./...` green; `dayflow doctor` on this machine
still reports grim selected (wlroots preserved).

### U2. Portal+PipeWire capture helper

**Goal:** `dayflow-portal` helper owns ScreenCast+PipeWire and streams frames
to the engine; consent lifecycle per KTD3.

**Requirements:** R2. **Dependencies:** U1.

**Files:** `app/portal-capture/` (new dir: `CMakeLists.txt` or `go.mod` +
sources), `engine/capture_portal.go` (supervisor backend: spawn, supervise,
read length-prefixed frames, restart policy), `engine/store.go` or a
data-dir state file (`portal_restore_token` — deliberately **not**
config.json, see KTD3), `engine/capture_portal_test.go`.

**Approach:** KTD3. ~~Before writing SPA negotiation code, evaluate a
GStreamer `pipewiresrc`-based helper against the length-prefixed-JPEG
contract~~ **Outcome (implemented):** GStreamer rejected — `pipewiresrc`
still requires the full portal D-Bus flow in-process and adds a runtime
dep; direct libpipewire via a thin cgo bridge matches the robotgo/go2tv
references and stays self-contained.
The helper prints structured status lines on stderr
(`consent-needed`, `streaming`, `denied`, `stream-dead`, `parked`) —
the supervisor maps consent/denial/parks to `capture_paused` events so
`status` reports "paused" (not "down") while awaiting consent. Backoff:
denial parks until user re-auth action (persisted in `meta`, cleared by
`dayflow capture retry`; survives config reloads); stream death →
re-Start with rotated token; ≥3 consecutive frameless exits → parked for
5 min, then auto-retries (transient bus/portal outages self-heal; renewed
frameless deaths re-park for another interval — `dayflow capture retry`
is the manual unpark).
The restore token reaches the helper via `DAYFLOW_PORTAL_TOKEN` env
(argv is world-readable), with the child's whole env allowlisted.

**Test scenarios:**
- Unit: supervisor parses helper status lines → correct `captureState`
  transitions (streaming/denied/dead/parked).
- Unit: fake helper emitting fixture JPEG frames → frames land in store with
  dedup; helper kill → supervisor restarts with same session params.
- Integration on a Wayland node (cluster): real portal session → consent →
  frames; `persist_mode=2` restore after daemon restart → no second dialog.

**Verification:** unit suite + a documented manual session on a cluster node
(no CI Wayland portal exists).

### U3. Keyring backends — Secret Service + Secret portal

**Goal:** `keyringBackend` interface; Secret Service for unconfined; Secret
portal path ready for the snap; `key status` reports backend.

**Requirements:** R4. **Dependencies:** none.

**Files:** `engine/keyring.go` (interface + omaseal adapter),
`engine/keyring_secretservice.go` (new), `engine/keyring_secretportal.go`
(new), `engine/keyring_test.go`, `engine/go.mod` (add `zalando/go-keyring`,
`rymdport/portal`).

**Approach:** KTD4. Backend select: omaseal on PATH → omaseal; else session
bus `org.freedesktop.secrets` reachable → Secret Service; else file/env +
warn. Snap sets `DAYFLOW_KEYRING=portal` forcing the Secret-portal store.

**Test scenarios:**
- `fakeOmaSeal` PATH-stub path unchanged (existing test stays green).
- Secret Service backend behind an interface fake — Get/Set/Del round-trip;
  backend-absent → file fallback + warning surfaced in `key status`.
- `key status` reports `backend: omaseal|secret-service|portal|file`.
- `DAYFLOW_KEYRING=portal` forces portal backend even with SS present.

**Verification:** `go test ./...`; on this machine `key status` still shows
omaseal backend.

### U4. Qt6 host scaffold + shims

**Goal:** `dayflow-ui` builds and shows a window with tray; `Process`,
`StdioCollector`, `Color`, `Style` shims satisfy the reused QML contract.

**Requirements:** R1. **Dependencies:** none.

**Files:** `app/ui/CMakeLists.txt`, `app/ui/main.cpp`, `app/ui/process.h/.cpp`,
`app/ui/theme.h/.cpp`, `app/ui/tray.cpp`, `app/ui/qml/Main.qml` (+ tray icon
asset), `app/packaging/icons/`.

**Approach:** KTD1, KTD5, KTD11. `Process` element = `QProcess` subclass
registered via `QML_ELEMENT`; `Color`/`Style` as `QML_SINGLETON` objects
carrying the Omarchy fallback palette/type scale (values mirrored from
`/usr/share/omarchy/shell/Commons` — generated, not hand-copied, per the
drift lesson). Tray = `QSystemTrayIcon` with recording-state icon swap +
window show/hide menu. Bundled-engine resolution per KTD5.

**Test scenarios:**
- Harness-style smoke: `dayflow-ui` launches offscreen
  (`QT_QPA_PLATFORM=offscreen`), window instantiates, `Process` element
  runs a stub `dayflow` (`status --json` fixture) → stdout collected →
  `onStreamFinished` fires.
- `FailedToStart` (missing binary) → running:false, no exited, no
  streamFinished (didStart semantics).
- `stdinEnabled` write → stub records stdin (config patch - shape).
- Tray icon registers under an SNI watcher (manual check; skip assertion
  offscreen).

**Verification:** build passes in CI; offscreen smoke script exits 0.

### U5. QML port: Today + Settings + tray

**Goal:** reused QML renders in `dayflow-ui`: Today view, Settings (grouped
per the popup-polish plan's direction or classic fallback), onboarding
adapted, quick actions in the window chrome, and the R8 update-available
notice for deb/rpm installs.

**Requirements:** R1, R5 (partial), R6 (consent UX part), R8 (deb/rpm
half). **Dependencies:**
U4, U1 (engine must run for real data; stub for dev).

**Files:** `app/ui/qml/` (window root, adapted `Onboarding.qml`,
`InstallPrompt.qml` → engine-bundled path), reused `TodayTab.qml`,
`Settings.qml`, `SettingsField.qml`, `BusyBar.qml`, `CopyButton.qml`,
`DayNavRow.qml`, `CalendarPicker.qml`, `DailyWorkflowGrid.qml`,
`PagedText.qml`-equivalents as needed; `engine/main.go` (`export --copy`
xclip/xsel fallback; `wl-copy` dep removed from app path).

**Approach:** KTD10/KTD11/KTD12. Port = swap `Panel`/`KeyboardPanel`/
`BarWidget` roots for a plain `ApplicationWindow`; the `dayflow` controller
object from Panel.qml moves into an app-side facade keeping the same
property/function surface. `Qt.resolvedUrl("scripts/install.sh")` surface is
removed (bundled engine). Onboarding gains the consent-explanation step
before capture enablement, session-aware per KTD12. The update notice
per KTD5 surfaces here (deb/rpm only — hidden under snap and offline).
Controls that depend on an active-window source (`ignore_apps` field,
"Ignore current app" action) render disabled/annotated when the
compositor provides none — a privacy boundary must never look
functional while silently inert (GNOME is the primary target and has
no source).

**Test scenarios:**
- Offscreen render: Today + Settings load with stub engine, no QML errors,
  no undefined `Color`/`Style`/`Process` references.
- Onboarding flow: stubbed detect/doctor/install/key set — consent copy
  precedes the capture-enable action.
- Clipboard export writes to Qt clipboard on X11 fixture.
- `bash -c` pipe call sites: all six re-pointed at the clipboard path
  (grep zero `["bash","-c"]` in app QML).
- Update notice: with a stubbed "newer release exists" response, the
  banner appears, links to the Releases page, dismisses, and stays
  hidden under a stubbed snap env.
- Dead-control gating: with a stub engine reporting no active-window
  source (GNOME), `ignore_apps` and "Ignore current app" render
  disabled/annotated rather than functional-looking.

**Verification:** offscreen smoke + live run on this machine (Wayland,
non-Omarchy context still works: tokens come from the builtin palette).

### U6. Desktop integration assets

**Goal:** launcher entry, icon set, AppStream metainfo, autostart option —
the app is a first-class desktop citizen.

**Requirements:** R5. **Dependencies:** U5.

**Files:** `app/packaging/dayflow.desktop`,
`app/packaging/io.dayflow.appdata.xml`, `app/packaging/icons/` (svg + png
sizes), `app/ui/` (XDG autostart toggle in Settings or onboarding).

**Approach:** `StartupWMClass=dayflow` matching `setDesktopFileName`;
categories `Utility;`; metainfo carries the privacy posture (local-first,
consent, indicator) — it's storefront-facing copy. Autostart = user-visible
opt-in toggle writing `~/.config/autostart/dayflow.desktop` (unconfined
lane).

**Test scenarios:** `desktop-file-validate` + `appstreamcli validate` clean;
icon cache update in package scripts guarded.

**Verification:** validation tools pass in CI.

### U7. nfpm deb + rpm in release CI

**Goal:** `dayflow_<v>_amd64.deb` and `.rpm` built per tag — the **deb**
publishes to GitHub Releases immediately; the **rpm** builds in CI but
attaches to Releases only after the U9 node3 Fedora gate passes
(publishing an unverified rpm would contradict R7's sequential gate).
amd64 only for v1 — arm64 artifacts deferred until a target arm64 node
exists (the cgo/Qt6 toolchain matrix for arm64 is real cost with no
current consumer).

**Requirements:** R6, R7 (build half). **Dependencies:** U1–U6 (packages
need real artifacts).

**Files:** `app/packaging/nfpm.yaml`, `.github/workflows/release.yml` (new
packaging job), `scripts/install.sh` (doc note only — unchanged behavior).

**Approach:** KTD7, KTD9. CI matrix builds dayflow (static), dayflow-portal
(cgo), dayflow-ui (Qt6) — amd64 for v1. deb Depends per KTD7's expanded
list (qt6 runtime + `qml6-module-*` + `qt6-wayland` + `libpipewire-0.3-0`
+ `libspa-0.2-modules`). rpm deps: `qt6-qtbase, qt6-qtdeclarative,
qt6-qtwayland, pipewire-libs`.

**As-shipped corrections (post-U2):** `dayflow-portal` is a Go+cgo module
at `app/portal-capture/` — its build needs `libpipewire-0.3-dev` +
pkg-config and `CGO_CFLAGS_ALLOW='-f.*'` (pkg-config emits flags cgo's
default allowlist rejects; already wired in ci.yml). The package must
install `dayflow-portal` beside `dayflow` (sibling resolution in
`portalHelperPath` — `/usr/bin` satisfies both sibling and PATH lookup)
or document `DAYFLOW_PORTAL_HELPER`. The helper is spawned by the engine
with an allowlisted env (`DAYFLOW_PORTAL_TOKEN` travels via env, not
argv) — no packaging config needed beyond binary placement.

**Test scenarios:** package contents assertions (paths, .desktop, units,
metainfo, `dayflow-portal` present beside `dayflow`); `dpkg-deb --info`
sanity in CI; SHA256SUMS extended to cover packages.

**Verification:** CI green; artifacts on a draft release.

### U8. Strict snap + store submission

**Goal:** `snapcraft.yaml` produces a working strict snap on Ubuntu; store
listing submitted with the privacy justification.

**Requirements:** R6, R8. **Dependencies:** U2, U3, U5, U6.

**Files:** `snap/snapcraft.yaml`, `snap/local/` (store listing copy,
screenshots plan), `engine/config.go` (SNAP env handling — reads
`DAYFLOW_*` which snapcraft sets).

**Approach:** KTD6. In-app supervisor owns capture in the snap lane;
`org.freedesktop.portal.Background` request provides launch-at-login —
verify it actually grants under strict confinement on cluster1's shipped
xdp before relying on it (fallback: documented "open the app to capture"
limitation). Env redirects:
`DAYFLOW_DATA_DIR=$SNAP_USER_COMMON`, `DAYFLOW_CONFIG=$SNAP_USER_COMMON/config.json`,
`DAYFLOW_KEYRING=portal`. Store copy: the requirements doc's privacy posture
verbatim-ish (local-first, consent, indicator, no telemetry) **plus a
per-plug justification table** — explicitly acknowledging that the `x11`
plug can observe XWayland clients without portal consent (retained only
for the Qt XWayland fallback) — reviewers of screen recorders look for
exactly this honesty.

**Test scenarios:**
- `snap install --dangerous dayflow.snap` on cluster1 → launch → consent →
  frames (portal fd path — watch `snap audit`/journal for denials,
  including SPA module loading from the staged libspa). The staged
  `dayflow-portal` is spawned in-snap by the engine, so its allowlisted
  env (`DAYFLOW_PORTAL_TOKEN`) never crosses the confinement boundary —
  but the helper binary must be staged next to `dayflow` inside the snap
  (sibling resolution) and libpipewire stage-packages verified.
- Confined HOME: journal lands in `~/snap/dayflow/common`, NOT
  `~/.local/share/dayflow`.
- `snap remove` leaves documented snapshot state (data retention copy).
- Session-bound behavior: closing the app stops capture; Background-
  portal autostart restores it at login; no unattributed consent prompt
  at install or login (capture_enabled gate).

**Verification:** real node run on cluster1 (can't be faked off-machine).

### U9. Per-distro verification gate + Fedora rpm

**Goal:** scripted smoke defines "fully built and tested"; Fedora rpm
verified on node3.

**Requirements:** R6, R7, R8. **Dependencies:** U7, U8.

**Files:** `scripts/test-standalone.sh` (install → launch → consent gate →
`status --json` frames>0 → tray check → uninstall), plus runbook notes for
cluster1–4 (Wayland + X11 sessions) and node3.

**Approach:** KTD8. The script drives the binary headlessly where possible
(`--version`, `status`, `doctor`, `timeline --json`) and leaves a human
checklist for consent/tray (GUI steps can't be fully scripted over SSH —
X-forwarding or local login on node needed). The tray check runs only
when an SNI host exists on the node (Ubuntu ships appindicator); on
tray-less sessions the expected result is the window-only path, asserted
as such rather than failed. Fedora node3: rpm install,
Wayland capture, window-only fallback documented (no SNI host on stock
Fedora GNOME).

**Test scenarios:** the script IS the test — pass on ≥1 cluster node per
session type (Wayland, X11) before "Ubuntu done" is claimed; node3 rpm pass
before "Fedora done".

**Verification:** recorded smoke output attached to the release notes.

---

## Verification Contract

- `cd engine && go build ./... && go vet ./... && go test ./...` — existing
  suite + new backend/keyring tests; `CGO_ENABLED=0` still produces a static
  `dayflow` binary.
- `cmake --build app/ui` + offscreen smoke script for the Qt host.
- `bash scripts/test-install.sh` — unchanged install path still green.
- `nfpm package` + `dpkg-deb --info` / `rpm -qpi` assertions in CI.
- `scripts/test-standalone.sh` on cluster nodes (Wayland + X11) — the
  per-distro gate.
- `snapcraft` build + `snap install --dangerous` smoke on cluster1.
- Live check on this machine: Omarchy plugin + grim path unchanged —
  `dayflow status` and the panel keep working throughout.

## Definition of Done

- Every unit's scenarios pass; the Ubuntu gate (deb + snap on cluster,
  Wayland + X11) is green and the Fedora rpm smoke on node3 is green.
- Omarchy install unregressed: `dayflow status` + panel live check clean.
- Snap Store submission filed (listing copy + justification); approval
  latency tracked, not claimed as code-complete.
- RHEL/Arch recorded as follow-on plans in ROADMAP (sequential, per R7).
- No abandoned-attempt code left in the diff; no stray generated trees
  staged (the `launch/` lesson — explicit `git add` only).

## Risks & Dependencies

- **Session-bound snap capture** — the in-app supervisor means snap
  capture stops when the app is fully closed; Background-portal
  autostart is unverified under strict confinement on Ubuntu's shipped
  xdp. If it fails, the snap lane is "capture while the app runs" —
  documented, or the lane defers.
- **Portal persist on shipped Ubuntu xdp** — if restore fails, consent
  re-prompts per login; that's a UX bug class, surface rather than ship
  silent re-prompt spam. Ubuntu floor pinned at 24.04 LTS (xdp-gnome
  with restoration).
- **Secret-portal secret stability** — if the host keys the master
  secret on an app identity that changes across snap refreshes (or the
  user revokes permissions), the snap's encrypted store file becomes
  undecryptable; recovery is a re-entry into the onboarding key step —
  verify stability across a `snap refresh` on cluster1.
- **Concurrent installs share the journal but not the restore token** —
  deb + plugin installs share `~/.config/dayflow`; a restore token is a
  single-use-ish bearer capability, so one install's daemon can consume
  or invalidate another's. Document precedence; the token lives per
  data-dir state, which the shared installs share anyway.
- **cgo build matrix** — dayflow-portal and dayflow-ui need Qt6 +
  libpipewire toolchains in CI (amd64 for v1; arm64 deferred per U7).
- **KWallet Secret Service opt-in** — KWallet's Secret Service interface
  is per-user opt-in on some Plasma releases and can raise a wallet
  unlock prompt on first access; `key set` on KDE targets is verified
  during the Fedora/later smokes, not assumed.
- **Snap Store human review** — screen recorders draw scrutiny; the privacy
  posture copy is a deliverable, not an afterthought.
- **Downgrade schema brick** — `openDB` hard-fails on newer schema; snap
  revert or older deb after update crash-loops capture. Policy: document
  no-downgrade support; doctor already detects it.
- **Concurrent installs** — snap sees its own journal (sandboxed HOME);
  deb + plugin share `~/.config/dayflow` — two UIs over one engine is fine;
  document unit-name ownership precedence.
- **External review latency** — store approval is days-weeks; it is a
  parallel track, explicitly not the gate for Fedora work.

## Sources & Research

- Grounding dossier: `/tmp/compound-engineering/ce-brainstorm/u4-standalone/grounding.md`
  (capture seams, install mechanics, keyring, UI coupling inventory).
- Prior-art implementations cribbed: `go2tv.app/screencast` (portal+
  PipeWire cgo dlopen), `marang/robotgo/screen/portal` (Go portal flow),
  `go-freedesktop/screencast` (pure-Go X11 wire), `zalando/go-keyring`
  (Secret Service via godbus), `rymdport/portal` (Secret portal).
- Mozilla bug 1726211 — `pw_context_connect_fd` on the portal-passed fd is
  the strict-confinement-viable path.
- Portal persist support: xdp-gnome 42.rc+, xdp-kde 5.25+, xdph 1.3.5+;
  xdp#2001 restore-token fix in current releases.
- Snap interface reality (2026): `desktop` plug already grants portal D-Bus;
  `pipewire`/`password-manager-service`/`screencast-legacy` plugs are
  review-gated and unneeded.
- Reversal note: prior plans ruled out standalone GUI as a plugin
  *replacement* (`docs/plans/2026-09-05-001-…:17,61`) — this keeps the
  plugin; additive not contradictory.
- Learnings: `docs/solutions/best-practices/restores-must-carry-forward-fixes-inside-reverted-files.md`
  (reused QML must come from the carried tree); `docs/research/audio-capture-spike.md`
  (consent-bar grammar, PipeWire verified on this machine);
  `docs/research/local-decisions-endpoint.md` (contract-exact shims).
