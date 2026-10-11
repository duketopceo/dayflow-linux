# Plan: real week calendar view — app icons, hover-expand, FullView parity

**Created:** 2026-10-11
**Origin:** user request — "fix that week view a lot. much prettier. what apps at
what times blocked out. just like dayflow main. expand to see more detail. full
view to see the most detail. also need full view full update too."

## Summary

Rebuild the week presentation on the macOS Dayflow main-view pattern
(`WeekTimelineGridView.swift` in JerryZLiu/Dayflow): a 7-column grid whose blocks
are real activity cards — app icon + wrapped title + time range — that
hover-expand to their natural height and click through to a detail surface with a
per-app breakdown. The same component scales through three density tiers: compact
popup, expanded popup, and FullView's week hero — the deepest tier adds frame
thumbnails and the per-app activity breakdown the engine already emits. FullView
gets the parity pass the same work makes cheap: week hero grid + detail pane, and
its Today pane moves to the shared card timeline.

## Problem Frame

The current `WeekCalendar` grid renders category-colored slivers: a block's
title appears only when a crude size gate allows, so most of the week reads as
unlabeled color. The user's words: "it shows category not literal doing this in
this and this in this" and now "much prettier needed — what apps at what times
blocked out." Two structural gaps:

1. **Blocks carry no app identity.** The data supports it — every card and child
   block has `app`/`app_name`, children carry per-app `activities[]`, and
   `pal.appIcon(cls)` already resolves themed icons via `Quickshell.iconPath` —
   but the grid never draws it.
2. **No density tiers.** One fixed 16px/hour scale serves the 540px popup and the
   full window alike. macOS Dayflow draws at 111px/hour with hover-expand; ours
   needs tiers, not one size.

FullView compounds it: its Week pane is analytics-only (no grid at all) and its
Today pane still renders a hand-rolled Repeater instead of the card timeline.

## Requirements

- **R1.** Week-grid blocks render as activity cards: dominant-app icon, title
  wrapped to as many lines as the block's measured height allows, time range —
  never an unlabeled sliver when space permits a line of text.
- **R2.** Multi-app identity: a card whose children span multiple apps shows the
  dominant app icon plus a secondary icon (macOS Dayflow's primary/secondary
  favicon pattern, applied to app icons).
- **R3.** Hover-expansion: hovering a block raises it and grows it to its
  natural content height (bounded), mirroring the reference's "expand requires
  intent" feel; pointer leave collapses.
- **R4.** Click selects the day/span and opens detail: compact popup keeps the
  card-below pattern; expanded popup and FullView show a side detail pane.
- **R5.** Detail shows the per-app breakdown — each child block's app icon +
  title + time — the literal "what apps at what times" answer, plus a frame
  thumbnail from the nearest captured frame to the span start.
- **R6.** Three density tiers on one component: compact (~16px/hr), expanded
  (~26px/hr), FullView (~64px/hr) — content visibility scales with height.
- **R7.** FullView week section becomes a grid hero (big equal columns) with the
  detail pane beside it; existing analytics sections stay below.
- **R8.** FullView Today pane uses the shared card timeline — the hand-rolled
  Repeater goes.
- **R9.** Axis continues to start at the week's first recording (shipped
  `cf07c979`); the 4am→4am convention is rejected (see KTD3).
- **R10.** Harness fixtures exercise multi-app children, hover states, all three
  densities, and the FullView layout; `verify-fit` stays at zero failures.

## Key Technical Decisions

### KTD1 — One `WeekCalendar` component, density-prop-driven content

Rather than three grids, `WeekCalendar` gains `hourH` (already added) plus a
derived `density` tier: `< 20px/hr` compact, `< 40` expanded, `>= 40` full. The
block delegate branches on tier: compact draws icon + 1-line title; expanded adds
a second wrapped line + time; full adds app name, multi-line title, secondary
icon, and child-segment dividers. Rationale: the layout math (columns, axis,
positioning, selection) is identical across tiers — duplicating it in three
components guarantees drift. Alternative considered: separate `WeekGridFull.qml`
for FullView — rejected, same drift argument the DayPane extraction already made.

### KTD2 — Hover-expand via measured natural height, z-raised

Port the reference pattern: each block keeps a hidden `Text` measuring natural
title height; on hover the block's height animates to `min(natural, remaining)`
and `z` raises above neighbors; on leave it collapses back to the duration
height. Implemented inside the existing per-block MouseArea (already tracks
`containsMouse` for the readout). No new state surface — the parent only sees
`hoveredSpan` as today. Rejected alternative: click-to-expand inline — the user
asked "expand to see more detail," and the reference's answer is hover-growth +
click-select; duplicating both gestures on one target matches it.

### KTD3 — Keep first-recording axis, not the reference's 4am→4am

The reference fixes `startHour 4, endHour 28` (a rolling workday). Our
already-shipped axis starts at the week's first recording floored to the
gridline — which the user explicitly requested ("start from first recording
whichever that may be") and which adapts to nights-with-data and weekends alike.
No change; recorded here so the parity pass doesn't silently re-litigate it.

### KTD4 — Detail = per-child rows, shared `SpanDetail.qml`

`SpanCard` becomes/renames to `SpanDetail.qml` (a rename keeps `panel_carry_test`
and imports honest) and gains an apps section: one row per child block —
`appIcon(child.app)` + child title + `start–end` — sourced from
`span.children`. Cards merge contiguous blocks precisely because the app/topic
drifted; children are where "what apps at what times" actually lives. The same
component renders in all three placements (compact below-timeline, expanded side
pane, FullView side pane). Frame thumbnail: a `framesProc` on the panel fetches
`dayflow frames <viewDateStr> --json` (existing command); the detail picks the
frame nearest `start_ts` — TimelapsePane already proves `file://` frame loading.

### KTD5 — FullView week = hero grid + side detail, analytics demoted below

FullView's `week` section becomes a two-column pane: `WeekCalendar` at full
density (left, ~65%) + `SpanDetail` pane (right, ~35%) with the frame thumbnail
and app breakdown. The existing forecast strip, stat strip, heatmap, and
breakdowns remain but render below the hero. `FullView.qml` gets a `framesProc`
and forwards `selectedSpan`/`dayOffset` the same way the popup panel does — the
pane-level API (`host.dayflow`) already exists.

### KTD6 — No engine work; the payloads are already sufficient

`week --json` emits cards with children incl. `activities[]`; `frames --json`
lists `{ts, path, exists}`; `get_week` MCP already returns the segmented grid.
Everything the prettier view needs is in-process data + one new Process per
selected day. If a detail surface later wants thumbnails for *past* days without
the frames CLI, that's an engine gap — flagged, not built.

## High-Level Technical Design

### Block anatomy by density

```text
compact (≤20px/hr)          expanded (~26px/hr)         full (≥40px/hr)
┌──────────────┐            ┌───────────────┐           ┌────────────────┐
│▌● title…      │           │▌● title wraps │           │▌● title wraps  │
└──────────────┘            │  2:30–3:15 PM │           │  to 3 lines    │
  accent+icon+1ln           └───────────────┘           │  2:30–3:15 PM  │
                             accent+icon+2ln+time       │  foot · +icon2 │
                                                        │  ─child seams──│
                                                        └────────────────┘
                                                          accent+icons+3ln
                                                             +time+app+seams
```

`▌` = category accent edge (payloadColor), `●` = dominant app icon. Child seams
(full tier only): hairlines inside tall cards at each child block's start, so a
merged card still reveals "the apps changed here."

### Expand + select flow

```text
hover block ──> hoveredSpan set (readout line, unchanged)
          └──> block.z++, height→min(naturalHeight, columnBottom)  [KTD2]
leave      ──> collapse to duration height, z--, hoveredSpan=null
click      ──> dayClicked(dayIndex, span)  [existing signal]
             ├── popup: dayOffset jumps; SpanDetail appears below timeline
             └── full/expanded: side pane fills from selected span
detail     ──> SpanDetail: icon+title+time+summary+edit (existing)
                        + per-child app rows (KTD4)
                        + frame thumbnail when frames exist for the day
```

### FullView week layout

```text
┌─ rail ─┬──────────── week grid (~65%) ────────┬── detail pane (~35%) ──┐
│ Today  │ Mon Tue Wed Thu Fri Sat Sun          │  [frame thumbnail]     │
│ Week◄──│ ┌──┐┌──┐┌──┐┌──┐┌──┐┌──┐┌──┐          │  title + summary       │
│ Timelap│ │▌ ││▌ ││▌ ││▌ ││▌ ││▌ ││▌ │         │  per-child app rows    │
│ Context│ └──┘└──┘└──┘└──┘└──┘└──┘└──┘         │  productive/category   │
│ Agents │ ...hourH≈64px, equal columns...      │  (SpanDetail)          │
└────────┴──────────────────────────────────────┴────────────────────────┘
            forecast strip · stats · heatmap · breakdowns  (below, scroll)
```

## Implementation Units

### U1. WeekCalendar card blocks — icons, wrapped titles, density tiers

**Goal:** Blocks render as activity cards with app icon, wrapped title, and time
at every density the grid supports.

**Requirements:** R1, R2, R6, R9, R10

**Dependencies:** none

**Files:**
- `WeekCalendar.qml` (rework block delegate; add `density` tier from `hourH`)
- `tests/ui/fit.qml` (fixture: multi-app children, varying durations incl. slivers)

**Approach:**
- Add `readonly property string density: hourH >= 40 ? "full" : (hourH >= 24 ? "high" : "low")`.
- Block delegate becomes a small card: category accent edge, `Image` bound to
  `pal.appIcon(modelData.app)` (`visible: status === Image.Ready`, themed icons
  already resolve), title `Text` with `maximumLineCount`/`wrapMode` derived from
  measured available height (reference formula: `(height - paddings - reserved) /
  lineHeight`, min 1), and at high/full density a dim `start–end` caption and
  `appName`.
- Secondary icon: compute the card's second-most-common `child.app` when it
  differs from `modelData.app`; render a second small icon at full density.
- Failed/processing spans keep their current styling path (`payloadFill` on
  error categories) — add the reference's dimmed-failed treatment only if the
  data exposes a failure marker; otherwise defer.
- Keep `clip: true`, `hoveredSpan`, `dayClicked`, `viewStart`, `todayScale`,
  `selectedIndex` contracts unchanged — callers don't change.

**Test scenarios:**
- Harness: dense week fixture — every block at low density still shows icon
  (or title if icon unresolved); no overflow past column bounds.
- Multi-app card: secondary icon appears at full density, hidden at low.
- Sliver (<13px): icon-only or accent-only render, no layout failure.
- Screenshot-level: no text paints outside block bounds (clip verified by
  fit harness bounds check).

### U2. Hover-expand + click-select in the grid

**Goal:** Hovering a week block grows it to natural height above its neighbors;
clicking selects day+span and drives the detail surface.

**Requirements:** R3, R4

**Dependencies:** U1

**Files:**
- `WeekCalendar.qml` (hover-expand inside block delegate)
- `tests/ui/fit.qml` (hover scenario if the harness can simulate; else geometry
  only — expanded bounds must not count as overflow)

**Approach:**
- Each block owns a hidden natural-height `Text` sibling measuring the wrapped
  title; `onContainsMouseChanged` animates `height` toward
  `min(natural, cal.height - y)` and bumps `z` to top; on leave, animate back.
  Duration ~120ms, matching shell motion norms.
- Click stays `dayClicked(dayIndex, span)` — the compact popup keeps jumping the
  day timeline; expanded/FullView placements also set a `selectedSpan` the
  caller feeds to `SpanDetail`.
- Bounds-check discipline: expanded blocks must extend downward (below), never
  upward into prior columns' space — same as reference.

**Test scenarios:**
- Hovering a 2-line-truncated block reveals full title; bounds check treats the
  expanded height as in-column (harness must whitelist expanded-state blocks or
  assert only at rest).
- Clicking a block on day d fires `dayClicked(d, span)` — verified via uilog or
  a stub signal counter in the harness.
- Rapid hover in/out during animation leaves block at rest height.

### U3. SpanDetail — per-app breakdown + frame thumbnail

**Goal:** One detail component shows span summary plus the per-app "what at
what times" rows and a captured-frame thumbnail; used in all three placements.

**Requirements:** R4, R5

**Dependencies:** none (parallel to U1/U2)

**Files:**
- `SpanDetail.qml` (new — rename/extension of `SpanCard.qml`; check
  `panel_carry_test.go` for name-sensitive strings before renaming; keeping the
  `SpanCard` filename and adding the sections is the lower-risk path if the test
  greps the filename)
- `Panel.qml` (`framesProc`: `dayflow frames <viewDateStr> --json` →
  `dayflow.dayFrames`; run on span selection and on `dayOffset` change)
- `DayPane.qml` (swap `SpanCard` for `SpanDetail`)
- `tests/ui/fit.qml` (fixture with multi-app `children`, `dayFrames` stub)

**Approach:**
- Extend the existing card layout: below summary/edit, an "Apps" section maps
  `span.children` to rows — `appIcon`, child `title`, `start–end`, `minutes` —
  so a merged card's internal app switches are visible. Skip the section when
  the span has ≤1 child or no `children`.
- Thumbnail: pick `dayFrames` entry with `ts` nearest `span.start_ts` and
  `exists`; `Image` `source: "file://" + path`, `PreserveAspectCrop`,
  `sourceSize` ~2× display (TimelapsePane pattern). Hidden when the day has no
  frames — never a placeholder box.
- `framesProc` runs on selection change; ~50ms CLI, no loading UI needed.

**Test scenarios:**
- Multi-child span: apps section lists every child in order with icon+time.
- Single-child span: apps section hidden entirely.
- Frame nearest-miss: fixture with frames straddling the span start picks the
  closest `ts`; empty `dayFrames` hides the image (no broken-source warning).
- Harness: `procByName("framesProc")` stubbed; `dayFrames` present/absent cases.

### U4. FullView week hero — grid + detail pane, analytics demoted

**Goal:** FullView's week section shows the big grid with a live detail pane;
existing analytics move below.

**Requirements:** R6, R7

**Dependencies:** U1, U2, U3

**Files:**
- `WeekPane.qml` (restructure: hero row = `WeekCalendar` at `hourH ≈ 64`,
  `todayScale: 1.0`, full density + `SpanDetail` column; existing sections move
  below in the same Flickable)
- `FullView.qml` (`framesProc`, `selectedSpan`/`dayOffset` plumbing mirroring
  `Panel.qml`; day-click sets `dayOffset` + loads that day's timeline OR selects
  the week span directly — prefer week-span selection so detail doesn't refetch)
- `tests/ui/fit.qml` (FullView scenario — needs a `host` stub with
  `dayflow`/`forecast`; check how the harness currently stands up panes)

**Approach:**
- The pane is already a `Flickable` + `Column`; insert the hero `Row` at the
  top: `WeekCalendar` (`width: parent.width * 0.65`) + `SpanDetail`
  (`width: 0.35`), both fed `pane.dayflow`; then the existing analytics
  sections unchanged below.
- `SpanDetail` binds `host.selectedWeekSpan` (a `{dayIndex, span}` pair) —
  selection lives on the window so the grid and pane share it.
- `WeekCalendar.dayClicked` on this surface sets `host.selectedWeekSpan` rather
  than `dayOffset` — clicking a span opens its detail without reloading a day.
- Clicking the day header (or a `View day` affordance in the detail) can still
  jump `dayOffset` for the Today section — nice-to-have, flag in code.

**Test scenarios:**
- FullView harness case: hero row fits width, grid columns equal (todayScale
  1.0), detail pane present.
- Selecting a span in the grid populates the detail pane without a timeline
  proc run (assert no `timeline` command issued in stub).
- Analytics sections still render below the fold (bounds check passes).

### U5. FullView Today pane parity + harness coverage

**Goal:** FullView's Today section renders the shared card timeline with
selection/detail, matching the popup's quality.

**Requirements:** R8, R10

**Dependencies:** U3

**Files:**
- `TodayPane.qml` (replace hand-rolled Repeater with `DayTimeline` +
  `SpanDetail`; keep its LoadingBar and empty state)
- `tests/ui/fit.qml` (FullView-today scenario)

**Approach:**
- Swap the span Repeater for `DayTimeline` at a larger `pxPerMin`/`hourH` if the
  full window warrants it (expose a density prop if needed — prefer reuse at
  defaults first).
- Selection → `SpanDetail` below or beside (pane width allows beside; mirror
  the week hero's two-column shape for consistency).
- Keep `host.dayflow.spans`, `timelineLoading`, `viewDateStr` contracts.

**Test scenarios:**
- FullView-today renders cards (not list rows); click selects and detail shows.
- Empty day still shows the empty-state text.
- Harness: zero geometry failures for both FullView scenarios at 1280×800 and
  a narrow case (~900×700).

## Scope Boundaries

**In scope:** week-grid card rendering + icons + hover-expand; per-app detail +
frame thumbnail; FullView week hero and Today parity; harness coverage.

**Out of scope:**
- Website favicons — the engine doesn't capture URLs; app icons only.
- Video/timelapse playback inside the detail pane (a single thumbnail now;
  TimelapsePane already owns playback).
- Standalone-app port of these components (the U5 QML-port unit in the desktop
  plan consumes them as-is — injectable props were designed for it).
- Engine payload changes — none needed (KTD6).
- The `week` rail/section model in FullView — no IA change.

### Deferred to Follow-Up Work

- Failed/processing-card visual treatment if engine exposes a per-card failure
  flag worth surfacing in the grid.
- Hover-expand on `DayTimeline` day cards if the week version lands well.
- A "jump to day in Today section" affordance from the FullView week detail.

## Open Questions

- Hover-expand debounce: the reference tunes "snappy while expand requires
  intent" — likely wants a short (~150ms) intent delay before growth. Decide at
  implementation with feel, not a config.
- Should clicking a week block in the *compact* popup also select the span for
  the detail card, or keep day-jump only? Current plan: day-jump only (matches
  shipped behavior); span-select is the expanded/FullView gesture.
- Frame thumbnails at compact popup detail size — worth the space or
  expanded/FullView only? Default: show whenever `dayFrames` has a match; cheap
  to gate later.

## Risks & Dependencies

- **Icon coverage.** `appIcon(cls)` returns "" for unmapped WM classes — cards
  must degrade to accent-edge + title with no broken image (`Image.Ready` gate
  already spec'd). Risk of "still looks empty" for icon-less apps; mitigated by
  the category accent + secondary text.
- **Hover-expand geometry.** A block expanding past the column bottom could
  overlap the readout line or next section — the height clamp is to remaining
  column space, and `clip` on the column contains it.
- **255 children in fixtures vs real weeks** — real weeks showed 255 children /
  98 cards; grid repeater cost is fine (each day column is a bounded list).
- **`framesProc` churn** — fires on every span selection; the command is a
  local `readdir`, but selection-spamming could thrash — debounce if noticed.
- **panel_carry_test** — `SpanCard` rename risk: grep the test before renaming;
  safest path is extending `SpanCard.qml` in place (noted in U3).

## Sources & Research

- `WeekTimelineGridView.swift`, `WeekTimelineActivityCard.swift`,
  `CanvasTimelineDataView.swift` — JerryZLiu/Dayflow (open source). Config:
  `hourHeight 111`, `startHour 4 → endHour 28`, equal 7 columns, hover-expand
  with measured natural height, primary/secondary favicons, per-day card
  segmentation, dimmed future hour labels.
- `engine/store.go` `Activity`/`Block` — per-app `activities[]` on every child
  block (verified live: 98 cards / 255 children, 3 apps inside one card).
- `Panel.qml:410` `appIcon(cls)` — `Quickshell.iconPath` resolves WM class →
  themed icon path; `SpanCard.qml` already renders it at 14px.
- `TimelapsePane.qml` — `file://` frame `Image` pattern; `dayflow frames
  <date> --json` emits `{ts, path, exists}`.
- `get_week` MCP (`cf07c979`) — per-day segmented spans + `first_activity_min`;
  the QML `weekDaySpans` segmentation it mirrors is the grid's data path.
