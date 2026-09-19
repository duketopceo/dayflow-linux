---
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
execution: code
product_contract_source: ce-plan-bootstrap
title: "refactor: polish Dayflow's Omarchy-native UI and UX"
date: 2026-09-18
plan_type: refactor
origin: "docs/plans/2026-09-05-001-feat-dayflow-macos-parity-roadmap-plan.md"
---

# refactor: Polish Dayflow's Omarchy-Native UI and UX

## Summary

Dayflow Linux has the right product surfaces but uneven visual density and interaction feedback. Refine the bar widget, popover, and Full View so each has a clear job: status and launch, quick daily operation, and detailed analysis. The macOS Dayflow build remains an information-hierarchy reference only; this work preserves the compact, theme-driven Omarchy character rather than recreating SwiftUI/AppKit styling.

## Problem Frame

The current plugin has grown from a simple timeline into a five-tab panel and a multi-pane `FloatingWindow`. It uses Omarchy's `Style` and `Color` primitives consistently, but the individual surfaces were added in feature tranches. That leaves variable card padding, data-dependent popup heights, weak width budgets for long text, feedback that reads as an error even on success, and analytic layouts that can be cramped at the Full View minimum size.

The product already established the correct architecture: the bar widget is a glance-and-launch surface, the 540/780 px popup is an operational surface, and the separate Full View is the dense work surface. This plan improves those surfaces without adding a second design system, changing the Go data model, or widening the product scope.

## Scope

### In scope

- Audit the rendered widget, panel, and Full View against an Omarchy-native reference lock and a focused screenshot matrix.
- Normalize spacing rhythm, content hierarchy, card treatment, and text-overflow behavior across existing QML surfaces.
- Make the compact panel stable at both persisted widths and keep its header/capture controls visible while tab content scrolls.
- Improve Full View layout composition, including a defined small-window fallback for dense Week content.
- Make loading, success, warning, error, hover, focus, and disabled states visually distinct and truthful.
- Add reproducible QML/static and native-display validation guidance for this UI surface.

### Out of scope

- Copying macOS Dayflow's visual language, layouts, assets, or platform-specific interactions.
- New journal, analytics, provider, capture, or engine capabilities.
- A standalone GUI, web view, alternative chart stack, or a replacement Omarchy theme system.
- Broad rearchitecture of `Panel.qml` state/process ownership or Full View lifetime ownership.

## Design Direction

**Authority:** Dayflow macOS establishes the hierarchy of concise daily review, a dedicated workflow/standup surface, and a spacious analytical workspace. Omarchy establishes the visual language: direct utility, system-derived colors and typography, compact controls, and an opaque readable surface.

**Reference lock:**

- Preserve `Style.space`, `Style.font`, `Style.cornerRadius`, `Color.background`, and the existing category-color meanings.
- Use a deliberately compact spacing ladder: tight control gaps, normal row/card padding, and clear section breaks. Do not create visual separation with both a card border and an adjacent separator unless they serve different groups.
- Keep the bar glyph-first, panel scan-first, and Full View analysis-first.
- Use a small number of bounded cards. Avoid a dashboard of equally weighted bordered containers.
- Keep Full View opaque and owned by `BarWidget.qml`; these are functional constraints, not aesthetic choices.

## Key Technical Decisions

### KTD1. Refine existing Omarchy tokens rather than introduce local theme tokens

**Decision:** Apply a documented spacing and hierarchy contract with existing `Style`/`Color` helpers. Extract only a shared QML primitive where repeated behavior cannot stay consistent otherwise.

**Rationale:** The plugin already inherits the active Omarchy look. A parallel palette or typography scale would drift across themes and make the plugin feel less native.

### KTD2. Constrain content by surface responsibility

**Decision:** Keep the widget status-first, the popover focused on current-day review and actions, and detailed multi-column visuals in Full View.

**Rationale:** This follows the established Dayflow macOS hierarchy while respecting the Linux plugin's constrained popup geometry. Expanding the panel into a dashboard would duplicate Full View and worsen scanability.

### KTD3. Prefer responsive layout fallbacks over clipping or larger arbitrary gaps

**Decision:** Set explicit width budgets and elision for compact rows. At the Full View minimum geometry, use one-column or reduced-density variants where two-column cards or hourly grids stop being readable.

**Rationale:** The active display is large, but the window permits `840x560`; polished QML must be usable at supported sizes rather than relying on the maintainer's preferred geometry.

### KTD4. Add semantic feedback without changing engine contracts

**Decision:** Represent UI notices as neutral/progress, success, warning, and error within the QML state already owned by `Panel.qml` and `FullView.qml`. Preserve current subprocess calls and `ui:` action logging.

**Rationale:** A shared urgent-colored notice makes successful actions look broken. This is an interaction and trust problem, not a reason to introduce backend state.

## Implementation Units

### U1. Capture the UX baseline and write the surface contract

**Goal:** Establish the exact visual and interaction issues to fix before altering spacing values.

**Files:**
- Create `docs/ui-audits/2026-09-18-omarchy-ui-ux-polish.md`
- Create `docs/ui-audits/assets/2026-09-18-omarchy-ui-ux-polish/README.md`

**Approach:** Record native-scale baseline screenshots and a concise issue ledger for the widget, panel at 540/780 px, and Full View at default/minimum geometry. Define the approved spacing ladder, each surface's responsibility, text-overflow rules, and the visual meaning of action states. Use the macOS app only to compare hierarchy and density; document any intentional Omarchy divergence.

**Patterns to follow:** Existing marketplace asset documentation in `README.md`; the explicit popup-versus-window decision in `docs/plans/2026-09-16-0128-feat-dayflow-1-0-release-plan.md`.

**Test scenarios:**
- Screenshots cover recording, paused, unavailable, populated, empty, loading, failure, and long-content states.
- The audit identifies an expected result for each state at default and constrained geometry.
- The contract names intentional Linux differences from the macOS authority, preventing a visual-copy implementation.

### U2. Polish widget and panel shell geometry

**Goal:** Make the always-visible widget legible without growing it, and make the panel frame stable and easy to scan.

**Files:**
- Modify `BarWidget.qml`
- Modify `Panel.qml`
- Modify `BusyBar.qml` if its sizing/state treatment needs alignment
- Modify `DayNavRow.qml` if date navigation needs shared spacing/focus treatment

**Approach:** Tighten icon/status alignment within the host `WidgetButton` contract while retaining a usable pointer target and clear tooltip copy. Normalize panel edge padding, header/tab rhythm, separators, and footer actions around the U1 spacing ladder. Ensure the header and capture controls do not move with scrollable tab content. Preserve persisted `panel_expanded` behavior and Full View ownership in `BarWidget.qml`.

**Patterns to follow:** Existing `WidgetButton` usage in `BarWidget.qml`, status persistence in `Panel.qml`, and lightweight asynchronous indication in `BusyBar.qml`.

**Test scenarios:**
- Widget accurately distinguishes recording, paused, and unavailable status through icon, tooltip, and non-color text.
- Left-click panel toggle and right-click pause/resume behavior remain unchanged.
- Panel opens without clipping at 540 and 780 px; tabs and quick actions neither wrap accidentally nor push the footer off-screen.
- Opening Full View closes the popover while the new window survives and reopens correctly.

### U3. Rebalance panel tab density and action feedback

**Goal:** Make the panel a compact operational review rather than a compressed dashboard.

**Files:**
- Modify `TodayTab.qml`
- Modify `StandupTab.qml`
- Modify `ChatTab.qml`
- Modify `WeekTab.qml`
- Modify `Settings.qml`
- Modify `CopyButton.qml` if needed to standardize action-state presentation

**Approach:** Apply the U1 hierarchy consistently: informative groups receive cards, adjacent controls share a quieter container, and section gaps reveal task order. Give activity titles, summaries, app names, pills, model labels, and action text explicit width budgets; preserve trailing duration/status and expose elided content through existing native affordances. Keep long analytics, playback, and detailed settings out of the initial compact scan. Replace the single urgent notice treatment with semantic local feedback near the initiating action.

**Patterns to follow:** Fixed-row truncation in `TodayPane.qml`; duplicate-action protection in `CopyButton.qml`; lazy tab loading from `Panel.qml`.

**Test scenarios:**
- Long activity title, summary, app class, provider name, ignored-app list, and export text do not resize critical rows or hide trailing metadata.
- Empty, pending-summary, paused, malformed-command, and failed-start states explain the condition and an available next action.
- Save, copy, pause/resume, summarize, and navigation actions show distinct progress, success, and error feedback with no indefinitely busy control.
- Tab selection, date selection, and future-date blocking retain current behavior at both panel widths.

### U4. Rework Full View composition and small-window behavior

**Goal:** Use Full View's space for clear analysis while retaining a readable fallback at its supported minimum size.

**Files:**
- Modify `FullView.qml`
- Modify `TodayPane.qml`
- Modify `WeekPane.qml`
- Modify `TimelapsePane.qml`
- Modify `ContextPane.qml`
- Modify `AgentsPane.qml`

**Approach:** Keep the fixed section rail and opaque shell, then standardize pane insets, title/action alignment, scroll ownership, and card rhythm. Reorder each pane around one primary artifact followed by secondary detail. Add a width/height breakpoint where Full Week's two-column analysis becomes one column or its dense elements reduce safely; replace hardcoded available-height deductions with layout-derived space where practical. Preserve lazy pane loading and the `host` injection pattern.

**Patterns to follow:** `FullView.qml` loader/pane ownership, `WeekPane.qml` two-column `Flow`, and date-aware reload rules already owned by Full View.

**Test scenarios:**
- Full View remains opaque, maps at default size, and stays usable at minimum size without clipped cards, unreadable heatmap cells, or overlapping labels.
- Today, Week, Timelapse, Context, and Agents panes each have populated and honest empty states.
- The Week primary visualization and its legend/textual values remain understandable without relying only on color.
- Changing date/section preserves expected refresh behavior; timelapse does not eagerly load every frame and stops cleanly at the final frame.

### U5. Complete interaction, focus, and visual-regression verification

**Goal:** Prevent the polish from becoming an unverified local visual adjustment.

**Files:**
- Create `scripts/ui-smoke.sh`
- Modify `README.md`
- Update `docs/ui-audits/2026-09-18-omarchy-ui-ux-polish.md`

**Approach:** Add a non-destructive smoke script that runs available QML static validation for touched files and reports expected unresolved `qs.*` import limitations clearly. Document the native Omarchy reload procedure and a screenshot acceptance matrix. During the QML pass, ensure interactive controls have clear hover/disabled treatment, visible keyboard focus where the host supports it, and Escape/Enter/arrow behavior for panel close, tabs, dates, and calendar navigation without breaking existing chat editing shortcuts.

**Patterns to follow:** `scripts/stress.sh` command-failure handling; current plugin sync instructions in `README.md`; `PanelKeyCatcher` behavior in `Panel.qml`.

**Test scenarios:**
- Static validation exits nonzero for syntax errors and distinguishes known unresolved Omarchy imports from Dayflow errors.
- A documented manual pass verifies focus order, Enter/Space activation, Escape dismissal, and keyboard date/tab movement.
- Screenshot evidence covers widget states; panel widths/states; Full Today and Week at default and constrained geometries; active and contrasting supported themes.
- Plugin rescan/reload shows no new Dayflow QML errors, and `ui:` logging records normal interactions without unhandled failure messages.

## Validation

- Run `scripts/ui-smoke.sh` for all touched QML files. Treat only pre-existing unresolved `qs.*` module warnings as acceptable.
- Synchronize the plugin to `~/.config/omarchy/plugins/io.github.duketopceo.dayflow/`, then run `omarchy-shell shell rescanPlugins` and reload the shell through the supported Omarchy flow.
- Validate manually on `eDP-1` at `3456x2160`, scale `1.5`: widget, panel at 540/780 px, and Full View at default plus supported minimum geometry.
- Capture and compare every scenario listed in U1 and U5 before/after the polish pass.
- Run `cd engine && go test ./...` and `go vet ./...` if any engine-adjacent behavior or version contract changes unexpectedly; this plan intends no engine changes.
- Run `dayflow doctor --deep` after plugin verification to confirm UI state remains consistent with the installed engine and schema.

## Risks and Guardrails

- **Theme drift:** hardcoded colors or fonts would break Omarchy integration. Use existing theme-derived helpers except where a semantic state needs a documented existing color role.
- **Scope drift:** no new engine data or dashboard capability belongs in this audit. Record feature ideas separately rather than adding them while polishing.
- **Popup regression:** scroll/layout changes can hide privacy controls or alter persisted width. Verify at both widths and preserve panel expansion loading semantics.
- **Full View lifetime regression:** never move the Full View loader back under `Panel.qml`; popup dismissal must not destroy the window.
- **Privacy misrepresentation:** paused, ignored, retention, and unavailable states must state what Dayflow is actually doing, not merely change color or copy.

## Dependencies and Sequence

1. U1 establishes the reference lock and visual baseline.
2. U2 defines shared shell rhythm before changing tab interiors.
3. U3 applies constrained-panel hierarchy and semantic feedback.
4. U4 applies the same system to Full View with responsive fallbacks.
5. U5 captures proof and protects the refined behavior.
