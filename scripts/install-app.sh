#!/usr/bin/env bash
# install-app.sh: build and install the Dayflow desktop app (dayflow-ui, the
# bundled engine, the launcher entry and the icon) into a prefix.
#
#   scripts/install-app.sh                  # prefix ~/.local
#   scripts/install-app.sh --prefix /opt/x  # another prefix
#   scripts/install-app.sh --autostart      # also start in the tray at login
#   scripts/install-app.sh --dry-run        # print the plan, change nothing
#
# Needs: cmake, ninja (or make), a C++20 compiler, Qt6 (base, declarative,
# wayland) dev packages, and Go for the bundled engine. The background service
# (capture + timers) is separate: run `dayflow install` once to write and
# enable the systemd user units; this script does not touch them.
#
# On some Arch/Asahi installs CMake's FindOpenGL cannot see libOpenGL/libGLX
# although they are present; the script detects that and passes the two paths.

set -euo pipefail

PREFIX="${HOME}/.local"
AUTOSTART=0
DRY=0
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD="${DAYFLOW_BUILD_DIR:-${TMPDIR:-/tmp}/dayflow-ui-build-$(id -u)}"

fail() { echo "install-app.sh: $*" >&2; exit 1; }

while (( $# > 0 )); do
  case "$1" in
  --prefix) [[ $# -ge 2 ]] || fail "--prefix needs a value"; PREFIX="$2"; shift 2 ;;
  --autostart) AUTOSTART=1; shift ;;
  --dry-run) DRY=1; shift ;;
  -h|--help) sed -n '2,17p' "${BASH_SOURCE[0]}"; exit 0 ;;
  *) fail "unknown argument: $1" ;;
  esac
done

case "$PREFIX" in /*) ;; *) fail "--prefix must be an absolute path" ;; esac
command -v cmake >/dev/null || fail "cmake not found"
GEN=()
command -v ninja >/dev/null && GEN=(-G Ninja)

GL=()
if [[ -e /usr/lib/libOpenGL.so && -e /usr/lib/libGLX.so ]]; then
  GL=(-DOPENGL_opengl_LIBRARY=/usr/lib/libOpenGL.so -DOPENGL_glx_LIBRARY=/usr/lib/libGLX.so)
fi

CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
AUTO_DIR="$CONFIG_HOME/autostart"

echo "prefix:     $PREFIX"
echo "build dir:  $BUILD"
echo "autostart:  $([[ $AUTOSTART == 1 ]] && echo "yes ($AUTO_DIR/dayflow.desktop)" || echo no)"
[[ $DRY == 1 ]] && { echo "(dry run: nothing built or installed)"; exit 0; }

cmake -S "$HERE/app/dayflow-ui" -B "$BUILD" "${GEN[@]}" "${GL[@]}" \
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$PREFIX"
cmake --build "$BUILD"
cmake --install "$BUILD"

if [[ $AUTOSTART == 1 ]]; then
  mkdir -p "$AUTO_DIR"
  install -m 0644 "$PREFIX/share/dayflow/autostart/dayflow.desktop" "$AUTO_DIR/dayflow.desktop"
  echo "autostart entry: $AUTO_DIR/dayflow.desktop"
fi

command -v update-desktop-database >/dev/null && update-desktop-database "$PREFIX/share/applications" 2>/dev/null || true
command -v gtk-update-icon-cache >/dev/null && gtk-update-icon-cache -q -t "$PREFIX/share/icons/hicolor" 2>/dev/null || true

echo
echo "Installed. Launch Dayflow from the application menu, or: $PREFIX/bin/dayflow-ui"
echo "Background service: run \`$PREFIX/bin/dayflow install\` once if the capture units are not set up yet."
