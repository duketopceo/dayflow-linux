# Plan: classic UI polish round 2 — live-feedback batch

**Created:** 2026-10-14
**Origin:** live walkthrough feedback on the running panel (2026-10-13/14),
plus the standing contract in
`docs/brainstorms/2026-10-05-classic-ui-polish-requirements.md` — classic layout
is the base; tighten, don't surface-new; compact and neat, not huge and empty.

## Summary

A second polish round on the classic popup, driven by the user's live
walkthrough of the merged week-grid work. Nine concrete fixes across six
surfaces: the week grid must be readable without hovering and clicks must
produce a visible result; the header drops the model-name status; the bottom
chrome replaces square "MD"/"MINI" buttons with real labeled controls; the
Standup tab gets renamed (to "Recap"), pruned (no Blockers, no draft editor),
and its workflow bars get hover detail; Chat and expanded-Today gain useful
right-side content instead of stretched dead space; Agents progress stops
stalling at 13% and refresh visibly refreshes; Settings compresses cost knobs
to one line and keeps local models collapsible.

## Problem Frame

The product is "in a very usable state" but the walkthrough surfaced a batch
of legibility, affordance, and clutter problems:

1. **Week grid unreadable at rest.** "It probably needs to default to a higher
   zoom because I just cannot read any of that." Hover-expand works but
   reading requires hovering every block.
2. **Week clicks look dead.** "I can't click on it, that's a problem." Code
   analysis: `dayClicked → jumpToDay → pendingSpanTs → spanSelected` does
   fire, but in the compact popup the `SpanCard` it selects sits below the
   360px Flickable fold, and clicking today's own column changes nothing
   visible at all — day data is already that day. The selected-column wash is
   subtle. Net effect: click does nothing the user can see. Related: "I'm
   looking at Tuesday, it's already shown Wednesday" — while inspecting
   another column the day pane still shows today, which reads as the click
   landing on the wrong day.
3. **Bottom chrome is slop.** "Square buttons with things like MD and M-I-N-I
   in it… like slop from a third grader." `DayPane.qml:137-138` ships
   `CopyButton` squares labeled "Copy day · md" / "Copy day · mini".
4. **Header noise.** The `recording · <model>` caption (`Panel.qml:1446`) is
   information nobody acts on — remove it. The recording dot + pause control
   carry that state already.
5. **Standup tab: right content, wrong frame.** Name disliked; "Blockers" and
   the "Standup draft" editor never used; empty Yesterday/Today rows should
   collapse; the daily-workflow bars are liked but "if I hover on the colored
   bars I don't see anything" — `DailyWorkflowGrid` slots are bare Rectangles
   with no hover, though every `WorkflowSlot` carries `time`, `title`,
   `category`, `productive` (`engine/daily.go:12-17`).
6. **Chat stretches.** In expanded mode `ChatTab` has no layout response — the
   thread spans the full 1080px width with dead space on the right.
7. **Agents stalls at ~13% and refresh is invisible.** Engine emits `decode`
   progress *before* each session's `Turns()` decode
   (`engine/agents_briefing.go:295`), so one fat transcript parks the bar at
   ~15% for minutes; `polish` is a single long model call at 80%. Pressing R
   queues behind the running process with no visible acknowledgment.
8. **Expanded Today right side is "totally trash".** `DayPane` is squeezed
   into ~38% of the expanded row (`TodayTab.qml:91-96`) — filter chips, a
   cramped timeline, dead space below.
9. **Settings cost sprawl.** Pricing/routing/cap fields spread over multiple
   lines in the Advanced group; local models should stay reachable but
   collapsed.

## Requirements

### Week grid

- **R1.** The week grid is readable without hovering at both popup densities —
  raise default hour height and card typography minimums; a block tall enough
  for a line of text shows a line of real text.
- **R2.** Clicking any week block produces an unmistakable visible result:
  the day pane switches to that day AND the selected span's detail card is
  scrolled into view with a transient selection flash. Clicking today's own
  column visibly selects the span.
- **R3.** While inspecting a non-viewed column, the day pane's date label makes
  the viewed day unambiguous (it already shows the date — verify it reads
  correctly after a jump; fix if the label lags).

### Chrome

- **R4.** The `recording · <model>` header caption is removed. Recording state
  remains visible via the status dot and the Pause button.
- **R5.** Bottom copy controls become descriptive labeled buttons (no "MD"/
  "MINI" abbreviations), styled with the existing `PanelButton` kit — same
  density, no new chrome rows. Copy failure is surfaced: a non-zero proc
  exit or empty export writes `dayflow.notice` instead of failing silently
  ("buttons at the bottom of today fail" was observed without any feedback).

### Recap (renamed Standup)

- **R6.** The tab and section title read "Recap" (single constant so the name
  is a one-word change). Internal ids (`standup` tab id, procs, files) are
  unchanged.
- **R7.** Yesterday/Today cards render only when they have entries; when both
  are empty the existing one-line empty state remains.
- **R8.** The "Blockers" section and the "Standup draft" editor are removed
  from the UI. Engine-side `standup save` stays untouched.
- **R9.** Hovering a daily-workflow slot shows `time · title · category` in a
  readout line (same pattern as the week-grid hover readout).

### Chat / Agents / expanded surfaces

- **R10.** In expanded mode Chat gains a right column — the conversation list
  always visible plus a compact "today at a glance" strip — so the thread
  stops spanning full width. Compact mode unchanged.
- **R11.** Agents progress never parks silently: the engine emits progress at
  a finer grain during session decode (or the loader shows an elapsed-time
  heartbeat within the phase), and refresh visibly restarts the bar with a
  "refreshing…" state.
- **R12.** The expanded-Today right column is rebuilt: selected-span detail
  and the day's card list fill the height — no dead space below a cramped
  stack.

### Settings

- **R13.** Storage/cost controls compress to one line (caps + "currently
  using" readout merged; pricing/routing stay in Advanced but tightened).
- **R14.** Local models move into (or remain in) a collapsible group —
  reachable, never primary.

## Key Decisions

- **Scroll-to-detail, not popover** for click feedback — the Flickable
  already exists; animating `contentY` to the `SpanCard` plus a flash reuses
  the selection model instead of adding a second detail surface. Rejected:
  in-grid expansion (rejected last round — column layout churn).
- **"Recap" as the new name** — content is yesterday+today+workflow, not a
  meeting ritual. Held behind a constant; "Daily" or "Briefing" drop in
  cleanly if the user vetoes.
- **Delete, don't hide**, the Blockers section and draft editor — "never use
  that" is a product verdict; hiding preserves the maintenance surface.
- **Heartbeat + finer-grained engine events** for Agents — emit `decode`
  progress *after* each session completes (progress = work done, not work
  started) and surface elapsed seconds in the loader label so a long phase
  reads as working, not frozen. Refresh sets a visible resetting state even
  when queued.
- **Right-rail pattern** for expanded Chat/Today — one shared narrow context
  column rather than per-tab bespoke splits.

## Existing patterns to reuse

- `PanelButton` (`PanelButton.qml`) — already used in footer/filter chips;
  the copy buttons migrate onto it.
- Hover readout Text under `WeekCalendar` in `TodayTab.qml` — the exact
  pattern for workflow-bar hover (R9).
- `SettingsGroup` collapsible sections — local models slot into one.
- `pendingSpanTs`/`spanSelected`/`spanSelected` flash — extend, don't replace.
- `LoadingBar` with `estimate` — keep for Agents; add elapsed label.

## Implementation Units

| Unit | Scope | Files |
|------|-------|-------|
| U1 | Week grid legibility defaults: raise `hourH` baselines per density, bump min card type sizes, retune title gates | `WeekCalendar.qml`, `TodayTab.qml`, `WeekPane.qml` |
| U2 | Click → visible selection: Flickable scrolls `SpanCard` into view on `spanSelected`, transient flash, verify day-label correctness | `TodayTab.qml`, `DayPane.qml`, `WeekCalendar.qml`, `WeekPane.qml` |
| U3 | Header/footer chrome: remove model caption, copy buttons → `PanelButton` with real labels + failure notice | `Panel.qml`, `DayPane.qml`, `CopyButton.qml` |
| U4 | Recap tab: rename, hide empty cards, delete Blockers + draft editor, workflow hover readout | `Panel.qml` (tab label), `StandupTab.qml`, `DailyWorkflowGrid.qml` |
| U5 | Agents progress: post-decode events in engine, elapsed heartbeat in loader, visible refresh state | `engine/agents_briefing.go`, `AgentBriefingLoader.qml`, `AgentsPane.qml` |
| U6 | Expanded right rails: Today right column rebalance + Chat context column | `TodayTab.qml`, `ChatTab.qml`, (opt) new `ContextRail.qml` |
| U7 | Settings: storage/cost one-line, local models collapsible | `Settings.qml` |
| U8 | Harness scenarios + live verify: expanded-chat/recap/settings scenarios, click-jump fixture, sync + `omarchy-restart-shell` | `tests/ui/fit.qml`, `tests/ui/verify-fit.py` |

## Dependencies / sequencing

- U1 → U2 (legibility first so selection flash lands on a readable grid).
- U3/U4 independent; U4's draft removal frees `draftSaveProc` consumers —
  keep the proc for the engine command, just unwire the UI.
- U5 is the only unit touching Go — land after UI units so `go test` noise
  doesn't gate visual fixes.
- U8 last, plus live sync; remember the stale-instance gotcha — verify with
  `omarchy-restart-shell`, not just the file-watcher reload.

## Risks

- **Scroll-to-detail timing** — `spanSelected` fires on `spansChanged`; the
  Flickable needs `contentY` update after layout settles (`Qt.callLater` or a
  `Behavior`). If the scroll fights user scrolling, gate it to jump-originated
  selections only.
- **Heartbeat lying** — an elapsed counter must reset per run and clearly
  derive from real phase events; never animate fake progress.
- **Rename drift** — "Standup" appears in copy strings beyond the tab label
  ("Copy standup" button, draft strings). Sweep all user-visible instances;
  keep internal identifiers.
- **Right-rail width** — a 38% rail that just re-squeezes DayPane recreates
  the "trash" problem; the rail must get its own content, not a compressed
  clone.

## Test scenarios

- Compact popup, populated week: every block ≥2 gridline-rows tall shows a
  readable title line; no fragment-only labels.
- Click a Saturday block → day nav shows that date, detail card scrolled into
  view, selection flash visible. Click a today block → detail selects +
  scrolls without a day jump.
- Recap tab: fixture with empty yesterday → only Today card renders; both
  empty → single empty-state line; no "Blockers"/"draft" text anywhere.
- Workflow grid: hover a mid-morning slot → readout shows `HH:MM · title ·
  category`.
- Expanded mode: Chat shows right column; Today right rail fills height.
- Agents: stalled-decode fixture (long gap between decode events) shows
  elapsed time; refresh visibly restarts.
- Settings: cost line single-row; local models group collapsed by default.
- `tests/ui/verify-fit.py` zero-failure, zero QML warnings, plus new
  scenarios for each rule above; live reload via `omarchy-restart-shell`.

## Out of scope

- Flow parity, Fable, standalone desktop packaging, provider redesign.
- Engine-side standup/draft data model changes.
- Any new chrome rows or surfaces beyond the named fills.
