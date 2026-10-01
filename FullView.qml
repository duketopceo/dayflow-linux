import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// Full-view window — the expanded Dayflow surface opened from the panel's
// "Full view" button. Owns a left section rail; each feature pane registers
// itself in `sections`. State stays on the panel root (`dayflow`), passed
// in by the panel's Loader.
FloatingWindow {
  id: root

  property var dayflow: null
  property string section: "today"

  // Rail model — later feature units append their pane entries here.
  readonly property var sections: [
    { key: "today",     label: "Today" },
    { key: "week",      label: "Week" },
    { key: "timelapse", label: "Timelapse" },
    { key: "context",   label: "Context" },
    { key: "agents",    label: "Agents" }
  ]

  // ---- forecast ----
  property var forecast: null

  Process {
    id: forecastProc
    command: ["dayflow", "forecast", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try { root.forecast = JSON.parse(text) } catch (e) { root.forecast = null }
      }
    }
  }

  // ---- agents pane state ----
  // The day's briefing payload from `dayflow briefing --json`:
  // {day, mode, recaps_enabled, workstreams[], sources[]} — null until the
  // first load so the pane can distinguish "not loaded" from "empty day".
  property var agentBriefing: null
  // Per-source scan status ({source, sessions, status, note?, drift?}) —
  // surfaced so an unavailable or drifted store doesn't read as a quiet day.
  property var agentSources: []
  // recaps_enabled from briefing --json. Defaults true so a pre-field binary
  // doesn't flash the opt-in hint; false only when the engine says so.
  property bool agentRecapsEnabled: true
  property bool agentsLoading: false
  property string agentsError: ""

  function agentsLoad(refresh) {
    if (!root.dayflow) return
    root.dayflow.uilog("agents load " + root.dayflow.viewDateStr() + (refresh ? " (refresh)" : ""))
    root.agentsRun = root.agentsRun + 1
    root.agentsTimedOut = false
    var cmd = ["dayflow", "briefing", root.dayflow.viewDateStr(), "--json"]
    if (refresh) cmd.push("--refresh")
    agentsProc.command = cmd
    root.agentsLoading = true
    if (agentsProc.running) {
      if (refresh === true || root.agentsPending !== "refresh")
        root.agentsPending = refresh === true ? "refresh" : "load"
    } else {
      // Stamp the run on the process only at actual start — callbacks from a
      // still-terminating previous run then fail the runId check.
      agentsProc.runId = root.agentsRun
      agentsProc.running = true
    }
  }

  // Runs the pending reload/refresh recorded by agentsLoad when a process
  // was mid-flight. A queued refresh outranks a queued load.
  function agentsDrainPending() {
    var pending = root.agentsPending
    root.agentsPending = ""
    if (pending !== "") root.agentsLoad(pending === "refresh")
  }

  Process {
    id: agentsProc
    property int runId: 0
    command: ["dayflow", "briefing", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        // Stale callbacks from a previous process (or a watchdog-killed run)
        // carry old results — ignore them so a fresh run's state and the
        // timeout message survive. (streamFinished can fire on exit.)
        if (agentsProc.runId !== root.agentsRun || root.agentsTimedOut) return
        root.agentsLoading = false
        try {
          var d = JSON.parse(text)
          // Day changed mid-run — this payload is for the old day; the
          // queued reload (drained on exit) owns state now.
          if (root.dayflow && d.day !== root.dayflow.viewDateStr()) return
          root.agentBriefing = d
          root.agentSources = d.sources || []
          root.agentRecapsEnabled = d.recaps_enabled !== false
          root.agentsError = ""
        } catch (e) {
          root.agentBriefing = null
          root.agentSources = []
          root.agentsError = "could not load agent briefing"
        }
      }
    }
    onExited: function(exitCode) {
      if (agentsProc.runId !== root.agentsRun) {
        // A previous process's exit — never touch the new run's state, but
        // still service the queued reload it was waiting on.
        Qt.callLater(root.agentsDrainPending)
        return
      }
      root.agentsLoading = false
      if (exitCode !== 0 && !root.agentsTimedOut) {
        root.agentBriefing = null
        root.agentSources = []
        root.agentsError = "agent briefing failed"
      }
      // Defer the drain until every callback for this dead run has fired —
      // restarting here would restamp runId and let the old run's remaining
      // callbacks (exited/runningChanged/streamFinished) misattribute to it.
      Qt.callLater(root.agentsDrainPending)
    }
    // FailedToStart emits neither exited nor streamFinished — clear the
    // flag and drain the queue so the bar never sticks.
    onRunningChanged: {
      if (!agentsProc.running) {
        if (agentsProc.runId !== root.agentsRun) {
          Qt.callLater(root.agentsDrainPending)
          return
        }
        root.agentsLoading = false
        // If neither exited nor streamFinished ran (FailedToStart — e.g.
        // dayflow missing from PATH), no callback sets an error: check
        // after they settle so the pane reports failure, not blank.
        Qt.callLater(function() {
          if (agentsProc.runId === root.agentsRun && !agentsProc.running
              && root.agentBriefing === null && root.agentsError === ""
              && root.agentsPending === "")
            root.agentsError = "agent briefing failed"
        })
        Qt.callLater(root.agentsDrainPending)
      }
    }
  }

  // Watchdog: the engine bounds recap generation internally (~45s), but a
  // wedged provider or hung scan could still outlive it — kill the process
  // and surface the metadata-only state rather than spinning forever.
  // agentsPending is cleared BEFORE stopping so onRunningChanged doesn't
  // see a queued reload and restart the proc it just killed; agentsTimedOut
  // makes onExited keep the timeout message instead of overwriting it.
  Timer {
    id: agentsWatchdog
    interval: 75000
    running: agentsProc.running
    repeat: false
    onTriggered: {
      if (root.dayflow) root.dayflow.uilog("agents watchdog: killing hung agentsProc")
      root.agentsPending = ""
      root.agentsTimedOut = true
      agentsProc.running = false
      root.agentsLoading = false
      root.agentBriefing = null
      root.agentSources = []
      root.agentsError = "agent briefing timed out"
    }
  }

  // ---- context-shift graph ----
  // Bipartite flow: same categories on both columns, links sized by minutes.
  function ctxNodes() {
    if (!root.dayflow) return { names: [], mins: {}, total: 0 }
    var links = root.dayflow.weeklyPayload.context_shifts || []
    var agg = {}
    var order = []
    for (var i = 0; i < links.length; i++) {
      var l = links[i]
      for (var s = 0; s < 2; s++) {
        var name = s === 0 ? l.source : l.target
        if (name === "" || name === undefined) continue
        if (agg[name] === undefined) { agg[name] = 0; order.push(name) }
        agg[name] += Number(l.minutes) || 0
      }
    }
    order.sort(function(a, b) { return agg[b] - agg[a] })
    var total = 0
    for (var n = 0; n < order.length; n++) total += agg[order[n]]
    return { names: order, mins: agg, total: total }
  }

  // ---- timelapse state ----
  property var tlFrames: []
  property int tlIndex: 0
  property bool tlPlaying: false
  property bool tlLoading: false
  property string tlError: ""

  property bool tlPending: false
  property string agentsPending: ""
  property bool agentsTimedOut: false
  property int agentsRun: 0

  function tlLoad() {
    if (!root.dayflow) return
    root.dayflow.uilog("timelapse load " + root.dayflow.viewDateStr())
    framesProc.command = ["dayflow", "frames", root.dayflow.viewDateStr(), "--json"]
    root.tlLoading = true
    root.tlPlaying = false
    // command changes on a running Process only affect the next start —
    // queue a rerun so the requested day isn't dropped mid-flight.
    if (framesProc.running) {
      root.tlPending = true
    } else {
      framesProc.running = true
    }
  }

  function tlSeek(i) {
    if (root.tlFrames.length === 0) return
    root.tlIndex = Math.max(0, Math.min(root.tlFrames.length - 1, i))
  }

  Process {
    id: framesProc
    command: ["dayflow", "frames", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        root.tlLoading = false
        try {
          var d = JSON.parse(text)
          var list = (d.frames || []).filter(function(f) { return f.exists })
          root.tlFrames = list
          root.tlIndex = 0
          root.tlError = ""
        } catch (e) {
          root.tlFrames = []
          root.tlError = "could not load frames"
        }
      }
    }
    onExited: function(exitCode) {
      root.tlLoading = false
      if (exitCode !== 0) {
        root.tlFrames = []
        root.tlError = "frames listing failed"
      }
      if (root.tlPending) {
        root.tlPending = false
        root.tlLoad()
      }
    }
    onRunningChanged: {
      if (!framesProc.running) {
        root.tlLoading = false
        if (root.tlPending) {
          root.tlPending = false
          root.tlLoad()
        }
      }
    }
  }

  Process {
    id: playbackProc
    property string pendingArg: "status"
    command: ["dayflow", "playback", pendingArg]
    onExited: function(exitCode) {
      if (root.dayflow) {
        root.dayflow.notice = exitCode === 0
          ? "playback " + playbackProc.pendingArg
          : "playback toggle failed"
      }
      if (exitCode === 0) {
        playbackStatusProc.running = true
        root.tlLoad()
      }
    }
  }

  // Called by TimelapsePane's enable button.
  function enablePlayback() {
    if (root.dayflow) root.dayflow.uilog("playback enable")
    playbackProc.pendingArg = "on"
    playbackProc.running = true
  }

  Timer {
    interval: 250
    repeat: true
    running: root.tlPlaying && root.section === "timelapse" && root.tlFrames.length > 1
    onTriggered: {
      if (root.tlIndex >= root.tlFrames.length - 1) {
        root.tlPlaying = false
      } else {
        root.tlIndex++
      }
    }
  }

  title: "Dayflow"
  // Opaque theme surface — a low-alpha window assumed Hyprland blur,
  // which Omarchy 4.x ships off (same fill as other FloatingWindows).
  color: Color.background
  implicitWidth: 1100
  implicitHeight: 720
  minimumSize: Qt.size(840, 560)
  visible: root.dayflow !== null
  readonly property bool compactLayout: width < Style.space(980) || height < Style.space(640)
  readonly property int railWidth: Style.space(compactLayout ? 136 : 170)
  readonly property int headerHeight: Style.space(compactLayout ? 46 : 52)

  // dayflow is assigned by the panel's Loader.onLoaded AFTER this component's
  // onCompleted — trigger the initial loads on assignment instead.
  onDayflowChanged: {
    if (root.dayflow) {
      root.dayflow.loadTimeline()
      root.dayflow.refreshForTab("week")
    }
  }

  Component.onCompleted: {
    playbackStatusProc.running = true
    forecastProc.running = true
  }

  Rectangle {
    anchors.fill: parent
    radius: Style.cornerRadius
    color: root.dayflow ? root.dayflow.fgFill(0.03) : "transparent"
    border.color: root.dayflow ? root.dayflow.fgFill(0.10) : "transparent"
    clip: true

    Row {
      anchors.fill: parent

      // ---- section rail ----
      Rectangle {
        width: root.railWidth
        height: parent.height
        color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
        border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

        Column {
          anchors.fill: parent
          anchors.margins: Style.space(compactLayout ? 10 : 12)
          spacing: Style.space(4)

          Text {
            text: "Dayflow"
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.subtitle
            font.bold: true
            bottomPadding: Style.space(6)
          }

          Repeater {
            model: root.sections

            delegate: Rectangle {
              required property var modelData
              width: parent.width
              height: Style.space(28)
              radius: Style.cornerRadius
              color: root.section === modelData.key
                ? (root.dayflow ? root.dayflow.accentFill(0.14) : "transparent")
                : (railMouse.containsMouse
                    ? (root.dayflow ? root.dayflow.fgFill(0.06) : "transparent")
                    : "transparent")
              border.color: root.section === modelData.key
                ? (root.dayflow ? root.dayflow.accentFill(0.4) : "transparent")
                : "transparent"

              Text {
                anchors.verticalCenter: parent.verticalCenter
                anchors.left: parent.left
                anchors.leftMargin: Style.space(8)
                text: modelData.label
                textFormat: Text.PlainText
                color: root.section === modelData.key
                  ? (root.dayflow ? root.dayflow.foreground : "white")
                  : (root.dayflow ? root.dayflow.dim : "gray")
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
              }

              MouseArea {
                id: railMouse
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                  if (root.dayflow) root.dayflow.uilog("fullview section " + modelData.key)
                  root.section = modelData.key
                  if (modelData.key === "timelapse" && root.tlFrames.length === 0 && !root.tlLoading)
                    root.tlLoad()
                  if (modelData.key === "agents" && root.agentBriefing === null && !root.agentsLoading)
                    root.agentsLoad(false)
                }
              }
            }
          }

          Item { width: 1; height: Style.space(8) }

          Text {
            width: parent.width
            visible: root.dayflow !== null
            text: root.dayflow
              ? root.dayflow.framesToday + " frames today · " +
                root.dayflow.blocksPending + " pending"
              : ""
            color: root.dayflow ? root.dayflow.dim : "gray"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }
        }
      }

      // ---- main area ----
      Column {
        width: parent.width - root.railWidth
        height: parent.height

        // header
        Item {
          width: parent.width
          height: root.headerHeight

          Text {
            anchors.verticalCenter: parent.verticalCenter
            anchors.left: parent.left
            anchors.leftMargin: Style.space(16)
            text: {
              for (var i = 0; i < root.sections.length; i++)
                if (root.sections[i].key === root.section) return root.sections[i].label
              return ""
            }
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.subtitle
            font.bold: true
          }

          Text {
            anchors.verticalCenter: parent.verticalCenter
            anchors.right: parent.right
            anchors.rightMargin: Style.space(16)
            visible: root.dayflow !== null && root.dayflow.dateLabel !== ""
            text: root.dayflow ? root.dayflow.dateLabel : ""
            color: root.dayflow ? root.dayflow.dim : "gray"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
          }
        }

        // Panes live in their own files and only exist while dayflow is set,
        // so a pane can rely on host.dayflow being non-null.
        Loader {
          width: parent.width
          height: parent.height - root.headerHeight
          active: root.dayflow !== null
          source: root.section === "week" ? "WeekPane.qml"
            : root.section === "timelapse" ? "TimelapsePane.qml"
            : root.section === "context" ? "ContextPane.qml"
            : root.section === "agents" ? "AgentsPane.qml"
            : "TodayPane.qml"
          onLoaded: item.host = root
        }
      }
    }
  }

  // Day changes invalidate loaded day-scoped data and reload visible panes.
  Connections {
    target: root.dayflow
    function onDayOffsetChanged() {
      root.tlFrames = []
      root.tlIndex = 0
      root.tlPlaying = false
      root.agentBriefing = null
      root.agentSources = []
      root.agentsError = ""
      // Invalidate any in-flight briefing run — its payload is for the old
      // day. Queue a reload so fresh data is ready whichever section the
      // user lands on; the run's exit drains this.
      if (root.agentsLoading) {
        root.agentsRun = root.agentsRun + 1
        if (root.agentsPending !== "refresh") root.agentsPending = "load"
      }
      if (root.section === "timelapse") root.tlLoad()
      if (root.section === "agents") root.agentsLoad(false)
    }
  }

  // playback on/off state for the pane gate
  property bool tlPlaybackOn: false

  Process {
    id: playbackStatusProc
    command: ["dayflow", "playback", "status", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try {
          var d = JSON.parse(text)
          root.tlPlaybackOn = d.enabled === true
        } catch (e) {}
      }
    }
  }

  onVisibleChanged: {
    if (!visible && root.dayflow) root.dayflow.fullViewOpen = false
  }
}
