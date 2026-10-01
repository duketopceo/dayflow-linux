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
# Version precedence: --version X.Y.Z > manifest.json in the plugin dir
# (the manifest-pinned plugin path — widget and engine stay matched) >
# latest GitHub release (standalone installs).
#
# Env overrides (mostly for tests):
#   DAYFLOW_RELEASE_BASE — release-asset base URL (default github download path)
#   DAYFLOW_RELEASE_API  — latest-release endpoint override; when unset the
#                          script resolves /releases/latest's redirect instead
#                          of touching the rate-limited JSON API
#   DAYFLOW_BUILD=local  — `go build` the repo this script lives in instead
#                          of downloading (dev/testing)

set -euo pipefail

REPO="duketopceo/dayflow-linux"
RELEASE_BASE="${DAYFLOW_RELEASE_BASE:-https://github.com/$REPO/releases/download}"
RELEASE_API="${DAYFLOW_RELEASE_API:-}"
# Bounded fetches — a stalled download must not wedge the widget's
# installer process forever (the panel is persistent, not destroyed).
CURL_OPTS="--connect-timeout 15 --max-time 300 --retry 2 --retry-all-errors"
# SCRIPT_DIR is only trusted when this script is a real file — under
# `curl | bash` BASH_SOURCE[0] is "bash", and resolving the caller's cwd
# would let a stray manifest.json there hijack the version pin. The
# interpreter-name check matters too: `-f bash` is true when the caller's
# cwd happens to contain a regular file named `bash`.
src="${BASH_SOURCE[0]:-}"
SCRIPT_DIR=""
if [[ -n $src && $src != bash && $src != -bash && $src != sh && -f $src ]]; then
  SCRIPT_DIR="$(cd "$(dirname "$src")" && pwd)"
fi

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
    # `|| true`: head -1 can SIGPIPE grep on multi-match input → 141 under pipefail
    printf '%s' "$input" | grep -o "\"$field\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 | sed 's/.*"\([^"]*\)"$/\1/' || true
  fi
}

cli_version=""
while (( $# > 0 )); do
  case "$1" in
  --version)
    [[ $# -ge 2 ]] || fail "--version needs a value (e.g. --version 1.4.0)"
    [[ $2 =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "bad --version value: $2 (want X.Y.Z)"
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
elif [[ -n $SCRIPT_DIR && -f "$SCRIPT_DIR/../manifest.json" ]]; then
  # Unreadable/corrupt manifest → fall through to latest-release rather
  # than dying on a bare jq error.
  version=$(json_field "$SCRIPT_DIR/../manifest.json" version || true)
elif [[ -n $SCRIPT_DIR && -f "$SCRIPT_DIR/manifest.json" ]]; then
  version=$(json_field "$SCRIPT_DIR/manifest.json" version || true)
fi
if [[ -z $version ]]; then
  note "no manifest or --version given; resolving latest release"
  if [[ -n $RELEASE_API ]]; then
    # Override endpoint (tests point this at a file:// fixture).
    latest=$(curl -fsSL $CURL_OPTS "$RELEASE_API") || fail "could not query latest release"
    tag=$(printf '%s' "$latest" | json_field - tag_name)
  else
    # /releases/latest 302s to /releases/tag/vX.Y.Z — resolve the
    # redirect instead of hitting the rate-limited JSON API.
    tag=$(curl -fsSIL $CURL_OPTS -o /dev/null -w '%{url_effective}' \
      "https://github.com/$REPO/releases/latest" | sed 's|.*/||') \
      || fail "could not resolve latest release"
  fi
  [[ -n $tag ]] || fail "latest release resolution gave no tag"
  version="${tag#v}"
fi
# Whatever the source — flag, manifest, or tag — the resolved version must
# be clean semver: it lands in URLs, filenames, and panel-rendered output.
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "resolved version '$version' is not X.Y.Z"
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
  # The module lives in engine/ — there is no go.work at the root.
  (cd "$SCRIPT_DIR/../engine" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$bin_path.tmp" .)
  mv "$bin_path.tmp" "$bin_path"
elif [[ $current == "$version" ]]; then
  note "dayflow $version already installed — refreshing units only"
else
  asset="dayflow-$version-$arch"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT

  # Checksum manifest first — a release without one fails closed before
  # the (larger) binary download wastes the fetch.
  curl -fsSL $CURL_OPTS "$RELEASE_BASE/v$version/SHA256SUMS" -o "$tmp/SHA256SUMS" ||
    fail "could not fetch SHA256SUMS for v$version (missing asset or network) — refusing to install unverified binary"

  note "downloading $asset"
  curl -fsSL $CURL_OPTS "$RELEASE_BASE/v$version/$asset" -o "$tmp/$asset" ||
    fail "download failed: $RELEASE_BASE/v$version/$asset"

  note "verifying checksum"
  # awk field-compare, not grep: asset names carry regex metachars (dots)
  # and a prefix-substring could match a longer asset name. sha256sum -c
  # fails if ANY supplied line fails — a duplicated entry can't weaken it.
  entries=$(awk -v a="$asset" '$2 == a {n++} END {print n+0}' "$tmp/SHA256SUMS")
  [[ $entries -gt 0 ]] || fail "no checksum entry for $asset in SHA256SUMS"
  (cd "$tmp" && awk -v a="$asset" '$2 == a' SHA256SUMS | sha256sum -c --status -) ||
    fail "checksum mismatch for $asset — aborted before placing anything"

  # Stage then rename — a killed `install` can't leave a truncated binary
  # at the live path.
  install -Dm755 "$tmp/$asset" "$bin_path.staged"
  mv -f "$bin_path.staged" "$bin_path"
  note "installed $bin_path"
fi

# ---- systemd user units ----
"$bin_path" install || fail "engine installed but 'dayflow install' failed — run it yourself for the error detail"

# Capture unit: swap the running daemon onto the new binary on upgrades,
# and make the one-click path actually record on fresh installs (the
# engine's own installer deliberately leaves capture off for manual
# control; the scripted path owns it instead). Both tolerant — a
# standalone install with no user bus should not fail the run.
if command -v systemctl >/dev/null 2>&1; then
  systemctl --user try-restart dayflow-capture.service 2>/dev/null || true
  systemctl --user enable --now dayflow-capture.service 2>/dev/null ||
    note "could not start dayflow-capture (no user session?) — later: systemctl --user enable --now dayflow-capture.service"
fi

# The widget spawns `dayflow` bare — warn when PATH resolves it to
# something else (or nothing). stderr so the panel's installErr line
# surfaces it; a bare note is swallowed by the widget's stdout tail.
resolved=$(command -v dayflow 2>/dev/null || true)
if [[ $resolved != "$bin_path" ]]; then
  [[ -n $resolved ]] ||
    resolved="nothing on PATH"
  echo "install.sh: WARNING: 'dayflow' resolves to $resolved, not $bin_path — restart the shell/session for the widget to find it" >&2
fi

note "done. Remaining step: configure a provider with \`dayflow setup\` (or open the widget's onboarding)."
