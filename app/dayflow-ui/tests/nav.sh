#!/usr/bin/env bash
# Shell navigation and day-stepping, driven through DAYFLOW_UI_SCRIPT: the steps
# call the same functions the Ctrl+N, Alt+Left/Right, Left/Right and T shortcuts
# call, then log the state. Checks the log against the expected sequence.
set -u
BIN=${1:?usage: nav.sh path/to/dayflow-ui}
ENGINE="$(dirname "$BIN")/dayflow"
[ -x "$ENGINE" ] || { echo "FAIL: engine not built beside dayflow-ui"; exit 1; }
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
export DAYFLOW_DATA_DIR=$TMP/data DAYFLOW_CONFIG=$TMP/config.json DAYFLOW_ENGINE=$ENGINE
export XDG_CONFIG_HOME=$TMP/xdg XDG_DATA_HOME=$TMP/xdgdata XDG_RUNTIME_DIR=$TMP
export QT_FORCE_STDERR_LOGGING=1 QT_STYLE_OVERRIDE= QT_QPA_PLATFORM=offscreen DAYFLOW_UI_ALLOW_MULTI=1
mkdir -p "$DAYFLOW_DATA_DIR"
"$ENGINE" dev seed-day >/dev/null 2>&1 || { echo "FAIL: seed-day"; exit 1; }

STEPS="goto:week;day:-1;day:-1;day:1;today;key:3;day:-1;goto:settings;day:-1;rel:1;rel:-2;key:8;refresh"
DAYFLOW_UI_SCRIPT=$STEPS timeout 12 "$BIN" >"$TMP/log" 2>&1

got=$(grep -o 'SHELL-STATE.*' "$TMP/log")
want='SHELL-STATE step=goto:week section=week dayOffset=0
SHELL-STATE step=day:-1 section=week dayOffset=-1
SHELL-STATE step=day:-1 section=week dayOffset=-2
SHELL-STATE step=day:1 section=week dayOffset=-1
SHELL-STATE step=today section=week dayOffset=0
SHELL-STATE step=key:3 section=standup dayOffset=0
SHELL-STATE step=day:-1 section=standup dayOffset=0
SHELL-STATE step=goto:settings section=settings dayOffset=0
SHELL-STATE step=day:-1 section=settings dayOffset=0
SHELL-STATE step=rel:1 section=today dayOffset=0
SHELL-STATE step=rel:-2 section=context dayOffset=0
SHELL-STATE step=key:8 section=settings dayOffset=0
SHELL-STATE step=refresh section=settings dayOffset=0'
if [ "$got" != "$want" ]; then
  echo "FAIL: shell state sequence differs"; diff <(echo "$want") <(echo "$got"); exit 1
fi
if grep -Eq "(qrc|file):.*(Error|error)|ReferenceError|TypeError|Binding loop" "$TMP/log"; then
  echo "FAIL: QML errors"; grep -E "(qrc|file):|ReferenceError|TypeError|Binding loop" "$TMP/log" | sort -u | head; exit 1
fi
echo "OK"
