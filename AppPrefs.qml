import QtQuick
import Quickshell.Io

// Desktop-app preferences that live outside the engine config: today only
// "start in the tray at login" (an XDG autostart entry, ~/.config/autostart/
// dayflow.desktop). The engine's config.json is never touched, so this does
// not disturb the daemon's hot-reload.
// The app shell owns one instance and hands it to the Settings page.
Item {
  id: prefs

  // Absolute path of the running dayflow-ui (context property from main.cpp).
  property string execPath: ""
  property bool autostart: false
  property bool busy: writeProc.running

  readonly property string autostartFile: "${XDG_CONFIG_HOME:-$HOME/.config}/autostart/dayflow.desktop"

  function refresh() {
    if (!probeProc.running) probeProc.running = true
  }

  function setAutostart(on) {
    if (writeProc.running) return
    if (on && execPath === "") return
    // Exec path is passed as $1, never interpolated into the script.
    writeProc.command = on
      ? ["sh", "-c",
         "d=\"${XDG_CONFIG_HOME:-$HOME/.config}/autostart\" && mkdir -p \"$d\" && " +
         "printf '[Desktop Entry]\\nType=Application\\nVersion=1.0\\nName=Dayflow\\n" +
         "Comment=Start Dayflow in the tray at login\\nExec=%s --background\\n" +
         "Icon=dayflow\\nTerminal=false\\nX-GNOME-Autostart-enabled=true\\n' \"$1\" > \"$d/dayflow.desktop\"",
         "dayflow-autostart", execPath]
      : ["sh", "-c", "rm -f \"" + autostartFile + "\""]
    writeProc.running = true
  }

  Process {
    id: probeProc
    command: ["sh", "-c", "test -f \"" + prefs.autostartFile + "\""]
    onExited: function(code) { prefs.autostart = (code === 0) }
  }

  Process {
    id: writeProc
    onExited: prefs.refresh()
  }

  Component.onCompleted: refresh()
}
