#!/usr/bin/env bash

# dayflow engine uninstaller — reverses scripts/install.sh.
#
# Removes the systemd user units (via `dayflow uninstall`) and the
# ~/.local/bin/dayflow binary. Intentionally keeps your captured data and
# config — this is teardown of the *install*, not deletion of your
# history. Safe to run when partially installed; every step is tolerant.

set -uo pipefail

BIN="$HOME/.local/bin/dayflow"
DATA="$HOME/.local/share/dayflow"
CONF="${XDG_CONFIG_HOME:-$HOME/.config}/dayflow"

if [[ -x $BIN ]]; then
  echo "uninstall.sh: removing systemd user units"
  "$BIN" uninstall || echo "uninstall.sh: 'dayflow uninstall' reported an error — continuing" >&2
  rm -f "$BIN"
  echo "uninstall.sh: removed $BIN"
else
  echo "uninstall.sh: no binary at $BIN — skipping unit teardown and removal"
  echo "uninstall.sh: if units were installed from a different binary path, run: dayflow uninstall"
fi

echo "uninstall.sh: kept your data and config:"
echo "  $DATA"
echo "  $CONF"
echo "uninstall.sh: to delete everything: rm -rf \"$DATA\" \"$CONF\""
