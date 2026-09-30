#!/usr/bin/env bash
# dayflow adversarial battery — proves the binary survives hostile input:
# malformed args, corrupt stores, protocol abuse, concurrency. Complements
# stress.sh (functional smoke) — this script tries to BREAK it.
#
# Everything runs against an isolated data dir + a neutered config (dead
# api_base_url, no key) so no live journal data and no provider egress is
# involved. Live-DB cases at the end are read-only or derived-state only.
#
# Usage: scripts/adversarial.sh
set -u
cd "$(dirname "$0")/../engine"
go build -o /tmp/dayflow-adv . || exit 1
BIN=/tmp/dayflow-adv

STRESS=$(mktemp -d)
export DAYFLOW_DATA_DIR=$STRESS/data
export DAYFLOW_CONFIG=$STRESS/config.json
export DAYFLOW_CLAUDE_DIR=$STRESS/claude
export DAYFLOW_CODEX_DIR=$STRESS/codex
export DAYFLOW_OPENCODE_DB=$STRESS/opencode.db
export DAYFLOW_DEVIN_DIR=$STRESS/devin
export DAYFLOW_CURSOR_DB=$STRESS/state.vscdb
export DAYFLOW_CURSOR_WORKSPACES=$STRESS/ws
mkdir -p "$STRESS"
$BIN config set api_base_url http://127.0.0.1:9/dead >/dev/null 2>&1 || true
$BIN config set request_timeout_sec 5 >/dev/null 2>&1 || true

RESULTS=$STRESS/results.txt
: > "$RESULTS"
run() { # name | timeout_secs | cmd... → record ok(rc)/TIMEOUT/SIGNAL(n)
  local name="$1" tmo="$2"; shift 2
  local out rc verdict
  out=$(timeout "$tmo" "$@" 2>&1); rc=$?
  verdict="ok($rc)"
  [ "$rc" -eq 124 ] && verdict="TIMEOUT"
  [ "$rc" -ge 128 ] && verdict="SIGNAL($((rc-128)))"
  printf '%-42s %s\n' "$name" "$verdict" | tee -a "$RESULTS"
  echo "$out" | tail -2 | sed 's/^/    /' >> "$RESULTS"
}
hdr() { echo; echo "=== $* ===" | tee -a "$RESULTS"; }

hdr "A. CLI argument abuse"
run "search-agents: no args"            5 $BIN search-agents
run "search-agents: 100KB arg"          5 $BIN search-agents "$(head -c 100000 /dev/zero | tr '\0' 'a')"
run "search-agents: FTS injection"      5 $BIN search-agents '" OR 1=1 --"'
run "search-agents: control chars"      5 $BIN search-agents $'\x01\x02\x03\x7f'
run "search-agents: unicode flood"      5 $BIN search-agents "$(python3 -c 'print("🔥"*2000)' 2>/dev/null || echo 'x')"
run "search-agents: lone flag"          5 $BIN search-agents --json
run "timeline: not-a-date"              5 $BIN timeline garbage
run "timeline: dash positional"         5 $BIN timeline -1
run "timeline: year 99999"              5 $BIN timeline 99999-12-31
run "unknown command"                   5 $BIN frobnicate
run "empty subcommand"                  5 $BIN ""
run "ingest on empty env"              30 $BIN ingest --json
run "ask: no provider (dead base)"     15 $BIN ask "hello"

hdr "B. Store corruption"
mkdir -p "$DAYFLOW_CLAUDE_DIR/proj" "$DAYFLOW_DEVIN_DIR"
printf '\x00\xff\xfe binary garbage \x80\x81\n' > "$DAYFLOW_CLAUDE_DIR/proj/bin.jsonl"
python3 -c "open('$DAYFLOW_CLAUDE_DIR/proj/huge.jsonl','w').write('{\"type\":\"user\",\"timestamp\":\"2026-09-30T10:00:00Z\",\"message\":{\"role\":\"user\",\"content\":\"'+'x'*3000000+'\"}}\n')" 2>/dev/null || true
head -c 512 /dev/urandom > "$DAYFLOW_OPENCODE_DB"
run "agents: garbage+binary+3MB-line"   20 $BIN agents --json
run "ingest: corrupt stores"           30 $BIN ingest --json
run "search on corrupt stores"         10 $BIN search-agents "x"

hdr "C. Journal DB corruption (isolated)"
rm -rf "$DAYFLOW_DATA_DIR" && mkdir -p "$DAYFLOW_DATA_DIR"
run "status on fresh dir"               5 $BIN status
echo "not a database" > "$DAYFLOW_DATA_DIR/dayflow.db"
run "status on garbage db"              5 $BIN status
run "search on garbage db"             10 $BIN search-agents "test"
run "ingest on garbage db"             10 $BIN ingest --json
rm -f "$DAYFLOW_DATA_DIR/dayflow.db" && touch "$DAYFLOW_DATA_DIR/dayflow.db"
run "empty-file db"                    10 $BIN status

hdr "D. MCP abuse (read-only)"
run "mcp: truncated json"               5 bash -c "printf '{\"jsonrpc\":\"2.0' | $BIN mcp --read-only"
run "mcp: not json"                     5 bash -c "printf 'garbage line\n' | $BIN mcp --read-only"
run "mcp: arg wrong type"               5 bash -c "printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"search_agent_sessions\",\"arguments\":{\"query\":123}}}\n' | $BIN mcp --read-only"
run "mcp: arg object type"              5 bash -c "printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"get_timeline\",\"arguments\":{\"date\":{\"nested\":true}}}}\n' | $BIN mcp --read-only"
run "mcp: null args"                    5 bash -c "printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"search_agent_sessions\",\"arguments\":null}}\n' | $BIN mcp --read-only"
run "mcp: unknown tool"                 5 bash -c "printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"rm_rf\",\"arguments\":{}}}\n' | $BIN mcp --read-only"
run "mcp: unknown method"               5 bash -c "printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"system/exec\",\"params\":{}}\n' | $BIN mcp --read-only"
run "mcp: 10MB request"                10 bash -c "python3 -c 'import json;print(json.dumps({\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"search_agent_sessions\",\"arguments\":{\"query\":\"x\"*10000000}}}))' | $BIN mcp --read-only"
run "mcp: chat under --read-only"       5 bash -c "printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"chat\",\"arguments\":{\"message\":\"hi\"}}}\n' | $BIN mcp --read-only"
run "mcp: 50 rapid requests"           10 bash -c "for i in \$(seq 50); do printf '{\"jsonrpc\":\"2.0\",\"id\":%d,\"method\":\"tools/list\",\"params\":{}}\n' \$i; done | $BIN mcp --read-only >/dev/null"
run "mcp: empty stdin"                  5 bash -c "printf '' | $BIN mcp --read-only"

hdr "E. Concurrency"
run "5x parallel search-agents"        30 bash -c "for i in 1 2 3 4 5; do $BIN search-agents 'q' --json >/dev/null & done; wait"
run "2x parallel ingest"               90 bash -c "$BIN ingest --json >/dev/null & $BIN ingest --json >/dev/null & wait"
run "search during ingest"             90 bash -c "$BIN ingest --json >/dev/null & sleep 0.3; $BIN search-agents 'q' >/dev/null; wait"

echo; echo "=== DONE ==="
if grep -qE 'TIMEOUT|SIGNAL' "$RESULTS"; then
  echo "!!! FAILURES:"; grep -E 'TIMEOUT|SIGNAL' "$RESULTS"; rm -rf "$STRESS"; exit 1
fi
echo "all survived — review $STRESS/results.txt for wrong-but-clean exits"
# Keep $STRESS for inspection: it holds results.txt and the throwaway data dir.
