#!/usr/bin/env bash
# run.sh <surface> <theme> [scale] [--state S]
# Sandbox preview: scratch HOME + staged theme + seeded fixture day,
# quickshell -p here offscreen, shot saved to shots/<surface>-<theme>-<scale>.png.
# Never touches the live session: no real HOME writes, no API keys,
# omaseal is a failing stub, dayflow is a shim.
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT="$PWD"
SURFACE="${1:?surface name (bar|popup|fullview-today)}"
THEME="${2:?theme name}"
SCALE="${3:-2}"
STATE="recording"
if [ "${4:-}" = "--state" ]; then STATE="${5:?state after --state}"; fi
shift $(( $# > 4 ? 5 : $# > 3 ? 4 : $# )) 2>/dev/null || true

SCRATCH=$(mktemp -d /tmp/dayflow-preview.XXXXXX)
trap 'rm -rf "$SCRATCH"' EXIT
export SHOT_OUT="$ROOT/scripts/preview/shots"
mkdir -p "$SHOT_OUT"

# --- stage theme into scratch HOME -------------------------------------
SRC=""
for base in /usr/share/omarchy/themes ~/.config/omarchy/themes; do
  [ -d "$base/$THEME" ] && SRC="$base/$THEME" && break
done
[ -z "$SRC" ] && { echo "theme '$THEME' not found" >&2; exit 1; }
mkdir -p "$SCRATCH/.local/state/omarchy/current" "$SCRATCH/.config/omarchy/current"
cp -r "$SRC" "$SCRATCH/.local/state/omarchy/current/next-theme"
HOME="$SCRATCH" omarchy-theme-set-templates >/dev/null 2>&1 || true
# shell theme resolves from current/theme
mv "$SCRATCH/.local/state/omarchy/current/next-theme" "$SCRATCH/.local/state/omarchy/current/theme"

# --- safety env (KTD5) ---------------------------------------------------
export DAYFLOW_DATA_DIR="$SCRATCH/.local/share/dayflow"
export DAYFLOW_CONFIG="$SCRATCH/.config/dayflow/config.json"
mkdir -p "$DAYFLOW_DATA_DIR" "$(dirname "$DAYFLOW_CONFIG")"
printf '{}\n' > "$DAYFLOW_CONFIG"
# Prefer the repo build (has dev seed-day); fall back to installed.
if [ -x "$ROOT/engine/dayflow-dev" ]; then
  export DAYFLOW_REAL_BIN="$ROOT/engine/dayflow-dev"
else
  (cd "$ROOT/engine" && go build -o dayflow-dev .)
  export DAYFLOW_REAL_BIN="$ROOT/engine/dayflow-dev"
fi
export DAYFLOW_STATE="$STATE"
export DAYFLOW_SURFACE="$SURFACE"
export DAYFLOW_SCALE="$SCALE"
export DAYFLOW_SHOT="$SHOT_OUT/$SURFACE-$THEME-$SCALE.png"
mkdir -p "$SCRATCH/bin"
export PATH="$ROOT/scripts/preview:$SCRATCH/bin:$PATH"
cp "$ROOT/scripts/preview/dayflow-shim" "$SCRATCH/bin/dayflow"; chmod +x "$SCRATCH/bin/dayflow"
cp "$ROOT/scripts/preview/omaseal" "$SCRATCH/bin/omaseal"; chmod +x "$SCRATCH/bin/omaseal"
unset OPENROUTER_API_KEY || true

# --- seed fixture --------------------------------------------------------
dayflow dev seed-day --date "$(date +%F)" >/dev/null

# --- link repo QML into the preview package ------------------------------
ln -sfn "$ROOT"/*.qml "$ROOT/scripts/preview/" 2>/dev/null || true
mkdir -p "$SCRATCH/qs"
for mod in Commons Ui; do
  src="/usr/share/omarchy/shell/$mod"
  [ -d "$src" ] && ln -sfn "$src" "$SCRATCH/qs/$mod"
done

HOME="$SCRATCH" QT_QPA_PLATFORM=offscreen QML_IMPORT_PATH="$SCRATCH" quickshell -p "$ROOT/scripts/preview" 2>&1 | tee "$SCRATCH/qs.log" || {
  tail -20 "$SCRATCH/qs.log" >&2; exit 1; }
[ -f "$DAYFLOW_SHOT" ] && echo "wrote $DAYFLOW_SHOT"
