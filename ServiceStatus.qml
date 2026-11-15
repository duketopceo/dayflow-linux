import QtQuick
import Quickshell.Io

// Background-service state for the desktop app: what `dayflow status --json`
// and `systemctl --user show` say about the capture daemon and its timers.
// The contract (commands, keys, units) is docs/service-contract.md. This item
// only reads, plus three actions: toggle pause, restart capture, start a unit.
// No page imports it: the app shell owns one instance and passes it to
// ServiceStrip, so the bar widget can reuse the same parsing later.
Item {
  id: root

  // ---- status --json ----
  property bool engineAvailable: false
  property bool paused: false
  property string captureState: "unknown"   // recording | paused | locked | down | unknown
  property double lastFrameTs: 0            // epoch seconds, 0 = never
  property int framesToday: 0
  property int blocksPending: 0
  property string engineVersion: ""

  // ---- systemd --user ----
  readonly property var watchedUnits: [
    "dayflow-capture.service",
    "dayflow-summarize.timer",
    "dayflow-backup.timer",
    "dayflow-export.timer"
  ]
  // [{id, active, sub, enabled}] in watchedUnits order; empty when systemd
  // cannot be queried (no user manager, sandbox).
  property var units: []
  property bool unitsKnown: false
  property bool summarizerRunning: false

  // Clock for age text; ticks so "12s ago" keeps moving between polls.
  property double nowSec: Date.now() / 1000

  readonly property bool captureUnitActive: unitById("dayflow-capture.service").active === "active"

  // ok: capture unit active and every timer active
  // degraded: a unit is inactive/failed, or capture is down
  // unmanaged: systemd not queryable (units never known)
  // unknown: no engine answer yet
  readonly property string health: {
    if (!engineAvailable) return "unknown"
    if (!unitsKnown) return captureState === "down" ? "degraded" : "unmanaged"
    if (captureState === "down") return "degraded"
    for (var i = 0; i < units.length; i++)
      if (units[i].active !== "active") return "degraded"
    return "ok"
  }

  function unitById(id) {
    for (var i = 0; i < units.length; i++)
      if (units[i].id === id) return units[i]
    return { id: id, active: "unknown", sub: "", enabled: "" }
  }

  // "never", "12s ago", "4m ago", "2h ago"
  function ageText() {
    if (lastFrameTs <= 0) return "never"
    var s = Math.max(0, Math.round(nowSec - lastFrameTs))
    if (s < 60) return s + "s ago"
    if (s < 3600) return Math.floor(s / 60) + "m ago"
    if (s < 86400) return Math.floor(s / 3600) + "h ago"
    return Math.floor(s / 86400) + "d ago"
  }

  function applyStatus(raw) {
    try {
      var s = JSON.parse(raw)
      root.paused = s.paused === true
      root.captureState = String(s.capture_state || "unknown")
      root.lastFrameTs = Number(s.last_frame_ts || 0)
      root.framesToday = Number(s.frames_today || 0)
      root.blocksPending = Number(s.blocks_pending || 0)
      root.engineVersion = String(s.version || "")
      root.engineAvailable = true
    } catch (e) {
      root.engineAvailable = false
    }
  }

  // `systemctl show a b c -p ...` prints one property block per unit,
  // separated by a blank line; property order inside a block is not fixed.
  function applyUnits(raw) {
    var blocks = raw.split(/\n\s*\n/)
    var byId = {}
    for (var i = 0; i < blocks.length; i++) {
      var rec = {}
      var lines = blocks[i].split("\n")
      for (var j = 0; j < lines.length; j++) {
        var eq = lines[j].indexOf("=")
        if (eq > 0) rec[lines[j].substring(0, eq)] = lines[j].substring(eq + 1)
      }
      if (rec.Id) byId[rec.Id] = {
        id: rec.Id,
        active: rec.ActiveState || "unknown",
        sub: rec.SubState || "",
        enabled: rec.UnitFileState || ""
      }
    }
    var out = []
    for (var k = 0; k < root.watchedUnits.length; k++) {
      var id = root.watchedUnits[k]
      // A unit systemd has never heard of is reported as inactive/not-found,
      // not dropped: a missing unit is a degraded install the user can fix.
      out.push(byId[id] || { id: id, active: "inactive", sub: "dead", enabled: "not-found" })
    }
    root.units = out
    root.unitsKnown = Object.keys(byId).length > 0
    root.summarizerRunning = byId["dayflow-summarize.service"] !== undefined
      && byId["dayflow-summarize.service"].active === "active"
  }

  function refresh() {
    if (!statusProc.running) statusProc.running = true
    if (!unitsProc.running) unitsProc.running = true
  }

  function togglePause() {
    if (actionProc.running) return
    actionProc.command = ["dayflow", "toggle"]
    actionProc.running = true
  }

  function restartCapture() {
    if (actionProc.running) return
    actionProc.command = ["systemctl", "--user", "restart", "dayflow-capture.service"]
    actionProc.running = true
  }

  function startUnit(id) {
    if (actionProc.running) return
    actionProc.command = ["systemctl", "--user", "start", id]
    actionProc.running = true
  }

  Process {
    id: statusProc
    command: ["dayflow", "status", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyStatus(text)
    }
    onExited: function(code) { if (code !== 0) root.engineAvailable = false }
  }

  Process {
    id: unitsProc
    command: ["systemctl", "--user", "show",
      "dayflow-capture.service", "dayflow-summarize.service",
      "dayflow-summarize.timer", "dayflow-backup.timer", "dayflow-export.timer",
      "-p", "Id", "-p", "ActiveState", "-p", "SubState", "-p", "UnitFileState"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyUnits(text)
    }
    onExited: function(code) {
      // No user manager: leave units as last known only if we had them.
      if (code !== 0) { root.units = []; root.unitsKnown = false }
    }
  }

  Process {
    id: actionProc
    onExited: Qt.callLater(root.refresh)
  }

  Timer {
    interval: 10000
    running: true
    repeat: true
    triggeredOnStart: true
    onTriggered: root.refresh()
  }

  Timer {
    interval: 5000
    running: true
    repeat: true
    onTriggered: root.nowSec = Date.now() / 1000
  }
}
