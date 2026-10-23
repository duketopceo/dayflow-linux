---
title: Ribbon U2 CodeRabbit review follow-up - Plan
type: fix
date: 2026-10-23
origin: docs/plans/2026-10-02-2315-feat-ribbon-log-ui-redesign-plan.md (U2); PR #76
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-plan-bootstrap
execution: code
---

# Ribbon U2 CodeRabbit review follow-up - Plan

## Goal Capsule

- **Objective:** Make Ribbon Log U2 (token layer, glyphs, theme math, `color`/`color_role`) merge-ready on PR #76 by clearing still-valid CodeRabbit findings without changing U2 product behavior.
- **Means:** Targeted fixes in engine color enrichment, `Glyphs.js` codepoint construction, `ThemeMath.js` null-safety, `Tokens.qml` reload/parse, and stronger QML unit assertions. Push to the open PR branch; do not merge.
- **Authority hierarchy:** Ribbon plan U2 R-IDs and KTDs win on product behavior. This plan wins on review-fix scope only.
- **Stop conditions:** Paid model calls; editing live Omarchy theme paths; expanding into Ribbon U3 purge work.
- **Execution profile:** One PR update (#76). CI must stay green. Do not merge.
- **Who finishes:** Implementer lands fixes; repo owner merges #76.

## Product Contract

### Summary

PR #76 already implements Ribbon U2. Insights cards were aligned with timeline colors in a follow-up commit. Remaining review findings: weekly donut/heatmap omit `ColorRole`; config reload errors discarded; Glyphs use `\u` escapes CodeRabbit flags; `correctContrast` can throw on invalid `fg`; Tokens lacks `colorsFile.watchChanges`, weak hex boundary, empty `onHuesChanged`, undebounced Color reloads; QML tests assert existence instead of values / weak budget-exhaustion check.

### Requirements

- R1. Donut and heatmap items assign `ColorRole` via `categoryRole` (empty when user hex overrides).
- R2. Failed `loadConfig` reloads on timeline/MCP paths keep the already-loaded `cfg`.
- R3. Glyph constants use `String.fromCodePoint` (no `\ufxxxx` string escapes).
- R4. `correctContrast` never throws; invalid conversion returns current/fallback hex.
- R5. `colorsFile` watches changes; parse hex has hex boundary; Color-driven reloads debounce; remove empty `onHuesChanged`.
- R6. QML tests assert concrete category/contrast outcomes for the named scenarios.

### Scope Boundaries

- Not in scope: Ribbon U3 literal purge; bar states (U4); app mark (U5); merging #76.
- Insights/timeline card color parity already shipped on this branch — verify tests pass; do not rework unless red.

### Key Technical Decisions

- KTD1. session-settled: Push fixes onto the existing #76 head (`feat/ribbon-u2-tokens` lineage / this agent branch) rather than opening a duplicate U2 PR. Provenance: user-directed LFG "open PR; do not merge" on highest-priority shippable item which is already #76. Rejected: new parallel U2 PR (duplicates review surface).
- KTD2. Keep an empty `color_role` when `categoryRole` returns empty (user-configured color), matching timeline card contract in `engine/cards_test.go`.
- KTD3. Debounce Color → `colorsFile.reload()` with a single short Timer (≈50ms) so burst updates collapse to one reload.

## Implementation Units

### U1. Engine: weekly ColorRole + safe config reload

- **Goal:** Weekly JSON and timeline reload paths match the shared color contract.
- **Files:** `engine/weekly.go`, `engine/main.go`, `engine/mcp.go`, tests as needed (`engine/cards_test.go` or weekly tests).
- **Approach:** Set `ColorRole: categoryRole(...)` in `buildCategoryDonut` and `buildHeatmap`. On reload sites that currently use `cfg, _ := loadConfig()`, keep prior `cfg` when err != nil.
- **Test scenarios:** Donut/heatmap items for a standard category expose non-empty `color_role`; user override yields empty role. Reload failure does not wipe colors (unit or existing timeline tests green).
- **Verification:** `go test ./...` in `engine/`.

### U2. QML/JS: Glyphs, ThemeMath, Tokens, tests

- **Goal:** Address remaining UI-layer review findings.
- **Files:** `Glyphs.js`, `ThemeMath.js`, `Tokens.qml`, `tests/qml/tst_thememath.qml`, `tests/qml/tst_tokens.qml`.
- **Approach:** fromCodePoint glyphs; null-guard `toOklch` in `correctContrast`; Tokens watch/debounce/regex; strengthen tests (budget exhaustion asserts ≥4.5; category chain asserts resolved hex ≠ undefined and matches expected corrected/default path).
- **Verification:** `go test ./...`; run `qmltestrunner` when available, else rely on Go + lint; `scripts/lint-ui.sh` if present.

## Verification Contract

- `cd engine && go test ./...`
- `go vet ./...` and gofmt clean on touched Go files
- PR #76 CI `build-and-test` green after push
- Do not merge

## Definition of Done

- All still-valid CodeRabbit findings from the listed set addressed or explicitly skipped with reason in the PR/commit message
- CI decided green (or babysit residuals recorded)
- PR URL returned; merge left to humans
