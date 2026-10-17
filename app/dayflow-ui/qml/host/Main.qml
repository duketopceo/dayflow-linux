import QtQuick
import QtQuick.Window
import Dayflow.App
import qs.Ui as Ui

// Window host. Panel.qml (unmodified) is the controller + Today/Settings tab
// UI; its KeyboardPanel shim reports the content size the window follows.
Window {
  id: win
  title: "Dayflow"
  visible: false
  color: "#101315"
  width: 560
  height: 640
  minimumWidth: 420
  minimumHeight: 360

  signal quitRequested()

  // Tray hooks (set from C++).
  function toggleVisible() { if (visible) hide(); else showAndRaise() }
  function showAndRaise() { show(); raise(); requestActivate() }

  Panel {
    id: panel
    anchors.fill: parent
    Component.onCompleted: { panel.open(); if (initialTab !== "") panel.currentTab = initialTab }
  }

  // Follow the panel's fitted size (Expand/Shrink changes it).
  property int wantW: Ui.HostSizing.width
  property int wantH: Ui.HostSizing.height
  onWantWChanged: if (wantW > 0) width = wantW
  onWantHChanged: if (wantH > 0) height = wantH

  onClosing: function(close) {
    if (!trayAvailable) { close.accepted = true; quitRequested() }
    else { close.accepted = false; hide() }
  }
}
