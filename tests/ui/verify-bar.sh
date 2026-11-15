#!/usr/bin/env bash
# Offscreen check of the bar widget's "assist" wiring: the real BarWidget.qml and
# AppLauncher.qml run under quickshell against inert qs.Commons/qs.Ui stubs, a
# stub Panel (counts toggles, carries fullViewOpen/appInstalled), a stub FullView
# and a stub dayflow-ui on PATH. Checks, with and without the app installed:
#   left click   -> launches the app (installed) / toggles the popup (not)
#   middle click -> toggles the popup
#   fullViewOpen -> launches the app and clears the flag (installed) /
#                   loads the FullView window (not installed)
set -u
cd "$(dirname "$0")/../.."
ROOT_REPO=$PWD
command -v quickshell >/dev/null || { echo "skip: quickshell not installed"; exit 0; }
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
R=$TMP/root; mkdir -p "$R/Commons" "$R/Ui" "$TMP/bin-with" "$TMP/bin-without" "$TMP/home"

ln -s "$ROOT_REPO/BarWidget.qml" "$R/BarWidget.qml"
ln -s "$ROOT_REPO/AppLauncher.qml" "$R/AppLauncher.qml"

cat > "$R/Commons/qmldir" <<'E'
module qs.Commons
singleton Color 1.0 Color.qml
singleton Style 1.0 Style.qml
E
cat > "$R/Commons/Color.qml" <<'E'
pragma Singleton
import QtQuick
QtObject { readonly property color foreground: "#cccccc"; readonly property color background: "#101010"
  readonly property color accent: "#88aaff"; readonly property color urgent: "#cc5555"; readonly property color muted: "#888888" }
E
cat > "$R/Commons/Style.qml" <<'E'
pragma Singleton
import QtQuick
QtObject { readonly property QtObject bar: QtObject { readonly property int sizeHorizontal: 26 } }
E
cat > "$R/Ui/qmldir" <<'E'
module qs.Ui
BarWidget 1.0 BarWidget.qml
WidgetButton 1.0 WidgetButton.qml
E
cat > "$R/Ui/BarWidget.qml" <<'E'
import QtQuick
Item { property QtObject bar: null; property string moduleName: "" }
E
cat > "$R/Ui/WidgetButton.qml" <<'E'
import QtQuick
Item { objectName: "widgetButton"; property QtObject bar: null; property string text: ""; property string tooltipText: ""
  implicitWidth: 20; implicitHeight: 20; signal pressed(int button) }
E
# Stubs for the files BarWidget loads by name.
cat > "$R/Tokens.qml" <<'E'
import QtQuick
QtObject {}
E
cat > "$R/Panel.qml" <<'E'
import QtQuick
Item {
  property bool opened: false
  property bool popoutSwitchClosing: false
  property bool fullViewOpen: false
  property bool appInstalled: false
  property var bar; property var anchorItem; property var hostWidget; property var df
  property int toggles: 0
  signal statusChanged()
  function open() { opened = true }
  function close() { opened = false }
  function toggle() { toggles++; opened = !opened }
  function closeForPopoutSwitch() {}
}
E
cat > "$R/FullView.qml" <<'E'
import QtQuick
Item { objectName: "fullView"; property var dayflow: null }
E

cat > "$R/shell.qml" <<'E'
import QtQuick
import Quickshell
ShellRoot {
  id: t
  property int step: 0
  BarWidget { id: bw }
  function find(item, name) {
    if (item.objectName === name) return item
    for (var i = 0; i < item.children.length; i++) { var r = find(item.children[i], name); if (r) return r }
    return null
  }
  Timer {
    interval: 600; repeat: true; running: true
    onTriggered: {
      var btn = t.find(bw, "widgetButton")
      var p = bw.testPanel()
      if (t.step === 0) console.log("BAR installed=" + p.appInstalled)
      else if (t.step === 1) btn.pressed(Qt.LeftButton)
      else if (t.step === 2) console.log("BAR after-left toggles=" + p.toggles)
      else if (t.step === 3) btn.pressed(Qt.MiddleButton)
      else if (t.step === 4) console.log("BAR after-middle toggles=" + p.toggles)
      else if (t.step === 5) p.fullViewOpen = true
      else if (t.step === 6) console.log("BAR after-fullview flag=" + p.fullViewOpen + " fullViewLoaded=" + bw.testFullViewLoaded())
      else if (t.step === 7) Qt.quit()
      t.step++
    }
  }
}
E
fail=0
# BarWidget exposes no test accessors; add them to a copy, never to the real file.
python3 -I - "$ROOT_REPO/BarWidget.qml" "$R/BarWidget.qml" <<'PY'
import sys, os
src = open(sys.argv[1]).read()
os.remove(sys.argv[2])
marker = "  function injectPanel() {"
assert marker in src
extra = """  function testPanel() { return panelLoader.item }
  function testFullViewLoaded() { return fullViewLoader.item !== null }

"""
open(sys.argv[2], "w").write(src.replace(marker, extra + marker, 1))
PY

printf '#!/bin/sh\necho "{}"\n' > "$TMP/bin-with/dayflow"
printf '#!/bin/sh\ntouch "%s/app-ran"\n' "$TMP" > "$TMP/bin-with/dayflow-ui"
chmod +x "$TMP/bin-with/dayflow" "$TMP/bin-with/dayflow-ui"
cp "$TMP/bin-with/dayflow" "$TMP/bin-without/dayflow"

run() { # run <bindir> <log>
  HOME=$TMP/home PATH="$1:/usr/bin:/bin" QT_QPA_PLATFORM=offscreen QT_FORCE_STDERR_LOGGING=1 \
    timeout 20 quickshell --path "$R" >"$2" 2>&1
}
check() { grep -q "$2" "$1" && echo "ok   $3" || { echo "FAIL: $3 (wanted: $2)"; grep -E "BAR|ERROR|error" "$1" | head -8; fail=1; }; }

run "$TMP/bin-with" "$TMP/with.log"
check "$TMP/with.log" "BAR installed=true" "app detected on PATH"
check "$TMP/with.log" "BAR after-left toggles=0" "left click does not toggle the popup when the app is installed"
[ -e "$TMP/app-ran" ] && echo "ok   left click launched the app" || { echo "FAIL: left click did not launch the app"; fail=1; }
check "$TMP/with.log" "BAR after-middle toggles=1" "middle click opens the popup"
check "$TMP/with.log" "flag=false fullViewLoaded=false" "Full view launches the app, clears the flag, loads no window"

rm -f "$TMP/app-ran"
run "$TMP/bin-without" "$TMP/without.log"
check "$TMP/without.log" "BAR installed=false" "app absent detected"
check "$TMP/without.log" "BAR after-left toggles=1" "left click toggles the popup when the app is missing"
[ ! -e "$TMP/app-ran" ] && echo "ok   nothing launched when the app is missing" || { echo "FAIL: launched without the app"; fail=1; }
check "$TMP/without.log" "flag=true fullViewLoaded=true" "Full view still opens the plugin window without the app"

[ "$fail" -eq 0 ] && echo "OK"
exit "$fail"
