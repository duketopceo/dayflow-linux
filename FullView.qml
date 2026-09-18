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
  property var agentSessions: []
  property bool agentsLoading: false
  property string agentsError: ""

  function agentsLoad() {
    if (!root.dayflow) return
    root.dayflow.uilog("agents load " + root.dayflow.viewDateStr())
    agentsProc.command = ["dayflow", "agents", root.dayflow.viewDateStr(), "--json"]
    root.agentsLoading = true
    if (agentsProc.running) {
      root.agentsPending = true
    } else {
      agentsProc.running = true
    }
  }

  Process {
    id: agentsProc
    command: ["dayflow", "agents", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        root.agentsLoading = false
        try {
          var d = JSON.parse(text)
          root.agentSessions = d.sessions || []
          root.agentsError = ""
        } catch (e) {
          root.agentSessions = []
          root.agentsError = "could not load agent sessions"
        }
      }
    }
    onExited: function(exitCode) {
      root.agentsLoading = false
      if (exitCode !== 0) {
        root.agentSessions = []
        root.agentsError = "agent scan failed"
      }
      if (root.agentsPending) {
        root.agentsPending = false
        root.agentsLoad()
      }
    }
    // FailedToStart emits neither exited nor streamFinished — clear the
    // flag and drain the queue so the bar never sticks.
    onRunningChanged: {
      if (!agentsProc.running) {
        root.agentsLoading = false
        if (root.agentsPending) {
          root.agentsPending = false
          root.agentsLoad()
        }
      }
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
  property bool agentsPending: false

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
        width: Style.space(170)
        height: parent.height
        color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
        border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

        Column {
          anchors.fill: parent
          anchors.margins: Style.space(12)
          spacing: Style.space(4)

          Text {
            text: "Dayflow"
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.subtitle
            font.bold: true
            bottomPadding: Style.space(8)
          }

          Repeater {
            model: root.sections

            delegate: Rectangle {
              required property var modelData
              width: parent.width
              height: Style.space(30)
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
                anchors.leftMargin: Style.space(10)
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
                  if (modelData.key === "agents" && root.agentSessions.length === 0 && !root.agentsLoading)
                    root.agentsLoad()
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
        width: parent.width - Style.space(170)
        height: parent.height

        // header
        Item {
          width: parent.width
          height: Style.space(52)

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
          height: parent.height - Style.space(52)
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
      root.agentSessions = []
      if (root.section === "timelapse") root.tlLoad()
      if (root.section === "agents") root.agentsLoad()
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
