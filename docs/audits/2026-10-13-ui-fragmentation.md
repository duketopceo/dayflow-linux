# UI fragmentation audit (2026-10-13)

Base: `master` @ `235254e4` (+ this PR). Scope: the 9.7k-line root QML set.
Read first: `DESIGN.md` (S1-S19, section 6 tokens), `docs/plans/2026-09-18-001`
(UI polish), `2026-10-05-002` (popup polish; U1 shared components),
`2026-10-11-001` (week calendar). Open work this audit deliberately does not
duplicate: **#71** (theme-token `ContextPane`/`InstallPrompt`), **#62**
(program roadmap docs), **#59** (pop-out switch).

Line numbers are against the base commit unless marked "now".

## 1. Surface map

Manifest exposes exactly one entry point: `manifest.json` ->
`BarWidget.qml` (`kind: bar-widget`). Everything else hangs off it.

| Entry | Chain | Notes |
|---|---|---|
| Bar glyph | `BarWidget.qml:119` `WidgetButton` | left-click popup, right-click pause |
| Popup (540/780 px) | `BarWidget` -> `Panel.qml` (root type `Panel` from `qs.Ui`, id `dayflow`) | chrome at `Panel.qml:1400+`; tab Loader `Panel.qml:1582-1588` |
| Popup tabs | `today`->`TodayTab`, `standup`->`StandupTab`, `chat`->`ChatTab`, `agents`->`AgentsTab`->`AgentsPane`, `settings`->`Settings` (tab strip `Panel.qml:1519`) | no `week` tab since a89f7787 |
| Popup gates | `InstallPrompt` (engine missing), `Onboarding` (not configured) | `Panel.qml:1582-1583` |
| Full View window | `BarWidget.qml:115` loads `FullView.qml` (`FloatingWindow`); rail `FullView.qml:18-24`; pane Loader `FullView.qml:470-474` | sections today/week/timelapse/context/agents |
| Full View panes | `TodayPane`, `WeekPane`, `TimelapsePane`, `ContextPane`, `AgentsPane` | `host` = FullView, `host.dayflow` = Panel |

Data shown more than once (same payload, different widget):

| Data | Where | Verdict |
|---|---|---|
| Day card timeline | `DayPane` hosted by `TodayTab` (x2: compact + expanded), `TodayPane`, `WeekPane` | single component already, but hosted by **3 wrappers** |
| Week 7-day grid | `TodayTab` (compact card, expanded split) and `WeekPane` hero; each re-implemented `jumpToDay()` (`TodayTab.qml:23`, `WeekPane.qml:32`) and a verbatim 12-line hover readout (`TodayTab.qml:77,126`, `WeekPane.qml:75`) | **fixed**: `WeekHero.qml` |
| Day switcher | `DayPane.qml:29-109` (prev, label, next, back-to-today, Cal) **and** `DayNavRow` stacked above it in `TodayPane.qml:28` -> FullView > Today showed two navs | **fixed**: `TodayPane` drops `DayNavRow`; it remains for Timelapse/Agents (no `DayPane`) |
| Week stats/charts | `WeekStats.qml` (popup, from `dayflow.insights`) vs `WeekPane.qml:~155-520` (FullView; stat strip, trends, heatmap, donut, top apps, summary from `dayflow.weeklyPayload`) | **open**: ~390 + ~700 lines, two data sources (`insights week` vs `weekly`), not byte-equivalent; merging needs a payload decision (see 5) |
| Standup text | `StandupTab` draft + `DailyWorkflowGrid` 15-min grid; `WeekStats` "Copy week" buttons | different data, fine |
| Copy/summary actions | `CopyButton` used by `DayPane:140`, `WeekStats:25`; Panel footer builds its own buttons (`Panel.qml:1632-1671`) | inconsistent, see 4 |

## 2. Dead / orphaned files

Reference check = instantiated (`Name {`) or `source:` string outside the file.

| File | Status |
|---|---|
| `PanelChip.qml` (44) | **dead**: only a probe in `tests/ui/fit.qml`. Removed, probe removed |
| `StatRow.qml` (35) | **dead**: same. Removed |
| `WeekTab.qml` | already gone (a89f7787 renamed to `WeekStats`) |
| `WeekPane.qml` | **live** (FullView `week` rail entry), despite "Week merged into Today" |
| `AgentsTab.qml` (32) | thin wrapper over `AgentsPane` for the popup; live |
| `BarWidget`, `FullView`, `Panel`, `Onboarding`, `InstallPrompt`, `Settings`, `TodayTab`, `StandupTab`, `ChatTab`, `ContextPane`, `TimelapsePane`, `AgentsPane`, `TodayPane` | reached only via `manifest`/`source:`/Loader strings, so a plain `grep Name {` shows none; all live |

`docs/plans/2026-10-05-002` records `PanelChip`/`StatRow` as U1 deliverables
that later units were meant to adopt; the adoption never landed (0
call sites on master). If a unit needs them, recreate from git.

## 3. Hard-coded colours and sizes vs theme

`Style.space()` is used 476x, `Style.font.*` 267x, `Style.cornerRadius` 60x:
sizing and type are largely tokenised. Colour is not:

| Where | What |
|---|---|
| `Panel.qml:762-772` | 11-colour category palette as `Qt.rgba` literals (also `engine/weekly.go:461-471` defaults, `DESIGN.md` S6.2) |
| `AgentsPane.qml:34-42` | agent status + insight palette (blue/green/red/gray/purple/orange) |
| `AgentsPane.qml:312-321` | source-badge tints (2 more literal pairs) |
| `SpanCard.qml:117` | productive/low-confidence amber `Qt.rgba(0.95,0.70,0.15)` |
| `TimelapsePane.qml:86,110,133` | `"black"` / `"white"` |
| `InstallPrompt.qml:91`, `ContextPane.qml:109` | covered by **#71**, untouched here |
| `SettingsField.qml:36,39,47` | re-derives `Qt.rgba(Color.foreground,...)` fills that `PanelCard well` already owns (0.04/0.12) |
| `DayTimeline.qml:106,125,158`, `WeekCalendar.qml:102,147,201,327`, `LoadingBar.qml:42,50` | white-RGBA *fallbacks* when `pal` is null; harmless, but 9 copies of the pattern |
| glyphs | `⚡` `SpanCard.qml:98,249`; `‹ ›` `DayPane.qml:37,71`, `DayNavRow.qml:15-17`; `×` `Settings.qml:634`, `Panel.qml:1677`; `•` `AgentsPane.qml:265` (DESIGN "glyph soup") |

`Tokens.qml`/`Glyphs.qml` from DESIGN section 6 still do not exist. Not added
here: a `pragma Singleton` needs a `qmldir` the Omarchy plugin loader may not
honour for plugin-local files, and the offscreen harness stubs `qs.*` only.
`Panel` already acts as the token carrier (`fgFill`, `accentFill`,
`categoryColor`, `payloadFill`, `Panel.qml:760-840`), passed as `pal`. The
low-risk path is to lift those into a plain `QtObject` (`Palette.qml`) that
`Panel` instantiates, which is also what a standalone app needs.

## 4. Inconsistent components

- **Card literal.** `Rectangle { radius: Style.cornerRadius; color: fgFill(0.04); border.color: fgFill(0.08) }` was hand-written 19 times with identical values beside an existing `PanelCard`. **Fixed**: now `PanelCard` in `StandupTab` (4), `WeekPane` (6), `WeekStats` (6), `TodayTab`, `DailyWorkflowGrid`, `SpanCard` (root). Remaining hand-rolled variants (hover/selected colour logic, different borders) were left: `Settings.qml:585,604,928`, `ChatTab.qml:274,342`, `Onboarding.qml:345,360`, `AgentsPane.qml:285`, `FullView.qml:343`, `DayPane.qml:29,62,105`.
- **Buttons.** `PanelButton` exists (19 uses) but small buttons are still hand-built Rectangle+MouseArea: `DayPane.qml:29,62,105` (prev/next/Cal), `DayNavRow.qml:20`, `Panel.qml:1456,1487,1715,1742` (footer pills), tab strip `Panel.qml:1519-1530`, `CalendarPicker` arrows. Each re-implements hover alpha, border alpha and disabled opacity (0.4 vs PanelButton's 0.45).
- **Kit bypass** (DESIGN cross-cutting #2): only `WidgetButton` from `qs.Ui` is used for real; `import qs.Ui` appears in 23 files, mostly unused.
- **Panel.qml (1795 lines)** is state + engine IO + chrome: state/`Process` runners lines 1-1400, palette helpers 760-840, popup chrome 1400-1795. FullView and every pane reach into it through `dayflow`/`host.dayflow`.

## 5. Recommended follow-ups (not in this PR)

1. **Week analytics merge**: choose one of `insights week` / `weekly` payload, then render a single `WeekStats` in popup and FullView (removes ~390 lines from `WeekPane`).
2. `Palette.qml` (plain `QtObject`) from `Panel.qml:760-840`; adopt `Color.*` roles for status/category (overlaps Ribbon P0.1 and #71; coordinate).
3. Move the small hand-rolled buttons onto `PanelButton`; unify disabled opacity.
4. Split `Panel.qml` after the Devin session's edits to it land (it was being edited in the main clone while this PR was written).
5. Retire `AgentsTab` by letting the Loader point at `AgentsPane` with a host adapter.

## 6. Reuse by the standalone desktop app

Omarchy/Quickshell-only (must be adapted or shimmed): `BarWidget.qml`
(`WidgetButton`, `qs.Ui`), `Panel.qml` (root type `Panel`, `Color`, `Quickshell.Io`
`Process`), `FullView.qml` (`FloatingWindow`, `Color.background`).
Host-agnostic given a `dayflow`/`pal` object exposing the Panel API:
`PanelCard`, `PanelButton`, `WeekHero` (new), `WeekCalendar`, `DayPane`,
`DayTimeline`, `SpanCard`, `LoadingBar`, `CopyButton`, `TodayPane`, `WeekPane`,
`DayNavRow`, Settings* components. All of them still import `qs.Commons`
(`Style`, `Color`) and several `Quickshell.Io`; a stub `qs.Commons`, as
`tests/ui/verify-fit.py` already provides, is enough for the pure-view set.
