---
title: "Automated Install — install.sh + In-Shell Bootstrap + Release Workflow"
date: 2026-09-30
type: feat
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
origin: docs/brainstorms/2026-09-30-automated-install-requirements.md
---

# Automated Install — install.sh + In-Shell Bootstrap + Release Workflow

## Goal Capsule

**Objective:** After `omarchy plugin add`, a user gets a working Dayflow
with at most one button click — engine binary installed and verified,
capture units enabled — and non-Omarchy users get the same one-command
path. Plugin removal leaves user data intact; engine updates surface as
an actionable banner.

**Means:** a canonical `scripts/install.sh` (pinned manifest version,
SHA256SUMS-verified release download, `dayflow install`) invoked by a
thin in-shell bootstrap, plus a tag-triggered release workflow that
produces the assets it fetches (KTD1, KTD2, KTD4).

**Authority:** requirements and product decisions come from the origin
brainstorm; this plan owns how-level decisions only.

**Stop conditions:** any step that would touch `/usr/share/omarchy/`,
auto-download without a user click, or tear down user data on uninstall.

## Product Contract

### Summary

`omarchy plugin add` is a bare git clone — no install hooks exist. The
QML lands, the engine binary, systemd units, and provider config do not.
This plan ships the canonical installer, the panel bootstrap that calls
it when the binary is missing or stale, an uninstall path, and the
release automation that makes the downloads reproducible.

### Problem Frame

See origin. The marketplace listing's "Manual setup" label is accurate
today: `install -Dm755` + `dayflow install` + `dayflow setup` are all
terminal steps, and a missing binary surfaces as a silently empty panel
(`statusProc` has no FailedToStart handling).

### Requirements

- **R1 — Canonical `scripts/install.sh`**: manifest-pinned version when
  run from a plugin checkout, `--version X.Y.Z` override, latest-release
  fallback for standalone use; `uname -m` → amd64/arm64 map with loud
  failure otherwise; `dayflow-<ver>-<arch>` + `SHA256SUMS` from the
  `v<ver>` GitHub release; checksum verified before placement;
  `install -Dm755` into `~/.local/bin/`; `dayflow install` for units;
  idempotent (same-version re-run is a fast no-op, new version replaces);
  `DAYFLOW_BUILD=local` env to `go install` the working tree instead of
  downloading; never touches provider config; fails loudly per stage; no
  sudo.
- **R2 — In-shell bootstrap**: `statusProc` failure (binary absent) →
  "Install engine" surface preceding Onboarding, one click runs
  `bash <resolved>/scripts/install.sh` with streamed output, success
  continues into the existing wizard, failure shows the stderr line +
  copyable fallback command. Version mismatch (existing banner at
  `Panel.qml:1327`) gains an Update button reusing the same Process —
  never auto-downloads in the background.
- **R3 — Release workflow**: `.github/workflows/release.yml` on `v*` tag
  push → `CGO_ENABLED=0 go build` for linux/amd64 + linux/arm64,
  `SHA256SUMS`, `gh release upload` of binaries + sums + `install.sh`.
- **R4 — Uninstall path**: `scripts/uninstall.sh` → `dayflow uninstall`
  (already exists — `engine/install.go:228`) + remove
  `~/.local/bin/dayflow`; prints retained `~/.local/share/dayflow` +
  `~/.config/dayflow` with a one-line delete hint. Idempotent.
- **R5 — Docs**: README one-command install; `docs/install-linux.md`
  scripted-primary/manual-appendix; marketplace submission wording moves
  off "Manual setup".

### Scope Boundaries

Out: provider setup in bootstrap (Onboarding owns it), background
auto-update, AUR/pacman packaging, non-Linux install, binary signing
beyond SHA256SUMS.

### Key Decisions

- **Install.sh canonical + thin bootstrap** — one script owns all
  failure modes; QML only surfaces it. Governs R1, R2.
- **Manifest-pinned version** — widget/engine can never drift at install
  time. Governs R1, R2.
- **Binary + units automatically; provider stays consented** — capture
  works after one click; `dayflow setup`/consent stay in Onboarding.
  Governs R1, R2.
- **Update = user click** — mismatch banner is actionable but inert
  until clicked. Governs R2.
- **install.sh is a release asset** — the curl-pipe path for non-plugin
  users doubles as generic Linux install. Governs R1, R3.

## Planning Contract

### Key Technical Decisions

- **KTD1 — Single script, three entry points.** `scripts/install.sh` is
  the only artifact that knows how to fetch/verify/place the binary.
  Bootstrap invokes it; users can run or pipe it. Keep it `#!/bin/bash`
  + `set -euo pipefail` per repo convention (`scripts/stress.sh`,
  `scripts/adversarial.sh` shape).
- **KTD2 — Checksum gate before placement.** Download binary +
  `SHA256SUMS` to a temp dir, `sha256sum -c` (filtered to the fetched
  asset name), and only then `install` into `~/.local/bin/`. Missing or
  mismatched sums abort before any file moves — fail closed.
- **KTD3 — Version comes from manifest, verified against tag.** The
  engine `const version` (`engine/main.go:17`) stays the binary's source
  of truth — the release workflow *asserts* tag == const version == asset
  names rather than introducing `-ldflags -X` injection (a second truth
  that can drift silently). install.sh reads `manifest.json` via `jq`
  (guaranteed on Omarchy — `omarchy-plugin-add` uses it) with a
  `grep`-only fallback for standalone installs.
- **KTD4 — Bootstrap triggers on Process failure + version skew.** A new
  `engineMissing` flag set by `statusProc` failure drives an
  "Install engine" step ahead of Onboarding (`configured` defaults true
  today, so a dead spawn currently looks like an empty configured
  panel). The skew banner at `Panel.qml:1327` becomes actionable.
  Script path resolves via `Qt.resolvedUrl("scripts/install.sh")` —
  correct from both the installed plugin dir and a dev checkout.
- **KTD5 — Everything under `$HOME`, no sudo.** Binary →
  `~/.local/bin/`, units → `~/.config/systemd/user/` via the existing
  `dayflow install`/`uninstall` (`engine/install.go`). Data dir
  `~/.local/share/dayflow` is never touched by install or uninstall.

### High-Level Technical Design

```
scripts/install.sh          canonical installer (R1)
  ├─ version: manifest.json > --version > latest release
  ├─ arch:    uname -m -> amd64|arm64
  ├─ fetch:   dayflow-<v>-<arch> + SHA256SUMS  (or DAYFLOW_BUILD=local)
  ├─ verify:  sha256sum -c (fail closed)
  ├─ place:   install -Dm755 ~/.local/bin/dayflow
  └─ units:   ~/.local/bin/dayflow install

scripts/uninstall.sh        dayflow uninstall + rm binary, keep data (R4)

.github/workflows/release.yml   v* tag -> build x2 arch -> SHA256SUMS
                                -> gh release upload (assets + install.sh)

Panel.qml
  statusProc ──fail──> engineMissing=true
                        └─> InstallPrompt surface:
                             "Install engine" [button]
                              └─> installProc: bash <resolved>/scripts/install.sh
                                   ├─ ok  -> statusProc.running = true (re-detect)
                                   └─ err -> stderr tail + fallback command
  skew banner (1327) ──> same installProc via Update button
```

### Assumptions

- `curl`, `sha256sum`, `install` are present on the target (Omarchy/Arch
  base — true by inspection).
- GitHub release asset naming `dayflow-<ver>-<arch>` (established at
  v1.4.0) stays stable — the release workflow emits exactly these names.
- A `v*` tag is the release trigger the repo wants (matches existing
  tag history `v1.4.0`).

## Implementation Units

### U1. Canonical `scripts/install.sh`

**Goal:** One script that installs the engine end-to-end from any of the
three entry points. **Requirements:** R1.

**Files:** `scripts/install.sh` (new), `scripts/test-install.sh` (new —
follows `scripts/adversarial.sh` conventions: isolated env, per-case
verdicts).

**Approach:** Bash + `set -euo pipefail`. Order of operations: resolve
version (manifest.json sibling when the script's own dir contains one —
`jq -r .version` with `grep`-fallback; `--version` flag; else GitHub
latest-release API) → arch map → if `DAYFLOW_BUILD=local`, `go install`
repo root instead of downloading → else fetch both assets into
`mktemp -d`, `sha256sum -c` filtered to the asset, abort on mismatch →
`install -Dm755` → run `dayflow install` → print remaining step
(`dayflow setup` / open widget). Idempotency: compare `dayflow
--version` output to target before downloading; identical → skip fetch,
still re-run `dayflow install` (cheap, repairs half-state). Each stage
writes a one-line stderr diagnosis before exiting non-zero. Trap cleans
the temp dir.

**Test scenarios** (in `scripts/test-install.sh`, all offline via a
fake release dir served from `file://` or a stub `curl` on PATH):
- Happy path against fake assets: binary lands, checksum verified,
  `dayflow install` invoked (stub binary records it).
- Checksum mismatch → aborts before placement; `~/.local/bin/dayflow`
  absent.
- Missing SHA256SUMS → aborts (fail closed).
- Unknown arch (`uname` stub) → loud exit.
- `--version` override picks the named asset; manifest pin takes
  precedence over latest when run from a checkout-shaped dir.
- Same-version re-run → no download (stub curl counts calls), units
  reinstalled.
- New-version re-run → binary replaced.
- `DAYFLOW_BUILD=local` skips curl entirely.

**Verification:** `bash scripts/test-install.sh` green; `bash -n` clean.

### U2. Release workflow

**Goal:** Tag push produces verified assets; no manual uploads.
**Requirements:** R3.

**Files:** `.github/workflows/release.yml` (new).

**Approach:** Trigger `push: tags: ["v*"]`. Job on `ubuntu-latest`:
checkout; assert `grep 'const version ='` in `engine/main.go` equals the
tag's `v`-stripped value (fail the release on drift — KTD3); `go build`
the engine twice with `GOOS=linux GOARCH=amd64|arm64 CGO_ENABLED=0
-trimpath -ldflags="-s -w"`; emit `SHA256SUMS`; `gh release upload`
binaries + sums + `scripts/install.sh`. `permissions: contents: write`.

**Test scenarios:** workflow YAML parses (`actionlint` if available,
else `python3 -c 'yaml.safe_load'`); version-assert step rejects a
mismatched tag (local dry-run of the grep assertion).

**Verification:** CI green on the PR branch (workflow itself only fires
on tags); validate the assert logic locally.

### U3. Missing-binary bootstrap surface

**Goal:** A panel launched with no engine offers install instead of a
silent dead panel. **Requirements:** R2 (missing-binary half).

**Files:** `Panel.qml` (statusProc failure handling + InstallPrompt UI +
installProc), possibly a small `InstallPrompt.qml` if the surface grows
past ~60 lines (match repo granularity: Onboarding.qml is its own file).

**Approach:** `statusProc` gains failure detection (`onExited` non-zero
with empty stdout, plus Quickshell's process-error signal — check the
pattern used by other dayflow Procs) → `dayflow.engineMissing = true`.
When set, the content Loader shows the install surface (ordered ahead of
the `!configured` Onboarding gate) instead of tabs: headline, one-line
explanation, **Install engine** button → `installProc` running `bash`
with the path from `Qt.resolvedUrl("scripts/install.sh")` stripped of
`file://`. Stream stderr/stdout into a status line; non-zero exit shows
the last line + a copyable `bash ~/.config/omarchy/plugins/io.github.duketopceo.dayflow/scripts/install.sh`
fallback. Zero exit → `engineMissing=false`, re-run `statusProc`. Guard
against double-clicks (`!installProc.running`).

**Test scenarios:** `qmltestrunner` existing suite stays green; manual
verification — temporarily move `~/.local/bin/dayflow`, open panel, see
the prompt, click Install, panel recovers (script short-circuits on
PATH or installs from local build). Screenshot per repo convention.

**Verification:** `qmltestrunner` suite green; manual before/after.

### U4. Actionable version-skew update

**Goal:** The existing "engine ≠ panel" text becomes an update path.
**Requirements:** R2 (mismatch half).

**Files:** `Panel.qml` (extends the banner at ~line 1327).

**Approach:** Replace text-only notice with a row: existing text +
**Update** button + **Dismiss** affordance (session-scoped `var
skewDismissed`). Update runs the same `installProc` from U3 — the
script's manifest pin resolves the target version, no arguments needed.
While running, disable + show "updating…"; on success re-run
`statusProc` (new version lands in `engineVersion`, banner clears
naturally); on failure show stderr tail and keep the banner.

**Test scenarios:** manual — install older binary, verify banner +
button → update → banner clears; dismiss hides for the session.

**Verification:** same suite + manual pass as U3.

### U5. `scripts/uninstall.sh`

**Goal:** Clean reversible teardown. **Requirements:** R4.

**Files:** `scripts/uninstall.sh` (new); extend `scripts/test-install.sh`
with uninstall cases.

**Approach:** `~/.local/bin/dayflow uninstall` if the binary exists
(skips gracefully when absent); `rm -f ~/.local/bin/dayflow`; print
retained paths (`~/.local/share/dayflow`, `~/.config/dayflow`) + one-line
delete hint. Non-fatal at every stage, zero exit even mid-state.

**Test scenarios:** units present → removed (stub `systemctl` on PATH
records calls) + binary gone; no binary → still exits 0; data dir
untouched.

**Verification:** `bash scripts/test-install.sh` green.

### U6. Docs

**Goal:** Listing and docs describe the automated path. **Requirements:**
R5.

**Files:** `README.md`, `docs/install-linux.md`,
`docs/publish/marketplace-submission.md`, `docs/publish/submission-body.md`.

**Approach:** README install becomes the one-liner (plugin add → click
Install; or `curl -fsSL …/install.sh | bash` standalone). install-linux.md:
scripted path primary, existing manual steps move to an appendix named
"Manual install". Marketplace docs: replace "Manual setup" with the
automated flow + trust boundary note (release binary, SHA256-verified,
user-consented provider setup). Keep the draft/owner-gated framing —
submission is still not happening this phase.

**Verification:** docs render; commands in them match what
install.sh actually does.

## Verification Contract

- `cd engine && go test -count=1` — full suite green (engine untouched
  except possibly nothing; still the gate).
- `bash scripts/test-install.sh` — all cases green (new).
- `bash -n scripts/install.sh scripts/uninstall.sh` — syntax clean.
- `shellcheck scripts/install.sh scripts/uninstall.sh` if installed.
- `qmltestrunner` — existing QML suite green.
- `actionlint .github/workflows/release.yml` if installed, else YAML
  parse check.
- Manual: missing-binary → prompt → install → panel healthy; skew
  banner → update → clears; uninstall → units gone, binary gone, data
  kept.

## Definition of Done

- All six units implemented, tests green per Verification Contract.
- `omarchy plugin add` → panel Install click → `dayflow doctor` healthy:
  the marketplace "Manual setup" claim is now false and docs no longer
  say it.
- No abandoned code paths (e.g., an alternative downloader tried and
  dropped) left in the diff.
- Marketplace submission docs updated in wording only — still
  owner-gated, not submitted.
