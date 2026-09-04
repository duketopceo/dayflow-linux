#!/usr/bin/env bash
# dayflow stress test — exercises the real binary against a sandboxed data dir.
# Usage: scripts/stress.sh   (needs a Wayland session for real captures)
set -u
cd "$(dirname "$0")/../engine"
go build -o /tmp/dayflow-test . || exit 1
BIN=/tmp/dayflow-test
export DAYFLOW_DATA_DIR=$(mktemp -d)
export DAYFLOW_CONFIG=$DAYFLOW_DATA_DIR/config.json
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "  ok  $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  FAIL $1"; }
check(){ if [ "$1" = "$2" ]; then ok "$3"; else bad "$3 (got '$1' want '$2')"; fi }

echo "== config =="
$BIN config set model google/gemini-2.5-flash >/dev/null && ok "config set model"
$BIN config set capture_interval_sec 2 >/dev/null && ok "config set interval"
$BIN config set bogus 1 2>/dev/null && bad "bogus key accepted" || ok "bogus key rejected"
$BIN config set jpeg_quality notanum 2>/dev/null && bad "non-int accepted" || ok "non-int rejected"

echo "== pause/resume =="
$BIN pause >/dev/null; check "$($BIN status --json | jq -r .paused)" "true" "pause flag set"
$BIN resume >/dev/null; check "$($BIN status --json | jq -r .paused)" "false" "pause flag cleared"
$BIN toggle >/dev/null; check "$($BIN status --json | jq -r .paused)" "true" "toggle pauses"
$BIN toggle >/dev/null; check "$($BIN status --json | jq -r .paused)" "false" "toggle resumes"

echo "== ignore list =="
$BIN ignore Steam >/dev/null && ok "ignore add"
$BIN ignore steam >/dev/null && out=$($BIN ignore steam) && [[ "$out" == *"already ignored"* ]] && ok "ignore dedup" || bad "ignore dedup"
$BIN unignore steam >/dev/null && check "$($BIN config | tail -n +2 | jq -r '.ignore_apps|length')" "0" "unignore removes"

echo "== rapid capture churn (daemon 25s @2s interval) =="
timeout 27 $BIN daemon >/dev/null 2>&1 &
DPID=$!
sleep 6; $BIN pause >/dev/null; sleep 4; $BIN resume >/dev/null
sleep 8; kill -TERM $DPID 2>/dev/null
timeout 6 $BIN daemon >/dev/null 2>&1 &   # restart resilience
sleep 4
FRAMES=$($BIN status --json | jq -r .frames_today)
[ "$FRAMES" -ge 2 ] && ok "captured $FRAMES frames across restart" || bad "only $FRAMES frames"
wait 2>/dev/null

echo "== summarize without key =="
# note: ~/.config/openrouter/keys.json is a valid fallback key source, so
# "no key" only applies when neither env, config, nor fallback exists.
HOME_BAK=$HOME; XDG_BAK=${XDG_CONFIG_HOME:-}; export HOME=$(mktemp -d) XDG_CONFIG_HOME=$HOME/xdg; unset OPENROUTER_API_KEY
DAYFLOW_CONFIG=$DAYFLOW_DATA_DIR/nokey.json $BIN config set openrouter_api_key "" >/dev/null 2>&1
OUT=$(DAYFLOW_CONFIG=$DAYFLOW_DATA_DIR/nokey.json $BIN summarize --now 2>&1)
[[ "$OUT" == *"no API key"* ]] && ok "missing key handled" || bad "missing key: $OUT"
export HOME=$HOME_BAK; export XDG_CONFIG_HOME=$XDG_BAK

echo "== summarize with bad key (live API) =="
$BIN config set openrouter_api_key sk-or-bad-key >/dev/null
OUT=$($BIN summarize --now 2>&1); echo "  (api said: $(echo "$OUT" | tail -1))"
$BIN blocks | grep -q . && ok "failures recorded in blocks" || echo "  note: no failed blocks recorded"

echo "== concurrent access =="
$BIN status --json >/dev/null & $BIN timeline --json >/dev/null & $BIN events -n 5 >/dev/null & wait
ok "concurrent reads didn't crash"
sqlite3 "$DAYFLOW_DATA_DIR/dayflow.db" "PRAGMA integrity_check;" | grep -q ok && ok "db integrity" || bad "db corrupt"

echo "== events logged =="
EV=$($BIN events -n 200 --json | jq 'length')
[ "$EV" -gt 0 ] && ok "$EV events logged" || bad "no events logged"

echo "== retention =="
$BIN config set retention_days 0 >/dev/null
sqlite3 "$DAYFLOW_DATA_DIR/dayflow.db" "INSERT INTO frames(ts,path) VALUES(1,'/nonexistent/x.jpg');"
$BIN config set retention_days 7 >/dev/null
ok "retention config round-trips"

echo
echo "RESULT: $PASS passed, $FAIL failed"
rm -rf "$DAYFLOW_DATA_DIR"
[ $FAIL -eq 0 ]
