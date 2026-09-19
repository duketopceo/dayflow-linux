---
title: "feat: Jev judgment integration — calibrated decisions across Dayflow"
date: 2026-09-18
type: feat
origin: docs/brainstorms/2026-09-18-jev-judgment-integration-requirements.md
---

# feat: Jev judgment integration — calibrated decisions across Dayflow

## Summary

Integrate TypeSafe's Jev decision model (OpenRouter `/api/alpha/decisions`,
`jev-latest`) into every judgment-shaped seam of Dayflow. Jev produces
calibrated `noul` scores (0-1); the chat model keeps only true generation.
Scores are stored and surfaced in the UI. Jev unreachable always degrades
to current behavior — it is never a hard dependency.

## Problem Frame

Dayflow asks one generative chat model to do two different jobs: *generate*
(titles, summaries, standup prose) and *judge* (which category, is this
productive, did the activity change, is the summary good, is this failure
worth retrying). Generative models are weak judges: no calibrated
confidence, inconsistent category labels, no signal for "don't trust this."
Jev exists for exactly this — calibrated 0-1 judgments at ~$0.00002/call,
essentially free at Dayflow's ~96 blocks/day volume.

## Requirements

From `docs/brainstorms/2026-09-18-jev-judgment-integration-requirements.md`
(origin). All seven integrations ship in one tranche, priority order:

- R1 Category takeover — jev judges category + confidence per block
- R2 Merge judgment — "same activity as previous?" feeds block merging
- R3 Summary quality gate — specific/accurate judgment → retry or flag
- R4 Standup worthiness — score blocks for report inclusion
- R5 Dead-block triage — retryable vs terminal → auto-requeue once
- R6 Forecast confidence — calibrated probability in forecast output + UI
- R7 Context-shift salience — "real context change?" filters sankey edges
- R8 Infrastructure — `engine/decisions.go` client, degrade-safe
- R9 UI surface — confidence visible (Today pane/panel), forecast %, low-confidence flag
- R10 Fallback — jev down → current behavior everywhere

## Key Technical Decisions

- **KTD1: `var decisionsURL` injectable endpoint.** Mirror the existing
  `var openRouterURL` pattern in `engine/summarize.go:17` so tests point the
  client at `httptest.Server`. Endpoint:
  `https://openrouter.ai/api/alpha/decisions`, auth via existing
  `Config.OpenRouterAPIKey` bearer (same key, alpha path — verified live).
  Rejected: separate credentials plumbing (same OpenRouter account).
- **KTD2: One decisions call per block carries all per-block questions.**
  Category enum set + `productive` + quality-gate + same-as-prev questions
  ride in a single `questions` map — one HTTP call per block instead of
  four. Rejected: per-kind calls (4× latency, more failure surface).
- **KTD3: `blocks` gains `category_confidence REAL` and `same_as_prev
  INTEGER` via schemaV4.** Follows the `productive INTEGER NULL` precedent
  (nullable, computed post-insert). Rejected: a separate `judgments` table
  — over-normalized for two scalar facts read on every block row.
- **KTD4: Jev calls log to `llm_calls` with `task='judge:<kind>'`,
  `provider='typesafe'`.** The U4 usage breakdown (`by_task`,
  `by_provider`) picks them up free. Rejected: dedicated ledger.
- **KTD5: Chat summarize prompt keeps returning category.** Jev overrides
  when reachable; the chat category is the fallback baseline, preserving
  R10 without a second code path. (Supersedes the origin doc's "prompt
  drops category selection" — same product outcome, better fallback.)
- **KTD6: Judgments are synchronous inside the existing sweep, never in
  render paths.** `summarizePending` already blocks per block on a slower
  vision call; a ~1s decisions call is in the noise. Render-time jev calls
  fire only on user-initiated surfaces (standup, forecast, weekly payload)
  — never in timeline/panel painting. Rejected: a separate judge pass
  (extra sweep complexity, no benefit at this volume).
- **KTD7: Missing/partial jev answers mean "no opinion" → per-field
  fallback.** A failed call or absent key never zeroes a field; each
  consumer falls back independently. Rejected: all-or-nothing per block.
- **KTD8: Low-confidence flag threshold = 0.55**, one tunable const shared
  by engine consumers and mirrored in QML.

## High-Level Technical Design

Per-block judgment flow inside `summarizePending`:

```
frames → callOpenRouter (vision+chat: title/summary/activities/category)
      → sanitizeResult
      → decide(cfg, stateFromBlock, questions{        ── one HTTP call
          cat_<each enum>:   noul "primary category is X"
          productive:        noul "focused task work"
          quality:           noul "title+summary specific & accurate"
          same_as_prev:      noul "continues previous block's activity"
          retriable (if err): noul "failure likely transient"   })
      → argmax(cat_*) → category + category_confidence
      → productive ← productive noul ≥ 0.5
      → same_as_prev ← noul ≥ 0.5 → stored column
      → quality < 0.5 → one regeneration retry; still low → flag
      → fallback on any error: chat-provided fields stand
```

State sent to jev is text-only: app names, window titles, produced
title/summary, previous block's title/app, error text — never frame images
or file paths beyond what summaries already carry.

## Implementation Units

### U1. Decisions client — `engine/decisions.go`

- **Goal:** `decide(cfg, state string, qs map[string]string) (map[string]float64, usage, error)` — POST the verified schema, parse `answers.<key>.noul`, 15s timeout, record `llm_calls` row (`task='judge:<kind>'`, `provider='typesafe'`, resolved model name from response).
- **Requirements:** R8, R10 · **Dependencies:** none
- **Files:** `engine/decisions.go` (new), `engine/decisions_test.go` (new), `engine/store.go` (record-llm helper if needed), `engine/config.go` (`JevModel string` default `jev-latest`)
- **Approach:** mirror `callOpenRouter` structure (client build, strip/parse, token counts). `var decisionsURL` for tests (KTD1). Model resolves from response `model` field so `jev-latest` alias drift is recorded truthfully.
- **Patterns to follow:** `callOpenRouter` in `engine/summarize.go:118`; llm_calls recording near `engine/store.go:536`.
- **Test scenarios:**
  - Happy: mock server returns `{answers:{a:{noul:.9},b:{noul:.1}}}` → map `{a:.9,b:.1}`, usage captured, llm_calls row has task/provider/model
  - Edge: `answers` missing a requested key → absent from map, no error
  - Edge: empty questions map → no HTTP call, empty result
  - Error: HTTP 400/401/500 → error returned, llm_calls row records status/error
  - Error: timeout (server sleeps >15s) → error, no hang
- **Verification:** unit tests green against httptest server; no network in tests.

### U2. Schema v4 + Block fields

- **Goal:** persist `category_confidence` and `same_as_prev`; expose on `Block` JSON.
- **Requirements:** R1, R2, R9 · **Dependencies:** U1
- **Files:** `engine/store.go` (schemaV4, Block struct, insert/select paths), `engine/store_test.go` or `main_test.go`
- **Approach:** `schemaV4 = ALTER TABLE blocks ADD COLUMN category_confidence REAL; ALTER TABLE blocks ADD COLUMN same_as_prev INTEGER` in the existing migration sequence (after schemaV3). `Block` gains `CategoryConfidence *float64 json:"category_confidence,omitempty"` and `SameAsPrev *int json:"same_as_prev,omitempty"`. Update the `INSERT INTO blocks` and `blocksForDay`/`blocksBetween` selects.
- **Test scenarios:**
  - Happy: fresh DB applies v4; insert+read round-trips confidence
  - Edge: pre-v4 DB migrates cleanly; old rows read with nil confidence
  - Edge: nil confidence writes/reads as NULL (not 0.0)
- **Verification:** existing suite green; migration test covers v3→v4.

### U3. Summarize judgment batch — category + productive + quality + same-as-prev

- **Goal:** one `decide` call per block in `summarizePending` applying KTD2's four question groups; results land on the stored block.
- **Requirements:** R1, R2, R3, R10 · **Dependencies:** U1, U2
- **Files:** `engine/summarize.go`, `engine/summarize_test.go` (or `main_test.go`)
- **Approach:** after `sanitizeResult`, build `state` (apps, titles, produced title/summary, previous-block title/app) and the questions map (categories from `config.go`'s enum list, `productive`, `quality`, `same_as_prev` when a previous block exists). Skip for idle/locked blocks. argmax category → `res` category + confidence; `quality < 0.55` → one regeneration via `callOpenRouter`, keep better result, flag if still low. Errors → `debugf` + chat-provided fields stand (KTD7).
- **Test scenarios:**
  - Happy: jev returns cat coding .9/comms .05 → category=coding, confidence .9 stored
  - Quality fail: quality .3 → regenerate once; second call used
  - Edge: no previous block → `same_as_prev` question omitted
  - Edge: idle block → no decide call at all
  - Error: decide errors → chat category/productive retained, block still stored done
  - Partial: jev answers missing `productive` → chat productive retained
- **Verification:** tests green; live smoke — real block gets calibrated category+confidence visible in `dayflow timeline --json`.

### U4. Dead-block triage

- **Goal:** jev judges failure retryability once; retryables requeue, terminals go `dead`.
- **Requirements:** R5, R10 · **Dependencies:** U1
- **Files:** `engine/summarize.go` (failure path / `markDead` site in `store.go`)
- **Approach:** where blocks currently transition to `dead` (attempts exhausted), first `decide` with `state`=error text+block context, `retryable` noul. ≥0.5 and `attempts < cap+1` → requeue (reset eligible, log `judge_requeue` event); else `dead` as today. Judge errors → current behavior (dead).
- **Test scenarios:**
  - Happy: `429 rate limit` error → retryable .8 → block requeued, event logged
  - Terminal: `401 Missing Authentication` → retryable .1 → dead
  - Error: decide fails → dead (current behavior preserved)
- **Verification:** tests green; a forced transient failure requeues once, then deads on second failure.

### U5. Merge application — `mergeCards` uses `same_as_prev`

- **Goal:** block merging prefers the stored judgment; heuristic remains fallback.
- **Requirements:** R2, R10 · **Dependencies:** U2, U3
- **Files:** `engine/main.go` (`mergeCards` near line 1108), `engine/main_test.go`
- **Approach:** merge predicate becomes `same_as_prev == 1` OR existing title/app+category equality; `same_as_prev == 0` does NOT veto the heuristic merge (judgment informs, never hides data).
- **Test scenarios:**
  - Happy: same_as_prev=1 blocks with differing titles merge into one card
  - Fallback: nil same_as_prev → heuristic behavior identical to today
  - Edge: same_as_prev=0 but identical titles → still merges (no veto)
- **Verification:** tests green; standup/timeline cards merge across title variance.

### U6. Standup worthiness

- **Goal:** standup includes jev-scored noteworthy blocks rather than every block.
- **Requirements:** R4, R10 · **Dependencies:** U1
- **Files:** `engine/daily.go` (`blocksForDay` consumer at line 55), `engine/daily_test.go`
- **Approach:** on standup generation, one batched `decide` over the day's done blocks (state = block list, `worthy_<start_ts>` per block — bounded to ~40 newest/largest if a day exceeds the map). Include ≥0.5; all-below-threshold → include top-3 anyway so standup is never empty. Judge failure → all blocks (current output).
- **Test scenarios:**
  - Happy: 5 blocks, 2 scored .7+ → standup lists those 2
  - Edge: all scores < .5 → top-3 fallback included
  - Error: decide fails → all blocks listed (today's behavior)
- **Verification:** tests green; `dayflow standup --json` shows filtered set.

### U7. Forecast confidence

- **Goal:** `dayflow forecast` reports a calibrated probability for its own prediction.
- **Requirements:** R6, R10 · **Dependencies:** U1
- **Files:** `engine/forecast.go`, `engine/forecast_test.go`, `engine/main.go` (printForecast)
- **Approach:** after `forecast()` computes the mix, one `decide`: state = day profiles summary + predicted mix; `confident` noul → `Forecast.Confidence *float64`. JSON gains `confidence`; text prints e.g. `confidence: 72%`. Judge failure → field absent (output unchanged).
- **Test scenarios:**
  - Happy: confidence .72 → JSON field + text line present
  - Error: decide fails → output identical to today
- **Verification:** tests green; `dayflow forecast --json` carries `confidence`.

### U8. Context-shift salience

- **Goal:** sankey edges filtered to real context changes.
- **Requirements:** R7, R10 · **Dependencies:** U1
- **Files:** `engine/weekly.go` (`buildContextShifts` line 208), `engine/weekly_test.go`
- **Approach:** compute shifts as today; for the top ~12 edges by minutes, batch `decide` (`real_shift_<i>` per edge, state = endpoint app names + representative titles); drop edges < 0.4. Judge failure → unfiltered list (today's output).
- **Test scenarios:**
  - Happy: edge terminal→browser scored .7 stays; terminal→terminal-noise .1 dropped
  - Edge: >12 edges → only top-12 judged, rest pass through unjudged
  - Error: decide fails → all edges returned (today's behavior)
- **Verification:** tests green; weekly payload's sankey shows meaningful edges.

### U9. UI surface + MCP/CLI fields + contract doc

- **Goal:** calibrated scores visible where they inform.
- **Requirements:** R6, R9, R10 · **Dependencies:** U2, U3, U7
- **Files:** `TodayPane.qml`, `TodayTab.qml`, `Panel.qml`, `FullView.qml` (forecast section), `engine/mcp.go` (get_timeline/get_forecast fields pass through — verify), `engine/main.go` (timeline --json already emits Block fields), `docs/agent-contract.md`
- **Approach:** `category_confidence` rides existing Block JSON (no MCP change if passthrough — verify); low-confidence (`<0.55`) blocks get a `[Low confidence]`-style marker next to the `[Failed]` pattern from the maintenance tranche; FullView forecast section shows `72% confident` under the mix; today pane shows a small confidence chip or flag on low-confidence rows only (quiet when confident).
- **Test scenarios:**
  - Happy: block with confidence .4 → timeline shows flag; .9 → no flag
  - Integration: `get_timeline` MCP result includes `category_confidence`
  - Edge: pre-v4 blocks (null confidence) render with no flag, no errors
- **Verification:** QML loads clean (no binding errors in journal); live panel shows flag on a seeded low-confidence block; contract doc updated.

## Scope Boundaries

**Out:** generative-call replacement, image/frame judgments, backfill of
historical blocks, new ledger tables, a separate judge timer, config UI for
thresholds, macOS parity, marketplace submission.

### Deferred to Follow-Up Work

- Async/batched judge pass if per-block latency ever matters (KTD6 rejected
  it for now; revisit if sweeps feel slow)
- Jev-judged merge veto (`same_as_prev=0` actively splitting heuristic
  merges) — needs user feedback on judgment quality first
- Backfill tooling for historical blocks

## Risks & Dependencies

- **Alpha API stability:** `/api/alpha/decisions` is an alpha endpoint —
  shape could drift. Mitigation: KTD7 partial-answer tolerance; pinned
  model fallback (`typesafe/jev-1.13-20260917`) if `jev-latest` resolves
  badly; llm_calls rows record the resolved model for forensics.
- **Judgment quality is unproven on real dayflow data** — jev was verified
  live on a synthetic block. Mitigation: KTD8 threshold is conservative;
  quality-gate retries, never silently suppresses; week-1 review of
  flagged blocks tells us if thresholds need tuning.
- **Latency in the sweep:** +1 HTTP call/block. Mitigation: single batched
  call (KTD2), 15s timeout, failure never blocks storage.
- **Cost:** ~$0.002/day at current volume — not a risk, noted for the
  record.

## Sources & Research

- Live API verification this session: `POST /api/alpha/decisions`,
  `model: jev-latest` → `typesafe/jev-1.13-20260917`, `{state, questions:
  {k:{type:"noul",instructions}}}` → `{answers:{k:{noul}}}`, $0.000016 per
  4-question call.
- Working invocation precedent: `/tmp/jev-test/jev_pr_review.py` (same
  schema, noul judgments for PR review).
- `jev-compact` repo — `JevScorer` uses the same model for span scoring
  (calibrated 0-1 primitives; heuristic fallback when unreachable).
- Local engine patterns: `callOpenRouter` (`engine/summarize.go:118`),
  `llm_calls` ledger (`engine/store.go:125`), `productive INTEGER NULL`
  column precedent (`engine/store.go:37`), migration sequence at v3.
