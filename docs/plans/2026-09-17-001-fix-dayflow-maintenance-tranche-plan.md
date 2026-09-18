---
title: "fix: Dayflow v1.1.0 maintenance tranche — agent-contract hardening and Panel decomposition"
type: fix
date: 2026-09-17
status: draft
origin: docs/plans/2026-09-16-0128-feat-dayflow-1-0-release-plan.md (post-release review findings)
---

## Summary

Post-release maintenance tranche for Dayflow v1.1.0. Closes the residual findings from two code-review rounds that were deliberately deferred as design-sized: the `Panel.qml` god object, transcript day-bucketing, `dayflow log` injection, the MCP request-line limit, `get_usage` detail, and the remaining agent-contract gaps. No new features.

## Problem Frame

v1.1.0 shipped with two review rounds applied (commits `a8ffcb0`, `2af21bb`). The review's remaining actionable findings are individually small but collectively represent the maintenance debt the project agreed to carry: an agent-facing contract with three sharp edges (silent MCP death on oversized requests, day-mis-bucketed agent sessions, injectable log lines), a reporting tool that under-reports, and a 3,104-line QML file that keeps absorbing features.

## Requirements

- R1: `dayflow mcp` must never terminate silently on client input — oversized or malformed request lines produce a visible error and the server keeps serving. (Review residual)
- R2: Agent sessions are attributed to a day by message timestamps, not file mtime. A session active during day D appears in D's listing. (Review residual)
- R3: `dayflow log <text>` cannot forge additional log lines — control characters in argv text are neutralized before append. (Review residual)
- R4: `get_usage` reports per-task and per-model/provider breakdowns from both `llm_calls` and `api_calls` ledgers. (Review #32)
- R5: `Panel.qml` is decomposed along its existing `Component` seams (`todayTab`, `standupTab`, `weekTab`) into sibling `.qml` files, matching the FullView pane-extraction pattern shipped in `2af21bb`. (Review #23)

## Key Technical Decisions

- **MCP framing: line-oriented `bufio.Reader`, not `json.Decoder`.** The transport is NDJSON; a decoder cannot resync after a malformed value. `ReadBytes('\n')` preserves the per-line resync property the scanner had while removing its size cap. Reject lines over a documented cap (8 MB) with a JSON-RPC `-32600` error and continue — the server stays alive and the failure is observable. Rationale: the current failure mode (silent `sc.Err()` exit) is worse than any limit choice; error-loudly is the contract.
- **Session bucketing: overlap filter on `sess.Start`/`sess.End`.** A session is "in" day D when `[Start, End]` intersects the day's `[s, e)` range — spanning sessions appear on both days they touched, which is the truthful report. File prefilter relaxes to `mtime >= s` only (the `mtime < e` upper bound is what drops midnight-spanning files entirely). Message timestamps are already collected by `trackRange` — no new parsing needed.
- **Log sanitization: normalize on write, not on read.** Strip/escape `\n`, `\r`, and other control characters in `appendLog`'s argv path. One-line writes stay one-line; readers (QML tail, `get_log`) need no changes.
- **Panel split: `Loader` + `source:` with `host` property.** Mirror the FullView pattern exactly — each extracted tab receives `host` via `onLoaded` and dereferences `host.*` for shared state/procs. No new indirection layer; the proc wiring stays in `Panel.qml` root.

## Implementation Units

### U1. MCP request framing — survive oversized input

**Goal:** `dayflow mcp` never dies silently; oversized lines get a JSON-RPC error, malformed lines are skipped, service continues.

**Requirements:** R1
**Dependencies:** none
**Files:** `engine/mcp.go`, `engine/mcp_test.go` (create if absent), `docs/agent-contract.md`

**Approach:** Replace `bufio.Scanner` + 1MB `sc.Buffer` at `mcp.go:412` with a `bufio.Reader` loop. `ReadBytes('\n')` alone is unbounded — it buffers the whole line before returning — so bound the accumulation: use `ReadSlice` (returns `ErrBufferFull` fragments) and append until newline; once accumulated bytes exceed the documented cap, emit `mcpErr(null, -32600, "request too large")`, discard through the next newline, and continue. Empty lines → skip; unparseable → skip as today; unparseable lines get no response (id unknowable). Keep the cap as a named const, documented in the contract.

**Patterns to follow:** existing `mcpRespond`/`mcpErr` helpers; the `sc.Err()` skip pattern in `engine/agents.go:95` for "truncated → don't trust".

**Test scenarios:**
- Happy: `initialize` + `tools/list` + `tools/call` sequence over the new reader works identically to today.
- Edge: request line larger than the cap (test may use a small test-only cap) → single JSON-RPC error response, subsequent valid request still answered.
- Edge: blank lines and a non-JSON line interleaved with valid requests → all valid requests answered.
- Error: unparseable JSON line → no response emitted, loop continues.

**Verification:** new tests green; manual smoke — pipe an over-cap `tools/call` line and confirm the server responds with a JSON-RPC error and answers the next request.

---

### U2. Transcript day-bucketing by message timestamps

**Goal:** `agentSessionsForDay` (and `get_agent_sessions`) attribute sessions by when they were active, not file mtime.

**Requirements:** R2
**Dependencies:** none
**Files:** `engine/agents.go`, `engine/agents_test.go`

**Approach:** Relax `jsonlFiles` prefilter to `mtime >= s` (drop the `< e` bound — a file last written after midnight can still contain same-day messages). After `scanJSONL` produces a session, keep it when `sess.Start < e && sess.End >= s` (day overlap). Sessions already carry `Start`/`End` unix timestamps via `trackRange`; sessions with `Start == 0` are already skipped. Note the prefilter change widens the file scan — bounded by total transcript-file count, fine in practice.

**Patterns to follow:** existing `trackRange` semantics; `agents_test.go` fixture style (temp dirs via `DAYFLOW_CLAUDE_DIR`/`DAYFLOW_CODEX_DIR`).

**Test scenarios:**
- Session with messages both sides of midnight appears in both days' listings.
- Session entirely within day D appears only in D.
- File with `mtime` in D+1 containing a session fully inside D is still listed in D (regression for the `mtime < e` drop).
- File with `mtime` before D containing no in-range messages is excluded.
- Error path unchanged: truncated session (scan error) still skipped.

**Verification:** `go test ./engine` green; spot-check `dayflow agents <yesterday>` shows a known midnight-spanning session on both days.

---

### U3. `dayflow log` write-path sanitization

**Goal:** argv text cannot inject forged log lines.

**Requirements:** R3
**Dependencies:** none
**Files:** `engine/main.go` (log dispatch), `engine/debug.go` (`appendLog`), `engine/debug_test.go` or `main_test.go`

**Approach:** In the write path (`dayflow log <text...>`), sanitize the joined message before `appendLog`: replace `\n`/`\r` with spaces (or `␊`-style visible escapes — pick strip-to-space for simplicity), strip other ASCII control chars, and cap message length (e.g. 500 chars) to match the audit-log nature of the feature. Read path untouched.

**Patterns to follow:** `truncTitle` rune-safety pattern in `engine/agents.go` — sanitize on runes, not bytes.

**Test scenarios:**
- `dayflow log "line1\nline2"` produces exactly one appended line.
- Message with `\r\n` and a NUL byte → single line, control chars absent.
- `dayflow log --limit 3` still reads (regression for the dispatch fix in `2af21bb`).
- Write then `get_log` MCP read returns the sanitized line.

**Verification:** tests green; manual `dayflow log $'a\nb'` then `dayflow log --limit 2` shows one `ui:` line.

---

### U4. `get_usage` task/model breakdown

**Goal:** the MCP `get_usage` tool (and `dayflow usage` CLI for parity) report per-task and per-model/provider aggregation across both ledgers.

**Requirements:** R4
**Dependencies:** none
**Files:** `engine/mcp.go`, `engine/main.go`, `engine/mcp_test.go` or `main_test.go`, `docs/agent-contract.md`

**Approach:** Extend the two existing aggregate queries into grouped breakdowns: `llm_calls` grouped by `task` (column exists at `store.go:127`) and `api_calls` grouped by `model`. Keep the current top-level totals unchanged (backward compatible) and add a `breakdown` object: `{"by_task": {chat: {...}, review: {...}}, "by_model": {...}}`. Update the tool description and contract doc.

**Patterns to follow:** existing grouped queries in `engine/store.go` (`GROUP BY app` at line 325); the two-ledger merge shape already in `mcp.go:229`.

**Test scenarios:**
- Fixture rows across two tasks and two models → breakdown keys match, totals still equal sum of parts.
- Empty ledgers → breakdown objects are empty maps, not null.
- Error: same-day-only fixture doesn't leak into totals.

**Verification:** tests green; MCP smoke — `tools/call get_usage` returns `breakdown.by_task` matching known chat/review calls.

---

### U5. `Panel.qml` tab extraction

**Goal:** extract `todayTab`, `standupTab`, `weekTab` inline `Component`s into `TodayTab.qml`, `StandupTab.qml`, `WeekTab.qml`, cutting `Panel.qml` from ~3,104 to ~1,400 lines.

**Requirements:** R5
**Dependencies:** none (orthogonal to U1–U4)
**Files:** `Panel.qml`, `TodayTab.qml`, `StandupTab.qml`, `WeekTab.qml` (new)

**Approach:** Identical mechanics to the FullView split in `2af21bb`: each `Component { id: ... }` body moves to a file; the `Loader`s switch to `source:` with `active` gating where the current code lazily instantiates; each extracted file declares `property var host: null` and the loader assigns `item.host = root` in `onLoaded`. Sweep extracted bodies for `dayflow.`/`root.` references → `host.`. Keep `standupDay`/`draftField`/`sectionList` sub-components inside their owning tab files (they're only used there). Processes, state, and functions stay in `Panel.qml` root — the tabs render and call `host.*`.

**Patterns to follow:** `FullView.qml` + `TodayPane.qml`/`WeekPane.qml` split — same `host` injection, same lazy `Loader` gating, same file naming.

**Test scenarios:**
- `Test expectation: none` for Go — QML has no unit harness; verification is the syntax validator plus live panel exercise.

**Verification:** all new + changed QML files pass the syntax validation used in `2af21bb` (only expected `qs.*` import warnings); deploy to `~/.config/omarchy/plugins/io.github.duketopceo.dayflow/`, open each tab, confirm timeline/week/standup render and edit flows still work; rescan shell log for dayflow errors.

---

## Scope Boundaries

- No new features, no provider integrations, no macOS parity work.
- Release ops (pushing `2af21bb`, updating PR #20, tagging) are out — separate ship track.

### Deferred to Follow-Up Work

- **`typesafe/jev-latest` (TypeSafe Jev) calibrated classification** — decision-only model; interesting future use is confidence-scored category/productivity classification and sensitive-content gating before the generative call. Not routable via OpenRouter today (verified 2026-09-17: absent from `/api/v1/models`, `400 not a valid model ID` on an authenticated request). Revisit if it lands in the catalog.
- `--read-only` MCP open on a crashed-WAL database may still fail — needs a reproduction before fixing.
- Deeper `Panel.qml` proc-wiring refactor (moving process dispatch out of root) — deferred; the extraction in U5 is the safe mechanical pass.

## Risks & Dependencies

- **U5 regression risk** is the largest: 1,700 lines of QML moving files. Mitigated by doing the move in the exact pattern already proven on FullView, and validating + deploying live before commit.
- **U2 changes reported history**: sessions spanning midnight will now appear on both days — `agents`/`get_agent_sessions` output changes for multi-day sessions. Correct by definition, but worth noting in the commit message.
- **U1 cap choice** (8 MB) is arbitrary-but-documented; `chat` payloads embed journal context, so headroom matters.

## Sources & Research

- Code-review findings: `/tmp/compound-engineering/ce-code-review/20260916-220336-a9137b64/` (review rounds 1–2; applied fixes in `a8ffcb0`, `2af21bb`)
- Jev model status verified live 2026-09-17: TypeSafe early-access API only (`api.typesafe.ai/v1/systemone`); absent from OpenRouter catalog; authenticated routing attempt returned `400 not a valid model ID`.
- Inline research this session: `mcp.go:412` scanner cap, `agents.go:55` mtime prefilter + `trackRange`, `store.go:127` `llm_calls.task`, `Panel.qml` component map (lines 943/1457/1906).
