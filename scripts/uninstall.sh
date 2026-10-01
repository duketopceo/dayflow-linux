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

if [[ -e $BIN ]]; then
  if [[ -x $BIN ]]; then
    echo "uninstall.sh: removing systemd user units"
    if "$BIN" uninstall; then
      rm -f "$BIN"
      echo "uninstall.sh: removed $BIN"
    else
      # Keep the binary when unit teardown fails — deleting it orphans
      # enabled units (capture is Restart=always, timers fire daily) that
      # this script can no longer clean up.
      echo "uninstall.sh: 'dayflow uninstall' failed — keeping $BIN so the units stay cleanable" >&2
      echo "uninstall.sh: or remove units manually: rm ~/.config/systemd/user/dayflow-*" >&2
    fi
  else
    # Present but not executable — can't run its teardown; still remove it.
    rm -f "$BIN"
    echo "uninstall.sh: removed non-executable $BIN (units may remain — run: dayflow uninstall)"
  fi
else
  echo "uninstall.sh: no binary at $BIN — skipping unit teardown and removal"
  echo "uninstall.sh: if units were installed from a different binary path, run: dayflow uninstall"
fi

echo "uninstall.sh: kept your data and config:"
echo "  $DATA"
echo "  $CONF"
echo "uninstall.sh: to delete everything: rm -rf \"$DATA\" \"$CONF\""
