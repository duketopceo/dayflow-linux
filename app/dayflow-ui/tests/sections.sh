#!/usr/bin/env bash
# Per-section check: launch dayflow-ui offscreen once per rail section against a
# scratch data dir seeded with the fixture day, grab a screenshot, and fail on
# any QML error or an empty screenshot. Sections are the AppShell rail keys.
# Usage: sections.sh path/to/dayflow-ui [outdir]   (outdir keeps the PNGs)
set -u
BIN=${1:?usage: sections.sh path/to/dayflow-ui [outdir]}
OUT=${2:-}
ENGINE="$(dirname "$BIN")/dayflow"
[ -x "$ENGINE" ] || { echo "FAIL: engine not built beside dayflow-ui ($ENGINE)"; exit 1; }
SECTIONS="today week standup ask agents timelapse context settings"
fail=0
for sec in $SECTIONS; do
  TMP=$(mktemp -d)
  export DAYFLOW_DATA_DIR=$TMP/data DAYFLOW_CONFIG=$TMP/config.json DAYFLOW_ENGINE=$ENGINE
  export XDG_CONFIG_HOME=$TMP/xdg XDG_DATA_HOME=$TMP/xdgdata XDG_RUNTIME_DIR=$TMP
  export QT_FORCE_STDERR_LOGGING=1 QT_STYLE_OVERRIDE= QT_QPA_PLATFORM=offscreen DAYFLOW_UI_ALLOW_MULTI=1
  mkdir -p "$DAYFLOW_DATA_DIR"
  "$ENGINE" dev seed-day >/dev/null 2>&1 || { echo "FAIL[$sec]: seed-day failed"; fail=1; rm -rf "$TMP"; continue; }
  shot=$TMP/$sec.png
  DAYFLOW_UI_TAB=$sec DAYFLOW_UI_SCREENSHOT=$shot timeout 20 "$BIN" >"$TMP/log" 2>&1
  rc=$?
  if [ "$rc" -ne 0 ]; then echo "FAIL[$sec]: exit $rc"; cat "$TMP/log"; fail=1
  elif [ ! -s "$shot" ]; then echo "FAIL[$sec]: no screenshot"; fail=1
  elif grep -Eq "(qrc|file):.*(Error|error)|is not a type|is not installed|ReferenceError|TypeError|Cannot assign|Unable to assign|Binding loop" "$TMP/log"; then
    echo "FAIL[$sec]: QML errors"; grep -E "(qrc|file):|ReferenceError|TypeError|Binding loop" "$TMP/log" | sort -u | head -10; fail=1
  else
    echo "ok   $sec"
  fi
  if [ -n "$OUT" ] && [ -s "$shot" ]; then mkdir -p "$OUT"; cp "$shot" "$OUT/$sec.png"; fi
  rm -rf "$TMP"
done
[ "$fail" -eq 0 ] && echo "OK"
exit "$fail"
