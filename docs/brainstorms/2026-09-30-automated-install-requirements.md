---
title: "Automated Install — Marketplace 'Manual Setup' → One-Click Bootstrap"
date: 2026-09-30
type: feat
status: requirements-ready
---

# Automated Install — Marketplace 'Manual Setup' → One-Click Bootstrap

## Summary

Close the gap between "plugin installed" and "Dayflow working." Today
`omarchy plugin add` git-clones the repo into `~/.config/omarchy/plugins/`
— the QML lands but the Go engine binary, systemd user units, and
provider config are all manual steps. This phase adds a canonical
`scripts/install.sh`, a thin in-shell bootstrap that runs it from the
panel, version-pinned release downloads with SHA-256 verification, an
uninstall path, an engine-update detection path, and a tag-triggered
release workflow so the assets exist automatically.

Roadmap item: *Marketplace "Manual setup" → fully automated install
(prebuilt binaries via release, `omak` install path).* — "`omak`" resolves
to the `omarchy plugin add` install path; no `omak` command exists.

## Problem Frame

The marketplace listing says "Manual setup" and means it: after
`omarchy plugin add`, a user must `install -Dm755` a downloaded binary,
run `dayflow install` for systemd units, then `dayflow setup` for a
provider. Every step is a terminal incantation; miss one and the widget
silently shows nothing (the `dayflow status --json` Process just fails to
start). There is also no release automation — v1.4.0 assets were uploaded
by hand — and no path for engine updates when the plugin is updated.

## Requirements

### R1 — Canonical `scripts/install.sh`

One script that does the whole engine install, runnable three ways:
inside a plugin checkout (in-shell bootstrap and `omarchy plugin add`
users), standalone via `curl | bash` against a release asset, and by the
dev flow (`DAYFLOW_BUILD=local` to `go install` the working tree instead
of downloading — keeps dev/testing honest).

- Resolves the version to fetch: `manifest.json` sibling when run from a
  plugin checkout (pinned — widget/engine always matched), else
  `--version X.Y.Z` arg, else latest release.
- Resolves arch: `uname -m` → `x86_64`→`amd64`, `aarch64`/`arm64`→`arm64`;
  fails loudly on anything else.
- Downloads `dayflow-<ver>-<arch>` + `SHA256SUMS` from
  `github.com/duketopceo/dayflow-linux/releases/download/v<ver>/`,
  verifies checksum before the binary ever reaches `~/.local/bin/`.
- `install -Dm755` into `~/.local/bin/dayflow`, then runs
  `dayflow install` (writes/enables systemd user units — no sudo).
- Idempotent: re-running on the same version is a fast no-op
  (version-string check before download); re-running on a new version
  replaces the binary and re-installs units.
- Never touches provider config — `dayflow setup` and consent stay with
  the user (Onboarding.qml already handles first-run config).
- Fails loudly: non-zero exit + one-line stderr diagnosis at each stage
  (no silent half-installs). Prints the one remaining step
  (`dayflow setup` / open the widget) on success.
- No sudo anywhere. Everything lives under `~/.local/bin` and
  `~/.config/systemd/user`.

### R2 — In-shell bootstrap

When `dayflow status --json` fails to spawn (binary absent) or reports a
version older than `manifest.json` (update available), the panel surfaces
it instead of silently showing nothing:

- **Missing binary**: Onboarding gains a step-0 "Install engine" state —
  one button runs `bash <plugin-dir>/scripts/install.sh` via Process,
  streams output, shows success/failure. On success proceeds into the
  existing wizard; on failure shows the stderr line + "run it yourself"
  fallback with the command.
- **Version mismatch** (`status` spawns but `engineVersion` <
  manifest version): dismissible banner in the panel — "Engine update
  available (1.4.0 → 1.5.0)" + Update button running install.sh +
  "not now" dismiss. Never auto-downloads in the background.
- Both paths reuse the same run-install.sh Process plumbing; the only
  difference is which UI surfaces the trigger.

### R3 — Release workflow

`.github/workflows/release.yml` on `v*` tag push: build static binaries
for `linux/amd64` + `linux/arm64` (`CGO_ENABLED=0`, `-trimpath`,
`-ldflags="-s -w -X main.version=<tag>"`), emit `SHA256SUMS`, upload
assets + `install.sh` to the release. Removes the manual upload step that
produced v1.4.0; makes checksums always present.

### R4 — Uninstall path

`scripts/uninstall.sh`: `dayflow uninstall` (stops/disables/removes the
user units — add the subcommand if absent), removes
`~/.local/bin/dayflow`, prints what remains (`~/.local/share/dayflow`
data + `~/.config/dayflow` config — kept intentionally, one line on how
to delete if wanted). Idempotent; safe to run when partially installed.

### R5 — Docs

- README: install section becomes one command (`omarchy plugin add …
  --enable` then click Install in the widget — or the curl line for
  non-Omarchy).
- `docs/install-linux.md`: scripted path as primary, manual steps demoted
  to a "manual install" appendix; uninstall section.
- `docs/publish/marketplace-submission.md` + `submission-body.md`:
  "Manual setup" → automated-install description; prerequisites none
  beyond network access for the download.

## Non-Goals

- Provider setup in the bootstrap — `dayflow setup`/consent stays in
  Onboarding.qml where it already lives.
- Auto-updating the engine in the background — updates are
  user-initiated clicks (R2).
- AUR/pacman packaging — the release-binary path covers the target
  audience; packaging is a separate track if it ever happens.
- Windows/macOS install — Linux only; macOS parity is a separate roadmap
  track.
- Minisign/cosign signing — SHA256SUMS-only this phase; signing adds key
  management for marginal gain at this audience size.

## Constraints / Existing Patterns

- No sudo; everything under `$HOME` (`.local/bin`, `.config/systemd/user`).
- The panel already runs `dayflow status --json` (Panel.qml:731,
  BarWidget.qml:69) — "binary missing" is a FailedToStart, not a parse
  error; check the existing failure surface before adding a new check.
- Onboarding.qml already gates on "unconfigured" status — the install
  step precedes it, doesn't replace it.
- Release assets follow the existing `dayflow-<ver>-<arch>` naming
  (v1.4.0: `dayflow-1.4.0-amd64`, `dayflow-1.4.0-arm64`) — keep it.
- Omarchy plugin dir: `~/.config/omarchy/plugins/io.github.duketopceo.dayflow/`
  — the bootstrap resolves its own path relative to the QML file, no
  hardcoded id assumptions beyond manifest.json lookup.
- Never modify `/usr/share/omarchy/` (package-owned).

## Open Questions Resolved

- **How automated?** Both: `install.sh` canonical + thin bootstrap that
  calls it — failure modes live in one script.
- **Which version?** Manifest-pinned when run from a plugin checkout.
- **How much automation?** Binary + systemd units; provider setup stays
  consented in Onboarding.
- **Integrity?** SHA-256 checksums via SHA256SUMS release asset.
- **Uninstall?** Stop units + remove binary; keep user data.
- **Updates?** Version-mismatch banner → user clicks → install.sh re-run.
- **Standalone?** Yes — install.sh is a release asset; curl-pipe works.
- **Release CI?** Yes — tag-triggered workflow owns binary+checksum+script
  assets.

## Risks

- **curl|bash trust model**: mitigated by checksum verify + pinned
  version + HTTPS; still documented as the trust boundary in docs.
- **Checksum asset race**: SHA256SUMS must be uploaded atomically with
  binaries (same release job) — install.sh fails closed if it's absent.
- **Offline/dead-endpoint install**: bootstrap surfaces a clean error
  line; no retry storms, no partial state (checksum gate before
  placement).
- **systemd user session absent** (headless/TTY-only install):
  `dayflow install` already handles this; install.sh surfaces its error
  verbatim rather than masking it.
