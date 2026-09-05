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

  readonly property color foreground: dayflow.bar ? dayflow.bar.foreground : Color.foreground
  readonly property color dim: Qt.darker(dayflow.foreground, 1.5)

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
      case "other":         return Qt.darker(dayflow.foreground, 1.5)
      default:              return dayflow.foreground
    }
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
      dayflow.notice = exitCode === 0 ? "copied today's journal as markdown" : "copy failed"
    }
  }

  Process {
    id: standupProc
    command: ["bash", "-c", "dayflow standup | wl-copy"]
    onExited: function(exitCode) {
      dayflow.notice = exitCode === 0 ? "copied standup to clipboard" : "standup copy failed"
    }
  }

  Process {
    id: insightsProc
    command: ["bash", "-c", "dayflow insights | wl-copy"]
    onExited: function(exitCode) {
      dayflow.notice = exitCode === 0 ? "copied weekly insights to clipboard" : "insights copy failed"
    }
  }

  KeyboardPanel {
    id: panel
    anchorItem: dayflow.anchorItem
    owner: dayflow.hostWidget || dayflow
    bar: dayflow.bar
    open: dayflow.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(320))
    contentHeight: panel.fittedContentHeight(flick.implicitHeight)

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
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
              font.pixelSize: Style.font.subtitle
              font.bold: true
            }

            Text {
              text: (dayflow.paused ? "paused" : "recording") + " · " + dayflow.modelName
              color: dayflow.dim
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
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
              ? Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.12)
              : "transparent"
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)

            Text {
              id: tgl
              anchors.centerIn: parent
              text: dayflow.paused ? "Resume" : "Pause"
              color: dayflow.foreground
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
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
            font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Text {
            visible: !dayflow.configured
            width: parent.width
            text: "Not configured yet. Run `dayflow setup` in a terminal."
            color: dayflow.foreground
            font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }
        }

        // ---- timeline ----
        PanelSectionHeader {
          text: "Today · " + dayflow.dateLabel
          foreground: dayflow.foreground
          visible: dayflow.blocks.length > 0
        }

        Flickable {
          id: flick
          width: parent.width - content.leftPadding - content.rightPadding
          implicitHeight: Math.min(col.implicitHeight, Style.space(360))
          height: implicitHeight
          contentHeight: col.implicitHeight
          clip: true

          Column {
            id: col
            width: flick.width
            spacing: Style.space(10)

            Text {
              visible: dayflow.blocks.length === 0 && dayflow.errorText === "" && dayflow.configured
              width: parent.width
              text: "Nothing summarized yet — blocks land every 15 minutes."
              color: dayflow.dim
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
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
                      font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
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
                        font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
                        font.pixelSize: Style.font.caption
                      }
                    }
                  }

                  Text {
                    width: parent.width
                    text: modelData.title
                    color: dayflow.foreground
                    font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
                    font.pixelSize: Style.font.body
                    font.bold: true
                    wrapMode: Text.WordWrap
                  }

                  Text {
                    width: parent.width
                    text: modelData.summary
                    color: dayflow.foreground
                    opacity: 0.75
                    font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
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
                      font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
                      font.pixelSize: Style.font.caption
                      elide: Text.ElideRight
                    }
                  }
                }
              }
            }
          }
        }

        PanelSeparator { foreground: dayflow.foreground }

        // ---- actions ----
        Flow {
          width: parent.width - content.leftPadding - content.rightPadding
          height: implicitHeight
          spacing: Style.space(6)

          function bgColor(hot) {
            return hot
              ? Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.12)
              : "transparent"
          }

          Rectangle {
            height: Style.space(26)
            width: lbl1.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m1.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)

            Text {
              id: lbl1
              anchors.centerIn: parent
              text: dayflow.paused ? "Resume" : "Pause"
              color: dayflow.foreground
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
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
            width: lbl2.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m2.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)
            opacity: dayflow.activeApp !== "" ? 1 : 0.45

            Text {
              id: lbl2
              anchors.centerIn: parent
              text: "Ignore"
              color: dayflow.activeApp !== "" ? dayflow.foreground : dayflow.dim
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
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
            width: lbl3.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m3.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)

            Text {
              id: lbl3
              anchors.centerIn: parent
              text: "Summarize"
              color: dayflow.foreground
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
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
            width: lbl4.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m4.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)

            Text {
              id: lbl4
              anchors.centerIn: parent
              text: "Copy"
              color: dayflow.foreground
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: m4
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { if (!copyProc.running) copyProc.running = true }
            }
          }

          Rectangle {
            height: Style.space(26)
            width: lbl5.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m5.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)

            Text {
              id: lbl5
              anchors.centerIn: parent
              text: "Standup"
              color: dayflow.foreground
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: m5
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { if (!standupProc.running) standupProc.running = true }
            }
          }

          Rectangle {
            height: Style.space(26)
            width: lbl6.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: bgColor(m6.containsMouse)
            border.color: Qt.rgba(dayflow.foreground.r, dayflow.foreground.g, dayflow.foreground.b, 0.15)

            Text {
              id: lbl6
              anchors.centerIn: parent
              text: "Insights"
              color: dayflow.foreground
              font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: m6
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { if (!insightsProc.running) insightsProc.running = true }
            }
          }
        }

        // ---- status ----
        Text {
          width: parent.width - content.leftPadding - content.rightPadding
          text: dayflow.framesToday + " frames · " + dayflow.blocksPending + " pending" +
                (dayflow.ignoredApps.length ? " · ignoring " + dayflow.ignoredApps.map(dayflow.appShortName).join(", ") : "")
          color: dayflow.dim
          font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }

        Text {
          visible: dayflow.notice !== ""
          width: parent.width - content.leftPadding - content.rightPadding
          text: dayflow.notice
          color: Color.accent
          font.family: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
          wrapMode: Text.WordWrap
        }
      }
    }
  }
}
