#!/usr/bin/env bash
# Installs the built tree into a temp prefix and checks the layout: binaries,
# launcher entry (absolute Exec, validated when desktop-file-validate exists),
# icon, autostart template. Never touches the real home or prefix.
set -u
BUILD=${1:?usage: install-layout.sh path/to/build}
PFX=$(mktemp -d)
trap 'rm -rf "$PFX"' EXIT
fail=0
chk() { if [ ! -e "$PFX/$1" ]; then echo "FAIL: missing $1"; fail=1; else echo "ok   $1"; fi; }

cmake --install "$BUILD" --prefix "$PFX" >/dev/null || { echo "FAIL: cmake --install"; exit 1; }
chk bin/dayflow-ui
chk share/applications/dayflow.desktop
chk share/icons/hicolor/scalable/apps/dayflow.svg
chk share/dayflow/autostart/dayflow.desktop
[ -x "$BUILD/dayflow" ] && chk bin/dayflow

desk=$PFX/share/applications/dayflow.desktop
exec_line=$(grep '^Exec=' "$desk" | head -1 | cut -d= -f2-)
case "$exec_line" in /*) echo "ok   Exec is absolute" ;; *) echo "FAIL: Exec not absolute: $exec_line"; fail=1 ;; esac
if grep -q '@' "$desk" "$PFX/share/dayflow/autostart/dayflow.desktop"; then echo "FAIL: unsubstituted @VAR@"; fail=1; fi
grep -q -- '--background' "$PFX/share/dayflow/autostart/dayflow.desktop" || { echo "FAIL: autostart lacks --background"; fail=1; }
if command -v desktop-file-validate >/dev/null; then
  desktop-file-validate "$desk" || { echo "FAIL: desktop-file-validate"; fail=1; }
else
  echo "skip desktop-file-validate (not installed)"
fi
[ "$fail" -eq 0 ] && echo "OK"
exit "$fail"
