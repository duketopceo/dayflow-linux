# Plan: v1.6.0 — classic-UI release

**Created:** 2026-10-05
**Origin:** user direction — "plan the new changes, non-UI, and this UI as the
release; then polish and redesign of it" — after PR #58 (Pulse dashboard) was
merged then reverted by product decision (#60). The remembered UI is the
plugin checkout pinned at `91ee9fe` (Oct 1, v1.5.0, verified live on machine).

## Problem frame

Master (`9952c1d`) carries ~11 post-`91ee9fe` UI changes the user has never
seen and does not want: restructured Settings/Onboarding (#55), compact-panel
Agents tab + completion feed (#57 + earlier), PagedText pagination, per-source
stats surfaces. The *engine* work in the same range is good and stays: local
decisions endpoint (#56), completion recording (#57), provider readiness /
settings-sync / route-testing fixes (#51–53), privacy disclosure (#54),
knowledge sync (#45/#48), batch recaps (#50).

The release = **all non-UI changes + the `91ee9fe` QML tree**, tagged v1.6.0.
Polish/redesign of the classic UI is a follow-on track (U3), not part of the
release.

## Key research findings (why the restore is not a plain checkout)

1. **#52's engine fix has a QML half.** `Panel.qml` now sends
   `delete patch.providers; delete patch.routing` in `saveConfig()` — without
   it, the panel's stale provider snapshot clobbers independently-saved prompt
   overrides (the exact bug #52 fixed). Restoring `Panel.qml@91ee9fe`
   reintroduces the clobber **unless** those two deletes are carried forward.
   File: `Panel.qml`, `saveConfig()`, ~4 lines. Everything else in the
   post-`91ee9fe` Panel diff is UI surface (completions parse, agents tab,
   settings width, PagedText errors) — revert freely.
2. **No back-references.** `git grep` at `91ee9fe` shows zero references to
   `AgentsTab`, `CompletionFeed`, `PagedText`, `CompactButton`,
   `AgentBriefingLoader` — the added files delete cleanly.
3. **Version consistency is enforced.** `engine/version_test.go` reads
   `pluginVersion` from `Panel.qml`; `91ee9fe`'s Panel declares it. The bump
   trio is `manifest.json`, `engine/main.go` const, `Panel.qml` pluginVersion.
4. **Release is tag-driven.** `.github/workflows/release.yml` asserts
   tag == engine const, builds `dayflow-<ver>-<arch>` + SHA256SUMS, creates
   the GitHub release. `scripts/install.sh` pins plugin manifest → release.
5. **Known regressions accepted on restore** (flag in PR body): Settings can
   overflow small panels again (the bug #55 addressed); `decisions_url`,
   `knowledge_transport`, `agent_completions`, provider prompt overrides have
   no Settings fields (CLI/`config.json` only — README config table covers
   them); compact-panel Agents tab and completion feed disappear (engine
   keeps recording; `dayflow` CLI still surfaces them).

## Requirements traceability

| Ask | Where |
|---|---|
| "new changes, non UI" in the release | U1 keeps engine/docs/scripts; U2 ships them |
| "this UI as the release" | U1 restores `91ee9fe` QML exactly |
| "polish and redesign" later | U3 gated track |
| No data loss / rollback | U1 verification + plugin-checkout pin |

## Implementation units

### U1 — Restore classic QML on master (`chore/restore-classic-ui`)

**Approach:** restore the six modified QML files to `91ee9fe` content, delete
the added UI files, carry one surgical behavior fix.

Restore to `91ee9fe`:
- `Panel.qml` — then re-apply ONLY the `delete patch.providers` /
  `delete patch.routing` lines in `saveConfig()` (keeps #52's fix; kills the
  stale-snapshot clobber without the new layout)
- `AgentsPane.qml`, `FullView.qml`, `Onboarding.qml`, `Settings.qml`,
  `TodayTab.qml` — verbatim `91ee9fe`

Delete (added post-`91ee9fe`, unreferenced by old QML):
- `AgentsTab.qml`, `CompletionFeed.qml`, `PagedText.qml`,
  `CompactButton.qml`, `AgentBriefingLoader.qml`
- `tests/ui/fit.qml`, `tests/ui/fit-completions.qml`,
  `tests/ui/verify-fit.py`, `tests/ui/verify-completions.py`
  (harnesses exercise the new UI only)

Keep (non-UI): `engine/**`, `scripts/completion-hook.sh`,
`docs/immediate-completions.md`, `docs/plans/**`, `README.md`,
`PRIVACY.md`, `manifest.json` (identical across both commits), `.github/**`.

Doc touch-ups in the same PR:
- `ROADMAP.md` — fix "Agents tab (compact panel)" line (surface drops);
  mark v1.6.0 section
- `docs/immediate-completions.md` — if it claims a panel feed, adjust to
  "engine records; UI surface pending redesign" (check wording)

Test scenarios:
- `go test ./...` green (`version_test.go` reads restored `Panel.qml`)
- `grep -rn "PagedText\|AgentsTab\|CompletionFeed\|CompactButton\|AgentBriefingLoader" *.qml` → empty
- Providers roundtrip: set a `providers[].prompt override` via
  `dayflow provider set`, then a panel `saveConfig` → override survives
  (this is the #52 carry proving itself)
- Plugin checkout → this branch → shell hot-reload → journal shows no
  dayflow QML errors; open every tab (today/standup/chat/week/settings)

### U2 — Cut v1.6.0

- Bump trio to `1.6.0`: `manifest.json` `version`, `engine/main.go`
  `const version`, `Panel.qml` `pluginVersion` (version_test enforces)
- `ROADMAP.md`: move the shipped list under a v1.6.0 heading; refresh
  "Next" (playback ranges, #2–#4 issues, UI polish track)
- Release notes draft (PR body → release): local-model support
  (`decisions_url`, chat transport), immediate agent completions
  (engine-side), batch recaps, provider readiness/sync fixes, knowledge
  sync, privacy disclosure. Call out: UI intentionally ships the classic
  layout; a redesign track follows.
- `git tag v1.6.0 && git push origin v1.6.0` → release.yml builds and
  publishes assets
- Post-tag local update (canonical path): plugin checkout
  `git fetch && reset --hard v1.6.0` → `bash scripts/install.sh` (release
  binary, checksum-verified — not `DAYFLOW_BUILD=local` anymore) →
  `systemctl --user restart dayflow-capture` → `dayflow status` verify

Test scenarios:
- release.yml tag assertion passes (tag == const)
- `dayflow version` → 1.5.0→1.6.0 on installed binary
- `dayflow status` configured, capture active, frame count grows
- sqlite frame/block counts non-decreasing across the swap

### U3 — Classic-UI polish/redesign (gated follow-on, not this release)

Direction words from the user: **dense, specific, not bloated**. Needs its
own requirements pass before implementation — scope the brief around the
classic layout as the base (not the Pulse dashboard):

- Entry gate: short `ce-brainstorm`/spec answering per-page: what density
  means (data-per-screen targets), what "specific" means (real labels,
  real numbers, no decorative filler), what gets cut (bloat candidates:
  chrome, redundant notes, low-value stats)
- Constraints to carry: keep Panel/FullView architecture and component
  names; QML changes must not alter engine call sites; every fit claim
  needs a rendered check (reuse the harness approach from #55 — it was
  good machinery even if the visual direction isn't shipping)
- Explicit non-goals: no wholesale surface replacement, no new navigation
  model, keep the compact popup + full window split

## Risks

- **Settings overflow returns** at small window sizes (accepted; list in PR).
- **Stale-panel clobber** if the #52 carry is missed — the test scenario
  exists precisely to catch it.
- **Feature discoverability**: new engine features have no UI until U3 —
  acceptable per user direction; README config table is the surface.
- **nixfred's pending contributions** may assume the new UI — check open
  PRs after U1 lands; the fork stays free to carry the dashboard.

## Sequencing

U1 → merge → U2 (tag from master) → U3 brainstorm when user is ready.
