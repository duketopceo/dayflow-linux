#!/usr/bin/env bash

# dayflow engine installer — the canonical install path.
#
# Three entry points, one script:
#   * in-shell bootstrap: the omarchy plugin runs this from its checkout
#   * plugin users:        bash ~/.config/omarchy/plugins/io.github.duketopceo.dayflow/scripts/install.sh
#   * standalone:          curl -fsSL https://github.com/duketopceo/dayflow-linux/releases/download/vX.Y.Z/install.sh | bash
#
# What it does: picks the version (see below), maps the arch, downloads
# dayflow-<ver>-<arch> + SHA256SUMS from the matching GitHub release,
# verifies the checksum BEFORE the binary touches ~/.local/bin, then runs
# `dayflow install` to write+enable the systemd user units. Provider setup
# (`dayflow setup`) and data dirs are never touched — consent stays with
# the user.
#
# Version precedence: --version X.Y.Z > manifest.json next to this script
# (the manifest-pinned plugin path — widget and engine stay matched) >
# latest GitHub release (standalone installs).
#
# Env overrides (mostly for tests):
#   DAYFLOW_RELEASE_BASE — release-asset base URL (default github download path)
#   DAYFLOW_RELEASE_API  — latest-release API URL (default github api)
#   DAYFLOW_BUILD=local  — `go build` the repo this script lives in instead
#                          of downloading (dev/testing)

set -euo pipefail

REPO="duketopceo/dayflow-linux"
RELEASE_BASE="${DAYFLOW_RELEASE_BASE:-https://github.com/$REPO/releases/download}"
RELEASE_API="${DAYFLOW_RELEASE_API:-https://api.github.com/repos/$REPO/releases/latest}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)"

fail() {
  echo "install.sh: $*" >&2
  exit 1
}

note() {
  echo "install.sh: $*"
}

# json_field <file-or--for-stdin> <field>: extract a top-level string field.
# Prefers jq (always present on Omarchy), falls back to grep for minimal
# standalone environments.
json_field() {
  local src="$1" field="$2"
  if command -v jq >/dev/null 2>&1; then
    if [[ $src == "-" ]]; then
      jq -r --arg f "$field" '.[$f] // empty'
    else
      jq -r --arg f "$field" '.[$f] // empty' "$src"
    fi
  else
    local input
    if [[ $src == "-" ]]; then input=$(cat); else input=$(cat "$src" 2>/dev/null || true); fi
    printf '%s' "$input" | grep -o "\"$field\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 | sed 's/.*"\([^"]*\)"$/\1/'
  fi
}

cli_version=""
while (( $# > 0 )); do
  case "$1" in
  --version)
    [[ $# -ge 2 ]] || fail "--version needs a value (e.g. --version 1.4.0)"
    cli_version="$2"
    shift 2
    ;;
  -h | --help)
    cat <<'EOF'
Usage: install.sh [--version X.Y.Z]

Installs the dayflow engine: downloads the version-pinned release binary
for this arch (amd64/arm64), verifies its SHA256SUMS entry, places it in
~/.local/bin, and runs `dayflow install` for the systemd user units.
EOF
    exit 0
    ;;
  *) fail "unknown argument: $1" ;;
  esac
done

# ---- arch ----
machine=$(uname -m)
case "$machine" in
x86_64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "unsupported architecture: $machine (release assets exist for amd64/arm64)" ;;
esac

# ---- version ----
version=""
if [[ -n $cli_version ]]; then
  version="${cli_version#v}"
elif [[ -n $SCRIPT_DIR && -f $SCRIPT_DIR/../manifest.json ]]; then
  version=$(json_field "$SCRIPT_DIR/../manifest.json" version)
elif [[ -f $SCRIPT_DIR/manifest.json ]]; then
  version=$(json_field "$SCRIPT_DIR/manifest.json" version)
fi
if [[ -z $version ]]; then
  note "no manifest or --version given; resolving latest release"
  latest=$(curl -fsSL "$RELEASE_API") || fail "could not query latest release"
  tag=$(printf '%s' "$latest" | json_field - tag_name)
  [[ -n $tag ]] || fail "latest release response had no tag_name"
  version="${tag#v}"
fi
note "target: dayflow $version ($arch)"

bin_dir="$HOME/.local/bin"
bin_path="$bin_dir/dayflow"

# Same-version fast path: still run `dayflow install` (repairs half-state),
# just skip the download.
current=""
if [[ -x $bin_path ]]; then
  current=$("$bin_path" --version 2>/dev/null | tr -d '[:space:]' || true)
fi

if [[ ${DAYFLOW_BUILD:-} == "local" ]]; then
  [[ -n $SCRIPT_DIR && -d $SCRIPT_DIR/../engine ]] || fail "DAYFLOW_BUILD=local needs a repo checkout (no engine/ next to script dir)"
  command -v go >/dev/null 2>&1 || fail "DAYFLOW_BUILD=local needs go on PATH"
  note "building from working tree ($SCRIPT_DIR/..)"
  mkdir -p "$bin_dir"
  (cd "$SCRIPT_DIR/.." && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$bin_path.tmp" ./engine)
  mv "$bin_path.tmp" "$bin_path"
elif [[ $current == "$version" ]]; then
  note "dayflow $version already installed — refreshing units only"
else
  asset="dayflow-$version-$arch"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT

  note "downloading $asset"
  curl -fsSL "$RELEASE_BASE/v$version/$asset" -o "$tmp/$asset" ||
    fail "download failed: $RELEASE_BASE/v$version/$asset"
  curl -fsSL "$RELEASE_BASE/v$version/SHA256SUMS" -o "$tmp/SHA256SUMS" ||
    fail "checksum manifest missing for v$version — refusing to install unverified binary"

  note "verifying checksum"
  (cd "$tmp" && grep " $asset\$" SHA256SUMS | sha256sum -c --status -) ||
    fail "checksum mismatch for $asset — aborted before placing anything"

  mkdir -p "$bin_dir"
  install -Dm755 "$tmp/$asset" "$bin_path"
  note "installed $bin_path"
fi

# ---- systemd user units ----
"$bin_path" install || fail "engine installed but 'dayflow install' failed — run it yourself for the error detail"

note "done. Remaining step: configure a provider with \`dayflow setup\` (or open the widget's onboarding)."
