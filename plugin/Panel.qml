import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Panel {
  id: root
  moduleName: "io.github.lukedaduke.dayflow"
  manageIpc: false

  signal statusChanged()

  property var anchorItem: null
  property var hostWidget: null
  property var blocks: []
  property string dateLabel: ""
  property bool paused: false
  property string errorText: ""

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

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.hostWidget || root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(360))
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

        Row {
          width: parent.width
          spacing: Style.space(8)

          Text {
            width: parent.width - pauseBtn.implicitWidth
            text: root.paused ? "Dayflow — paused" : "Dayflow — " + root.dateLabel
            color: root.barForeground
            font.family: root.bar ? root.bar.fontFamily : Style.font.family
            font.pixelSize: Style.font.subtitle
            font.bold: true
            elide: Text.ElideRight
          }

          WidgetButton {
            id: pauseBtn
            bar: root.bar
            text: root.paused ? "resume" : "pause"
            tooltipText: root.paused ? "Resume screen capture" : "Pause screen capture"
            onPressed: toggleProc.running = true
          }
        }

        Text {
          visible: root.errorText !== ""
          width: parent.width
          text: root.errorText
          color: root.barForeground
          font.family: root.bar ? root.bar.fontFamily : Style.font.family
          font.pixelSize: Style.font.body
          wrapMode: Text.WordWrap
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
            spacing: Style.space(10)

            Text {
              visible: root.blocks.length === 0 && root.errorText === ""
              width: parent.width
              text: "No summarized blocks yet — they land every 15 minutes."
              color: root.barForeground
              opacity: 0.7
              font.family: root.bar ? root.bar.fontFamily : Style.font.family
              font.pixelSize: Style.font.body
              wrapMode: Text.WordWrap
            }

            Repeater {
              model: root.blocks

              delegate: Column {
                width: col.width
                spacing: Style.space(2)

                Text {
                  width: parent.width
                  text: modelData.start + "–" + modelData.end + "  " + modelData.title
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
                  opacity: 0.85
                  font.family: root.bar ? root.bar.fontFamily : Style.font.family
                  font.pixelSize: Style.font.body
                  wrapMode: Text.WordWrap
                }
              }
            }
          }
        }
      }
    }
  }
}
