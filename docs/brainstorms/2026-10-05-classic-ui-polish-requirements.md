# Classic UI Polish — Requirements (U3 entry gate)

Date: 2026-10-05 · Source plan: `docs/plans/2026-10-05-001-feat-classic-ui-v160-release-plan.md` (U3) · Grounding: `/tmp/compound-engineering/ce-brainstorm/u3-polish/grounding.md`

## Summary

Tighten the classic Dayflow popup UI — the v1.6.0 baseline — into a "tight and neat" presentation by normalizing repeated layout literals into shared components, compressing bottom chrome, and reworking Settings into a dense grouped layout that also fields the engine config keys with no UI today. The full window receives the same polish language as a follow-on unit.

## Problem Frame

The classic popup is the daily surface again after two rejected alternatives — the Pulse dashboard read as "huge and empty and overdone, not tight and neat." Three concrete gaps remain: ~39 hand-rolled card literals re-derive the same visual loosely across 12 files; every tab renders a four-row bottom chrome stack (quick-action Flow, version banner, status caption, notice text); and `Settings.qml` both overflows small panels and fields only ~16 of ~44 json-tagged engine config keys — `decisions_url`, `knowledge_transport`, `agent_completions`, `ignore_apps`, `jev_classification`, `classification_model`, `routing`, `providers` are CLI-only today.

## Key Decisions

- **Classic layout is the base** — the Pulse dashboard direction was rejected and reverted (session-settled: user-directed — chosen over dashboard re-adoption: "huge and empty and overdone").
- **Tighten, don't surface-new** — polish means compressing what exists (session-settled: user-directed — chose "visual staleness" over missing surfaces as the sharpest pain). Governs R1–R4.
- **Bottom chrome is the named bloat cut** (session-settled: user-directed — chosen over cards-in-cards, copy/notes, low-value stats). Governs R3.
- **Shared-component normalization first** (session-settled: user-approved — consistency through components beats per-file drift). Governs R2.
- **Popup first; full window follows in a later unit** (session-settled: user-directed). Governs R7.
- **Settings gains fields for every engine-only key** (session-settled: user-directed — "Visual + all gaps" over visual-only). Governs R5.

## Requirements

### Density and presentation

- **R1.** Every popup tab presents the same information it does today in visibly tighter form — spacing, card treatment, and filler copy compressed — with zero content regressions.
- **R2.** Repeated layout literals (rounded card, bordered button, stat row) are normalized into shared components before per-screen tightening; screens compose those, not re-derive them.
- **R3.** The Panel bottom chrome stack collapses to at most one status row by default; quick actions ("Ignore current app", "Summarize now", "Full view") and the version-skew banner remain reachable but stop consuming four stacked rows.
- **R4.** At the compact popup width (540 logical px), every tab's primary content fits inside its existing height cap without scrolling at typical data volumes.

### Settings coverage

- **R5.** Settings is reworked into a dense grouped layout that fixes the small-panel overflow and exposes fields for all engine config keys currently CLI-only: `decisions_url`, `decisions_model`, `decisions_api_key`, `knowledge_transport` (+ its URL/SSH subkeys), `agent_completions`, `ignore_apps`, `jev_classification`, `classification_model`, and routing/provider options via their dedicated write paths.
- **R6.** Secret fields keep existing masking semantics — `openrouter_api_key` and `decisions_api_key` strip `"***redacted***"` on save per `Panel.qml`'s current guard.

### Architecture and verification

- **R7.** `Panel.qml`/`FullView.qml` architecture, the popup/full-window split, the five-tab model, and component names are unchanged; no engine call sites change.
- **R8.** Every fit/density claim ships with a rendered check — recover the `tests/ui` harness approach deleted by U1 (it exists in git history) and extend it for the polished surfaces.
- **R9.** `git diff 91ee9fe` on restored files may show only the three documented carries; polish work is layered on top as new commits, not a re-restore.

## Popup shell — tightened layout (target shape)

```
┌──────────────────────────────────────┐
│ ● Dayflow   recording · gemma   [▶⏸] │  header: one row
│ today  standup  chat  week  settings │  tab bar: unchanged
│ ┌──────────────────────────────────┐ │
│ │ tab content — shared cards,      │ │  Loader: same components
│ │ tighter rows, no filler copy     │ │
│ └──────────────────────────────────┘ │
│ 1353 fr · 0 pend · 9.1 GB   [⋯ act.] │  footer: ONE row
└──────────────────────────────────────┘
```

## Acceptance Examples

- Panel at compact width shows the footer as a single line; "Summarize now" is still reachable from it.
- Settings on a 1280×720 viewport renders all groups without clipping; `decisions_url` accepts a URL and round-trips through `dayflow config --json`.
- TodayTab renders a 12-span day with zero regressions versus the classic baseline; rendered harness passes the same scenarios it covered before deletion.

## Scope Boundaries

- **Deferred:** full-window pane polish (same language, follow-on unit); "still working" Agents loading hint; masked-key edit UX beyond current semantics; stdin transport for prompt overrides; CI QML compile check.
- **Outside this work:** new tabs or navigation models, wholesale surface replacement, Pulse dashboard revival, new engine features, Flow parity, per-conversation playback ranges.

## Dependencies / Assumptions

- Style authority is the host's `qs.Commons`/`qs.Ui` tokens — shared components wrap them, not replace them.
- `providers`/`routing` edits go through dedicated write paths (`provider set`, `config set`), never the `configDraft` round-trip — per the U1 carry.
- The deleted `tests/ui` harness is recoverable from git history at commit `91ee9fe..master`.

## Outstanding Questions

- *Deferred to planning:* exact write path per gap key (`provider set` vs `config set` vs dedicated CLI).
- *Deferred to planning:* whether quick actions collapse into an overflow control or a compact inline row.
- *Deferred to planning:* data-density targets per tab beyond R4's no-scroll rule — the harness scenarios define "typical volume."
