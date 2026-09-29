#!/usr/bin/env bash
# dayflow install smoke test — proves `dayflow install` plus the basic
# read commands write only into scoped dirs and never reach the real
# systemd user manager.
#
# Scoping: DAYFLOW_CONFIG + DAYFLOW_DATA_DIR cover the app paths, and
# XDG_CONFIG_HOME + HOME are also redirected into the sandbox — `env HOME=`
# alone is NOT sufficient because unitDir() resolves through
# XDG_CONFIG_HOME, which bypasses HOME. systemctl is replaced on PATH by a
# stub that records argv, so no `systemctl --user` call reaches the real
# user manager.
#
# Honest limitation: this cannot exercise the unit *environment* — the
# PassEnvironment=DBUS_SESSION_BUS_ADDRESS line (notification bus access
# from the capture service) only takes effect under a real user manager.
# Covering it needs `systemd-run --user`, not a smoke trick. See
# docs/maintenance.md.
set -euo pipefail

cd "$(dirname "$0")/../engine"

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "  ok  $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  FAIL $1"; }

SANDBOX=$(mktemp -d /tmp/dayflow-smoke.XXXXXX)
trap 'rm -rf "$SANDBOX"' EXIT

BIN="$SANDBOX/dayflow"
STUB_BIN="$SANDBOX/bin"
mkdir -p "$STUB_BIN" "$SANDBOX/home" "$SANDBOX/xdg" "$SANDBOX/data" "$SANDBOX/cfg"

# systemctl stub: record argv, succeed silently.
cat > "$STUB_BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "$@" >> "$DAYFLOW_SMOKE_SYSTEMCTL_LOG"
exit 0
EOF
chmod +x "$STUB_BIN/systemctl"
export DAYFLOW_SMOKE_SYSTEMCTL_LOG="$SANDBOX/systemctl.log"

echo "== build =="
go build -o "$BIN" . && ok "engine builds"

# Scoped environment — everything dayflow can write lands under $SANDBOX.
export PATH="$STUB_BIN:$PATH"
export HOME="$SANDBOX/home"
export XDG_CONFIG_HOME="$SANDBOX/xdg"
export XDG_DATA_HOME="$SANDBOX/xdg-data"
export DAYFLOW_CONFIG="$SANDBOX/cfg/config.json"
export DAYFLOW_DATA_DIR="$SANDBOX/data"

# Marker for the outside-scope write assertion: any dayflow write in the
# real dirs newer than this file is a leak.
MARKER="$SANDBOX/marker"
touch "$MARKER"
sleep 1   # mtime resolution guard

leaks() {
  local out=""
  for d in "$REAL_HOME/.config/systemd/user" \
           "$REAL_HOME/.config/dayflow" \
           "$REAL_HOME/.local/share/dayflow"; do
    [ -d "$d" ] || continue
    # No -name filter: a leak is any write into these dirs, not just
    # dayflow-named files (e.g. config.json, or .dayflow-* temp files —
    # find includes dotfiles either way). -H follows a symlinked directory:
    # [ -d ] accepts symlinks, but find without -H would skip the target
    # and misses writes beneath it.
    out+=$(find -H "$d" -newer "$MARKER" 2>/dev/null)
  done
  echo "$out"
}
# Resolve the real home dir from the process identity, not the inherited
# USER (which can name a different account than the one running the smoke),
# failing hard: an empty REAL_HOME would make the leak check silently
# inspect nothing.
if ! REAL_HOME=$(getent passwd "$(id -un)" | cut -d: -f6); then
  echo "smoke: getent could not resolve the real user's home dir" >&2
  exit 1
fi
if [ -z "$REAL_HOME" ] || [ ! -d "$REAL_HOME" ]; then
  echo "smoke: resolved home ${REAL_HOME:-<empty>} is not a directory" >&2
  exit 1
fi

echo "== install =="
"$BIN" install > "$SANDBOX/install.out" 2>&1 && ok "dayflow install exits 0" \
  || { bad "dayflow install failed"; cat "$SANDBOX/install.out"; }

UNITDIR="$SANDBOX/xdg/systemd/user"
for u in dayflow-capture.service dayflow-summarize.service \
         dayflow-summarize.timer dayflow-backup.service dayflow-backup.timer \
         dayflow-export.service dayflow-export.timer; do
  [ -f "$UNITDIR/$u" ] && ok "unit written: $u" || bad "missing unit $u"
done
grep -q 'Managed by dayflow' "$UNITDIR/dayflow-capture.service" \
  && ok "unit marker present" || bad "unit marker absent"
grep -q '^PassEnvironment=DBUS_SESSION_BUS_ADDRESS' "$UNITDIR/dayflow-capture.service" \
  && ok "capture unit passes DBUS_SESSION_BUS_ADDRESS" \
  || bad "capture unit missing PassEnvironment=DBUS_SESSION_BUS_ADDRESS"

[ -f "$DAYFLOW_SMOKE_SYSTEMCTL_LOG" ] \
  && ok "systemctl stub was invoked" || bad "systemctl never called"
grep -q 'daemon-reload' "$DAYFLOW_SMOKE_SYSTEMCTL_LOG" \
  && ok "stub saw daemon-reload" || bad "no daemon-reload"
grep -q 'enable --now dayflow-summarize.timer' "$DAYFLOW_SMOKE_SYSTEMCTL_LOG" \
  && ok "stub saw enable --now" || bad "no enable --now"

[ -f "$DAYFLOW_CONFIG" ] && ok "default config written in scope" \
  || bad "config missing at $DAYFLOW_CONFIG"

echo "== scoped reads =="
"$BIN" status --json > "$SANDBOX/status.json" 2>/dev/null \
  && grep -q '"paused"' "$SANDBOX/status.json" \
  && ok "status --json" || bad "status --json"
"$BIN" doctor --json > "$SANDBOX/doctor.json" 2>/dev/null \
  || true   # doctor exits non-zero when checks fail (no API key here)
grep -q '"name"' "$SANDBOX/doctor.json" \
  && ok "doctor --json emits check list" || bad "doctor --json malformed"
"$BIN" search smoke-test-query > "$SANDBOX/search.out" 2>/dev/null \
  && ok "search runs on a fresh db" || bad "search failed"
"$BIN" search --reindex >> "$SANDBOX/search.out" 2>&1 \
  && ok "search --reindex runs" || bad "search --reindex failed"

[ -f "$SANDBOX/data/dayflow.db" ] && ok "journal db created in scope" \
  || bad "journal db missing"

echo "== outside-scope write check =="
LEAKED=$(leaks)
if [ -z "$LEAKED" ]; then
  ok "no dayflow writes outside the sandbox"
else
  bad "writes escaped the sandbox: $LEAKED"
fi

# Everything under the real user's dirs must still be the pre-run state —
# the scoped env redirected it all here.
echo
echo "$PASS passed, $FAIL failed (sandbox was $SANDBOX)"
[ "$FAIL" -eq 0 ]
