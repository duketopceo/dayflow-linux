---
title: Agents Pane Day Briefing - Plan
type: feat
date: 2026-09-30
topic: agents-pane-briefing
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Agents Pane Day Briefing - Plan

## Goal Capsule

- **Objective:** the Agents pane renders a day-level briefing of the user's agent activity — workstreams grouping the day's sessions, each thread carrying a status and a condensed outcome narrative — for every installed agent source, with the same local-first consent boundaries as today's recaps.
- **Product authority:** user-selected "Agents-pane upgrade" as the roadmap phase, then directed the shape: upstream macOS Agents model, all installed harnesses, no Flow, no Fable/headless-agent sweep, adapted single-screen UI.
- **Open blockers:** none.

## Product Contract

### Summary

Port the upstream Dayflow macOS Agents tab's product shape — an LLM-assisted daily briefing organized as workstreams → threads → condensed turns — onto all five Linux agent sources. The engine derives structure deterministically; the configured chat model writes only prose under the existing `agent_recaps` consent. The UI is one scrollable briefing view, not upstream's three-stage navigation.

### Problem Frame

The current `AgentsPane.qml` is a flat chronological list of sessions with one-line recaps. It answers "what sessions ran" but not "what did my agents actually do today." Upstream solves this with a generated briefing the user reviews top-down: named workstreams with accomplishment bullets, then per-thread statuses (blocked / review ready / in progress / completed) that surface what needs the human's attention — the "am I the bottleneck" question. That briefing is the part of upstream worth porting; its navigation chrome and its generation mechanism are not.

<!-- ce-section: work-relationships -->
### How This Work Fits Together

This plan covers the Agents-pane upgrade — the roadmap item the user selected from "next phase." The broader roadmap breakdown is current understanding, not committed scope:

- **Marketplace automated install** — Can proceed independently of this plan; separate workstream.
- **Flow parity tracking** — Resolved by inspection this session: upstream Flow is a hosted web app behind a waitlist with no local component. Removed from roadmap candidacy rather than deferred.
- **Windows capture adapter** — Parked until explicitly requested.
- **AgentPlayback-style visualizations** (working/waiting arcs, 24h swimlane, cost heat) — Shares the adapter data this plan uses; a later plan may build on the derived thread ranges.

### Key Decisions

- **Briefing model over timeline dial** (session-settled: user-directed — "find it in the upstream mac app, agents and flow": upstream's actual Agents tab is a workstream briefing; the earlier vinyl-dial research described AgentPlayback, a separate embedded web app). Governs R1, R2, R3.
- **All installed sources, not just Claude Code + Codex** (session-settled: user-directed — "for all the harnesses i have installed first"). Upstream reads two stores; the port reads every adapter we ship. Governs R1.
- **Hybrid generation — engine derives structure, model writes prose** (session-settled: user-approved — chosen over upstream's headless-agent sweep, which ships whole transcripts to a CLI provider; user rejected Fable). Governs R4, R5.
- **Adapted single-screen UI** (session-settled: user-directed — chosen over the full three-stage port). Governs R6, R7.
- **Flow excluded** (session-settled: user-directed — "no flow"; upstream Flow is a hosted web app behind a waitlist, rendered in a WebView — there is no local component to port).

### Requirements

**Data and derivation**

- R1. The briefing covers every adapter the install supports — Claude Code, Codex, OpenCode, Devin, Cursor — reusing the existing `sessionTurn` extraction; no new transcript-reading machinery is added.
- R2. Sessions are grouped into a small number of workstreams by project and purpose, each with a human-readable name, a one-line summary, and a short list of accomplishment bullets.
- R3. Each thread carries one status — `blocked`, `reviewReady`, `inProgress`, `completed` — derived deterministically (e.g., recently-modified → in progress; ended on an unanswered user question or agent error → blocked; finished with an unacted deliverable → review ready).
- R4. Each thread carries a condensed turn list (roughly a dozen alternating user/agent turns at most, outcome-phrased), optional per-turn highlights (`keyDecision`, `keyInfo`, `readyForReview`), and an optional artifact name/path on producing turns.

**Generation, consent, and egress**

- R5. Model involvement stays inside the existing `agent_recaps` consent: transcripts are excerpted and scrubbed under the current bounds before anything reaches a provider; the briefing's structure (grouping, statuses, turn skeleton) must render without any model call, with model-written prose degrading to deterministic fallback text.
- R6. Briefings are persisted per day and refreshed on demand from the pane; day navigation covers every day with a persisted briefing.

**UI**

- R7. The pane renders one scrollable view: a day header with per-status totals, then workstream sections (name, summary, bullets, status counts) containing thread cards.
- R8. A thread card shows source badge, project, status chip, latest outcome, the condensed turn list with highlight pills, and artifacts; it is the detail level — no separate navigation stage.
- R9. Existing pane affordances survive: per-source drift/unavailable notes, recap opt-in hint, busy and error states.
- R10. The briefing is engine-emitted data (JSON over the existing CLI contract), so non-panel surfaces can consume it; the QML pane is a renderer, not a second implementation.

### Acceptance Examples

- AE1. **Covers R5.** No provider configured or `agent_recaps` off → workstream sections, statuses, and deterministic turn skeletons still render; prose fields show fallback text; no network call is made.
- AE2. **Covers R3.** A session file modified within the last 15 minutes → `inProgress`; a session whose last turn is an unanswered user question → `blocked`.
- AE3. **Covers R1, R9.** Cursor's store is missing → a drift note appears and Claude Code/Codex/OpenCode/Devin threads still render normally.
- AE4. **Covers R6.** A day with no agent sessions → empty state; navigating to a day with a persisted briefing shows it without regenerating.

### Scope Boundaries

**Deferred for later**

- Working-vs-waiting time segmentation, 24-hour swimlane, token/cost estimates, month heat calendar — AgentPlayback features; valuable but a separate visualization workstream.
- Card-style switcher (messenger/transcript/brief/milestones), blob-scatter overview, tick scrubber — upstream navigation chrome, cut by the single-screen decision.
- Auto-refresh of the briefing from the daily summarize pass — refresh is pane-initiated this phase.

**Outside this product's identity**

- Flow tab — hosted SaaS upstream; nothing local to port.
- Headless agent-CLI summarization sweeps — full-transcript egress conflicts with the bounded-excerpt consent model.
- The AgentPlayback vinyl dial — a separate embedded app upstream, not the product shape chosen here.

### Dependencies / Assumptions

- All five adapters already emit `sessionTurn` values with role + timestamp; status derivation needs no new store reads.
- `agent_recaps` opt-in, provider plumbing, scrubbing, and drift reporting already exist (`engine/agents*.go`, `docs/agent-contract.md`).

### Outstanding Questions

None — the brainstorm's deferred items were resolved by KTD3–KTD6: storage is an `agent_briefings` SQLite table (not files), the CLI surface is `dayflow briefing`, status detection is deterministic-first with per-source predicates, and turn condensation is deterministic with model polish layered on top.

### Sources / Research

- Upstream implementation, cloned and read this session: `AgentsView.swift` (3-stage flow), `AgentsModels.swift` (workstream→thread→turn schema, status + highlight enums, lenient decoding), `AgentsRecapPrompt.swift` (headless sweep prompt, status definitions, ≤12-turn condensation rule), `AgentsRecapStore.swift` (user-initiated runs only, per-day JSON files), `AgentsOverviewView.swift` / `AgentsBriefingView.swift` / `AgentsCardsView.swift` / `AgentsScrubberView.swift` (stages + scrubber), `AgentPlaybackView.swift` (dial is an embedded WebView app), `FlowView.swift` / `FlowWaitlistView.swift` (hosted web app + waitlist).
- Earlier inference-based research (superseded where it conflicts): `docs/research/macos-agents-section.md`.
- Current Linux surfaces: `AgentsPane.qml`, `engine/agents.go`, `engine/agents_recap.go`, `engine/agents_{opencode,devin,cursor}.go`.

## Planning Contract

- **Approach:** two-layer generator. Layer 1 is a deterministic briefing builder — scan sessions, pull each session's normalized turn list, condense turns, derive status, group into workstreams — producing a complete renderable payload with zero model calls. Layer 2 is a single batched model pass per day that rewrites the prose fields (workstream names/summaries/bullets, thread titles, latest outcomes, turn highlights) over the deterministic skeleton, gated on `agent_recaps` and degrading cleanly when it never runs.
- **Storage:** one new SQLite table `agent_briefings` in the existing WAL store (`~/.local/share/dayflow/`), keyed by day — not upstream's loose JSON files, matching how `agent_recaps` already caches.
- **CLI surface:** new `dayflow briefing [YYYY-MM-DD] [--json] [--refresh]` subcommand; `dayflow agents` stays byte-for-byte compatible for existing consumers.
- **UI surface:** `AgentsPane.qml` rewritten as the single-scroll briefing view; `FullView.qml`'s loader process switches to the `briefing` subcommand.

### Key Technical Decisions

- **KTD1 — Reuse the `agentSource` seam, add `Turns(sess) []sessionTurn`.** DB adapters already build `[]sessionTurn` per session for scan/excerpt (each `*Turns` constructor over a message query); JSONL adapters need per-line timestamp extraction alongside the existing `lineRoleText`. No new store-reading machinery — satisfies R1 through the seam that already exists.
- **KTD2 — One batched model call per day, not per session.** The briefing payload is small (bounded turn skeletons, titles, projects — scrubbed and capped like the recap excerpt path). One JSON-shaped chat call writes all prose fields; Jev groundedness check optional. Avoids the per-session judge+chat fan-out that recaps pay, and keeps a whole day's cost under one recap's.
- **KTD3 — Status derivation is deterministic-first.** `inProgress` = session end within ~15 min of now (viewed day = today); `blocked` = last usable turn is user-authored (unanswered ask); `reviewReady` = assistant-ended, quiet ≥15 min, and model marks a deliverable — else `completed`. Model may only *upgrade* completed→reviewReady, never invent blocked.
- **KTD4 — Turn condensation merges, never drops the edge.** Consecutive same-role turns merge into one; cap ~12 by merging the middle run while always preserving the first user turn and final turn. Deterministic condensation is the rendered text when no model runs.
- **KTD5 — Briefing cache is fingerprint-invalidated like recaps.** `agent_briefings(day PK, fingerprint, payload_json, mode, created_at)`; fingerprint folds each session's `File`+`recapFingerprint`, so any transcript growth invalidates the cached briefing on next view.
- **KTD6 — `dayflow briefing` is a new subcommand; `agents` is untouched.** The pane migrates to `briefing --json`; MCP and other consumers of `agents` keep their contract.

### Implementation Units

**IU1 — Turn extraction seam** (engine)
- Files: `engine/agents.go` (`agentSource` gains `Turns`; `jsonlSource` gains a `lineTurn` per-line decoder carrying role+text+ts, with `lineRoleText` becoming a wrapper), `engine/agents_opencode.go`, `engine/agents_devin.go`, `engine/agents_cursor.go` (`Turns` impls reusing the message fetch their `Excerpt` already runs).
- Tests: `engine/agents_test.go` — per-source Turns extraction against existing fixtures; ts ordering preserved; usable-user predicate respected.

**IU2 — Deterministic briefing builder** (engine)
- Files: new `engine/agents_briefing.go` — schema types (`agentBriefing`/`briefingWorkstream`/`briefingThread`/`briefingTurn` mirroring upstream's JSON shape, source names ours), `condenseTurns`, `deriveStatus`, `groupWorkstreams` (project-keyed, singletons folded to "Miscellaneous" like upstream), briefing fingerprint.
- Tests: `engine/agents_briefing_test.go` — status truth table (recent-end, unanswered-user, quiet-assistant-end, spanning-midnight), condensation caps + edge preservation, grouping incl. Miscellaneous fold, fingerprint stability/drift.

**IU3 — Model polish pass + cache** (engine)
- Files: `engine/agents_briefing.go` (same file — polish function + `agent_briefings` table init alongside `agent_recaps` in `engine/store.go`). Payload = scrubbed, bounded skeleton (per thread: project, title, condensed turns ≤~120 chars each, total cap in the same spirit as the 2KB excerpt); response = lenient JSON `{workstreams:[{name,summary,bullets,threads:[{title,latestOutcome,turn_highlights,artifact?}]}]}`; artifact paths accepted only when they literally appear in the source turn text (substring guard against fabrication).
- Tests: `engine/agents_briefing_test.go` — payload byte cap honored, scrub applied (home path → ~, secret patterns), malformed/partial model JSON falls back to deterministic fields, cache hit/miss/invalidation, `agent_recaps` off → mode `fallback` and zero provider calls.

**IU4 — CLI + contract** (engine)
- Files: `engine/main.go` (`briefing` case: `dayflow briefing [YYYY-MM-DD] [--json] [--refresh]` — emits `{day, generated_at, mode, sources:[scan statuses], workstreams}`; `--refresh` forces regeneration past cache), `docs/agent-contract.md` (briefing payload documented), `README.md` CLI table row.
- Tests: extend existing CLI/doctor coverage pattern; `--refresh` bypass works; `agents` output unchanged (regression).

**IU5 — Agents pane rewrite** (QML)
- Files: `AgentsPane.qml` (rewrite — day header with per-status count legend; scrollable workstream sections each carrying name/summary/accomplishment bullets/status-count chips; thread cards with source badge, project, status chip, latest outcome, condensed turn list with highlight pills, artifact link when present; preserved: DayNavRow, BusyBar, error line, drift notes, recaps-off hint, empty state; new refresh control calling `briefing --refresh`), `FullView.qml` (agentsProc command → `dayflow briefing <day> --json`; `agentSessions`/`agentSources` state replaced by briefing payload state; refresh run path).
- Wireframe: section-per-workstream scroll, status palette adapted to existing theme (`pane.dayflow.*`), highlight pills as small tinted labels.
- Tests: none automated (no headless Quickshell harness) — manual verification checklist in Verification Contract.

### Verification Contract

- `go build ./engine && go test ./engine` green; `go vet` clean.
- `dayflow briefing --json` on a day with real sessions produces valid briefing JSON; `dayflow briefing --json` on an empty day produces `{workstreams: []}` + statuses.
- Consent: `agent_recaps=false` → `mode:"fallback"`, zero provider calls (assert via `llm_calls`/`api_calls` tables staying flat across a briefing run).
- Pane: manual check — workstreams render, status chips colored per status, turns list capped, refresh regenerates, day nav switches briefings, drift notes still surface.

### Risks

- **Per-source turn fidelity** (Cursor bubbles, Devin ATIF): thinner roles may make `blocked` detection noisy on some sources — mitigated by the "last usable turn is user" rule being per-source-predicate-aware already.
- **Model JSON leniency**: mirrored from upstream (decodeIfPresent-style fallbacks) — partial JSON degrades per-field, never sinks the briefing.
- **Scan cost**: Turns extraction reads full transcripts per session (same I/O the excerpt path already pays); multi-MB days stay bounded by the existing per-file scan.

### Definition of Done

- R1–R10 satisfied: briefing covers all five sources, workstream grouping + statuses + condensed turns, model prose under `agent_recaps` consent with full offline fallback, persisted per-day cache, single-scroll pane with day nav/refresh/drift notes, engine-emitted JSON consumable outside QML.
- All four Acceptance Examples verified.
- Suite green; `agents` subcommand output unchanged; docs updated (`agent-contract.md`, README CLI row).
