import QtQuick
import Quickshell
import Quickshell.Io
import qs.Ui

BarWidget {
  id: root
  moduleName: "io.github.duketopceo.dayflow"

  property bool paused: false
  property int blocksDone: 0
  property int framesToday: 0
  property bool available: false

  readonly property bool opened: panelLoader.item
    ? panelLoader.item.opened === true
    : false
  readonly property bool popoutSwitchClosing: panelLoader.item
    ? panelLoader.item.popoutSwitchClosing === true
    : false

  function open() {
    if (panelLoader.item) panelLoader.item.open()
  }

  function close() {
    if (panelLoader.item) panelLoader.item.close()
  }

  function toggle() {
    if (panelLoader.item) panelLoader.item.toggle()
  }

  function closeForPopoutSwitch() {
    if (panelLoader.item) panelLoader.item.closeForPopoutSwitch()
  }

  function injectPanel() {
    if (!panelLoader.item) return
    panelLoader.item.bar = root.bar
    panelLoader.item.anchorItem = button
    panelLoader.item.hostWidget = root
  }

  function refresh() {
    if (!statusProc.running) statusProc.running = true
  }

  function applyStatus(raw) {
    try {
      var s = JSON.parse(raw)
      root.paused = s.paused === true
      root.blocksDone = Number(s.blocks_done || 0)
      root.framesToday = Number(s.frames_today || 0)
      root.available = true
    } catch (e) {
      root.available = false
    }
  }

  visible: available
  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  onBarChanged: injectPanel()

  Process {
    id: statusProc
    command: ["dayflow", "status", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyStatus(text)
    }
    onExited: function(exitCode) {
      if (exitCode !== 0) root.available = false
    }
  }

  Timer {
    interval: 60000
    running: true
    repeat: true
    triggeredOnStart: true
    onTriggered: root.refresh()
  }

  Loader {
    id: panelLoader
    active: true
    source: Qt.resolvedUrl("Panel.qml")
    visible: false
    onLoaded: {
      root.injectPanel()
      Qt.callLater(root.injectPanel)
    }
  }

  WidgetButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: root.paused ? "ᛯ" : "󰚯"
    tooltipText: root.paused
      ? "Dayflow paused — click for timeline, right-click to resume"
      : "Dayflow recording — click for timeline, right-click to pause"
    onPressed: function(buttonCode) {
      if (buttonCode === Qt.LeftButton) {
        root.toggle()
      } else if (buttonCode === Qt.RightButton) {
        toggleProc.running = true
      }
    }
  }

  Process {
    id: toggleProc
    command: ["dayflow", "toggle"]
    onExited: Qt.callLater(root.refresh)
  }

  Connections {
    target: panelLoader.item
    ignoreUnknownSignals: true
    function onStatusChanged() { root.refresh() }
  }
}
