# Dayflow Omarchy UI/UX Polish

## Direction

Dayflow Linux keeps its own Omarchy-native utility character. Dayflow macOS informs
information hierarchy only: compact daily review first, richer analysis in a dedicated
workspace. It is not a visual-copy target.

| Surface | Job | Refinement rule |
|---|---|---|
| Bar widget | Status and entry point | One compact state glyph, truthful tooltip, unchanged click actions. |
| Panel | Current-day review and quick actions | Stable shell, compact rows, bounded text, and no dense dashboard layouts at 540 px. |
| Full View | Detailed review and analytics | More room for analysis, with a compact fallback before clipping becomes necessary. |

## Layout Contract

- Retain Omarchy `Style` spacing, typography, radius, and color roles.
- Use tighter gaps for sibling controls, normal inset for cards, and larger breaks only between distinct content groups.
- Keep category color semantic across timelines and analytics; pair it with text or a value.
- Elide row metadata before hiding durations, status, or the selected state.
- Success feedback uses the accent role; failure feedback uses the urgent role.
- Full View stays opaque and its loader remains owned by `BarWidget.qml`.

## Visual Acceptance Matrix

| Surface | Required states |
|---|---|
| Widget | Recording, paused, unavailable; tooltip matches the action. |
| Panel | 540 px and 780 px; populated, empty, loading, error, and long content. |
| Full View | Default and 840x560 minimum; populated and empty Today/Week panes. |
| Week | Compact fallback stacks statistics and analytical cards without overlap. |
| Interaction | Hover, disabled, progress, success, and error remain visually distinct. |

## Manual Native Check

Verify on `eDP-1` at 3456x2160 scale 1.5 after synchronizing the plugin and running
`omarchy-shell shell rescanPlugins`. Confirm there are no Dayflow QML errors, popup
actions retain their behavior, and opening Full View closes the popup without closing
the standalone window.
