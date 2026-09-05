import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Panel {
  id: dayflow
  moduleName: "io.github.duketopceo.dayflow"
  manageIpc: false

  signal statusChanged()

  property var anchorItem: null
  property var hostWidget: null
  property var blocks: []
  property string dateLabel: ""
  property bool paused: false
  property bool configured: true
  property string errorText: ""
  property string modelName: ""
  property string activeApp: ""
  property var ignoredApps: []
  property int framesToday: 0
  property int blocksPending: 0
  property string notice: ""
  property string currentTab: "today"
  property var standup: ({ yesterday: { date: "", highlights: [] }, today: { date: "", highlights: [] } })
  property var insights: ({ total_minutes: 0, focus_minutes: 0, distraction_minutes: 0, idle_minutes: 0, categories: [], apps: [], top_distractions: [], focus_blocks: [], days: 0 })

  readonly property color foreground: dayflow.bar ? dayflow.bar.foreground : Color.foreground
  readonly property color dim: Qt.darker(dayflow.foreground, 1.5)
  readonly property string fontFamily: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family

  function open() {
    dayflow.controller.show()
    refreshAll()
  }

  function close() {
    dayflow.controller.hide()
  }

  function switchPanel(direction) {
    if (dayflow.bar && typeof dayflow.bar.switchPanelFrom === "function")
      return dayflow.bar.switchPanelFrom(dayflow.hostWidget || dayflow, direction)
    return false
  }

  function refreshAll() {
    if (!timelineProc.running) timelineProc.running = true
    if (!statusProc.running) statusProc.running = true
    if (!standupFetchProc.running) standupFetchProc.running = true
    if (!insightsFetchProc.running) insightsFetchProc.running = true
  }

  function applyTimeline(raw) {
    try {
      var d = JSON.parse(raw)
      dayflow.blocks = d.blocks || []
      dayflow.dateLabel = d.date || ""
      dayflow.errorText = ""
    } catch (e) {
      dayflow.blocks = []
      dayflow.errorText = "could not read timeline"
    }
  }

  function applyStatus(raw) {
    try {
      var s = JSON.parse(raw)
      dayflow.paused = s.paused === true
      dayflow.configured = s.configured !== false
      dayflow.modelName = s.model || ""
      dayflow.activeApp = s.active_app || ""
      dayflow.ignoredApps = s.ignored_apps || []
      dayflow.framesToday = Number(s.frames_today || 0)
      dayflow.blocksPending = Number(s.blocks_pending || 0)
    } catch (e) {}
  }

  function applyStandup(raw) {
    try {
      var d = JSON.parse(raw)
      dayflow.standup = d
    } catch (e) {}
  }

  function applyInsights(raw) {
    try {
      var d = JSON.parse(raw)
      dayflow.insights = d
    } catch (e) {}
  }

  function appShortName(cls) {
    if (!cls || cls === "") return "?"
    var seg = cls.split(".").pop()
    return seg.length > 12 ? seg.substring(0, 12) : seg
  }

  function categoryColor(cat) {
    switch (cat) {
      case "coding":        return Color.accent
      case "communication": return Qt.lighter(Color.accent, 1.2)
      case "browsing":      return Qt.lighter(Color.accent, 1.4)
      case "writing":       return Qt.lighter(Color.accent, 1.4)
      case "meetings":      return Qt.lighter(Color.accent, 1.2)
      case "design":        return Qt.lighter(Color.accent, 1.3)
      case "idle":          return Qt.darker(dayflow.foreground, 1.8)
      case "personal":      return Color.urgent !== undefined ? Color.urgent : dayflow.foreground
      case "other":         return Qt.darker(dayflow.foreground, 1.5)
      default:              return dayflow.foreground
    }
  }

  function fmtHours(mins) {
    if (mins === undefined || isNaN(mins)) return "0.0"
    return (Number(mins) / 60).toFixed(1)
  }

  Process {
    id: timelineProc
    command: ["dayflow", "timeline", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyTimeline(text)
    }
    onExited: function(exitCode) {
      if (exitCode !== 0) dayflow.errorText = "dayflow CLI not found on PATH"
    }
  }

  Process {
    id: statusProc
    command: ["dayflow", "status", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyStatus(text)
    }
  }

  Process {
    id: standupFetchProc
    command: ["dayflow", "standup", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyStandup(text)
    }
  }

  Process {
    id: insightsFetchProc
    command: ["dayflow", "insights", "week", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyInsights(text)
    }
  }

  Process {
    id: toggleProc
    command: ["dayflow", "toggle"]
    onExited: {
      dayflow.statusChanged()
      Qt.callLater(dayflow.refreshAll)
    }
  }

  Process {
    id: ignoreProc
    command: ["dayflow", "ignore", "--active"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.notice = text.trim()
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: { if (text.trim() !== "") dayflow.notice = text.trim() }
    }
    onExited: {
      dayflow.statusChanged()
      Qt.callLater(dayflow.refreshAll)
    }
  }

  Process {
    id: summarizeProc
    command: ["dayflow", "summarize", "--now"]
    onExited: Qt.callLater(dayflow.refreshAll)
  }

  Process {
    id: copyProc
    command: ["bash", "-c", "dayflow export | wl-copy"]
    onExited: function(exitCode) {
      dayflow.notice = exitCode === 0 ? "copied today's journal" : "copy failed"
    }
  }

  Process {
    id: standupProc
    command: ["bash", "-c", "dayflow standup | wl-copy"]
    onExited: function(exitCode) {
      dayflow.notice = exitCode === 0 ? "copied standup" : "standup copy failed"
    }
  }

  Process {
    id: insightsProc
    command: ["bash", "-c", "dayflow insights | wl-copy"]
    onExited: function(exitCode) {
      dayflow.notice = exitCode === 0 ? "copied daily insights" : "insights copy failed"
    }
  }

  // ---- tab components ----
  Component {
    id: todayTab
    Flickable {
      width: parent.width
      implicitHeight: Math.min(col.implicitHeight, Style.space(360))
      height: implicitHeight
      contentHeight: col.implicitHeight
      clip: true

      Column {
        id: col
        width: parent.width
        spacing: Style.space(10)

        Text {
          visible: dayflow.blocks.length === 0 && dayflow.errorText === "" && dayflow.configured
          width: parent.width
          text: "Nothing summarized yet — blocks land every 15 minutes."
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: Text.WordWrap
        }

        Repeater {
          model: dayflow.blocks

          delegate: Rectangle {
            width: col.width
            height: cardCol.implicitHeight + Style.space(16)
            radius: Style.cornerRadius
            color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.04)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.08)

            Column {
              id: cardCol
              width: parent.width - Style.space(16)
              anchors.centerIn: parent
              spacing: Style.space(4)

              Row {
                width: parent.width
                spacing: Style.space(8)

                Text {
                  text: modelData.start + "–" + modelData.end
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  anchors.verticalCenter: parent.verticalCenter
                }

                Rectangle {
                  height: catText.implicitHeight + Style.space(4)
                  width: catText.implicitWidth + Style.space(10)
                  radius: height / 2
                  color: Qt.rgba(dayflow.categoryColor(modelData.category).r,
                                 dayflow.categoryColor(modelData.category).g,
                                 dayflow.categoryColor(modelData.category).b, 0.15)

                  Text {
                    id: catText
                    anchors.centerIn: parent
                    text: modelData.category
                    color: dayflow.categoryColor(modelData.category)
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.caption
                  }
                }
              }

              Text {
                width: parent.width
                text: modelData.title
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                font.bold: true
                wrapMode: Text.WordWrap
              }

              Text {
                width: parent.width
                text: modelData.summary
                color: dayflow.foreground
                opacity: 0.75
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                wrapMode: Text.WordWrap
                maximumLineCount: 4
                elide: Text.ElideRight
              }

              Repeater {
                model: modelData.activities || []

                delegate: Text {
                  width: parent.width
                  text: dayflow.appShortName(modelData.app) + " · " + modelData.title
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
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

  Component {
    id: standupTab
    Column {
      width: parent.width
      spacing: Style.space(10)

      Text {
        visible: dayflow.standup.yesterday.highlights.length === 0 && dayflow.standup.today.highlights.length === 0
        width: parent.width
        text: "No standup data yet — need a few summarized blocks."
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        wrapMode: Text.WordWrap
      }

      Rectangle {
        visible: dayflow.standup.yesterday.highlights.length > 0
        width: parent.width
        height: yCol.implicitHeight + Style.space(16)
        radius: Style.cornerRadius
        color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.04)
        border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.08)

        Column {
          id: yCol
          width: parent.width - Style.space(16)
          anchors.centerIn: parent
          spacing: Style.space(6)

          Text {
            text: "Yesterday" + (dayflow.standup.yesterday.date ? " · " + dayflow.standup.yesterday.date : "")
            color: Color.accent
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Repeater {
            model: dayflow.standup.yesterday.highlights || []
            delegate: Text {
              width: parent.width
              text: "• " + modelData
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              wrapMode: Text.WordWrap
            }
          }
        }
      }

      Rectangle {
        visible: dayflow.standup.today.highlights.length > 0
        width: parent.width
        height: tCol.implicitHeight + Style.space(16)
        radius: Style.cornerRadius
        color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.04)
        border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.08)

        Column {
          id: tCol
          width: parent.width - Style.space(16)
          anchors.centerIn: parent
          spacing: Style.space(6)

          Text {
            text: "Today" + (dayflow.standup.today.date ? " · " + dayflow.standup.today.date : "")
            color: Color.accent
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Repeater {
            model: dayflow.standup.today.highlights || []
            delegate: Text {
              width: parent.width
              text: "• " + modelData
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              wrapMode: Text.WordWrap
            }
          }
        }
      }

      Rectangle {
        visible: dayflow.standup.today.highlights.length > 0
        width: parent.width
        height: bCol.implicitHeight + Style.space(16)
        radius: Style.cornerRadius
        color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.04)
        border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.08)

        Column {
          id: bCol
          width: parent.width - Style.space(16)
          anchors.centerIn: parent
          spacing: Style.space(6)

          Text {
            text: "Blockers"
            color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Text {
            width: parent.width
            text: "What is in your way? (add manually in standup)"
            color: dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Rectangle {
            id: copyBtn
            height: Style.space(28)
            width: cpy.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: mcp.containsMouse
              ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.15)
              : "transparent"
            border.color: Color.accent

            Text {
              id: cpy
              anchors.centerIn: parent
              text: "Copy standup"
              color: Color.accent
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: mcp
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { if (!standupProc.running) standupProc.running = true }
            }
          }
        }
      }
    }
  }

  Component {
    id: weekTab
    Flickable {
      width: parent.width
      implicitHeight: Math.min(wcol.implicitHeight, Style.space(360))
      height: implicitHeight
      contentHeight: wcol.implicitHeight
      clip: true

      Column {
        id: wcol
        width: parent.width
        spacing: Style.space(10)

        Text {
          visible: dayflow.insights.total_minutes === 0
          width: parent.width
          text: "No weekly data yet."
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: Text.WordWrap
        }

        Rectangle {
          visible: dayflow.insights.total_minutes > 0
          width: parent.width
          height: statCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.04)
          border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.08)

          Column {
            id: statCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(6)

            Text {
              text: "This week" + (dayflow.insights.days ? " · " + dayflow.insights.days + " days" : "")
              color: Color.accent
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }

            Text {
              text: "Tracked: " + dayflow.fmtHours(dayflow.insights.total_minutes) + " hr"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
            }

            Text {
              text: "Focus: " + dayflow.fmtHours(dayflow.insights.focus_minutes) + " hr"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
            }

            Text {
              text: "Distraction / idle: " + dayflow.fmtHours(dayflow.insights.distraction_minutes + dayflow.insights.idle_minutes) + " hr"
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
            }
          }
        }

        Loader {
          id: catLoader
          width: parent.width
          height: item ? item.implicitHeight : 0
          sourceComponent: sectionList
          onLoaded: { item.title = "Categories" }
        }
        Binding { target: catLoader.item; property: "items"; value: dayflow.insights.categories; when: catLoader.status === Loader.Ready }

        Loader {
          id: appLoader
          width: parent.width
          height: item ? item.implicitHeight : 0
          sourceComponent: sectionList
          onLoaded: { item.title = "Top apps" }
        }
        Binding { target: appLoader.item; property: "items"; value: dayflow.insights.apps; when: appLoader.status === Loader.Ready }

        Loader {
          id: distLoader
          width: parent.width
          height: item ? item.implicitHeight : 0
          sourceComponent: sectionList
          onLoaded: { item.title = "Distractions to watch" }
        }
        Binding { target: distLoader.item; property: "items"; value: dayflow.insights.top_distractions; when: distLoader.status === Loader.Ready }

        Rectangle {
          visible: dayflow.insights.focus_blocks.length > 0
          width: parent.width
          height: fCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.04)
          border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.08)

          Column {
            id: fCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(6)

            Text {
              text: "Longest focus blocks"
              color: Color.accent
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }

            Repeater {
              model: dayflow.insights.focus_blocks
              delegate: Text {
                width: parent.width
                text: "• " + (modelData.start_str || modelData.start) + "–" + (modelData.end_str || modelData.end) + " " + modelData.title
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
                wrapMode: Text.WordWrap
              }
            }
          }
        }
      }
    }
  }

  // ---- helpers ----
  Component {
    id: sectionList
    Rectangle {
      visible: items.length > 0
      width: parent ? parent.width : 0
      implicitHeight: sCol.implicitHeight + Style.space(16)
      height: implicitHeight
      radius: Style.cornerRadius
      color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.04)
      border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.08)
      property var items: []
      property string title: ""

      Column {
        id: sCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(6)

        Text {
          text: title
          color: Color.accent
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        Repeater {
          model: items
          delegate: Row {
            width: parent.width
            spacing: Style.space(8)

            Text {
              width: parent.width - mins.implicitWidth - parent.spacing
              text: modelData.name
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
              anchors.verticalCenter: parent.verticalCenter
            }

            Text {
              id: mins
              text: dayflow.fmtHours(modelData.minutes) + " hr"
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              anchors.verticalCenter: parent.verticalCenter
            }
          }
        }
      }
    }
  }

  KeyboardPanel {
    id: panel
    anchorItem: dayflow.anchorItem
    owner: dayflow.hostWidget || dayflow
    bar: dayflow.bar
    open: dayflow.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(340))
    contentHeight: panel.fittedContentHeight(content.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onCloseRequested: dayflow.close()
      onTabRequested: function(direction) { dayflow.switchPanel(direction) }

      Column {
        id: content
        width: parent.width
        leftPadding: Style.space(12)
        rightPadding: Style.space(12)
        topPadding: Style.space(12)
        bottomPadding: Style.space(12)
        spacing: Style.space(10)

        // ---- header ----
        Row {
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(8)

          Column {
            width: parent.width - toggleBtn.width - parent.spacing
            spacing: 0

            Text {
              text: "Dayflow"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.subtitle
              font.bold: true
            }

            Text {
              text: (dayflow.paused ? "paused" : "recording") + " · " + dayflow.modelName
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
              width: parent.width
            }
          }

          Rectangle {
            id: toggleBtn
            height: Style.space(28)
            width: tgl.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: mtgl.containsMouse
              ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.15)
              : "transparent"
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)

            Text {
              id: tgl
              anchors.centerIn: parent
              text: dayflow.paused ? "Resume" : "Pause"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: mtgl
              anchors.fill: parent
              hoverEnabled: true
              onClicked: toggleProc.running = true
            }
          }
        }

        // ---- tab bar ----
        Row {
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(6)

          Repeater {
            model: ["today", "standup", "week"]

            delegate: Rectangle {
              height: Style.space(28)
              width: tabLabel.implicitWidth + Style.space(16)
              radius: Style.cornerRadius
              color: dayflow.currentTab === modelData
                ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.15)
                : (tabMouse.containsMouse
                    ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.10)
                    : "transparent")

              Text {
                id: tabLabel
                anchors.centerIn: parent
                text: modelData.charAt(0).toUpperCase() + modelData.slice(1)
                color: dayflow.currentTab === modelData ? Color.accent : dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
              }

              MouseArea {
                id: tabMouse
                anchors.fill: parent
                hoverEnabled: true
                onClicked: dayflow.currentTab = modelData
              }
            }
          }
        }

        PanelSeparator { foreground: dayflow.foreground }

        // ---- error / not-configured states ----
        Column {
          visible: dayflow.errorText !== "" || !dayflow.configured
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(6)

          Text {
            visible: dayflow.errorText !== ""
            width: parent.width
            text: "! " + dayflow.errorText
            color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Text {
            visible: !dayflow.configured
            width: parent.width
            text: "Not configured yet. Run `dayflow setup` in a terminal."
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }
        }

        // ---- content ----
        Loader {
          id: tabLoader
          width: parent.width - content.leftPadding - content.rightPadding
          height: item ? item.implicitHeight : Style.space(120)
          sourceComponent: dayflow.currentTab === "today" ? todayTab
            : dayflow.currentTab === "standup" ? standupTab
            : weekTab
        }

        PanelSeparator { foreground: dayflow.foreground }

        // ---- quick actions ----
        Flow {
          width: parent.width - content.leftPadding - content.rightPadding
          height: implicitHeight
          spacing: Style.space(6)

          function bgColor(hot) {
            return hot
              ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.15)
              : "transparent"
          }

          Rectangle {
            height: Style.space(26)
            width: a1.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m1.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)
            Text {
              id: a1
              anchors.centerIn: parent
              text: dayflow.paused ? "Resume" : "Pause"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: m1
              anchors.fill: parent
              hoverEnabled: true
              onClicked: toggleProc.running = true
            }
          }

          Rectangle {
            height: Style.space(26)
            width: a2.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m2.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)
            opacity: dayflow.activeApp !== "" ? 1 : 0.45
            Text {
              id: a2
              anchors.centerIn: parent
              text: "Ignore"
              color: dayflow.activeApp !== "" ? dayflow.foreground : dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: m2
              anchors.fill: parent
              hoverEnabled: true
              enabled: dayflow.activeApp !== ""
              onClicked: { if (!ignoreProc.running) ignoreProc.running = true }
            }
          }

          Rectangle {
            height: Style.space(26)
            width: a3.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m3.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)
            Text {
              id: a3
              anchors.centerIn: parent
              text: "Summarize"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: m3
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { if (!summarizeProc.running) summarizeProc.running = true }
            }
          }

          Rectangle {
            height: Style.space(26)
            width: a4.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m4.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)
            Text {
              id: a4
              anchors.centerIn: parent
              text: "Copy journal"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: m4
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { if (!copyProc.running) copyProc.running = true }
            }
          }
        }

        // ---- status ----
        Text {
          width: parent.width - content.leftPadding - content.rightPadding
          text: dayflow.framesToday + " frames · " + dayflow.blocksPending + " pending" +
                (dayflow.ignoredApps.length ? " · ignoring " + dayflow.ignoredApps.map(dayflow.appShortName).join(", ") : "")
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }

        Text {
          visible: dayflow.notice !== ""
          width: parent.width - content.leftPadding - content.rightPadding
          text: dayflow.notice
          color: Color.accent
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          wrapMode: Text.WordWrap
        }
      }
    }
  }
}
