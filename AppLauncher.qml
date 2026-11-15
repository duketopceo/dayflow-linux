import QtQuick
import Quickshell.Io

// Opens the Dayflow desktop app from the bar plugin. `installed` is whether a
// `dayflow-ui` is on PATH; `launch()` starts it detached (setsid -f, so a bar
// restart does not take the app down). A second launch hands the running app
// a "raise" and exits, so launch() is also "bring the window up".
Item {
  id: launcher

  property bool installed: false
  // Binary to look for; tests point this at a stub.
  property string binary: "dayflow-ui"
  signal launched()

  function refresh() {
    if (!probeProc.running) probeProc.running = true
  }

  function launch() {
    if (!installed || launchProc.running) return
    launchProc.command = ["sh", "-c", "setsid -f \"$1\" >/dev/null 2>&1", "dayflow-launch", launcher.binary]
    launchProc.running = true
  }

  Process {
    id: probeProc
    command: ["sh", "-c", "command -v \"$1\" >/dev/null 2>&1", "dayflow-probe", launcher.binary]
    onExited: function(code) { launcher.installed = (code === 0) }
  }

  Process {
    id: launchProc
    onExited: launcher.launched()
  }

  // Re-probe on a slow timer so installing the app later is picked up without
  // restarting the shell.
  Timer {
    interval: 300000
    running: true
    repeat: true
    triggeredOnStart: true
    onTriggered: launcher.refresh()
  }
}
