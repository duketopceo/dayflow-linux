---
title: "feat: v1.2 — agent recaps, goal streaks, WoW trends, marketplace publish"
date: 2026-09-20
type: feat
depth: deep
---

# feat: v1.2 — agent recaps, goal streaks, WoW trends, marketplace publish

## Summary

Dayflow v1.1.0 shipped with Jev calibrated judgments merged. The next phase
deepens three half-built surfaces and prepares the Omarchy marketplace
submission: agent-session **recaps** get real generated summaries instead of
first-message titles, **goals** get streak tracking so the unused feature
becomes a habit loop, and **weekly** gets true week-over-week trend deltas
instead of a single "biggest improvement" line. Publish prep (preview, README,
submission draft) ships in the same phase; the submission issue itself stays
owner-gated.

## Problem Frame

Each lane already has scaffolding but stops short of the thing users feel:

- `engine/agents.go` walks Claude Code / Codex JSONL transcripts and lists
  sessions with title = first user message + message count. That answers
  "a session happened" but not "what did it accomplish". No persistence, no
  judgment, no prose recap.
- `engine/goals.go` + `day_goals` + Panel.qml goal field implement
  set/complete for a single daily goal. The table has zero rows — with no
  streak or history feedback there is no reason to keep using it.
- `engine/weekly.go` computes `previousWeekInsights` but only surfaces
  `biggestImprovement` as one highlight string. Week-over-week comparison
  per category/app is invisible.

## Assumptions (pipeline-settled bets)

- Recaps send bounded transcript excerpts to the configured OpenRouter chat
  model — same egress class as block summarization; `readOnly` MCP paths and
  `DisableJudges`-equivalent gating still apply to any Jev judgments.
- "Agent-session recaps" means AI-generated per-session summaries (accomplished
  what, outcome, notable files/decisions), not just better titles.
- Streaks count consecutive days with `completed=1`, ending today or yesterday
  (today's goal can be incomplete without breaking the streak).
- WoW trends target per-category + per-app deltas against the immediately
  prior week, plus a compact multi-week totals strip; not a full analytics suite.

## Requirements

- R1: each agent session surfaced in `dayflow agents`/AgentsPane can carry a
  generated recap (1–2 sentences) with a Jev quality/worthiness gate.
- R2: `goal` output and the panel expose current streak and it renders in UI.
- R3: weekly payload carries per-category (and per-app) deltas vs. prior week,
  rendered in WeekPane/WeekTab.
- R4: all new AI work degrades safely (no key / endpoint down / Jev off →
  existing behavior), follows provider-chain config, records `llm_calls` rows,
  and never judges or sends content from read-only MCP paths.
- R5: publish prep artifacts produced: refreshed `preview.png`, README updated
  for Jev + new features, submission issue body drafted to
  `docs/publish/marketplace-submission.md`, `v1.2.0` tag after merge.
  Actual `gh issue create` requires owner approval — out of pipeline scope.

## Key Technical Decisions

- **KTD1 — Recap generation is lazy + cached, never batch-swept.** Recaps are
  produced on demand when the agents view is opened (or CLI asked), persisted
  keyed by transcript path + mtime + size so a file that hasn't grown is never
  re-captioned. Transcripts are append-mostly; mtime/size is the invalidation
  key — cheaper than hashing multi-MB JSONL. New table `agent_recaps`
  (path PK, source, session_start, recap, quality_confidence, file_mtime,
  file_size, model, created_at) via the existing `columnPatches`/migration
  pattern in `engine/store.go`.
- **KTD2 — Jev judges recaps the same way as blocks.** Reuse `decide()`:
  `worthy` (is this session substantial enough to recap — skip 2-message
  probes) and `quality` (does the recap say something specific). Scores persist
  on the recap row; failures degrade to showing the session without a recap.
- **KTD3 — Streaks are computed, not stored.** `streakDays(db, date)` walks
  `day_goals` backward; no new column, no migration beyond what's already
  shipped. Today counts as neutral: a streak ending yesterday survives until
  today's goal resolves.
- **KTD4 — WoW deltas ride inside the existing WeekPayload.** Add a `trends`
  field ([]TrendRow: name, current_min, prev_min, delta_min, pct) built from
  the already-computed `insights` pair — no second DB pass. QML renders a
  delta column; CLI `weekly` prints a trends block.
- **KTD5 — Recap excerpting is bounded and privacy-shaped.** Per session, take
  first + last user messages and a truncated assistant tail (hard byte cap,
  rune-safe via the existing `truncate`), strip file paths/URLs the way
  `boundState` does for Jev state. Session transcripts can contain secrets;
  bound aggressively.

## Implementation Units

### U1. Agent session recaps — engine

**Goal:** `dayflow agents` gains real recaps, persisted and Jev-gated.
**Requirements:** R1, R4. **Dependencies:** none.
**Files:** `engine/agents.go`, `engine/agents_recap.go` (new),
`engine/store.go` (migration + recap read/write), `engine/decisions.go`
(recap-worthy/quality questions), `engine/agents_recap_test.go` (new),
`engine/store_test.go` (migration).

**Approach:**

- New table `agent_recaps` (schema bump following the `columnPatches`
  convention — Dayflow uses additive migrations, not destructive).
- `sessionFingerprint(path)` → `{mtime, size}`; `getRecap`/`putRecap`
  round-trip; stale fingerprint → regenerate.
- `sessionExcerpt(sess)` → bounded text: first user message, last user
  message, last assistant text block; each rune-truncated; total cap ~2 KB;
  path/URL stripping shared with `boundState`.
- `generateRecap(cfg, sess)` → chat call (existing `callOpenRouter`, the
  provider chain, `llm_calls` with `task="agent_recap"`); then `decide()`
  with `worthy` + `quality` noul questions (`task="judge:agent_recap"`).
  `worthy < 0.4` → store empty recap marked judged (don't re-ask). Quality
  `< 0.55` → one regeneration with the low score fed back, keep the better.
- `agentSessionsForDay` gains a `withRecaps` variant or a post-pass
  `attachRecaps(db, cfg, sessions)` — CLI and the QML ProcLoader path share it.
  Generation runs only when recaps are requested (CLI flag `--recaps` default
  on for JSON payload consumed by FullView; keep `agents` bare listing fast
  with recaps off when `--no-recaps` passed).

**Patterns to follow:** `decide()`/`judgeBlock` seam in `engine/decisions.go`;
`callOpenRouter` + `logLLMCall` in `engine/chat.go`; `columnPatches` in
`engine/store.go`; `truncate`/`boundState` rune-safe bounding.

**Test scenarios:**

- Happy path: stub decisions endpoint + chat endpoint (httptest, per
  `decisions_test.go` `pointDecisionsAt`), JSONL fixture session → recap text
  persisted, fingerprint stored, second call hits cache (server hit count 0).
- Worthy gate: `worthy=0.1` → session returned without recap, row marked
  judged, no regen on second call.
- Quality regen: quality 0.3 then 0.8 → final stored recap is the second.
- Fingerprint invalidation: fixture file appended (mtime/size change) →
  recap regenerated.
- Jev off: `JevClassification=false` → recap still generates, no judge rows
  in `llm_calls`.
- Endpoint dead: decisions URL pointing at unreachable host → session list
  still returns, recap empty, no panic, no hang (bounded timeout).
- Excerpt bounding: fixture with 5 MB single message → excerpt under cap,
  valid UTF-8 boundary.

**Verification:** `dayflow agents <today> --json` returns sessions with
`recap` fields after one real run on this machine's transcripts;
`llm_calls` shows `agent_recap` + `judge:agent_recap` rows.

### U2. Agent recap surfacing — AgentsPane + FullView

**Goal:** recaps render in the Agents pane; loading is non-blocking.
**Requirements:** R1. **Dependencies:** U1.
**Files:** `AgentsPane.qml`, `FullView.qml` (agents loader passes the flag),
possibly `Panel.qml` if the compact panel shows agents (check).

**Approach:** AgentsPane rows grow an expandable recap line under the title
(existing `Text` + `MouseArea` patterns). FullView's `agentsProc` command
passes the JSON payload that now includes `recap`; loading flag already
exists (`agentsLoading`). Empty-recap sessions render exactly as today.

**Test scenarios:**

- Payload shape: `agents --json` output includes `recap` key (engine-side
  assertion is enough — QML is render-only; consistent with project's
  "compute in Go" rule).
- Render check is manual/live: pane shows recaps for sessions that have them.

**Verification:** open FullView → Agents pane → sessions show recap lines;
`agentsLoading` spinner still behaves.

### U3. Goal streaks — engine + UI

**Goal:** `dayflow goal` reports streak; panel + FullView show it.
**Requirements:** R2. **Dependencies:** none.
**Files:** `engine/goals.go`, `engine/goals_test.go` (new),
`Panel.qml` (goal display), `TodayPane.qml`/`TodayTab.qml` if goal shows
there (check).

**Approach:**

- `streakDays(db, asOf)` — count consecutive days with `completed=1` walking
  back from `asOf`; today-neutral rule per assumption.
- `goal --json` output gains `streak` field; text output prints
  `goal: <text> (done | open) — streak N`.
- Carry-forward nudge: when yesterday had an incomplete goal and today has
  none, payload includes `carried` (yesterday's goal text) — UI shows
  "yesterday's goal still open" hint. Cheap, high-value.
- QML: streak chip next to the existing goal line in Panel; same in FullView
  today pane if the goal renders there.

**Test scenarios:**

- Streak math: fixtures for done-done-done-today-open → streak 3;
  done-open-done → streak 1; done-yesterday + none-today → streak preserved.
- `carried` only when yesterday incomplete AND today empty.
- `goal --json` field presence; back-compat (absent streak tolerated by old
  readers — additive field).

**Verification:** seed a few completed goals, `dayflow goal --json` shows
`streak`, panel renders the chip.

### U4. Week-over-week trends — engine + weekly UI

**Goal:** real WoW deltas in weekly payload + UI.
**Requirements:** R3. **Dependencies:** none (uses existing
`previousWeekInsights`).
**Files:** `engine/weekly.go`, `engine/weekly_test.go` (new or extend),
`WeekPane.qml`, `WeekTab.qml` if it shows the same block, `docs/agent-contract.md`.

**Approach:**

- `TrendRow{Name, CurrentMin, PrevMin, DeltaMin, Pct}`; build from
  `in.Categories` vs prev week's categories, and `in.Apps` vs prev apps
  (cap at top ~8 each, sorted by |delta|).
- Add `trends` to `WeekPayload`; CLI `weekly` prints a "vs last week" block;
  JSON carries the array.
- Extend `biggestImprovement` usage — keep the string highlight, trends are
  the structured version.
- QML: delta column or up/down mini-bars next to the donut/treemap lists in
  WeekPane — match existing `ContextPane` visual language (Canvas or simple
  colored `Rectangle` rows).
- `docs/agent-contract.md` gains the `trends` field documentation.

**Test scenarios:**

- Delta math: fixture weeks → correct delta_min, pct; division-by-zero when
  prev=0 (pct null or omitted, no NaN in JSON).
- Ordering: top-N by absolute delta; categories absent last week appear as
  new (prev=0).
- Empty prev week (fresh install) → `trends` empty array, not null crash.
- Contract doc updated.

**Verification:** `dayflow weekly --json` shows `trends`; WeekPane renders
delta markers vs last week on this machine's real data.

### U5. Marketplace publish prep

**Goal:** listing-ready repo + drafted submission, owner-gated send.
**Requirements:** R5. **Dependencies:** U1–U4 (preview/README should reflect
the shipped phase).
**Files:** `preview.png` (regenerate from live FullView), `README.md`
(Jev + recaps/streaks/trends sections, config keys), `docs/publish/marketplace-submission.md` (new), `docs/HANDOFF-omarchy-max.md` (refresh or remove post-merge note).

**Approach:**

- Capture a FullView screenshot on live data showing timeline + Jev flags;
  save as `preview.png` (marketplace auto-generates card/detail images from it;
  limits: 50 MB / 40 MP — trivially satisfied).
- README: add Jev section (`jev_classification`, `classification_model`,
  `judge:*` ledger rows), the three new features, updated CLI surface.
- Draft submission file with the six required headings in order
  (Repository URL / Category / Tags / Suggest a missing tag / Maintainer
  notes / checklist with all five items checked). Category `Productivity`,
  tags `ai, hyprland, quickshell`, suggested missing tag `local-first`.
- Do NOT run `gh issue create` — external side effect, requires owner yes.
  The DONE report states the draft path and the exact command.

**Test scenarios:**

- `omarchy plugin validate <repo>` returns clean.
- Submission file contains all six headings verbatim + all five checklist
  boxes checked (assert by grep in a test or manual checklist — docs unit,
  light verification is fine).

**Verification:** `omarchy plugin validate` clean; preview.png committed;
submission draft readable top-to-bottom.

## Scope Boundaries

**Deferred to follow-up work:** Jev residual cleanup (judge-filter dedup,
`llm_calls`↔block correlation, `prevBlock` dedicated test, QML test harness);
FullView deep polish beyond what the three lanes need; marketplace submission
itself (owner-gated).

**Non-goals:** new data capture sources, cloud/sync features, multi-day goal
objects, mobile anything.

## Risks & Dependencies

- **Transcript privacy**: agent JSONL can contain secrets — KTD5 bounding +
  stripping is the mitigation; review-worthy.
- **Transcript format drift**: Claude/Codex JSONL schemas change; the scanner
  already tolerates unknown fields — keep recap excerpting equally tolerant.
- **Cost**: recaps are per-session-per-day lazy + cached; worst case a handful
  of small calls/day — same order as block summarization.
- **`agents` pane perf**: recap generation on first view could take seconds —
  `agentsLoading` exists; consider a two-phase load (list first, recaps fill
  in) if the first paint suffers — implementation-time tuning, not a plan
  blocker.

## Sources & Research

- Origin: user-directed scope (all three lanes + publish), settled in session.
- `engine/agents.go`, `engine/goals.go`, `engine/weekly.go`,
  `engine/decisions.go`, `engine/store.go`, `AgentsPane.qml`,
  `WeekPane.qml`, `Panel.qml` — current seams verified this session.
- Marketplace: `omacom/omarchy-plugin-marketplace` SUBMISSION.md —
  six-heading issue format, category/tag allowlists, commit-bound approval.
