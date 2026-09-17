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
  color: "transparent"
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

        Loader {
          width: parent.width
          height: parent.height - Style.space(52)
          sourceComponent: root.section === "week" ? weekPane
            : root.section === "timelapse" ? tlPane
            : root.section === "context" ? ctxPane
            : root.section === "agents" ? agentsPane
            : todayPane
        }
      }
    }
  }

  // ============ Today pane ============
  Component {
    id: todayPane

    Flickable {
      width: parent ? parent.width : 0
      height: parent ? parent.height : 0
      contentHeight: todayCol.implicitHeight
      clip: true

      Column {
        id: todayCol
        width: parent.width - Style.space(32)
        x: Style.space(16)
        spacing: Style.space(10)

        // day nav + actions
        Row {
          width: parent.width
          spacing: Style.space(6)

          Repeater {
            model: [
              { label: "‹", act: -1 },
              { label: "Today", act: 0 },
              { label: "›", act: 1 }
            ]

            delegate: Rectangle {
              required property var modelData
              height: Style.space(28)
              width: navText.implicitWidth + Style.space(16)
              radius: Style.cornerRadius
              color: navMouse.containsMouse ? root.dayflow.fgFill(0.08) : "transparent"
              border.color: root.dayflow ? root.dayflow.fgFill(0.15) : "transparent"

              Text {
                id: navText
                anchors.centerIn: parent
                text: modelData.label
                textFormat: Text.PlainText
                color: root.dayflow ? root.dayflow.foreground : "white"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
              }

              MouseArea {
                id: navMouse
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                  if (!root.dayflow) return
                  if (modelData.act === 0) {
                    if (root.dayflow.dayOffset !== 0) {
                      root.dayflow.dayOffset = 0
                      root.dayflow.loadTimeline()
                    }
                  } else {
                    root.dayflow.goDay(modelData.act)
                  }
                }
              }
            }
          }

          Text {
            anchors.verticalCenter: parent.verticalCenter
            text: root.dayflow ? root.dayflow.viewDateLabel() : ""
            color: root.dayflow ? root.dayflow.dim : "gray"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
          }
        }

        BusyBar {
          width: parent.width
          pal: root.dayflow
          active: root.dayflow ? root.dayflow.timelineLoading : false
        }

        Text {
          visible: root.dayflow !== null && root.dayflow.spans.length === 0 && !root.dayflow.timelineLoading
          text: "No activity recorded for this day."
          color: root.dayflow ? root.dayflow.dim : "gray"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }

        // merged-span timeline — wide rows
        Repeater {
          model: root.dayflow ? root.dayflow.spans : []

          delegate: Rectangle {
            required property var modelData
            width: todayCol.width
            height: Style.space(46)
            radius: Style.cornerRadius
            color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

            Rectangle {
              width: Style.space(4)
              height: parent.height - Style.space(8)
              anchors.left: parent.left
              anchors.leftMargin: Style.space(6)
              anchors.verticalCenter: parent.verticalCenter
              radius: Style.space(2)
              color: root.dayflow ? root.dayflow.categoryColor(modelData.category) : "gray"
            }

            Text {
              anchors.left: parent.left
              anchors.leftMargin: Style.space(18)
              anchors.verticalCenter: parent.verticalCenter
              width: Style.space(120)
              text: modelData.start + "–" + modelData.end
              color: root.dayflow ? root.dayflow.dim : "gray"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
            }

            Column {
              anchors.left: parent.left
              anchors.leftMargin: Style.space(150)
              anchors.right: durText.left
              anchors.rightMargin: Style.space(8)
              anchors.verticalCenter: parent.verticalCenter
              spacing: Style.space(1)

              Text {
                width: parent.width
                text: modelData.title + (modelData.count > 1 ? "  ·" + modelData.count : "")
                textFormat: Text.PlainText
                color: root.dayflow ? root.dayflow.foreground : "white"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
                font.bold: true
                elide: Text.ElideRight
              }

              Text {
                width: parent.width
                text: (modelData.summary !== "" ? modelData.summary + " · " : "") +
                      modelData.appName + " · " +
                      (root.dayflow ? root.dayflow.appDisplayName(modelData.category) : modelData.category)
                textFormat: Text.PlainText
                color: root.dayflow ? root.dayflow.dim : "gray"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
                elide: Text.ElideRight
              }
            }

            Text {
              id: durText
              anchors.right: parent.right
              anchors.rightMargin: Style.space(12)
              anchors.verticalCenter: parent.verticalCenter
              text: root.dayflow ? root.dayflow.fmtDur(modelData.minutes) : ""
              color: root.dayflow ? root.dayflow.foreground : "white"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.body
            }
          }
        }
      }
    }
  }

  // ============ Week pane ============
  Component {
    id: weekPane

    Flickable {
      width: parent ? parent.width : 0
      height: parent ? parent.height : 0
      contentHeight: weekCol.implicitHeight
      clip: true

      Column {
        id: weekCol
        width: parent.width - Style.space(32)
        x: Style.space(16)
        spacing: Style.space(12)

        // tomorrow forecast strip
        Rectangle {
          visible: root.forecast !== null && (root.forecast.items || []).length > 0
          width: parent.width
          height: fcRow.implicitHeight + Style.space(14)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
          border.color: root.dayflow ? root.dayflow.accentFill(0.25) : "transparent"

          Row {
            id: fcRow
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(8)

            Text {
              anchors.verticalCenter: parent.verticalCenter
              text: "Tomorrow · " + (root.forecast ? root.forecast.weekday : "") +
                    " · " + (root.forecast ? root.forecast.confidence : "") + " conf"
              textFormat: Text.PlainText
              color: root.dayflow ? root.dayflow.dim : "gray"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
            }

            Repeater {
              model: root.forecast ? (root.forecast.items || []).slice(0, 5) : []

              delegate: Rectangle {
                required property var modelData
                height: Style.space(20)
                width: fcChipText.implicitWidth + Style.space(14)
                radius: Style.cornerRadius
                color: root.dayflow
                  ? root.dayflow.payloadFill("", modelData.category, 0.15)
                  : "transparent"
                border.color: root.dayflow
                  ? root.dayflow.payloadFill("", modelData.category, 0.4)
                  : "transparent"

                Text {
                  id: fcChipText
                  anchors.centerIn: parent
                  text: (root.dayflow ? root.dayflow.appDisplayName(modelData.category) : modelData.category) +
                        " " + Math.round(modelData.pct) + "%"
                  textFormat: Text.PlainText
                  color: root.dayflow ? root.dayflow.foreground : "white"
                  font.family: root.dayflow ? root.dayflow.fontFamily : ""
                  font.pixelSize: Style.font.caption
                }
              }
            }
          }
        }

        // stat strip
        Row {
          width: parent.width
          spacing: Style.space(8)

          Repeater {
            model: root.dayflow ? [
              { label: "Tracked",     val: root.dayflow.fmtDur(root.dayflow.weeklyPayload.total_minutes) },
              { label: "Focus",       val: root.dayflow.fmtDur(root.dayflow.weeklyPayload.focus_minutes) },
              { label: "Distraction", val: root.dayflow.fmtDur(root.dayflow.weeklyPayload.distraction_minutes) },
              { label: "Shifts",      val: String(root.dayflow.weeklyPayload.context_shift_count) }
            ] : []

            delegate: Rectangle {
              required property var modelData
              height: Style.space(52)
              width: (weekCol.width - Style.space(24)) / 4
              radius: Style.cornerRadius
              color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
              border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

              Column {
                anchors.centerIn: parent
                spacing: Style.space(2)

                Text {
                  anchors.horizontalCenter: parent.horizontalCenter
                  text: modelData.val
                  color: root.dayflow ? root.dayflow.foreground : "white"
                  font.family: root.dayflow ? root.dayflow.fontFamily : ""
                  font.pixelSize: Style.font.subtitle
                  font.bold: true
                }
                Text {
                  anchors.horizontalCenter: parent.horizontalCenter
                  text: modelData.label
                  textFormat: Text.PlainText
                  color: root.dayflow ? root.dayflow.dim : "gray"
                  font.family: root.dayflow ? root.dayflow.fontFamily : ""
                  font.pixelSize: Style.font.caption
                }
              }
            }
          }
        }

        // heatmap — 7 days x 24 hours
        Rectangle {
          width: parent.width
          height: heatCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
          border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

          Column {
            id: heatCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(3)

            Text {
              text: "Focus heatmap"
              color: root.dayflow ? root.dayflow.foreground : "white"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.body
              font.bold: true
              bottomPadding: Style.space(4)
            }

            Repeater {
              model: 7

              delegate: Row {
                required property int index
                spacing: Style.space(2)

                Text {
                  width: Style.space(30)
                  text: root.dayflow ? root.dayflow.dayName(index) : ""
                  color: index === (root.dayflow ? root.dayflow.todayIndex() : -1)
                    ? (root.dayflow ? root.dayflow.foreground : "white")
                    : (root.dayflow ? root.dayflow.dim : "gray")
                  font.bold: index === (root.dayflow ? root.dayflow.todayIndex() : -1)
                  font.family: root.dayflow ? root.dayflow.fontFamily : ""
                  font.pixelSize: Style.font.caption
                  anchors.verticalCenter: parent.verticalCenter
                }

                Repeater {
                  model: 24

                  delegate: Rectangle {
                    required property int index
                    property int dayIndex: parent ? parent.index : 0
                    width: Style.space(16)
                    height: Style.space(12)
                    radius: Style.space(2)
                    color: root.dayflow
                      ? root.dayflow.cellColor(root.dayflow.categoryForHour(dayIndex, index))
                      : "transparent"
                  }
                }
              }
            }
          }
        }

        // two columns: category donut legend + top apps
        Row {
          width: parent.width
          spacing: Style.space(10)

          Rectangle {
            width: (parent.width - Style.space(10)) / 2
            height: catCol.implicitHeight + Style.space(16)
            radius: Style.cornerRadius
            color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

            Column {
              id: catCol
              width: parent.width - Style.space(16)
              anchors.centerIn: parent
              spacing: Style.space(4)

              Text {
                text: "Categories"
                color: root.dayflow ? root.dayflow.foreground : "white"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
                font.bold: true
                bottomPadding: Style.space(4)
              }

              Repeater {
                model: root.dayflow ? root.dayflow.weeklyPayload.category_donut : []

                delegate: Row {
                  required property var modelData
                  width: catCol.width
                  spacing: Style.space(8)

                  Rectangle {
                    width: Style.space(10)
                    height: Style.space(10)
                    radius: Style.space(5)
                    anchors.verticalCenter: parent.verticalCenter
                    color: root.dayflow
                      ? root.dayflow.payloadColor(modelData.color, modelData.name)
                      : "gray"
                  }

                  Text {
                    width: parent.width - Style.space(80)
                    text: root.dayflow ? root.dayflow.appDisplayName(modelData.display || modelData.name) : modelData.name
                    textFormat: Text.PlainText
                    color: root.dayflow ? root.dayflow.foreground : "white"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                    elide: Text.ElideRight
                  }

                  Text {
                    anchors.right: parent.right
                    text: root.dayflow ? root.dayflow.fmtDur(modelData.minutes) : ""
                    color: root.dayflow ? root.dayflow.dim : "gray"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                  }
                }
              }
            }
          }

          Rectangle {
            width: (parent.width - Style.space(10)) / 2
            height: appCol.implicitHeight + Style.space(16)
            radius: Style.cornerRadius
            color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

            Column {
              id: appCol
              width: parent.width - Style.space(16)
              anchors.centerIn: parent
              spacing: Style.space(4)

              Text {
                text: "Top apps"
                color: root.dayflow ? root.dayflow.foreground : "white"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
                font.bold: true
                bottomPadding: Style.space(4)
              }

              Repeater {
                model: root.dayflow ? root.dayflow.weeklyPayload.app_treemap : []

                delegate: Row {
                  required property var modelData
                  width: appCol.width
                  spacing: Style.space(8)

                  Text {
                    width: parent.width - Style.space(70)
                    text: root.dayflow ? root.dayflow.appDisplayName(modelData.display || modelData.name) : modelData.name
                    textFormat: Text.PlainText
                    color: root.dayflow ? root.dayflow.foreground : "white"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                    elide: Text.ElideRight
                  }

                  Text {
                    anchors.right: parent.right
                    text: root.dayflow ? root.dayflow.fmtDur(modelData.minutes) : ""
                    color: root.dayflow ? root.dayflow.dim : "gray"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                  }
                }
              }
            }
          }
        }

        // week summary
        Rectangle {
          visible: root.dayflow !== null && root.dayflow.weekSummary !== ""
          width: parent.width
          height: sumText.implicitHeight + Style.space(20)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
          border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

          Text {
            id: sumText
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            text: root.dayflow ? root.dayflow.weekSummary : ""
            textFormat: Text.PlainText
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }
        }

        Item { width: 1; height: Style.space(8) }
      }
    }
  }

  // ============ Timelapse pane ============
  Component {
    id: tlPane

    Column {
      width: parent ? parent.width : 0
      height: parent ? parent.height : 0
      spacing: Style.space(8)
      leftPadding: Style.space(16)
      rightPadding: Style.space(16)

      // controls
      Row {
        width: parent.width
        spacing: Style.space(6)

        Repeater {
          model: [
            { label: "‹", act: -1 },
            { label: "Today", act: 0 },
            { label: "›", act: 1 }
          ]

          delegate: Rectangle {
            required property var modelData
            height: Style.space(28)
            width: tlNavText.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: tlNavMouse.containsMouse ? root.dayflow.fgFill(0.08) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.15) : "transparent"

            Text {
              id: tlNavText
              anchors.centerIn: parent
              text: modelData.label
              textFormat: Text.PlainText
              color: root.dayflow ? root.dayflow.foreground : "white"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.body
            }

            MouseArea {
              id: tlNavMouse
              anchors.fill: parent
              hoverEnabled: true
              cursorShape: Qt.PointingHandCursor
              onClicked: {
                if (!root.dayflow) return
                if (modelData.act === 0) {
                  if (root.dayflow.dayOffset !== 0) {
                    root.dayflow.dayOffset = 0
                    root.dayflow.loadTimeline()
                  }
                } else {
                  root.dayflow.goDay(modelData.act)
                }
              }
            }
          }
        }

        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: root.dayflow ? root.dayflow.viewDateLabel() : ""
          color: root.dayflow ? root.dayflow.dim : "gray"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }

        Item { width: Style.space(8); height: 1 }

        // play / pause
        Rectangle {
          height: Style.space(28)
          width: playText.implicitWidth + Style.space(16)
          radius: Style.cornerRadius
          color: playMouse.containsMouse ? root.dayflow.accentFill(0.12) : "transparent"
          border.color: root.dayflow ? root.dayflow.accentFill(0.4) : "transparent"
          opacity: root.tlFrames.length > 1 ? 1 : 0.45

          Text {
            id: playText
            anchors.centerIn: parent
            text: root.tlPlaying ? "Pause" : "Play"
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
          }

          MouseArea {
            id: playMouse
            anchors.fill: parent
            hoverEnabled: true
            enabled: root.tlFrames.length > 1
            cursorShape: Qt.PointingHandCursor
            onClicked: {
              if (root.dayflow) root.dayflow.uilog("timelapse " + (root.tlPlaying ? "pause" : "play"))
              root.tlPlaying = !root.tlPlaying
            }
          }
        }

        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: root.tlFrames.length > 0
            ? "frame " + (root.tlIndex + 1) + " / " + root.tlFrames.length +
              " · " + Qt.formatTime(new Date(root.tlFrames[root.tlIndex].ts * 1000), "hh:mm:ss")
            : ""
          color: root.dayflow ? root.dayflow.dim : "gray"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.caption
        }
      }

      BusyBar {
        width: parent.width
        pal: root.dayflow
        active: root.tlLoading
      }

      // image area
      Rectangle {
        width: parent.width
        height: parent.height - Style.space(150)
        radius: Style.cornerRadius
        color: "black"
        clip: true

        Image {
          anchors.fill: parent
          fillMode: Image.PreserveAspectFit
          // downscale at decode — full-res JPEGs would stall the scrubber
          sourceSize.width: Math.round(width * 2)
          sourceSize.height: Math.round(height * 2)
          source: root.tlFrames.length > 0
            ? "file://" + root.tlFrames[root.tlIndex].path
            : ""
        }

        Text {
          anchors.centerIn: parent
          visible: root.tlFrames.length === 0 && !root.tlLoading
          width: parent.width - Style.space(40)
          horizontalAlignment: Text.AlignHCenter
          wrapMode: Text.WordWrap
          text: root.tlError !== "" ? root.tlError
            : (root.tlPlaybackOn
                ? "No frames kept for this day."
                : "Frame playback is off. Frames are deleted after summarization — enable playback to keep them (standard 10GB cap).")
          color: "white"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }

        // enable button, only when playback is off and day has no frames
        Rectangle {
          anchors.horizontalCenter: parent.horizontalCenter
          anchors.bottom: parent.bottom
          anchors.bottomMargin: Style.space(20)
          visible: !root.tlPlaybackOn && !root.tlLoading
          height: Style.space(30)
          width: enableText.implicitWidth + Style.space(20)
          radius: Style.cornerRadius
          color: enableMouse.containsMouse ? root.dayflow.accentFill(0.2) : root.dayflow.accentFill(0.12)
          border.color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"

          Text {
            id: enableText
            anchors.centerIn: parent
            text: "Enable playback"
            color: "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
          }

          MouseArea {
            id: enableMouse
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: {
              if (root.dayflow) root.dayflow.uilog("playback enable")
              playbackProc.pendingArg = "on"
              playbackProc.running = true
            }
          }
        }
      }

      // scrub strip
      Rectangle {
        width: parent.width
        height: Style.space(18)
        radius: Style.cornerRadius
        color: root.dayflow ? root.dayflow.fgFill(0.06) : "transparent"
        border.color: root.dayflow ? root.dayflow.fgFill(0.10) : "transparent"

        Rectangle {
          width: root.tlFrames.length > 1
            ? (root.tlIndex / (root.tlFrames.length - 1)) * parent.width
            : 0
          height: parent.height
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"
        }

        MouseArea {
          anchors.fill: parent
          enabled: root.tlFrames.length > 0
          cursorShape: Qt.PointingHandCursor
          onClicked: function(m) { root.tlSeek(Math.round(m.x / width * (root.tlFrames.length - 1))) }
          onPositionChanged: function(m) {
            if (pressed) root.tlSeek(Math.round(m.x / width * (root.tlFrames.length - 1)))
          }
        }
      }

      Text {
        width: parent.width
        visible: root.tlError !== ""
        text: "! " + root.tlError
        color: Color.urgent !== undefined ? Color.urgent : "red"
        font.family: root.dayflow ? root.dayflow.fontFamily : ""
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
    }
  }

  // ============ Context pane ============
  Component {
    id: ctxPane

    Column {
      width: parent ? parent.width : 0
      height: parent ? parent.height : 0
      spacing: Style.space(10)
      leftPadding: Style.space(16)
      rightPadding: Style.space(16)
      topPadding: Style.space(4)

      Text {
        text: "Context shifts — where your attention moves between categories"
        color: root.dayflow ? root.dayflow.dim : "gray"
        font.family: root.dayflow ? root.dayflow.fontFamily : ""
        font.pixelSize: Style.font.body
      }

      // bipartite flow canvas: sources left, targets right, ribbon width ∝ minutes
      Canvas {
        id: ctxCanvas
        width: parent.width
        height: parent.height - topList.height - Style.space(90)

        function layoutNodes() {
          var data = root.ctxNodes()
          var links = root.dayflow ? (root.dayflow.weeklyPayload.context_shifts || []) : []
          var nodeW = Style.space(10)
          var pad = Style.space(8)
          var gap = Style.space(8)
          var H = height - pad * 2
          var names = data.names

          var outM = {}, inM = {}
          for (var i = 0; i < links.length; i++) {
            var l = links[i]
            outM[l.source] = (outM[l.source] || 0) + l.minutes
            inM[l.target] = (inM[l.target] || 0) + l.minutes
          }
          var outTot = 0, inTot = 0
          for (i = 0; i < names.length; i++) {
            outTot += outM[names[i]] || 0
            inTot += inM[names[i]] || 0
          }
          var scaleL = outTot > 0 ? (H - gap * (names.length - 1)) / outTot : 0
          var scaleR = inTot > 0 ? (H - gap * (names.length - 1)) / inTot : 0

          var ly = pad, ry = pad
          var left = {}, right = {}
          for (i = 0; i < names.length; i++) {
            var n = names[i]
            left[n] = { y: ly, h: Math.max(2, (outM[n] || 0) * scaleL), used: 0 }
            ly += left[n].h + gap
            right[n] = { y: ry, h: Math.max(2, (inM[n] || 0) * scaleR), used: 0 }
            ry += right[n].h + gap
          }
          return { left: left, right: right, links: links, nodeW: nodeW, names: names }
        }

        onPaint: {
          var ctx = getContext("2d")
          ctx.reset()
          var L = layoutNodes()
          var x1 = Style.space(70)
          var x2 = width - Style.space(70) - L.nodeW
          var midX = (x1 + L.nodeW + x2) / 2

          // ribbons
          for (var i = 0; i < L.links.length; i++) {
            var l = L.links[i]
            var sl = L.left[l.source], sr = L.right[l.target]
            if (!sl || !sr) continue
            var sc = root.dayflow.categoryColor(l.source)
            // ribbon thickness ∝ this link's share of the node's flow
            var t1 = sl.h > 0 ? (l.minutes / sumOut(L.links, l.source)) * sl.h : 0
            var t2 = sr.h > 0 ? (l.minutes / sumIn(L.links, l.target)) * sr.h : 0
            var y1 = sl.y + sl.used, y2 = sr.y + sr.used
            sl.used += t1; sr.used += t2
            ctx.beginPath()
            ctx.moveTo(x1 + L.nodeW, y1)
            ctx.bezierCurveTo(midX, y1, midX, y2, x2, y2)
            ctx.lineTo(x2, y2 + t2)
            ctx.bezierCurveTo(midX, y2 + t2, midX, y1 + t1, x1 + L.nodeW, y1 + t1)
            ctx.closePath()
            ctx.fillStyle = Qt.rgba(sc.r, sc.g, sc.b, 0.30)
            ctx.fill()
          }

          // nodes + labels
          for (i = 0; i < L.names.length; i++) {
            var n = L.names[i]
            var c = root.dayflow.categoryColor(n)
            ctx.fillStyle = c
            ctx.fillRect(x1, L.left[n].y, L.nodeW, L.left[n].h)
            ctx.fillRect(x2, L.right[n].y, L.nodeW, L.right[n].h)
            ctx.fillStyle = Qt.rgba(0.8, 0.8, 0.85, 0.9)
            ctx.font = Math.round(Style.font.caption) + "px " + (root.dayflow ? root.dayflow.fontFamily : "sans")
            ctx.textAlign = "right"
            ctx.fillText(root.dayflow ? root.dayflow.appDisplayName(n) : n,
                         x1 - Style.space(6), L.left[n].y + L.left[n].h / 2 + 4)
            ctx.textAlign = "left"
            ctx.fillText(root.dayflow ? root.dayflow.appDisplayName(n) : n,
                         x2 + L.nodeW + Style.space(6), L.right[n].y + L.right[n].h / 2 + 4)
          }
        }

        function sumOut(links, name) {
          var s = 0
          for (var i = 0; i < links.length; i++) if (links[i].source === name) s += links[i].minutes
          return s
        }
        function sumIn(links, name) {
          var s = 0
          for (var i = 0; i < links.length; i++) if (links[i].target === name) s += links[i].minutes
          return s
        }

        onWidthChanged: requestPaint()
        onHeightChanged: requestPaint()
        Component.onCompleted: requestPaint()

        Connections {
          target: root.dayflow
          function onWeeklyPayloadChanged() { ctxCanvas.requestPaint() }
        }
      }

      // top shifts list
      Column {
        id: topList
        width: parent.width
        spacing: Style.space(3)

        Repeater {
          model: {
            var links = root.dayflow ? (root.dayflow.weeklyPayload.context_shifts || []) : []
            return links.slice(0, 6)
          }

          delegate: Text {
            required property var modelData
            textFormat: Text.PlainText
            text: (root.dayflow ? root.dayflow.appDisplayName(modelData.source) : modelData.source) +
                  " → " +
                  (root.dayflow ? root.dayflow.appDisplayName(modelData.target) : modelData.target) +
                  "  ·  " + modelData.count + "×  ·  " +
                  (root.dayflow ? root.dayflow.fmtDur(modelData.minutes) : modelData.minutes + "m")
            color: root.dayflow ? root.dayflow.dim : "gray"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
          }
        }

        Text {
          visible: root.dayflow !== null &&
            (root.dayflow.weeklyPayload.context_shifts || []).length === 0
          text: "No category shifts recorded this week."
          color: root.dayflow ? root.dayflow.dim : "gray"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }
      }
    }
  }

  // ============ Agents pane ============
  Component {
    id: agentsPane

    Column {
      width: parent ? parent.width : 0
      height: parent ? parent.height : 0
      spacing: Style.space(8)
      leftPadding: Style.space(16)
      rightPadding: Style.space(16)
      topPadding: Style.space(4)

      Row {
        width: parent.width
        spacing: Style.space(6)

        Repeater {
          model: [
            { label: "‹", act: -1 },
            { label: "Today", act: 0 },
            { label: "›", act: 1 }
          ]

          delegate: Rectangle {
            required property var modelData
            height: Style.space(28)
            width: agNavText.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: agNavMouse.containsMouse ? root.dayflow.fgFill(0.08) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.15) : "transparent"

            Text {
              id: agNavText
              anchors.centerIn: parent
              text: modelData.label
              textFormat: Text.PlainText
              color: root.dayflow ? root.dayflow.foreground : "white"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.body
            }

            MouseArea {
              id: agNavMouse
              anchors.fill: parent
              hoverEnabled: true
              cursorShape: Qt.PointingHandCursor
              onClicked: {
                if (!root.dayflow) return
                if (modelData.act === 0) {
                  if (root.dayflow.dayOffset !== 0) {
                    root.dayflow.dayOffset = 0
                    root.dayflow.loadTimeline()
                  }
                } else {
                  root.dayflow.goDay(modelData.act)
                }
              }
            }
          }
        }

        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: root.dayflow ? root.dayflow.viewDateLabel() : ""
          textFormat: Text.PlainText
          color: root.dayflow ? root.dayflow.dim : "gray"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }
      }

      BusyBar {
        width: parent.width
        pal: root.dayflow
        active: root.agentsLoading
      }

      Text {
        visible: root.agentsError !== ""
        text: "! " + root.agentsError
        textFormat: Text.PlainText
        color: Color.urgent !== undefined ? Color.urgent : "red"
        font.family: root.dayflow ? root.dayflow.fontFamily : ""
        font.pixelSize: Style.font.caption
      }

      Text {
        visible: !root.agentsLoading && root.agentSessions.length === 0 && root.agentsError === ""
        text: "No Claude Code or Codex sessions on this day."
        textFormat: Text.PlainText
        color: root.dayflow ? root.dayflow.dim : "gray"
        font.family: root.dayflow ? root.dayflow.fontFamily : ""
        font.pixelSize: Style.font.body
      }

      Flickable {
        width: parent.width
        height: parent.height - Style.space(110)
        contentHeight: sessCol.implicitHeight
        clip: true

        Column {
          id: sessCol
          width: parent.width
          spacing: Style.space(6)

          Repeater {
            model: root.agentSessions

            delegate: Rectangle {
              required property var modelData
              width: sessCol.width
              height: sessInner.implicitHeight + Style.space(16)
              radius: Style.cornerRadius
              color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
              border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

              Column {
                id: sessInner
                width: parent.width - Style.space(16)
                anchors.centerIn: parent
                spacing: Style.space(3)

                Row {
                  width: parent.width
                  spacing: Style.space(8)

                  Rectangle {
                    height: Style.space(18)
                    width: badgeText.implicitWidth + Style.space(12)
                    radius: Style.cornerRadius
                    color: modelData.source === "claude"
                      ? Qt.rgba(0.85, 0.55, 0.25, 0.2)
                      : Qt.rgba(0.30, 0.65, 0.85, 0.2)

                    Text {
                      id: badgeText
                      anchors.centerIn: parent
                      text: modelData.source
                      textFormat: Text.PlainText
                      color: modelData.source === "claude"
                        ? Qt.rgba(0.95, 0.70, 0.40, 1.0)
                        : Qt.rgba(0.45, 0.80, 1.0, 1.0)
                      font.family: root.dayflow ? root.dayflow.fontFamily : ""
                      font.pixelSize: Style.font.caption
                      font.bold: true
                    }
                  }

                  Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: modelData.project
                    textFormat: Text.PlainText
                    color: root.dayflow ? root.dayflow.foreground : "white"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.body
                    font.bold: true
                    elide: Text.ElideRight
                    width: parent.width - badgeText.implicitWidth - timeText.implicitWidth - Style.space(24)
                  }

                  Text {
                    id: timeText
                    anchors.verticalCenter: parent.verticalCenter
                    anchors.right: parent.right
                    text: Qt.formatTime(new Date(modelData.start * 1000), "hh:mm") +
                          "–" +
                          Qt.formatTime(new Date(modelData.end * 1000), "hh:mm") +
                          " · " + modelData.messages + " msgs"
                    textFormat: Text.PlainText
                    color: root.dayflow ? root.dayflow.dim : "gray"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                  }
                }

                Text {
                  width: parent.width
                  visible: modelData.title !== ""
                  text: modelData.title
                  textFormat: Text.PlainText
                  color: root.dayflow ? root.dayflow.dim : "gray"
                  font.family: root.dayflow ? root.dayflow.fontFamily : ""
                  font.pixelSize: Style.font.caption
                  elide: Text.ElideRight
                }
              }
            }
          }
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
