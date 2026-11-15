#!/usr/bin/env bash
# Bar-assist plumbing: (1) AppLauncher detects and launches a dayflow-ui on
# PATH, detached; (2) a second dayflow-ui launch raises the running one and
# exits 0. Scratch HOME / runtime dir; the launcher test uses a stub binary.
set -u
BIN=${1:?usage: launcher.sh path/to/dayflow-ui}
ENGINE="$(dirname "$BIN")/dayflow"
[ -x "$ENGINE" ] || { echo "FAIL: engine not built beside dayflow-ui"; exit 1; }
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
export DAYFLOW_DATA_DIR=$TMP/data DAYFLOW_CONFIG=$TMP/config.json DAYFLOW_ENGINE=$ENGINE
export XDG_CONFIG_HOME=$TMP/xdg XDG_DATA_HOME=$TMP/xdgdata XDG_RUNTIME_DIR=$TMP HOME=$TMP
export QT_FORCE_STDERR_LOGGING=1 QT_STYLE_OVERRIDE= QT_QPA_PLATFORM=offscreen
mkdir -p "$DAYFLOW_DATA_DIR" "$TMP/bin"
echo '{"openrouter_api_key":"test-key"}' > "$DAYFLOW_CONFIG"
fail=0

# ---- AppLauncher ----
cat > "$TMP/Test.qml" <<'QML'
import QtQuick
import Dayflow.App
Item {
  AppLauncher { id: l; binary: "df-stub-ui"; onLaunched: console.log("LAUNCHER launched") }
  Timer { interval: 700; running: true; onTriggered: { console.log("LAUNCHER installed=" + l.installed); l.launch() } }
  Timer { interval: 1800; running: true; onTriggered: Qt.quit() }
}
QML
printf '#!/bin/sh\ntouch "%s/stub-ran"\n' "$TMP" > "$TMP/bin/df-stub-ui"; chmod +x "$TMP/bin/df-stub-ui"

PATH="$TMP/bin:$PATH" DAYFLOW_UI_ALLOW_MULTI=1 DAYFLOW_UI_TESTQML="$TMP/Test.qml" timeout 10 "$BIN" >"$TMP/l1.log" 2>&1
grep -q "LAUNCHER installed=true" "$TMP/l1.log" && echo "ok   launcher sees a binary on PATH" || { echo "FAIL: installed not true"; fail=1; }
[ -e "$TMP/stub-ran" ] && echo "ok   launch() started it" || { echo "FAIL: stub not run"; fail=1; }
grep -q "LAUNCHER launched" "$TMP/l1.log" && echo "ok   launched signal fired" || { echo "FAIL: no launched signal"; fail=1; }

rm -f "$TMP/stub-ran"; rm "$TMP/bin/df-stub-ui"
DAYFLOW_UI_ALLOW_MULTI=1 DAYFLOW_UI_TESTQML="$TMP/Test.qml" timeout 10 "$BIN" >"$TMP/l2.log" 2>&1
grep -q "LAUNCHER installed=false" "$TMP/l2.log" && echo "ok   launcher reports missing binary" || { echo "FAIL: installed not false"; fail=1; }
[ ! -e "$TMP/stub-ran" ] && echo "ok   launch() is inert when not installed" || { echo "FAIL: launched a missing binary"; fail=1; }

# ---- second launch raises the first ----
timeout 12 "$BIN" >"$TMP/first.log" 2>&1 &
first=$!
sleep 3
timeout 5 "$BIN" >"$TMP/second.log" 2>&1
rc=$?
sleep 1
[ "$rc" -eq 0 ] && echo "ok   second launch exits 0" || { echo "FAIL: second launch rc=$rc"; fail=1; }
grep -q "raise requested" "$TMP/first.log" && echo "ok   first instance got the raise" || { echo "FAIL: no raise in first log"; cat "$TMP/first.log"; fail=1; }
# --background (autostart) must not raise
timeout 5 "$BIN" --background >"$TMP/third.log" 2>&1
sleep 1
[ "$(grep -c 'raise requested' "$TMP/first.log")" -eq 1 ] && echo "ok   --background does not raise" || { echo "FAIL: --background raised"; fail=1; }
kill "$first" 2>/dev/null; wait "$first" 2>/dev/null

[ "$fail" -eq 0 ] && echo "OK"
exit "$fail"
