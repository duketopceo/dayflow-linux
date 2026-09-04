import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Panel {
  id: root
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

  function open() {
    root.controller.show()
    refreshAll()
  }

  function close() {
    root.controller.hide()
  }

  function switchPanel(direction) {
    if (root.bar && typeof root.bar.switchPanelFrom === "function")
      return root.bar.switchPanelFrom(root.hostWidget || root, direction)
    return false
  }

  function refreshAll() {
    if (!timelineProc.running) timelineProc.running = true
    if (!statusProc.running) statusProc.running = true
  }

  function applyTimeline(raw) {
    try {
      var d = JSON.parse(raw)
      root.blocks = d.blocks || []
      root.dateLabel = d.date || ""
      root.errorText = ""
    } catch (e) {
      root.blocks = []
      root.errorText = "could not read timeline"
    }
  }

  function applyStatus(raw) {
    try {
      var s = JSON.parse(raw)
      root.paused = s.paused === true
      root.configured = s.configured !== false
      root.modelName = s.model || ""
      root.activeApp = s.active_app || ""
      root.ignoredApps = s.ignored_apps || []
      root.framesToday = Number(s.frames_today || 0)
      root.blocksPending = Number(s.blocks_pending || 0)
    } catch (e) {}
  }

  Process {
    id: timelineProc
    command: ["dayflow", "timeline", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyTimeline(text)
    }
    onExited: function(exitCode) {
      if (exitCode !== 0) root.errorText = "dayflow CLI not found on PATH"
    }
  }

  Process {
    id: statusProc
    command: ["dayflow", "status", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyStatus(text)
    }
  }

  Process {
    id: toggleProc
    command: ["dayflow", "toggle"]
    onExited: {
      root.statusChanged()
      Qt.callLater(root.refreshAll)
    }
  }

  Process {
    id: ignoreProc
    command: ["dayflow", "ignore", "--active"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.notice = text.trim()
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: { if (text.trim() !== "") root.notice = text.trim() }
    }
    onExited: {
      root.statusChanged()
      Qt.callLater(root.refreshAll)
    }
  }

  Process {
    id: summarizeProc
    command: ["dayflow", "summarize", "--now"]
    onExited: Qt.callLater(root.refreshAll)
  }

  Process {
    id: copyProc
    command: ["bash", "-c", "dayflow export | wl-copy"]
    onExited: function(exitCode) {
      root.notice = exitCode === 0 ? "copied today's journal as markdown" : "copy failed"
    }
  }

  function categoryColor(cat) {
    switch (cat) {
      case "coding":        return Color.accent
      case "communication": return root.barForeground
      case "idle":          return Qt.darker(root.barForeground, 1.8)
      default:              return root.barForeground
    }
  }

  function appIconSource(cls) {
    if (!cls || cls === "") return Quickshell.iconPath("application-x-executable", true)
    var direct = Quickshell.iconPath(cls, true)
    if (direct.length > 0) return direct
    // reverse-domain classes: try the last segment
    var seg = cls.split(".").pop()
    if (seg !== cls) {
      var t = Quickshell.iconPath(seg, true)
      if (t.length > 0) return t
    }
    return Quickshell.iconPath("application-x-executable", true)
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.hostWidget || root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(flick.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }

      Column {
        id: content
        width: parent.width
        spacing: Style.space(8)

        // ---- header ----
        Row {
          width: parent.width
          spacing: Style.space(8)

          Text {
            id: sunIcon
            text: "󰖨"
            color: root.paused ? Qt.darker(Color.accent, 1.6) : Color.accent
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.title
            anchors.verticalCenter: parent.verticalCenter
          }

          Column {
            width: parent.width - sunIcon.width - actions.implicitWidth - Style.space(16)
            spacing: 0

            Text {
              text: "Dayflow"
              color: root.barForeground
              font.family: root.bar ? root.bar.fontFamily : Style.font.family
              font.pixelSize: Style.font.subtitle
              font.bold: true
            }
            Text {
              text: (root.paused ? "paused" : "recording") + "  ·  " + root.modelName
              color: Qt.darker(root.barForeground, 1.5)
              font.family: root.bar ? root.bar.fontFamily : Style.font.family
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
              width: parent.width
            }
          }

          Row {
            id: actions
            spacing: Style.space(4)
            anchors.verticalCenter: parent.verticalCenter

            PanelActionButton {
              iconText: root.paused ? "󰐊" : "󰏤"
              tooltipText: root.paused ? "Resume capture" : "Pause capture"
              foreground: root.barForeground
              onClicked: toggleProc.running = true
            }
            PanelActionButton {
              iconText: "󰆏"
              tooltipText: "Copy today as markdown"
              foreground: root.barForeground
              onClicked: { if (!copyProc.running) copyProc.running = true }
            }
            PanelActionButton {
              iconText: "󰑓"
              tooltipText: "Refresh"
              foreground: root.barForeground
              onClicked: root.refreshAll()
            }
          }
        }

        PanelSeparator { foreground: root.barForeground }

        // ---- error / not-configured states ----
        Column {
          visible: root.errorText !== "" || !root.configured
          width: parent.width
          spacing: Style.space(6)

          Text {
            visible: root.errorText !== ""
            width: parent.width
            text: "⚠ " + root.errorText
            color: Color.urgent !== undefined ? Color.urgent : root.barForeground
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Text {
            visible: !root.configured
            width: parent.width
            text: "Not configured yet. Run `dayflow setup` in a terminal to connect OpenRouter."
            color: root.barForeground
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }
        }

        // ---- timeline ----
        PanelSectionHeader {
          text: "Today  ·  " + root.dateLabel
          foreground: root.barForeground
          visible: root.blocks.length > 0
        }

        Flickable {
          id: flick
          width: parent.width
          implicitHeight: Math.min(col.implicitHeight, Style.space(400))
          height: implicitHeight
          contentHeight: col.implicitHeight
          clip: true

          Column {
            id: col
            width: flick.width
            spacing: Style.space(12)

            Text {
              visible: root.blocks.length === 0 && root.errorText === "" && root.configured
              width: parent.width
              text: "Nothing summarized yet — blocks land every 15 minutes."
              color: Qt.darker(root.barForeground, 1.5)
              font.family: root.bar ? root.bar.fontFamily : Style.font.family
              font.pixelSize: Style.font.body
              wrapMode: Text.WordWrap
            }

            Repeater {
              model: root.blocks

              delegate: Column {
                width: col.width
                spacing: Style.space(2)

                Row {
                  width: parent.width
                  spacing: Style.space(6)

                  Image {
                    width: Style.font.body
                    height: Style.font.body
                    source: root.appIconSource(modelData.app)
                    fillMode: Image.PreserveAspectFit
                    anchors.verticalCenter: parent.verticalCenter
                  }

                  Text {
                    text: modelData.start + "–" + modelData.end
                    color: Qt.darker(root.barForeground, 1.4)
                    font.family: root.bar ? root.bar.fontFamily : Style.font.family
                    font.pixelSize: Style.font.caption
                  }

                  Rectangle {
                    height: catText.implicitHeight + 2
                    width: catText.implicitWidth + Style.space(8)
                    radius: height / 2
                    color: "transparent"
                    border.color: root.categoryColor(modelData.category)
                    border.width: 1
                    opacity: 0.8
                    anchors.verticalCenter: parent.verticalCenter

                    Text {
                      id: catText
                      anchors.centerIn: parent
                      text: modelData.category
                      color: root.categoryColor(modelData.category)
                      font.family: root.bar ? root.bar.fontFamily : Style.font.family
                      font.pixelSize: Style.font.caption
                    }
                  }
                }

                Text {
                  width: parent.width
                  text: modelData.title
                  color: root.barForeground
                  font.family: root.bar ? root.bar.fontFamily : Style.font.family
                  font.pixelSize: Style.font.body
                  font.bold: true
                  wrapMode: Text.WordWrap
                }

                Text {
                  width: parent.width
                  text: modelData.summary
                  color: root.barForeground
                  opacity: 0.8
                  font.family: root.bar ? root.bar.fontFamily : Style.font.family
                  font.pixelSize: Style.font.body
                  wrapMode: Text.WordWrap
                }

                Repeater {
                  model: modelData.activities || []

                  delegate: Row {
                    width: parent ? parent.width : 0
                    spacing: Style.space(6)
                    leftPadding: Style.space(10)

                    Image {
                      width: Style.font.caption
                      height: Style.font.caption
                      source: root.appIconSource(modelData.app)
                      fillMode: Image.PreserveAspectFit
                      anchors.verticalCenter: parent.verticalCenter
                    }

                    Text {
                      width: parent.width - Style.space(24)
                      text: modelData.app + "  ·  " + modelData.title
                      color: Qt.darker(root.barForeground, 1.3)
                      font.family: root.bar ? root.bar.fontFamily : Style.font.family
                      font.pixelSize: Style.font.caption
                      elide: Text.ElideRight
                    }
                  }
                }
              }
            }
          }
        }

        PanelSeparator { foreground: root.barForeground }

        // ---- controls ----
        Row {
          width: parent.width
          spacing: Style.space(6)

          PanelActionButton {
            iconText: "󰈈"
            tooltipText: "Ignore focused app" + (root.activeApp !== "" ? " (" + root.activeApp + ")" : "")
            foreground: root.barForeground
            enabled: root.activeApp !== ""
            onClicked: { if (!ignoreProc.running) ignoreProc.running = true }
          }

          PanelActionButton {
            iconText: "󰓨"
            tooltipText: "Summarize pending blocks now"
            foreground: root.barForeground
            onClicked: { if (!summarizeProc.running) summarizeProc.running = true }
          }

          Text {
            anchors.verticalCenter: parent.verticalCenter
            width: parent.width - Style.space(96)
            elide: Text.ElideRight
            text: root.framesToday + " frames · " + root.blocksPending + " pending" +
                  (root.ignoredApps.length ? "  ·  ignoring " + root.ignoredApps.join(", ") : "")
            color: Qt.darker(root.barForeground, 1.5)
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.caption
          }
        }

        Text {
          visible: root.notice !== ""
          width: parent.width
          text: root.notice
          color: Color.accent
          font.family: root.bar ? root.bar.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
          wrapMode: Text.WordWrap
        }
      }
    }
  }
}
