# Plan: rebrand + visual refresh — "actual care, not a reskin"

**Created:** 2026-10-11
**Origin:** user ask — "local dayflow screenshot. find original repo for that
too. needs rename. refresh, and actual care to it. ce plan"

## Context and provenance

- Upstream: `JerryZLiu/Dayflow` (macOS, Swift, ~7.2k★, **no LICENSE** — the
  port must never imply affiliation or reuse its assets/brand).
- This repo: `duketopceo/dayflow-linux`, MIT, a **clean-room port** (not a
  GitHub fork) that has diverged far past upstream: Omarchy/Hyprland
  integration, agents briefing across 5 coding agents, CLI/TUI/MCP, FTS5
  search, forecasts, per-app segments.
- Current identity: display name `Dayflow`, plugin id
  `io.github.duketopceo.dayflow`, engine binary `dayflow`, data dir
  `~/.local/share/dayflow/`, version 1.6.0.

## Decision: what "rename" means

The **product name** should change — it currently borrows upstream's brand on
a different platform while claiming none of its code. Recommendation:

- **Rename**: display name + README title + repo name + panel header + docs.
- **Keep**: plugin id `io.github.duketopceo.dayflow` (breaking it orphans
  every installed `shell.json` reference and the live plugin dir; it's
  already `duketopceo`-namespaced, not upstream's brand), engine binary
  `dayflow` (systemd units, scripts, MCP registrations, muscle memory), and
  data dir. The engine keeps the name internally; the product gets a new
  face. Document the split in README ("the engine binary stays `dayflow`").
- **Name candidates** (pick before U1): `Tideline`, `Dayprint`, `Lifelog`,
  `Chronline`. Recommend `Tideline` — evokes timeline/journal, no known
  collisions in the Linux-productivity space, reads well in a bar widget.
- Repo rename via `gh repo rename` keeps git redirects; update `origin` and
  any docs links after.

## Design audit (live panel, 2026-10-11)

Screenshot: compact popup on the Today tab.

1. **WeekCalendar labels unreadable** — every block truncates at ~6 chars
   (`Managi…`, `Develo…`, `Review…` are indistinguishable). The main data
   surface answers almost nothing at a glance.
2. **Hierarchy inverted** — the week heatmap dominates; the day detail
   (`DayPane`, the actually-useful part) sits below the fold in the Flickable.
3. **Header is bare** — dot + `Dayflow` + `recording · gemma-4-31b-it` +
   boxy `Expand`/`Pause` buttons. No GNOME-card structure like the fan/power
   panels now use.
4. **Footer crams** `81 frames · 0 pending · 10.1 GB · ignoring Screensaver`
   plus `⋯` — fine density, but no visual grouping.
5. **States**: paused/onboarding/error/install surfaces exist but aren't
   styled as a family.
6. **Tab bar**: text `PanelButton`s — workable but flat; no icons, low
   affordance on first open.

Shared vocabulary already exists (`PanelCard`, `PanelButton`, `PanelChip`,
`StatRow`, `LoadingBar`, `PanelSeparator`, `dayflow.fgFill/accentFill/
payloadFill`). The refresh composes these; it does not invent a new system.

## Units

**U1 — Rename mechanics.** Pick the name (see candidates). Update
`manifest.json` `name`/`description`/`barWidget.displayName`, README title +
identity paragraph ("a Linux-native work journal; the engine binary stays
`dayflow`"), panel header string, `InstallPrompt`/`Onboarding` copy, docs
strings. `gh repo rename` + retarget `origin`. Explicit non-goals: plugin id,
binary name, data dir, DB schema, MCP name.

**U2 — Header + footer.** Header becomes a hero row consistent with the
fan/power panels: product wordmark (new name), a single status chip
(`recording` / `paused` / `setup needed` with the dot colored by state), and
`Expand`/`Pause` as `PanelButton`s. Move the model name out of the subtitle
(it's settings detail, not status). Footer groups facts into a left cluster
(`frames · pending · storage`) and a right cluster (`ignoring …` + `⋯`),
using `StatRow` spacing.

**U3 — Today tab hierarchy.** Compact view: `DayPane` first (the day's
story), `WeekCalendar` demoted to a compact context strip below it. Fix the
week grid's labels: drop inline text for spans under a width threshold
(colored cells + dot for category), keep labels only where they fit, rely on
the existing hover readout line for detail. Selected day column gets the
accent border already used by `sel` cards.

**U4 — States as a family.** Pause/onboarding/error/install/empty surfaces
share one `PanelCard` + icon + single-line-copy + single-action pattern
(e.g. paused shows the same card as onboarding, just different copy + a
Resume action). Removes the current ad-hoc mixes of bare Text blocks.

**U5 — FullView + AgentsTab consistency.** Apply the same header/card/state
treatment inside the expanded window so compact and full views read as one
product. Agents briefing cards keep their status palette but adopt
`PanelCard` shells and the same spacing rhythm.

**U6 — Verification.** `tests/ui/verify-fit.py` scenarios stay green (extend
to cover the new header/states); `qmllint`/parser pass; live screenshots via
the new IPC (`quickshell ipc call io.github.duketopceo.dayflow open` →
`grim`) for each tab and each state, reviewed against this plan's audit list.

## Already landed (enabler, uncommitted in tree)

- `Panel.qml`: `ipcTarget` + `IpcHandler` (`open/show/close/hide/toggle`)
  matching the duketopceo plugin convention — the panel is now
  script-openable for screenshots/tests. File-watcher reload does **not**
  recreate live widgets; a shell restart is required to register the target.

## Risks / non-goals

- Do not reuse upstream's icon, name, or marketing copy (no license upstream).
- Do not change `weekDaySpans`/engine data shapes — refresh is presentation.
- Do not regress the 40-scenario fit harness or the capture-pause semantics
  (pause affects capture, not UI).
- Repo rename keeps redirects but CI/release URLs in `scripts/install.sh`
  must be updated in the same change or install breaks.
