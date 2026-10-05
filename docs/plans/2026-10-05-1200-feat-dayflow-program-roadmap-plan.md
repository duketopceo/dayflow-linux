---
title: Dayflow for Linux Program Roadmap - Plan
type: feat
date: 2026-10-05
origin: ROADMAP.md
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-plan-bootstrap
execution: code
---

# Dayflow for Linux Program Roadmap - Plan

## Goal Capsule

- **Objective:** A Linux user can install Dayflow in one command, trust that it records and summarizes their day with data leaving the machine only where they opted in, and read, correct, and export that day from the bar, the Full View, the terminal, or an agent, on a footprint small enough to leave running all day.
- **Means:** one program plan that sequences every workstream as stable units (U1-U24) in Now, Next, and Later bands. The UI workstream is delegated to its child plan, `docs/plans/2026-10-02-2315-feat-ribbon-log-ui-redesign-plan.md`; this plan does not restate its units. (KTD1)
- **Authority hierarchy:** the child Ribbon Log plan's R-IDs win on UI behavior. `PRIVACY.md` and the consent invariants in `CONTRIBUTING.md` win on anything that leaves the machine. This plan wins on cross-workstream order. Where this plan states a status, the cited evidence wins over memory.
- **Stop conditions:**
  - Any unit would add an egress path that is not default-off and named in `PRIVACY.md`. Stop.
  - Any unit would edit a system path outside the user's home (for example `/usr/share/omarchy/`). Stop.
  - A UI unit would land without the preview harness from Ribbon U1. Hold it (KTD3).
  - A unit needs a paid model call to verify. Stop and ask.
- **Execution profile:** one PR per unit, in band order. No commits or pushes without the user's explicit request. Status is derived from git and CI, never edited into the plan.
- **Who finishes:** an implementer (`ce-work` or a human) executes units; the repo owner approves merges. Contributors such as nixfred open PRs against the same units.

---

## Product Contract

### Summary

Dayflow for Linux is a port of the macOS Dayflow work journal: a Go engine in `engine/` (daemon, CLI, TUI, stdio MCP server, SQLite) plus a Quickshell QML panel for Omarchy at the repo root. Version 1.5.0 is tagged and master is ahead of it; v1.6.0 is pending (`ROADMAP.md`). Most engine capability has shipped. The open program is: the UI redesign, closing documented gaps in capture portability, privacy copy, MCP parity, and CI coverage, defining a measurable performance budget, and tightening the contributor workflow after the reverted Pulse dashboard.

### Problem Frame

Three things shape the program. First, the UI is being rebuilt once, after a first attempt failed: PR #58 (Pulse-style dashboard, by contributor nixfred) merged on 2026-10-04 and was reverted by #60 on 2026-10-05, and PR #61 then refreshed the Ribbon Log plan to a single tab set. Second, the engine grew quickly (83 Go files in `engine/`, 40 of them tests, 14 MCP tools) and several surfaces drifted apart: the immediate-completions journal exists in the CLI (`dayflow complete`, `dayflow completions`) and README but not in the MCP tool list or `PRIVACY.md`. Third, the "light" promise (README: "~25 MB RAM, sub-1% CPU") has no measuring script in the repo.

### Requirements

- R1. The UI workstream is delivered by the child Ribbon Log plan and is not duplicated here. This plan carries its sequencing and its blockers only.
- R2. Every engine capability has a status in this plan with cited evidence (PR number, file, or doc). Unverified claims are labeled as such.
- R3. Capture works out of the box on wlroots compositors and has a documented, doctor-verified path for GNOME, KDE, and X11 via `capture_command`.
- R4. Storage is bounded by configured caps and retention, and deletes keep the search index consistent.
- R5. Summaries and judgments can run with zero egress when the user configures local endpoints, and the default cloud paths stay named in `PRIVACY.md`.
- R6. Every surface that stores or transmits user content is described in `PRIVACY.md`, defaults off when it egresses, and has a delete path.
- R7. The CLI, TUI, and MCP server expose the same journal facts; gaps are listed and closed or explicitly declined.
- R8. A performance budget is stated as measurable targets with a script that reports against them.
- R9. CI covers every check a contributor is told to run, and UI changes are gated by a reproducible preview.
- R10. Docs have one entry point per audience and name where plans and status live.

### Scope Boundaries

- Not in scope: Windows capture (parked in `ROADMAP.md` until asked), re-implementing upstream's hosted Flow product (resolved dead-end), and any macOS-side change. Upstream (dayflow.so) owns the brand and product direction.
- Not in scope: new model providers beyond the existing OpenRouter, custom, local, MCP, and CLI kinds.

#### Deferred to Follow-Up Work

- Per-agent-conversation playback ranges (upstream `AgentPlaybackView` parity): `ROADMAP.md` item 2; Later band (U22).
- GitHub issues #2 (journal beta), #3 (summary quality rating), #4 (streaming chat): Later band (U23), need their own plans.

### Dependencies / Assumptions

- Assumption: the repo default branch is `master` and CI runs on pushes and PRs to `main` and `master` (`.github/workflows/ci.yml`).
- Assumption: PR #61 (open, mergeable, review decision `CHANGES_REQUESTED` at the time of writing) is the source of the refreshed Ribbon Log plan. This plan's branch includes #61's commits; whichever merges first, the other must not be merged blindly (see Sources).

### Outstanding Questions

#### Deferred to Implementation

- [Needs research] Which non-Hyprland compositors can supply the active window and output name without a new dependency (U3 spike).
- [Needs research] Real idle and active RAM and CPU of `dayflow daemon` on the reference machine (U17 measures it).
- The child plan's own open questions Q1 (brand ownership), Q2, and Q3 (bar recording color) remain the user's to answer; they block only Ribbon U5, U17, and U4.

### Sources

- `ROADMAP.md`, `README.md`, `DESIGN.md`, `CONTRIBUTING.md`, `PRIVACY.md`, `docs/install-linux.md`, `docs/agent-contract.md`, `docs/immediate-completions.md`, `docs/maintenance.md`, `docs/research/local-decisions-endpoint.md`.
- Plans: `docs/plans/2026-10-02-2315-feat-ribbon-log-ui-redesign-plan.md` (child), `docs/plans/2026-10-03-001-feat-full-local-model-support-plan.md`, `docs/plans/2026-09-30-1500-feat-automated-install-plan.md`, `docs/plans/2026-09-28-001-feat-completion-and-maintenance-plan.md`, and the four 2026-10-04 `fix-*` plans.
- GitHub: PRs #1-#61 (`gh pr list --state all`), open issues #2, #3, #4, open PRs #59 and #61. There is no `AGENTS.md` in the repo.

---

## Planning Contract

### Key Technical Decisions

- KTD1. One parent plan, one child plan. The Ribbon Log plan keeps its own U1-U22 IDs; this plan uses U1-U24 for program units and cites child units as "Ribbon U<N>" to avoid collision. Rationale: the child plan is already refreshed and under review in #61; renumbering it would conflict.
- KTD2. Bands, not dates. Now = work that is unblocked and either in review or the next merge; Next = depends on Now; Later = needs its own plan or a user trigger. Rationale: the repo's plans carry no mutable status (progress derives from git), and `ROADMAP.md` already uses Shipped/Next.
- KTD3. UI work is gated by the sandbox preview harness (Ribbon U1) and the lightweight rule (lazy per-tab load, no LLM on tab open). Rationale: the Pulse dashboard (#58) landed and was reverted within a day; the existing `tests/ui/` checks (`verify-fit.py`, `verify-completions.py`) are not run in CI.
- KTD4. Consent defaults are load-bearing (`CONTRIBUTING.md`). Any new egress ships default-off with copy naming every destination. `agent_recaps`, `agent_completions`, and `knowledge_sync` already default false.
- KTD5. Performance claims need a script. No target in this plan is treated as fact until U17 reports it.
- KTD6. Land docs and CI units before large UI units so contributors (including nixfred, who authored 7 commits and most of the 2026-10-04 PRs) have the checks first.

### Technical Design

Dependency flow between bands (directional):

```
Now:   U1 (Ribbon P0) ----> Next: U2 (Ribbon P1) ----> U3 (Ribbon P2)
       U5 CI gaps  -------> U17 perf script ---------> U18 perf gate
       U8 privacy copy ---> U9 completions MCP parity
       U13 triage #59 ----> (folds into Ribbon U8/U10 or closes)
Later: U22 playback ranges, U23 issues #2/#3/#4, U24 Windows capture
```

### Risks

| Risk | Mitigation |
|---|---|
| A second UI landing is reverted like #58 | KTD3 gates; one PR per child unit; Ribbon U19 deletes duplicates only after replacements ship |
| #59 (explicit pop-out) conflicts with Ribbon U8/U10 popup and tab-host work | U13 decides fold-in or close before Ribbon U8 starts |
| Non-wlroots users cannot capture | U3/U4 document and doctor-check `capture_command` backends |
| Privacy docs lag features (completions journal) | U8 lands before more features |
| README performance claim is unmeasured | U17/U18 |

---

## Program State (verified 2026-10-05)

| Area | Status | Evidence |
|---|---|---|
| Release | v1.5.0 tagged; master ahead; v1.6.0 pending | `ROADMAP.md` header |
| UI | Pulse dashboard merged (#58) then reverted (#60); Ribbon plan refreshed in open PR #61 | `gh pr list`, git log |
| Capture | `grim` backend; `capture_command` for others; PNG accepted; quiet pause with no Wayland session | `engine/capture.go` (`grimArgv`, `resolveCaptureCommand`), #36, #27 |
| Focus/output | `hyprctl` only; no sway/niri/other query found in `engine/*.go` | `engine/capture.go` hyprctl calls; search for `swaymsg`/`niri` returned no hits |
| Storage | `retention_days`, `max_frames_mb` (default 20480), `max_db_mb` (default 10240), `keep_frames` default false; FTS follows deletes; backup/restore/verify | `engine/config.go`, `runRetention`, `PRIVACY.md`, `CONTRIBUTING.md` |
| Summaries/judgments | OpenRouter default; local via `api_base_url`/`task_provider`; `decisions_url` chat transport (#56); batch recaps (#50); keyring auto-heal (#41); retry (#26) | README config table, `docs/research/local-decisions-endpoint.md` |
| Agent completions | `dayflow complete` hook + opt-in watcher, default off (#57) | `docs/immediate-completions.md`, README `agent_completions` row |
| MCP | stdio only, no auth/remote, 14 tools, `--read-only`; 8 MiB request cap | `engine/mcp.go`, `docs/agent-contract.md` |
| TUI/CLI | `dayflow tui`; wide CLI; `completions` and `playback` commands | `engine/main.go` command switch |
| Install | `scripts/install.sh` checksum-verified, pins engine to manifest version; Omarchy marketplace listing | `ROADMAP.md`, #39, `docs/install-linux.md` |
| CI | build, vet, gofmt, test, `scripts/test-install.sh`; release workflow on tag | `.github/workflows/ci.yml`, `release.yml` |

---

## Implementation Units

Statuses use: Shipped, Partial, Not started, In review. Evidence is cited in each unit.

### Unit Index

| ID | Band | Workstream | Title | Status | Depends on |
|---|---|---|---|---|---|
| U1 | Now | UI | Ribbon Log P0: fixture day, tokens, purge literals, bar states, app mark, notifications (Ribbon U1-U6) | Not started (plan in review, #61) | none |
| U2 | Next | UI | Ribbon Log P1: shared components, lazy tab host, Today/Week/Ask/Settings tabs, Ctrl+K search, Week-glance popup, duplicate deletion (Ribbon U7, U21, U18, U11, U20, U22, U9, U12, U10, U19) | Not started | U1, U13 |
| U3 | Next | UI | Ribbon Log P2: Replay, Agents polish, onboarding overlay, TUI ribbon, marketing assets (Ribbon U13-U17) | Not started | U2 |
| U4 | Now | Engine | Capture portability: doctor-verified `capture_command` recipes, compositor focus spike | Partial | none |
| U5 | Now | CI | Close CI coverage gaps | Partial | none |
| U6 | Next | Engine | Storage and retention verification under caps | Shipped, verification gap | U17 |
| U7 | Now | Engine | Local-vs-remote model matrix and zero-egress check | Shipped, doc/verification gap | none |
| U8 | Now | Privacy | Privacy copy parity (completions journal, frames at rest) | Partial | none |
| U9 | Next | Engine | MCP and TUI/CLI parity for completions | Not started | U8 |
| U10 | Next | Privacy | Screenshot handling review: ignore-by-class on non-Hyprland, at-rest frames | Partial | U4 |
| U11 | Next | Packaging | v1.6.0 release cut and three-place version check | Partial | U5 |
| U12 | Later | Packaging | Non-Omarchy panel story (engine-only vs Quickshell elsewhere) | Documented, no panel | U4 |
| U13 | Now | Contributor | Triage open PR #59 and the #61 review | In review | none |
| U14 | Now | Docs | ROADMAP and docs entry points; plan index; add AGENTS-style guidance | Partial | none |
| U15 | Next | Contributor | Contributor workflow after the #58 revert | Not started | U5 |
| U16 | Next | Docs | CONTRIBUTING sync with CI | Partial | U5 |
| U17 | Next | Performance | Measure RAM/CPU of daemon and panel; state targets | Not started | none |
| U18 | Later | Performance | Performance gate in CI or release checklist | Not started | U17 |
| U19 | Later | Engine | Agent-completion hook installer | Not started | U9 |
| U20 | Later | Privacy | Redaction roadmap beyond best-effort text filter | Not started | U10 |
| U21 | Later | Testing | QML/UI verification in CI | Not started | U1, U5 |
| U22 | Later | Engine/UI | Per-agent-conversation playback ranges | Not started | U3 |
| U23 | Later | Product | Issues #2, #3, #4 each get a plan | Not started | U3 |
| U24 | Later | Engine | Windows capture adapter research | Parked | none |

### U1. Ribbon Log P0 (child plan units Ribbon U1-U6)

- **Goal:** theming, glyphs, fixture day, bar states, app mark, notifications, so everything later renders on any Omarchy theme.
- **Requirements:** R1, R9.
- **Dependencies:** none. Blockers: child Q1 and Q3 hold Ribbon U5 and U4 respectively.
- **Child plan order (summary):** P0 identity and theming (Ribbon U1-U6); P1 shared components and lazy tab host (U7, U21), Today/Week/Ask/Settings tabs (U18, U11, U20, U22, U9), Ctrl+K search (U12), Week-glance popup (U10), delete duplicate Today/Week code (U19); P2 Replay, Agents polish, onboarding, TUI ribbon, assets (U13-U17). Next up in the child plan: its U1.
- **Files:** per child plan (`Tokens.qml`, `Glyphs.js`, `BarWidget.qml`, `engine/devseed.go`, `scripts/preview/`, `scripts/lint-ui.sh`). Not created yet in this tree.
- **Status and evidence:** Not started. Child plan refreshed by #61; the refreshed `ROADMAP.md` line 0 says "Next up: U1".
- **Test scenarios:** per child plan U1 (fixture day renders in two themes, one light; lint gate returns no literal colors).
- **Verification:** child plan's Verification Contract; this plan adds that `go build ./...`, `go vet`, `gofmt`, `go test ./...` and `scripts/test-install.sh` stay green on every PR.

### U2. Ribbon Log P1

- **Goal:** one tab set built once: Today, Week (Context flow folded in), Standup, Ask, Agents, Replay, Settings; Ctrl+K overlay; Week-glance popup; lazy per-tab load; no LLM call on tab open; duplicate code deleted (about 1,500 lines).
- **Requirements:** R1, R8, R9.
- **Dependencies:** U1, U13 (so #59's pop-out decision is made before the tab host).
- **Status and evidence:** Not started; settled in the child plan Refresh section and `ROADMAP.md` item 0.
- **Test scenarios:** per child plan units; plus a fresh-profile open of each tab issues zero provider calls (check `api_calls` table delta via `dayflow usage`).
- **Verification:** preview harness screenshots in two themes; U17 numbers before and after.

### U3. Ribbon Log P2

- **Goal:** Replay frame scrubber, Agents pane on tokens, 3-step onboarding overlay, TUI and CLI ribbon, social card and marketplace previews.
- **Requirements:** R1, R7.
- **Dependencies:** U2.
- **Status and evidence:** Not started. Child plan Ribbon U13-U17.
- **Test scenarios:** per child plan.
- **Verification:** child plan; `dayflow tui` renders the ribbon on the fixture day.

### U4. Capture portability

- **Goal:** non-Hyprland users get a tested capture path and honest doctor output.
- **Requirements:** R3.
- **Dependencies:** none.
- **Files:** `engine/capture.go`, `engine/setup.go` (doctor), `docs/install-linux.md`, tests in `engine/capture_test.go`, `engine/setup_test.go`.
- **Approach:** `docs/install-linux.md` already tabulates backends (wlroots `grim`; GNOME/KDE/X11 via `capture_command` to stdout). Evidence of the gap: active-window and output selection call `hyprctl` only (`engine/capture.go`), `ignore --active` is Hyprland-only per the doc, and `PassEnvironment` in `engine/install.go` passes `XDG_CURRENT_DESKTOP` to the unit but the engine does not branch on it. Work: (1) doctor reports which backend and which focus source is active; (2) spike whether sway/niri focus queries are worth adding or `ignore_apps` is documented as Hyprland-only; (3) fixture-backed tests for a stdout `capture_command` returning JPEG and PNG (PNG accepted since #36).
- **Status and evidence:** Partial. Shipped: #27 (quiet pause with no session), #32 (start from `graphical-session.target`), #36 (PNG frames).
- **Execution note:** characterization tests first for current `resolveCaptureCommand` behavior.
- **Test scenarios:** `capture_command` absent and `grim` missing yields the actionable error; stdout PNG accepted; `output: auto` without `hyprctl` falls back to composite capture; focus failure counter stops spawning `hyprctl` after repeated failures.
- **Verification:** `go test ./...`; `dayflow doctor` output on a machine without `hyprctl`.

### U5. Close CI coverage gaps

- **Goal:** CI runs everything the repo asks contributors to run.
- **Requirements:** R9.
- **Dependencies:** none.
- **Files:** `.github/workflows/ci.yml`, `scripts/smoke-install.sh`, `tests/ui/`.
- **Approach:** CI today: `go build ./...`, `go vet ./...`, gofmt check, `go test ./...`, `bash scripts/test-install.sh`. Gaps verified: `scripts/smoke-install.sh` (named in `CONTRIBUTING.md`) and `scripts/adversarial.sh` and `scripts/stress.sh` are not in CI; QML checks in `tests/ui/` are not in CI; `CONTRIBUTING.md` says `go test -count=1 .` while CI runs `go test ./...`. Decide per script whether it is CI-safe (offline, no Wayland) and add or document.
- **Status and evidence:** Partial (`ci.yml`).
- **Test scenarios:** a PR that breaks gofmt fails CI; a PR that breaks the installer battery fails CI.
- **Verification:** workflow runs green on a docs-only PR and red on an injected failure in a throwaway branch.

### U6. Storage and retention verification

- **Goal:** prove caps and retention hold under load.
- **Requirements:** R4.
- **Dependencies:** U17 (shared harness).
- **Files:** `engine/capture.go` (`runRetention`, `pruneOldestBlocks`), `engine/search.go`, tests in `engine/main_test.go`, `engine/capture_test.go`.
- **Status and evidence:** Shipped with tests: `TestRetention`, `TestStorageCapRunsWithRetentionOff`, `TestReconcileKeepsQuarantineWhenRetentionOff`. Gap: no long-run soak result recorded.
- **Test scenarios:** frames over `max_frames_mb` pruned oldest-first while terminal blocks are kept; deleting blocks removes FTS rows and `block_edits`; completions journal respects retention days (documented in `docs/immediate-completions.md`).
- **Verification:** `go test ./...`; soak result recorded under U17.

### U7. Local-vs-remote model matrix

- **Goal:** one documented table of every call site (vision, summary, chat, recap, briefing, decisions) and where it can run, with a check that a fully local config makes zero outbound calls.
- **Requirements:** R5.
- **Dependencies:** none.
- **Files:** `README.md` ("Fully local" section), `PRIVACY.md`, `engine/provider.go`, `engine/decisions.go`.
- **Status and evidence:** Shipped: per-call-site routing (#50), `decisions_url` chat transport (#56), plan `2026-10-03-001-feat-full-local-model-support-plan.md`. The shim research found a decisions-API shim contract-incompatible (`docs/research/local-decisions-endpoint.md`). Gap: no test asserting zero egress for a local-only config.
- **Test scenarios:** with all providers pointed at a loopback stub and `knowledge_sync` off, a day of fixture work produces requests only to the stub; non-OpenRouter endpoints get no OpenRouter attribution headers and no Authorization unless configured.
- **Verification:** `go test ./...`; stub server request log.

### U8. Privacy copy parity

- **Goal:** `PRIVACY.md` describes every stored and transmitted surface.
- **Requirements:** R6.
- **Dependencies:** none.
- **Files:** `PRIVACY.md`, `README.md`.
- **Status and evidence:** Partial. #54 added knowledge sync. Gap verified by search: `PRIVACY.md` does not mention the agent completions journal (stored locally, redacted for home paths and credential patterns, replies to 64 KiB, retention applies, per `docs/immediate-completions.md`) or the `agent_completions` watcher. The sensitive-content filter in `PRIVACY.md` is described as best-effort.
- **Test scenarios:** none (docs); review against `docs/immediate-completions.md` claims.
- **Verification:** each `PRIVACY.md` claim traced to a doc or file; `scrub` delete path stated for completions if it exists, else flagged.

### U9. MCP and TUI/CLI parity for completions

- **Goal:** agents and the TUI can read recorded completions.
- **Requirements:** R7.
- **Dependencies:** U8.
- **Files:** `engine/mcp.go`, `engine/mcp_test.go`, `engine/completions.go`, `engine/tui.go`.
- **Status and evidence:** Not started. `dayflow completions` exists in `engine/main.go`; the 14-tool list in `engine/mcp.go` has no completions tool; `docs/agent-contract.md` documents the stdio contract.
- **Test scenarios:** `tools/list` includes the new read-only tool and it remains available under `--read-only`; oversized request still returns `-32600` and the server keeps serving; empty journal returns an empty array.
- **Verification:** `go test ./...`; `dayflow mcp` handshake manually.

### U10. Screenshot handling review

- **Goal:** state exactly where frames live, when they are deleted, and what controls exclude content.
- **Requirements:** R6, R3.
- **Dependencies:** U4.
- **Files:** `engine/capture.go`, `engine/config.go`, `PRIVACY.md`.
- **Status and evidence:** Partial. `keep_frames` defaults false; `auto_pause_locked` default true; `ignore_apps` is by Hyprland window class; `filter_inappropriate` defaults true and applies to edit text (`engine/edit.go`). Gap: ignore and focus depend on `hyprctl` (see U4); frames sent to the chosen provider are the one default egress.
- **Test scenarios:** ignored class produces no frame; locked session produces no frame; a frame is removed after its block is summarized when `keep_frames` is false.
- **Verification:** `go test ./...`; copy review in `PRIVACY.md`.

### U11. v1.6.0 release cut

- **Goal:** tag v1.6.0 when ready.
- **Requirements:** R9, R10.
- **Dependencies:** U5.
- **Files:** `engine/main.go` (`version`), `manifest.json`, `Panel.qml` (`pluginVersion`), `.github/workflows/release.yml`.
- **Status and evidence:** Partial. `ROADMAP.md` item 3: everything except playback ranges is shipped. `engine/version_test.go` pins the three versions equal (`CONTRIBUTING.md`). Release workflow builds amd64 and arm64 with `SHA256SUMS` (commit `25f6061`).
- **Test scenarios:** version pin test; installer battery against the new version string.
- **Verification:** CI green; tag triggers release workflow.

### U12. Non-Omarchy panel story

- **Goal:** decide and document whether a Quickshell panel is supported outside Omarchy.
- **Requirements:** R3.
- **Dependencies:** U4.
- **Status and evidence:** `docs/install-linux.md` states the engine runs standalone (CLI, TUI, MCP); the panel is an Omarchy plugin (marketplace id `io.github.duketopceo.dayflow`).
- **Test scenarios:** none until a decision is made.
- **Verification:** decision recorded in `docs/install-linux.md`.

### U13. Triage open PRs #59 and #61

- **Goal:** settle the two open PRs before the tab host work starts.
- **Requirements:** R1, R9.
- **Dependencies:** none.
- **Status and evidence:** In review. #61 is open with review decision `CHANGES_REQUESTED`. #59 (nixfred: explicit pop-out and "Back to plugin") is open; it touches the same popup and app-window flow that Ribbon U8 and U10 rebuild.
- **Test scenarios:** none (decision unit).
- **Verification:** each PR merged, folded into a Ribbon unit, or closed with a comment.

### U14. Docs entry points and plan index

- **Goal:** one place says what is shipped, next, and where plans live.
- **Requirements:** R10.
- **Dependencies:** none.
- **Files:** `ROADMAP.md`, `README.md`, `CONTRIBUTING.md`, `docs/plans/`.
- **Status and evidence:** Partial. `ROADMAP.md` lists the Ribbon plan first and this program plan above it (this PR). The repo has 21 plans in `docs/plans/` and no `AGENTS.md`; no index.
- **Test scenarios:** none (docs); links resolve.
- **Verification:** link check by `ls` of each cited path.

### U15. Contributor workflow after the #58 revert

- **Goal:** UI contributions land with preview evidence and one-unit scope.
- **Requirements:** R9.
- **Dependencies:** U5.
- **Files:** `CONTRIBUTING.md`, PR template (new, `.github/pull_request_template.md`).
- **Status and evidence:** Not started. #58 merged and was reverted by #60 in about nine hours (timestamps in `gh pr list`). Contributors with merged work: nixfred (7 commits), CymaticStatic (#32), Ben Governale.
- **Test scenarios:** none (process).
- **Verification:** template shown on a new PR.

### U16. CONTRIBUTING sync with CI

- **Goal:** the commands in `CONTRIBUTING.md` equal CI's.
- **Requirements:** R9, R10.
- **Dependencies:** U5.
- **Status and evidence:** Partial; gap described in U5.
- **Test scenarios:** none (docs).
- **Verification:** run the documented commands from a clean checkout.

### U17. Performance measurement

- **Goal:** replace the README claim with numbers.
- **Requirements:** R8.
- **Dependencies:** none.
- **Files:** `scripts/perf.sh` (new), `README.md`.
- **Approach:** the README says "~25 MB RAM, sub-1% CPU"; no script in `scripts/` measures it (`scripts/stress.sh` exercises CLI behavior against a sandbox). Measure resident memory and CPU of `dayflow daemon` over a capture hour on the fixture or live session, and of the Quickshell panel with each tab opened. Proposed targets, not facts: daemon at or under the README claim; no tab open triggers a provider call; hidden tabs unloaded (the child plan's lightweight rule).
- **Status and evidence:** Not started.
- **Test scenarios:** script exits non-zero when a target is exceeded; reports idle vs capture-tick values.
- **Verification:** results appended to the README claim.

### U18. Performance gate

- **Goal:** keep U17's targets from regressing.
- **Requirements:** R8.
- **Dependencies:** U17.
- **Status and evidence:** Not started.
- **Test scenarios:** injected regression trips the gate.
- **Verification:** release checklist or scheduled job runs `scripts/perf.sh`.

### U19. Agent-completion hook installer

- **Goal:** one command adds the Stop hook for Claude and Codex without hand-editing JSON.
- **Requirements:** R7.
- **Dependencies:** U9.
- **Status and evidence:** Not started. `docs/immediate-completions.md` currently instructs users to append the hook by hand and notes Codex needs hook trust review.
- **Test scenarios:** existing hook entries are preserved; running twice is idempotent; failure emits valid empty hook JSON (as `scripts/completion-hook.sh` does).
- **Verification:** `go test ./...` with a temp HOME.

### U20. Redaction beyond best-effort

- **Goal:** decide whether frame-level or text redaction beyond `ignore_apps` and the text filter is warranted.
- **Requirements:** R6.
- **Dependencies:** U10.
- **Status and evidence:** Not started; `PRIVACY.md` calls the filter best-effort.
- **Test scenarios:** deferred until the decision.
- **Verification:** decision recorded in `PRIVACY.md`.

### U21. QML and UI verification in CI

- **Goal:** UI regressions are caught before merge.
- **Requirements:** R9.
- **Dependencies:** U1, U5.
- **Status and evidence:** Not started. `tests/ui/` has `fit.qml`, `fit-completions.qml`, `verify-fit.py`, `verify-completions.py`; none run in CI.
- **Test scenarios:** the lint gate (Ribbon U1) fails on a literal color.
- **Verification:** CI job running headless where feasible; otherwise documented manual gate.

### U22. Per-agent-conversation playback ranges

- **Goal:** upstream `AgentPlaybackView` parity.
- **Dependencies:** U3.
- **Status and evidence:** Not started; stats half shipped in #44 (`ROADMAP.md` item 2).
- **Test scenarios:** deferred to its own plan.
- **Verification:** own plan.

### U23. Open issues #2, #3, #4

- **Goal:** each enhancement gets a plan before work.
- **Dependencies:** U3.
- **Status and evidence:** Open since 2026-09-11 (journal beta, summary quality rating, streaming chat in panel).
- **Test scenarios:** deferred to their plans.
- **Verification:** own plans.

### U24. Windows capture adapter research

- **Goal:** none until asked.
- **Dependencies:** none.
- **Status and evidence:** Parked in `ROADMAP.md` item 4.
- **Test scenarios:** none.
- **Verification:** none.

---

## Verification Contract

Run from the repo root; these mirror `.github/workflows/ci.yml`.

- `cd engine && go build ./...`
- `cd engine && go vet ./...`
- `cd engine && test -z "$(gofmt -l .)"`
- `cd engine && go test ./...`
- `bash scripts/test-install.sh`

Per-unit gates: UI units add preview-harness screenshots (Ribbon U1) in a dark and a light theme; privacy units add a `PRIVACY.md` claim trace; performance units report U17 numbers.

## Definition of Done

- Global: every PR keeps the five CI commands green; no new egress without default-off and `PRIVACY.md` copy; version triple stays equal; `ROADMAP.md` lists this plan first and the Ribbon Log plan as its UI child.
- Now band done: U1 P0 merged, U4, U5, U7, U8, U13, U14 closed.
- Next band done: U2, U3, U6, U9, U10, U11, U15, U16, U17 closed.
- Later band: each unit has its own plan before work starts.

## Appendix

### Relationship to PR #61

This plan's branch is built on #61's head (`docs/ribbon-log-plan-refresh`). If #61 merges first, this PR's diff reduces to the new plan plus the `ROADMAP.md` edit. If this PR merges first, #61 is superseded.
