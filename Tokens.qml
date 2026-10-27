// Tokens.qml — the plugin token layer (KTD1/KTD2/KTD3).
// One instance owned by BarWidget and handed down on the `dayflow`
// object. Holds no palette of its own: every value derives from Color,
// Style, or the active theme's colors.toml (the only file R1 exempts
// from the literal ban — fallback hexes live here and only here).
// Reload triggers: the watched theme.name FileView (survives omarchy's
// directory-replace theme switch, KTD2) plus changes to Color's own
// loaded values (covers same-name rewrites).
import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import "ThemeMath.js" as TM

Item {
  id: tk

  // ---- df.* surface + text roles (DESIGN.md 6.1) ----
  readonly property color bg:      Color.popups.background || Color.background || "#101315"
  readonly property color fg:      Color.foreground
  readonly property color fgDim:   Color.muted || Qt.rgba(fg.r, fg.g, fg.b, 0.6)
  readonly property color fgFaint: hues["dark_foreground"] || Qt.rgba(fg.r, fg.g, fg.b, 0.35)
  readonly property color raised:  hues["lighter_background"] || Qt.rgba(fg.r, fg.g, fg.b, 0.04)
  readonly property color sunken:  hues["dark_background"] || Qt.darker(bg, 1.1)
  readonly property color accent:  Color.accent
  readonly property color selection: hues["selection"] ||
                                     Qt.rgba(accent.r, accent.g, accent.b, 0.25)
  readonly property color recording: Color.bar.active || Color.urgent
  readonly property color danger:  Color.urgent
  readonly property color hair:    Qt.rgba(fg.r, fg.g, fg.b, 0.08)
  readonly property color border:  Color.popups.border || Qt.rgba(fg.r, fg.g, fg.b, 0.12)
  readonly property real  fillNormal:   0.04
  readonly property real  fillHover:    0.08
  readonly property real  fillSelected: 0.18
  readonly property real  fillPressed:  0.22

  // ---- category hues (DESIGN.md 6.2) ----
  // Role key -> ordered theme-key fallback chain (Q4): named hue,
  // then ANSI slot, then the engine's default hex.
  readonly property var hueKeys: ({
    coding:        ["blue", "color4"],
    writing:       ["green", "color2"],
    communication: ["orange", "color1"],
    meetings:      ["yellow", "color3"],
    browsing:      ["magenta", "color5"],
    design:        ["bright_magenta", "red", "color1"],
    media:         ["red", "color1"],
    personal:      ["brown", "color3"],
    system:        ["cyan", "color6"],
    other:         ["muted"],
    idle:          ["dark_foreground"],
  })
  readonly property var engineDefaults: ({
    coding: "#388bee", communication: "#f27326", browsing: "#8b5cf6",
    writing: "#34d399", meetings: "#f3b13d", design: "#f53d7a",
    media: "#f53d3d", system: "#80808f", idle: "#6b7280",
    personal: "#f85a6a", other: "#9ca3af",
  })

  // Raw key/value pairs parsed from the theme's colors.toml.
  property var hues: ({})
  // Corrected category colors, recomputed when hues or Color change.
  function buildCategories() {
    var out = {}
    for (var cat in hueKeys) {
      var hex = ""
      var chain = hueKeys[cat]
      for (var i = 0; i < chain.length; i++) {
        if (hues[chain[i]]) { hex = hues[chain[i]]; break }
      }
      if (!hex) hex = engineDefaults[cat] || "#808080"
      // Ribbon fill: hue at 0.85 over bg must reach 3:1.
      out[cat] = TM.correctContrast(hex, bg, fg, 3.0, 0.85)
    }
    return out
  }
  readonly property var categories: buildCategories()

  // categoryColor(name, hex, role): R3 precedence — user hex wins,
  // then the corrected theme-mapped hue, then the engine default.
  function categoryColor(name, hex, role) {
    if (hex) {
      // Tag-text legibility on the same surface gets the 4.5:1 pass.
      return TM.correctContrast(hex, bg, fg, 4.5, 1.0)
    }
    var cat = String(name || "").toLowerCase()
    if (categories[cat]) return categories[cat]
    var def = engineDefaults[cat]
    return def ? TM.correctContrast(def, bg, fg, 3.0, 0.85) : fgDim
  }

  // ---- theme reload (KTD2) ----
  FileView {
    id: themeNameFile
    path: (Quickshell.env("HOME") || "") + "/.local/state/omarchy/current/theme.name"
    watchChanges: true
    printErrors: false
    onLoaded: tk.reloadHues()
    onFileChanged: reload()
  }

  FileView {
    id: colorsFile
    path: (Quickshell.env("HOME") || "") + "/.local/state/omarchy/current/theme/colors.toml"
    printErrors: false
    watchChanges: true
    onFileChanged: reload()
    onLoaded: tk.parseColors(text())
    onLoadFailed: tk.hues = {}
  }

  function reloadHues() {
    // Directory was replaced wholesale — re-point the colors FileView at
    // the (possibly new) file and force a re-read.
    colorsFile.reload()
  }

  function parseColors(raw) {
    var m = {}
    var lines = String(raw || "").split("\n")
    for (var i = 0; i < lines.length; i++) {
      var match = lines[i].match(/^\s*([A-Za-z0-9_-]+)\s*=\s*["']?(#[0-9A-Fa-f]{6})(?![0-9A-Fa-f])/)
      if (match) m[match[1]] = match[2].toLowerCase()
    }
    hues = m
  }

  // hues assignment itself re-binds categories — no handler needed.
  // Same-name palette rewrites fire colorsFile.onFileChanged (watchChanges).

  // Color signals can arrive in bursts during a theme swap — debounce
  // into a single file re-read.
  Timer {
    id: colorDebounce
    interval: 100
    onTriggered: colorsFile.reload()
  }
  Connections {
    target: Color
    function onBackgroundChanged() { colorDebounce.restart() }
    function onForegroundChanged() { colorDebounce.restart() }
    function onAccentChanged()     { colorDebounce.restart() }
  }
}
