import QtQuick
import QtQuick.Window
import QtCore as Core
import Dayflow.App

// Window host. AppShell (repo root, shared QML) is the section rail, the lazy
// page host and the service strip; this file owns only the native window:
// size and last-section persistence, tray hooks, close-to-tray.
Window {
  id: win
  title: "Dayflow"
  visible: false
  color: "#101315"
  width: persist.width
  height: persist.height
  minimumWidth: 840
  minimumHeight: 560

  signal quitRequested()

  // Tray hooks (set from C++).
  function toggleVisible() { if (visible) hide(); else showAndRaise() }
  function showAndRaise() { show(); raise(); requestActivate() }

  // Stored under ~/.config/dayflow-ui/ (organization name set in main.cpp),
  // never in the engine's config.json, whose mtime drives daemon hot-reload.
  Core.Settings {
    id: persist
    category: "window"
    property int width: 1100
    property int height: 760
    property string section: "today"
  }

  AppShell {
    id: shell
    anchors.fill: parent
    // initialTab is the dev hook (DAYFLOW_UI_TAB); it wins over the restored section.
    initialSection: initialTab !== "" ? (initialTab === "chat" ? "ask" : initialTab) : persist.section
    onSectionChanged: persist.section = section
    onRaiseRequested: win.showAndRaise()
  }

  // Test hook: DAYFLOW_UI_SCRIPT="goto:week;day:-1;..." runs shell steps one
  // per tick and logs "SHELL-STATE step=<s> section=<k> dayOffset=<n>".
  Timer {
    id: scriptTimer
    property var steps: uiScript !== "" ? uiScript.split(";") : []
    property int i: 0
    interval: 400
    repeat: true
    running: steps.length > 0
    onTriggered: {
      if (i >= steps.length) { stop(); return }
      var st = steps[i++]
      console.log("SHELL-STATE step=" + st + " " + shell.runStep(st))
    }
  }

  // Persist the size only after the user stops resizing.
  Timer {
    id: sizeDebounce
    interval: 500
    onTriggered: { persist.width = win.width; persist.height = win.height }
  }
  onWidthChanged: if (visible) sizeDebounce.restart()
  onHeightChanged: if (visible) sizeDebounce.restart()

  onClosing: function(close) {
    if (!trayAvailable) { close.accepted = true; quitRequested() }
    else { close.accepted = false; hide() }
  }
}
