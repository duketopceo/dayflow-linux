import QtQuick

// Window-host version of Omarchy's qs.Ui.Panel base: open/close state and
// the property surface Panel.qml expects, minus bar/IPC wiring.
Item {
  id: root
  property QtObject bar: null
  property string moduleName: ""
  property var settings: ({})
  property string ipcTarget: ""
  property bool manageIpc: true
  property alias controller: panelController
  property bool popoutSwitching: false
  property bool popoutSwitchClosing: false
  readonly property bool opened: panelController.open

  function open() { panelController.show() }
  function close() { panelController.hide() }
  function toggle() { opened ? close() : open() }
  function switchPanel(direction) { return false }
  function setting(name, fallback) {
    var v = settings ? settings[name] : undefined
    return v === undefined || v === null ? fallback : v
  }

  PanelController { id: panelController }
}
