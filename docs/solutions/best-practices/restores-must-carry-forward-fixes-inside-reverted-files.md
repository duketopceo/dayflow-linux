---
title: Restoring a pinned UI must carry forward bugfixes that lived in the reverted files
date: 2026-10-05
module: QML plugin UI (Panel/FullView/Settings/AgentsPane)
problem_type: best_practice
component: development_workflow
severity: medium
tags:
  - "restore"
  - "revert"
  - "carry-forward"
  - "qml"
  - "pin"
---

# Restoring a pinned UI must carry forward bugfixes that lived in the reverted files

## Context

The v1.6.0 release restores the classic QML tree pinned at `91ee9fe` over the
current engine (plan: `docs/plans/2026-10-05-001-feat-classic-ui-v160-release-plan.md`).
The plan called for one surgical carry (`#52`'s `delete patch.providers` /
`delete patch.routing` in `Panel.qml`'s `saveConfig`); code review found two
more post-pin fixes that also lived inside reverted or deleted UI files and
would have silently regressed.

## The pattern

Fixes often land inside UI files, not only in the engine. A verbatim restore
reverts them along with the look. Before restoring a pinned tree:

1. `git log --oneline <pin>..master -- '<file-glob>'` on the files being
   restored AND on files being deleted — a fix may live in a component the
   restore removes entirely (here: `#43`'s watchdog bump lived in
   `AgentBriefingLoader.qml`, deleted by the restore).
2. For each post-pin fix commit on those paths, decide: carry it surgically
   (re-apply just the fix into the restored file) or list it as an accepted
   regression. Never let it revert silently.
3. Verify the final claim deterministically:
   `git diff <pin> HEAD -- <restored files>` should show ONLY the intended
   carries — nothing else.
4. Pin the carries with a test when a silent drop is damaging
   (`engine/panel_carry_test.go` asserts the `saveConfig` deletes and the
   300s watchdog interval).

## Carries in the v1.6.0 restore (for future re-derivers)

- `Panel.qml` `saveConfig()`: `delete patch.providers` / `delete patch.routing`
  (#52 — stale snapshot clobbering `provider set` prompt overrides), plus
  `decisions_api_key` sentinel strip.
- `FullView.qml` `agentsWatchdog`: `interval: 300000` (#43 — pane killed
  mid-briefing).
- `Settings.qml`: prompt-overrides display reads `spec.key` — the pinned
  `spec.field` names were never emitted by the engine.

## When this applies

Any "restore/revert to a known-good tree" change where the pin predates
fixes that shipped in the files being restored. U3 (the gated classic-UI
redesign) must start from the carried tree, not a fresh `91ee9fe` checkout.
