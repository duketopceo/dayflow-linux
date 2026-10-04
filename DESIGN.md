# Dayflow for Linux: Design Spec (Plan B, v2 UI/UX + assets)

Status: queued research spec. Not scheduled. No code changed.
Date: 2026-10-02. Scope: plugin v1.5.0 (`manifest.json`), master @ `3b5abaa`.
Method: `refero-design` research-first workflow + `design-taste-frontend` anti-slop
gates. Refero MCP was not connected, so the reference layer is web research with
cited URLs (section 4) plus a code audit of every surface and the live Omarchy
shell token system (`/usr/share/omarchy/shell/Commons/{Color,Style}.qml`).

---

## 0. Design brief

```text
Designing: the Dayflow plugin UI (bar widget, popup panel, Full View window,
  onboarding, settings, notifications) plus CLI/TUI/MCP text output and a
  custom asset set, for one power user on Omarchy (Hyprland, Quickshell shell).
Goal: glance at "what did I do today" in under 2 seconds; correct and export it
  in under 30.
Tone: instrument, not app. A quiet, dense, keyboard-first log that looks like
  it shipped with Omarchy and re-skins itself with every theme.
Main risk: a dashboard-in-a-popup. Too many tabs, too many pill buttons,
  colors that ignore the theme.
Must remember: the day as a vertical ribbon of colored time, read like a
  terminal log, with the screen recording behind every block.
Constraints: QML on Quickshell, qs.Ui kit, Style/Color singletons, monospace
  font from `omarchy font set`, 30+ themes incl. light ones, 2x HiDPI + 1x
  ultrawide, no Electron, no web views.
Path: audit + reference lock + spec. Build is a later plan.
```

Design read (taste skill 0.B): *Omarchy-native utility tool for a single expert
user; dials VARIANCE 3 / MOTION 3 / DENSITY 8.* High density, low motion, low
layout variance. Nothing decorative earns a place unless it encodes time.

---

## 1. Product + stack (what exists)

- **Engine**: one Go binary `dayflow` (`engine/`), systemd user units, SQLite
  journal at `~/.local/share/dayflow/`. Captures every 10 s via `grim`, dedupes,
  summarizes 15-min blocks with a vision model, classifies with Jev, merges
  consecutive same-activity blocks into cards. Exposes CLI (`--json` on every
  query), a Bubble Tea TUI (`engine/tui.go`), an MCP server (`engine/mcp.go`),
  and `notify-send` notifications (`engine/notify.go`).
- **UI**: an Omarchy shell plugin (`manifest.json`, kind `bar-widget`,
  id `io.github.duketopceo.dayflow`). 21 QML files, ~8.3k lines, all at repo
  root. The UI shells out to `dayflow ... --json` through `Quickshell.Io.Process`.
- **Theme plumbing available but under-used**: `Color` exposes `foreground`,
  `background`, `accent`, `urgent`, `muted` plus per-surface roles from
  `~/.local/state/omarchy/current/theme/shell.toml`; `Style` exposes
  `space()`, `cornerRadius` (mirrors Hyprland rounding), state alphas
  (`normalFillAlpha` 0.04, `hoverFillAlpha` 0.08, `selectedFillAlpha` 0.18,
  `pressedFillAlpha` 0.22), and a type scale (`caption` 10 to `displayLarge` 28
  at base 12). The theme's `colors.toml` also carries a full ANSI-like hue set
  (`red yellow orange green cyan blue magenta brown`, plus `selection`,
  `lighter_background`, `dark_foreground`), which the plugin never reads.

## 2. Surface audit

Verdicts: **keep** (works, polish only), **rework** (right job, wrong form),
**redo** (rebuild), **cut** (merge elsewhere).

| # | Surface | File(s) | What it does now | Friction found | Verdict |
|---|---|---|---|---|---|
| S1 | Bar widget | `BarWidget.qml` | `WidgetButton` with glyph `󰚯` (recording) / `ᛯ` (paused, a runic letter), 45% opacity when engine missing, 60 s status poll, right-click toggles pause | Paused glyph is an arbitrary Unicode rune, not a Nerd Font icon; no visible difference between "recording healthy" and "capture stalled" (the most important state, per README "silence means data loss"); no use of `Color.bar.active` which Omarchy reserves for recording indicators; 60 s poll lags a pause click | **rework** |
| S2 | Popup panel shell | `Panel.qml` (1619 lines) | 540 px / 780 px expanded popup; header (dot + name + model), 6 text tabs (`today standup chat week agents settings`), footer of pill action buttons + stats line | Six tabs in a popup is a dashboard-in-a-popup. Settings and Chat in a 540 px popup are cramped. Footer has up to 6 pill buttons (screenshot 2026-09-15) that read as a button wall. Header model name ("gemma-4-31b-it") is engine trivia in prime position | **redo** |
| S3 | Today timeline (popup) | `TodayTab.qml`, `TodayPane.qml`, `DayNavRow.qml` | Card list: time range, duration, app, category pill, `edit`, bold title, 4-line summary, left category bar | Every card is equal weight, so a 15-min idle block costs as much space as a 2 h deep-work card. Summaries truncated mid-sentence. `⚡` emoji for productive, bare `?` in hardcoded amber for low confidence. No visual axis of time: gaps are invisible | **redo** |
| S4 | Standup | `StandupTab.qml`, `DailyWorkflowGrid.qml` | Draft fields, copy, 15-min grid | Good job, wrong home: a tab. `✓` text glyph | **rework** (becomes a sheet/command) |
| S5 | Chat | `ChatTab.qml` | Q&A over journal | Unlikely to be used at 540 px; competes with MCP + CLI which do this better | **cut from popup**, keep in Full View |
| S6 | Week | `WeekTab.qml`, `WeekPane.qml` | Week list with colored bars, donut, treemap, trends | Popup week view is a flat text list; analytics only legible in Full View | **rework**: popup gets a 7-day strip only |
| S7 | Agents | `AgentsTab.qml`, `AgentsPane.qml`, `AgentBriefingLoader.qml` | Workstream briefing, status chips with hardcoded RGB | Hardcoded status colors (blue/green/red/gray) ignore theme; strong content, weak hierarchy | **rework** |
| S8 | Full View window | `FullView.qml` | 840x560 min floating window, left rail (`Today Week Timelapse Context Agents`) | Right structure. Missing: search, a real time axis, keyboard nav, and it duplicates the popup instead of deepening it | **keep shell, redo panes** |
| S9 | Timelapse | `TimelapsePane.qml` | Frame playback (opt-in retention) | Hardcoded `"black"` background; playback disconnected from the timeline (cannot scrub from a card) | **rework**: becomes the scrubber under the ribbon |
| S10 | Context shifts | `ContextPane.qml` | Canvas flow of app switches | Hardcoded `Qt.rgba(0.8,0.8,0.85,0.9)` label color breaks light themes | **rework** |
| S11 | Settings | `Settings.qml` (946 lines), `SettingsField.qml` | Provider, model, presets, capture numbers, recaps, knowledge sync, categories, prompts, save/reload | Explicit Save/Reload (no live apply), 9 numeric capture fields shown to everyone, prompts editor in a popup. Reimplements fields instead of qs.Ui `TextField`/`NumberField`/`Dropdown`/`ToggleSwitch` (0 uses today) | **redo** (move to Full View, progressive disclosure, qs.Ui kit) |
| S12 | Onboarding | `Onboarding.qml` (658 lines) | 5 steps: welcome, provider, model + categories, recaps consent, result | Good consent copy. Steps are walls of text; no "what you'll get" preview; model picker before first value | **rework** |
| S13 | Install prompt | `InstallPrompt.qml` | One-click engine install | Hardcoded `#c06c60` error color instead of `Color.urgent` | **keep**, token fix |
| S14 | Calendar picker | `CalendarPicker.qml` | Month grid | `<` `>` ASCII arrows; no heat (days with data look like days without) | **rework**: heat-tinted days |
| S15 | Busy bar, copy button | `BusyBar.qml`, `CopyButton.qml` | Progress line, copy affordance | Fine | **keep** |
| S16 | Notifications | `engine/notify.go` | `notify-send -a dayflow title body`; classes stall/paused/recovered/standup/goal | No icon (`-i`), no urgency mapping (stall should be `critical`), no action buttons (e.g. "Resume"), no category hints for mako/swaync styling | **rework** |
| S17 | TUI | `engine/tui.go` (273 lines) | Bubble Tea + lipgloss, ANSI colors 12/8/15 | Uses fixed ANSI slots, so it already follows the terminal theme. Good. Lacks the ribbon | **keep**, add ribbon glyph row |
| S18 | CLI text output | `engine/main.go` `printTimeline` etc. | Plain lines | No color bar per category, no `NO_COLOR` statement | **rework** (small) |
| S19 | MCP output | `engine/mcp.go` | JSON tool results | Not a visual surface; keep stable. Only make field names match UI vocabulary (card/block/session) | **keep** |

Cross-cutting findings:

1. **Theme leak.** 30+ hardcoded `Qt.rgba` colors (`Panel.qml:668-678`
   category map, `AgentsPane.qml:29-37`, `TodayTab.qml:320`,
   `ContextPane.qml:109`, `TimelapsePane.qml:85`, `InstallPrompt.qml:91`) plus
   engine defaults in `engine/weekly.go:461-471`. On `catppuccin-latte`,
   `flexoki-light` or `white` several will fail contrast.
2. **Kit bypass.** The plugin uses `WidgetButton`, `Panel`, `PanelSeparator`
   and nothing else from `qs.Ui`; every button, toggle, field and tab strip is
   hand-rolled, so they drift from Omarchy's state tokens.
3. **Glyph soup.** `⚡ ? ✓ × ‹ › < > • ! ᛯ 󰚯` mixed from four sources.
4. **Equal-weight cards** hide the shape of the day; time is printed, never drawn.
5. **Two parallel apps** (popup and Full View) with overlapping tabs.

## 3. Asset inventory

| Asset | Path | Current | Verdict |
|---|---|---|---|
| Bar glyph (recording) | `BarWidget.qml:123` | Nerd Font `󰚯` | **redo** as part of the glyph set (A2) |
| Bar glyph (paused) | `BarWidget.qml:123` | `ᛯ` runic letter | **redo** (A2) |
| App icon | none | no `.desktop` icon, no SVG, nothing for notifications or the Full View window class | **missing** (A1) |
| Marketplace preview | `preview.png` (560x786) | Sept screenshot of old popup over another window, green border from a past theme | **redo** (A5) |
| Social card | `docs/assets/social.png` (1280x640) | Emoji sun `☀️` as logo, mono wordmark, olive accent | **redo** (A4). Emoji-as-logo is a slop tell |
| Fonts | none bundled | inherits `Style.font.family` (`monospace` alias, JetBrainsMono Nerd Font) | **keep**. Never bundle a font; follow `omarchy font set` |
| Category palette | `Panel.qml:666`, `engine/weekly.go:461` | 11 fixed Tailwind-like hexes | **redo** as theme-mapped tokens (section 6) |
| Status palette (agents) | `AgentsPane.qml:29` | 4 fixed RGB | **redo**, map to theme |
| Empty-state art | none | text only | **missing** (A6), but keep it typographic |
| Notification icon | none | `notify-send` without `-i` | **missing** (A1 reuse) |
| Illustrations | none | none | **keep none**. This tool doesn't need illustration |

### New custom assets (briefs)

**A1. App mark ("the ribbon")**
- Concept: a vertical capsule split into 4 to 5 stacked segments of uneven
  height, one segment offset by 1 unit. It reads as a day ribbon, a film strip
  and a bar chart at once. No sun, no clock, no eye/camera (privacy-anxious
  imagery is wrong for a screen recorder).
- Deliverables: `assets/icon/dayflow.svg` (1-color, `currentColor`, 24-unit
  grid, 2-unit stroke-free shapes), `dayflow-symbolic.svg` (16 px hinting),
  `dayflow-256.png` for the `.desktop` file and notifications, and a full-color
  variant that pulls 4 category tokens at build time.
- Rules: renders in a single theme color in the bar; must survive 14 px; no
  gradients; corner radius on the capsule follows a fixed 2-unit value (not
  `Style.cornerRadius`, since icons can't change per theme).

**A2. Glyph set (12 icons, one family)**
- Source: Nerd Font Material Design glyphs (`nf-md-*`) only, because the
  font is already loaded via `Style.font.family` and the bar uses the same
  family. One family, one weight.
- Map: recording `nf-md-record_circle_outline`, paused `nf-md-pause_circle_outline`,
  stalled `nf-md-alert_circle_outline`, engine missing `nf-md-download_circle_outline`,
  productive `nf-md-lightning_bolt_outline` (replaces `⚡`), low confidence
  `nf-md-help_circle_outline` (replaces `?`), edit `nf-md-pencil_outline`,
  copy `nf-md-content_copy`, prev/next `nf-md-chevron_left/right`, search
  `nf-md-magnify`, agent session `nf-md-robot_outline`, timelapse
  `nf-md-play_box_outline`.
- Deliverable: `Glyphs.qml` singleton with named constants; ban raw
  codepoints in panes. Exact codepoints are verified at build time with
  `fc-query`.

**A3. Category swatch set**: not image assets, a token table (section 6.2)
plus a 12-chip SVG sprite generated from the active theme for README use only.

**A4. Social card (1280x640)**: the ribbon mark (A1) at left, wordmark
`dayflow` in the system mono, tagline "Your day, read back to you." Right half:
a real (anonymized fixture) day ribbon rendered from `engine/fixtures.go`
data, not a mock. Background = theme `background`, rendered with 2 themes
(`vantablack`-like dark, `flexoki-light`) to prove theming.

**A5. Marketplace preview (`preview.png`)**: native screenshot of the new
popup at scale 2 on a clean wallpaper, with fixture data, no other windows.
Plus a 3-frame set: popup, Full View Today, Full View Week.

**A6. Empty and error states**: typographic only. One line of mono text and
one dim ribbon outline glyph (A1 outline variant). Copy is specific:
"No blocks yet. First summary lands at 10:15." Never a cartoon.

## 4. Reference research and lock

Caveat: Rewind's desktop app has been discontinued (now Limitless), and Rize
and Screenpipe publish little concrete UI detail. The Dayflow findings come
from its SwiftUI source and are the most reliable.

### 4.1 Dayflow macOS (upstream)
- Cards have a **6 pt colored accent bar** on the left edge, radius 2 (nearly
  square), a 0.25 pt border, and **height scaled by duration**. Failed cards
  get a dashed red border.
  [CanvasActivityCard.swift](https://github.com/JerryZLiu/Dayflow/blob/main/Dayflow/Dayflow/Views/UI/CanvasActivityCard.swift)
- The detail panel has an editable title, a time badge, a category pill with
  a dot, uppercase section labels, and a timelapse thumbnail with a play
  overlay.
  [ActivityCard.swift](https://github.com/JerryZLiu/Dayflow/blob/main/Dayflow/Dayflow/Views/UI/MainView/ActivityCard.swift)
- There are 4 default categories: Work `#B984FF`, Personal `#6AADFF`,
  Distraction `#FF5950`, and Idle `#A0AEC0`. Idle is system-managed.
  [TimelineCategory.swift](https://github.com/JerryZLiu/Dayflow/blob/main/Dayflow/Dayflow/Models/TimelineCategory.swift)
- **Review mode:** each card is rated by swipe or arrow key (right means
  focused, left distracted, up neutral). It supports undo, shows a `3/12`
  counter, and ends on a split bar.
  [TimelineReviewOverlay.swift](https://github.com/JerryZLiu/Dayflow/blob/main/Dayflow/Dayflow/Views/UI/TimelineReviewOverlay.swift)
- Fonts are bundled: Figtree for the UI and Instrument Serif for display. The
  look is pastel and cream, with a flower logo.
  [Fonts](https://github.com/JerryZLiu/Dayflow/tree/main/Dayflow/Dayflow/Fonts),
  [dayflow.so](https://dayflow.so)
- From HN feedback: "weird to have to wait 30 minutes, just show me at least
  a card". [HN 45361268](https://news.ycombinator.com/item?id=45361268)
- **Steal:** the accent bar, duration-scaled rows, arrow-key review with
  undo, and a sample card on first run.
- **Avoid:** the cream/orange palette, bundled fonts, the flower mark (that
  is upstream's brand), and growth prompts during onboarding.

### 4.2 Rewind / Limitless
- One hotkey opens search and the timeline together. Search does the real
  work, and the reviewer found scrolling far back "cumbersome".
  [freshvanroot review](https://freshvanroot.com/blog/review-rewindai/)
- **Steal:** `/` opens search over the timeline, with app filter chips.
- **Avoid:** making scrubbing the only way to navigate.

### 4.3 Timing
- The timeline is made of colored blocks. You select a range to create an
  entry and drag an activity onto a project to assign it. With ⌥ held, the
  drag also creates a rule, which Timing says cuts manual sorting by 80–90%.
  [Quick start](https://timingapp.com/help/quick-start?lang=en),
  [Projects and rules](https://timingapp.com/help/3-projects-and-rules)
- **Steal:** "assign and remember" as one gesture. Here that means `r` on a
  re-categorized row, which also writes a classification instruction.
- **Avoid:** rules that only apply going forward without offering a backfill.

### 4.4 Rize
- The Focus Quality Score combines 20+ signals. Session review asks you to fix
  categories first, then shows the score together with context switches and
  the apps that interrupted most.
  [Features](https://rize.io/features/productivity),
  [Session timer](https://rize.io/changelog/the-rize-session-timer)
- **Steal:** correct the categories before showing any score, and show the
  number next to the interruptors that explain it.
- **Avoid:** an opaque single score.

### 4.5 ActivityWatch
- The developers say advanced visualization "is not a priority".
  [docs](https://docs.activitywatch.net/en/latest/features/user-interface.html)
- Users ask for a **vertical, day-by-day layout**, because the horizontal
  timeline hides activity behind popovers.
  [forum](https://forum.activitywatch.net/t/new-timeline-view-with-a-vertical-layout-of-docs-files-and-pages-active-during-the-day/4637)
- Category colors used to be inconsistent across views (since fixed).
  [forum](https://forum.activitywatch.net/t/unified-category-colors-in-the-activity-and-timeline-view/2775)
- **Steal:** an "uncategorized" filter.
- **Avoid:** horizontal popover timelines, raw app names, and colors that
  change per view. This is why one ribbon color system is used everywhere.

### 4.6 Screenpipe
- The timeline scrolls like a DVR. Clicking a moment shows the frame and its
  text. Search covers OCR, accessibility text and audio, with app, window,
  URL and date filters.
  [docs](https://docs.screenpi.pe/search-screen-history)
- You can select a time range and use it as AI context.
  [0.14 post](https://screenpipe.com/blog/screenpipe-014)
- **Steal:** select a range on the ribbon, then "Ask about this range",
  which feeds the existing chat. This is a P2 ceiling.

### 4.7 Linear and Things 3
- **Linear:** chrome is quiet and aligned on both axes. Themes are generated
  in LCH from 3 inputs (base, accent, contrast), down from 98 variables, with
  contrast pushed up for readability. Almost everything is reachable by
  keyboard, and menus teach the shortcuts.
  [Redesign](https://linear.app/now/how-we-redesigned-the-linear-ui),
  [Invisible details](https://linear.app/now/invisible-details)
- **Things 3:** type anywhere to jump ("Type Travel"), hold a modifier to
  show a shortcut sheet, and items unfold in place to keep you oriented.
  [MacStories](https://www.macstories.net/reviews/things-3-6-reimagines-external-keyboard-control-on-ipad/),
  [Things](https://culturedcode.com/things/)
- **Steal:** derive every color from theme inputs, use `?` for a shortcut
  overlay, and expand rows in place.

### 4.8 Omarchy theming
- `colors.toml` is the source for every themed config. Its keys are `mode`,
  `accent`, `selection`, `muted`, 4 background tiers, 4 foreground tiers, and
  `red yellow orange green cyan blue magenta brown` plus `bright_*`. User
  themes live in `~/.config/omarchy/themes`, and `light.mode` is an optional
  flag file.
  [Manual: making a theme](https://learn.omacom.io/2/the-omarchy-manual/92/making-your-own-theme),
  [tokyo-night colors.toml](https://raw.githubusercontent.com/basecamp/omarchy/master/themes/tokyo-night/colors.toml)
- `shell.toml.tpl` maps palette roles onto shell tokens: font base-size,
  spacing scale, and state alphas (0.04 / 0.08 / 0.18).
  [shell.toml.tpl](https://raw.githubusercontent.com/basecamp/omarchy/master/default/themed/shell.toml.tpl)
- The default font is JetBrainsMono Nerd Font, used as both the system and
  terminal font, and the user can change it.
  [Manual: fonts](https://learn.omacom.io/2/the-omarchy-manual/94/fonts)
- Verified locally: the active theme is at
  `~/.local/state/omarchy/current/theme/` (`colors.toml`, `shell.toml`). The
  QML singletons `Color` and `Style` in `/usr/share/omarchy/shell/Commons/`
  already parse it and live-reload it on theme change.

### 4.9 Reference lock

- **Dominant:** Dayflow macOS for **information architecture**: a duration-
  scaled timeline with an accent bar, a detail panel with the timelapse, and
  review mode.
- **Visual language:** Omarchy shell for everything visual (theme roles, mono
  type, square-by-default radius, state alphas). Upstream's visual brand is
  explicitly not adopted.
- **Narrow borrows:**
  - Linear: density, LCH-style contrast correction, and the keyboard model.
  - Things: in-place expansion and type-to-jump.
  - Rewind/Screenpipe: search-first navigation and the frame scrubber.
  - Timing/Rize: the week as side-by-side columns, assign-and-remember, and
    fixing categories before showing a score.
- **Conflict resolved:** upstream's pastel, rounded, pill-heavy look versus
  Omarchy's flat mono style. Omarchy wins on all visual tokens. Upstream wins
  only on structure.

## 5. Direction: "Ribbon Log"

One sentence: **a terminal log with a time axis: a thin vertical ribbon of
category color on the left, dense mono rows on the right, and the screen
recording one keypress behind every row.**

Signature traits (preserve, do not soften):

1. **The ribbon.** A 6 px vertical bar whose segment heights are proportional
   to real minutes, colored by category, gaps shown as gaps. It appears in
   the bar tooltip (horizontal mini), popup (vertical), Full View (vertical,
   with hour ticks), Week (7 columns side by side, Timing/Rize style), TUI
   (block characters `▁▂▃▅▇` row) and CLI (a `█` prefix per row).
2. **Rows, not cards.** One line per merged activity by default:
   `09:15  1h 45m  ▌ Refactoring capture loop    Code · Ghostty`. Expand on
   select to show the summary, app breakdown and frames. Linear/Things density.
3. **Theme is the brand.** Dayflow has no fixed color. Accent, category hues,
   surfaces and borders are all theme roles. The only constant is the ribbon
   shape (the logo).
4. **Keyboard-first.** `j/k` rows, `h/l` days, `/` search, `e` edit, `space`
   preview frames, `c` copy, `s` standup, `p` pause, `?` help. Mirrors
   Omarchy's vim-ish defaults and the existing TUI.

Anti-goals (taste skill section 9): no glass, no gradient fills, no emoji,
no big rounded "cards with shadows", no 3-up stat tiles with fake trends, no
donut as the hero, no em-dash-heavy marketing copy in the UI, no sun logo.

### Decision ledger

| Decision | Choice | Source |
|---|---|---|
| Primary timeline form | Proportional vertical ribbon + compact rows | Dayflow accent-bar cards (4.1), Timing blocks (4.3), ActivityWatch vertical-layout requests (4.5) |
| Card weight | Duration-weighted; blocks under 15 min collapse into the ribbon only | Dayflow duration-scaled cards (4.1), Linear density (4.7) |
| Search | Global `/` across journal + agent sessions, results jump to time | Rewind/Screenpipe search-first model |
| Frames | Scrubber strip under the selected row, not a separate pane | Rewind scrubber, Screenpipe timeline |
| Color | Category hue = theme ANSI hue role, not a fixed hex | Omarchy `colors.toml` |
| Popup scope | Today only + 3 actions + 7-day strip; everything else in Full View | Things 3 "quick entry vs main window" split, Omarchy panel kit |
| Settings home | Full View only, live apply, progressive disclosure | Linear settings, taste skill 4.6 |
| Status in bar | Use `Color.bar.active` while recording; urgent glyph on stall | Omarchy `shell.toml` `[bar] active` contract |

## 6. Design system tokens

All tokens live in one new `Tokens.qml` (plugin-local singleton) that reads
only from `Color`, `Style` and the theme `colors.toml`. No pane may reference
a literal color.

### 6.1 Surface + text

| Token | Maps to | Fallback |
|---|---|---|
| `df.bg` | `Color.popups.background` (popup), `Color.background` (Full View) | `#101315` |
| `df.raised` | `colors.toml lighter_background` | `Util.alpha(fg, 0.04)` |
| `df.sunken` | `colors.toml dark_background` | `Util.alpha(bg, 1) darker 1.1` |
| `df.fg` | `Color.foreground` | |
| `df.fgDim` | `Color.muted` | `Util.alpha(fg, 0.6)` |
| `df.fgFaint` | `colors.toml dark_foreground` | `Util.alpha(fg, 0.35)` |
| `df.border` | `Color.popups.border` at `Style.normalBorderAlpha` | |
| `df.hair` | `Util.alpha(fg, 0.08)` | |
| `df.accent` | `Color.accent` | |
| `df.selection` | `colors.toml selection` | `Util.alpha(accent, Style.selectionFillAlpha)` |
| `df.recording` | `Color.bar.active` | `Color.urgent` |
| `df.danger` | `Color.urgent` | |
| state fills | `Style.normalFillAlpha` / `hoverFillAlpha` / `selectedFillAlpha` / `pressedFillAlpha` | 0.04 / 0.08 / 0.18 / 0.22 |

### 6.2 Category hues (theme-mapped)

| Category | Theme key | Rationale |
|---|---|---|
| coding | `blue` | matches current mental model (blue = code) |
| writing | `green` | |
| communication | `orange` | |
| meetings | `yellow` | |
| browsing | `magenta` | |
| design | `bright_magenta` then `red` | |
| media | `red` | |
| personal | `brown` | separates from urgent red |
| system | `cyan` at 0.6 | |
| other | `muted` | |
| idle | `fgFaint`, rendered as a gap with hatch | idle is absence, not a color |
| failed | `urgent` outline only, no fill | |

Rules: ribbon fill uses hue at 0.85 alpha; row tag uses hue text on hue at
0.14; never hue text on hue fill. Custom category colors from config still
override. Engine `engine/weekly.go` default hexes become fallback only, and
`--json` emits a `color_role` next to `color` so the UI can prefer the role.
Contrast gate: every hue against `df.bg` at least 3:1 for the ribbon, 4.5:1
for tag text; if a theme fails, darken/lighten toward `fg` in OKLCH until it
passes (computed once on theme change).

### 6.3 Space, radius, size

- Spacing: only `Style.space(n)` with n in {2, 4, 6, 8, 12, 16, 24, 32}.
- Row height: `Style.font.body * 2` (24 px at base 12). Expanded row:
  auto, max 6 lines of summary.
- Ribbon width: `Style.space(6)`; hour tick: `Style.hairline`.
- Radius: `Style.cornerRadius` for containers (0 on most Omarchy themes,
  so the plugin goes square when the theme does); rows and ribbon segments
  are square always. Chips use `min(Style.cornerRadius, 4)`.
- Popup width: keep `Style.space(540)`; drop the 780 expanded mode (Full View
  replaces it).
- Full View min stays 840x560; target layout at 1280x800 and the 3440-wide
  G8 (two-column: ribbon+rows | detail).

## 7. Typography

- Family: `Style.font.family` only (the `monospace` alias, currently
  JetBrainsMono Nerd Font). One family everywhere, as Omarchy does. Never
  bundle or hardcode a font.
- Scale (from `Style.font`): `caption` 10 for ticks and meta, `bodySmall` 11 for
  summaries, `body` 12 for row titles, `title` 14 for the day header,
  `display` 24 for the single hero number (focused hours today). Nothing
  else. Today's code uses `subtitle`/`title` 7 times total; fine.
- Numerals: monospace gives tabular figures for free; durations always
  `1h 45m`, never `1.8h` (current `fmtHours` returns `"1.8"`).
- Times: follow locale 24h/12h. Default to 24h on Omarchy (matches the bar
  clock); ranges as `09:15–11:00`.
- Weight: regular for body, bold only for the row title of the selected
  row and the day header. No italics.
- Case: sentence case labels, lowercase meta (`recording · 3 pending`).

## 8. Motion

MOTION_INTENSITY 3. Motion only explains state change.

| Event | Motion | Duration / easing |
|---|---|---|
| Row expand/collapse | height + summary opacity | 140 ms, `Easing.OutCubic` |
| Day change (`h/l`) | rows cross-fade, ribbon slides 8 px in travel direction | 160 ms |
| New block lands (live) | segment grows from 0 height at ribbon tail | 240 ms, OutCubic, once |
| Recording dot | none. A static dot. Pulsing record dots are anxiety UI | — |
| Stall | glyph swaps to alert, no blink | instant |
| Hover | fill alpha change only | 80 ms |
| Popup open | inherit Omarchy panel animation; add nothing | — |

Respect a `reduced_motion` config flag and Hyprland `animations:enabled = false`
(read once via `hyprctl getoption`): all durations go to 0.

## 9. Screen plan

Each item lists the **first pass** (what a single focused PR ships) and the
**supreme ceiling** (where it goes if we keep investing).

### P0: identity and theming (ship first, smallest diff, biggest effect)

**P0.1 Tokens + theme mapping.** `Tokens.qml`, `Glyphs.qml`, purge all
literal colors, engine `color_role`.
- First pass: every hardcoded color replaced; verified on `vantablack`,
  `catppuccin-latte`, `flexoki-light`, `everforest`, `tokyo-night`.
- Ceiling: OKLCH contrast correction per theme, theme preview in Settings
  showing the ribbon in the next theme before switching.

**P0.2 Bar widget.**
- First pass: new glyph set; `Color.bar.active` while recording; urgent
  alert glyph on stall (engine `status --json` gains `stalled`); tooltip
  `Recording · 4h 12m today · last block 10:15`; status poll drops to 10 s
  when the popup is closed but a toggle just happened.
- Ceiling: optional tiny horizontal ribbon (24x4 px) of today in the bar.

**P0.3 Popup = Today only.**
- Layout: header row (`Today · Thu 2 Oct` left, `4h 12m` right, pause toggle
  icon), ribbon + rows list, footer with exactly three actions (`Summarize now`,
  `Copy today`, `Open Dayflow`), and an overflow `…` menu for ignore-app/copy
  week/copy mini.
- Kills: Standup/Chat/Week/Agents/Settings tabs in the popup. Standup becomes
  an action (`s`), Week becomes a 7-day mini strip above the footer.
- First pass: rows with expand, ribbon, keyboard nav.
- Ceiling: hover a ribbon segment to preview the middle frame of that block
  (if `playback` on).

**P0.4 App mark A1 + glyphs A2 + `.desktop` + notification icon.**

### P1: Full View becomes the app

**P1.1 Today pane**: three columns at ≥1280 px: ribbon with hour ticks |
rows | detail (summary, app breakdown, edit fields, frame scrubber). Compact
fallback (<980) stacks detail under the row.
- Ceiling: drag ribbon edges to re-time a block; multi-select rows to merge.

**P1.2 Search (`/`)**: one field across journal FTS5 + agent FTS, results grouped
by day, enter jumps to the row with frames open.
- Ceiling: Rewind-style "find the moment" with frame thumbnails in results.

**P1.3 Week pane**: 7 vertical ribbons side by side (Timing/Rize style) with
category totals as a single stacked bar, not a donut. Treemap and donut move
to an "Analytics" sub-view.

**P1.4 Settings in Full View**: sections `Capture`, `Model`, `Privacy`,
`Categories`, `Agents`, `Advanced`. Live apply with inline validation,
qs.Ui `TextField`/`NumberField`/`Dropdown`/`ToggleSwitch`. Advanced hides the
9 numeric capture fields and prompt editors.

**P1.6 Review mode** (upstream parity, sec. 4.1/4.4): `R` in Full View
steps through today's unreviewed rows. First the category is fixed (`1-9`),
then the row is rated with `←` distracted / `→` focused / `↑` neutral. `u`
undoes the last rating and the header shows a `3/12` counter. The pass ends
on a one-line split bar and only then shows the focus number.
- Ceiling: assign-and-remember (`r`) writes a classification rule and offers
  to backfill past days.

**P1.5 Notifications**: `-i dayflow`, `-u critical` for stall, `-A resume=Resume`
on paused-too-long, `-h string:category:dayflow.<class>` so mako/swaync can
style them. Copy stays event-class only (privacy rule already in `notify.go`).

### P2: depth

**P2.1 Agents pane**: workstreams as rows with the same ribbon language
(session spans on the day axis); status chips mapped to `accent`/`green`/
`urgent`/`muted`. Ceiling: upstream AgentPlayback 24h dial as an optional view.
**P2.2 Timelapse merged into the frame scrubber**; standalone pane removed.
**P2.3 Onboarding rework**: 3 steps (Where does the model run → Privacy
summary with the two toggles that matter → "First block lands at HH:MM"
live countdown). First pass already shows a fixture-day ribbon
behind the countdown, because upstream testers balked at an empty first run
(sec. 4.1). Ceiling: an immediate "summarize the last 5 minutes now" block so
the first real row appears within the session.
**P2.4 TUI + CLI ribbon**: `dayflow today` prints a `█` category bar per row
(ANSI theme slots, honors `NO_COLOR`); TUI header gets a one-line ribbon.
**P2.5 Social card A4 + preview set A5**, rendered from fixtures in 2 themes.
**P2.6 Calendar heat**: days tinted by tracked hours (accent alpha ramp).

## 10. Quality gates (before any of this ships)

- Screenshot matrix: popup + Full View Today/Week x {vantablack,
  catppuccin-latte, flexoki-light, tokyo-night} x {eDP-1 scale 2, DP-3 scale 1}.
- `grep -nE 'Qt\.rgba\([0-9.]+|"#[0-9a-fA-F]{3,8}"' *.qml` returns nothing
  outside `Tokens.qml`.
- `hyprctl` / omarchy-shell logs show no Dayflow QML warnings.
- Keyboard-only walkthrough of P0.3 and P1.1 with no mouse.
- Taste-skill section 9 tell scan: no emoji in UI, no em dashes in UI copy,
  no fake numbers, no gradient fills.

## 11. Open questions

1. **Brand independence.** ROADMAP says upstream owns the brand. Is a new
   mark (A1) acceptable, or should the port keep a neutral "Dayflow for
   Linux" wordmark and defer to Jerry's icon? (Spec assumes a distinct mark
   that does not imitate upstream's.)
2. **Popup tab cut.** OK to remove Chat/Settings/Standup/Agents from the
   popup entirely, or keep a slim `More` route?
3. **24h vs 12h default.** Current UI is 12h (`4:45 PM`); Omarchy clock is
   user-configured. Follow the bar clock format?
4. **Category hue mapping** for themes missing `brown`/`bright_*` keys: fall
   back to hue rotation of `accent`, or to the engine hexes?
5. **Frames in the UI** require `playback on` (10 GB cap). Should the
   scrubber be the default and make retention opt-out, or stay opt-in and
   show a locked state?
6. **Category model.** Upstream ships 4 categories (Work/Personal/Distraction/Idle); the port has 11. Keep 11 for the ribbon but let review mode collapse to upstream's focused/distracted/neutral axis?
7. **Repo layout.** 21 QML files at root. Move to `ui/` with `qmldir` as
   part of P0, or leave the marketplace-expected layout alone?
