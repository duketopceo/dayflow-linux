#!/usr/bin/env bash
# First-run onboarding gate and the autostart preference.
#  1. unconfigured install  -> onboarding=true; "Skip" state is the panel's
#  2. configured install    -> onboarding=false
#  3. autostart:on          -> XDG autostart entry written with an absolute
#                              Exec ... --background; autostart:off removes it
# Everything runs under a scratch HOME / XDG dirs; the real ones are untouched.
set -u
BIN=${1:?usage: prefs.sh path/to/dayflow-ui}
ENGINE="$(dirname "$BIN")/dayflow"
[ -x "$ENGINE" ] || { echo "FAIL: engine not built beside dayflow-ui"; exit 1; }
fail=0

run() { # run <script> <tmp>  -> log at $2/log
  local tmp=$2
  export DAYFLOW_DATA_DIR=$tmp/data DAYFLOW_CONFIG=$tmp/config.json DAYFLOW_ENGINE=$ENGINE
  export XDG_CONFIG_HOME=$tmp/xdg XDG_DATA_HOME=$tmp/xdgdata XDG_RUNTIME_DIR=$tmp HOME=$tmp
  export QT_FORCE_STDERR_LOGGING=1 QT_STYLE_OVERRIDE= QT_QPA_PLATFORM=offscreen DAYFLOW_UI_ALLOW_MULTI=1
  mkdir -p "$DAYFLOW_DATA_DIR"
  DAYFLOW_UI_SCRIPT=$1 DAYFLOW_UI_SCREENSHOT=$tmp/shot.png timeout 15 "$BIN" >"$tmp/log" 2>&1
}
last_state() { grep -o 'SHELL-STATE.*' "$1/log" | tail -1; }

# 1. unconfigured
T1=$(mktemp -d); # The status reply that flips `configured` arrives asynchronously; later steps see it.
run "goto:today;goto:today;goto:today;goto:today" "$T1"
case "$(last_state "$T1")" in *onboarding=true*) echo "ok   unconfigured shows onboarding" ;;
  *) echo "FAIL: unconfigured did not show onboarding: $(last_state "$T1")"; fail=1 ;; esac
[ -s "$T1/shot.png" ] || { echo "FAIL: no onboarding screenshot"; fail=1; }
grep -Eq "(qrc|file):.*(Error|error)|ReferenceError|TypeError|Binding loop" "$T1/log" \
  && { echo "FAIL: QML errors (onboarding)"; grep -E "(qrc|file):|ReferenceError|TypeError|Binding loop" "$T1/log" | sort -u | head; fail=1; }
[ -n "${ONBOARDING_SHOT:-}" ] && cp "$T1/shot.png" "$ONBOARDING_SHOT"
rm -rf "$T1"

# 2. configured, then 3. autostart on/off
T2=$(mktemp -d)
mkdir -p "$T2"; echo '{"openrouter_api_key":"test-key"}' > "$T2/config.json"
run "goto:today;autostart:on;goto:today;goto:today;goto:today" "$T2"
case "$(last_state "$T2")" in *onboarding=false*) echo "ok   configured hides onboarding" ;;
  *) echo "FAIL: configured still shows onboarding: $(last_state "$T2")"; fail=1 ;; esac
f=$T2/xdg/autostart/dayflow.desktop
if [ -f "$f" ] && grep -q "^Exec=$BIN --background\$" "$f"; then echo "ok   autostart:on wrote entry"
else echo "FAIL: autostart entry missing or wrong"; cat "$f" 2>/dev/null; fail=1; fi
run "autostart:off" "$T2"
[ ! -e "$f" ] && echo "ok   autostart:off removed entry" || { echo "FAIL: autostart entry still present"; fail=1; }
rm -rf "$T2"

[ "$fail" -eq 0 ] && echo "OK"
exit "$fail"
