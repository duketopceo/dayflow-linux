#!/usr/bin/env bash
# Ctrl+K search: the overlay runs `dayflow search`, and picking a result jumps
# to that result's day on Today. Seeds one fixture day two days back, searches
# a word from its blocks, picks the first hit, expects dayOffset=-2 on Today.
# A no-match query and Esc (close) are checked too. Scratch HOME throughout.
set -u
BIN=${1:?usage: search.sh path/to/dayflow-ui}
ENGINE="$(dirname "$BIN")/dayflow"
[ -x "$ENGINE" ] || { echo "FAIL: engine not built beside dayflow-ui"; exit 1; }
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
export DAYFLOW_DATA_DIR=$TMP/data DAYFLOW_CONFIG=$TMP/config.json DAYFLOW_ENGINE=$ENGINE
export XDG_CONFIG_HOME=$TMP/xdg XDG_DATA_HOME=$TMP/xdgdata XDG_RUNTIME_DIR=$TMP HOME=$TMP
export QT_FORCE_STDERR_LOGGING=1 QT_STYLE_OVERRIDE= QT_QPA_PLATFORM=offscreen DAYFLOW_UI_ALLOW_MULTI=1
mkdir -p "$DAYFLOW_DATA_DIR"
echo '{"openrouter_api_key":"test-key"}' > "$DAYFLOW_CONFIG"
DAY=$(date -d '2 days ago' +%F)
"$ENGINE" dev seed-day "$DAY" >/dev/null 2>&1 || { echo "FAIL: seed-day $DAY"; exit 1; }
want_hit=$("$ENGINE" search popup --json 2>/dev/null | grep -c Title)
[ "$want_hit" -ge 1 ] || { echo "FAIL: engine search found nothing for 'popup' (fixture or index)"; exit 1; }

STEPS="goto:week;search:popup;pick:0;search:zzzqqq;close"
DAYFLOW_UI_SCRIPT=$STEPS timeout 14 "$BIN" >"$TMP/log" 2>&1
got=$(grep -o 'SHELL-STATE.*' "$TMP/log")
fail=0
line() { echo "$got" | grep "step=$1 " | head -1; }

l=$(line "pick:0")
case "$l" in *"section=today dayOffset=-2"*"searchOpen=false"*) echo "ok   pick jumps to Today, 2 days back, overlay closed" ;;
  *) echo "FAIL: pick:0 state: $l"; fail=1 ;; esac
l=$(line "search:popup")
case "$l" in *"searchOpen=true"*) echo "ok   Ctrl+K opens the overlay" ;; *) echo "FAIL: search open: $l"; fail=1 ;; esac
l=$(line "close")
case "$l" in *"searchOpen=false"*"results=0"*) echo "ok   no-match query returns 0 results; Esc closes" ;;
  *) echo "FAIL: close state: $l"; fail=1 ;; esac
if grep -Eq "(qrc|file):.*(Error|error)|ReferenceError|TypeError|Binding loop" "$TMP/log"; then
  echo "FAIL: QML errors"; grep -E "(qrc|file):|ReferenceError|TypeError|Binding loop" "$TMP/log" | sort -u | head; fail=1
fi
[ "$fail" -eq 0 ] && echo "OK"
exit "$fail"
