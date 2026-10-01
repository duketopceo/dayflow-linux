#!/usr/bin/env bash
# Offline test battery for scripts/install.sh + scripts/uninstall.sh.
#
# Every case runs fully offline: DAYFLOW_RELEASE_BASE points at a file://
# fixture dir, the downloaded "binary" is a stub script that records
# `install`/`uninstall` calls, and HOME is redirected into a sandbox so
# ~/.local/bin writes land in the fixture, never on the real system.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
INSTALLER="$ROOT/scripts/install.sh"
UNINSTALLER="$ROOT/scripts/uninstall.sh"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); echo "  ok  $1"; }
bad() { FAIL=$((FAIL+1)); echo "  FAIL $1"; }

SBX=$(mktemp -d /tmp/dayflow-install-test.XXXXXX)
trap 'rm -rf "$SBX"' EXIT

FIX="$SBX/fixtures"
DL="$FIX/download"          # DAYFLOW_RELEASE_BASE
STUBBIN="$FIX/stub-bin"     # PATH stubs
STUB_LOG="$SBX/stub.log"    # records every stub-binary invocation
mkdir -p "$DL" "$STUBBIN"

# fake_binary <version> [arch]: emits a stub "dayflow" that reports its
# version, records install/uninstall calls, and carries an `# arch:` marker
# so tests can prove which asset was fetched. STUB_FAIL_INSTALL=1 /
# STUB_FAIL_UNINSTALL=1 make the subcommands exit 1.
fake_binary() {
  local v="$1" a="${2:-none}"
  cat <<EOF
#!/usr/bin/env bash
# arch: $a
case "\${1:-}" in
--version | -v | version) echo "$v" ;;
install)
  [[ \${STUB_FAIL_INSTALL:-} == "1" ]] && exit 1
  echo "\$1" >> "$STUB_LOG" ;;
uninstall)
  [[ \${STUB_FAIL_UNINSTALL:-} == "1" ]] && exit 1
  echo "\$1" >> "$STUB_LOG" ;;
esac
exit 0
EOF
}

# fake_release <version>: lays down dayflow-<v>-{amd64,arm64} + SHA256SUMS.
fake_release() {
  local v="$1" bad="${2:-}"
  mkdir -p "$DL/v$v"
  for a in amd64 arm64; do
    fake_binary "$v" "$a" > "$DL/v$v/dayflow-$v-$a"
    chmod +x "$DL/v$v/dayflow-$v-$a"
  done
  (cd "$DL/v$v" && sha256sum "dayflow-$v-amd64" "dayflow-$v-arm64" > SHA256SUMS)
  if [[ $bad == "bad" ]]; then
    # Corrupt BOTH arches — the test host's arch picks which line matters.
    for a in amd64 arm64; do
      echo "0000000000000000000000000000000000000000000000000000000000000000  dayflow-$v-$a"
    done > "$DL/v$v/SHA256SUMS"
  fi
}

fake_release 1.4.0
fake_release 9.9.9
fake_release 2.0.0 bad   # checksum-mismatch fixture
echo '{"tag_name":"v8.8.8"}' > "$FIX/latest.json"
fake_release 8.8.8

# A checkout-shaped dir: manifest.json + scripts/install.sh — proves the
# manifest pin wins over latest (latest fixture claims v8.8.8).
CK="$SBX/checkout"
mkdir -p "$CK/scripts"
cp "$INSTALLER" "$CK/scripts/"
echo '{"version":"1.4.0"}' > "$CK/manifest.json"

# A bare dir holding only the script — the standalone/curl-pipe shape.
LONE="$SBX/lone"
mkdir -p "$LONE"
cp "$INSTALLER" "$LONE/install.sh"

# systemctl stub: install.sh now enables/starts capture via systemctl —
# record calls so tests can assert it, exit 0 so envs without a user bus
# still exercise the enable path.
cat > "$STUBBIN/systemctl" <<EOF
#!/usr/bin/env bash
echo "systemctl \$*" >> "$STUB_LOG"
exit 0
EOF
chmod +x "$STUBBIN/systemctl"

env_base() {
  env -i PATH="$STUBBIN:$PATH" HOME="$1" \
    DAYFLOW_RELEASE_BASE="file://$DL" DAYFLOW_RELEASE_API="file://$FIX/latest.json" \
    "${@:2}"
}

new_home() {
  local h="$SBX/home-$1"
  mkdir -p "$h"
  echo "$h"
}

echo "== install.sh =="

# 1. Manifest-pinned happy path from a checkout-shaped dir.
H=$(new_home pin)
env_base "$H" bash "$CK/scripts/install.sh" > "$SBX/o1" 2>&1 \
  && ok "manifest-pinned install exits 0" || { bad "manifest-pinned install"; cat "$SBX/o1"; }
[[ -x $H/.local/bin/dayflow ]] && ok "binary placed in ~/.local/bin" || bad "binary missing"
[[ $($H/.local/bin/dayflow --version) == "1.4.0" ]] \
  && ok "manifest version 1.4.0 won over latest 8.8.8" || bad "wrong version installed"
grep -q "^install$" "$STUB_LOG" && ok "dayflow install ran for units" || bad "units not installed"
grep -q "systemctl --user enable --now dayflow-capture.service" "$STUB_LOG" \
  && ok "capture service enabled+started" || bad "capture never started"

# 2. Same-version re-run: no download needed (base pointed at nothing).
H2=$(new_home same)
mkdir -p "$H2/.local/bin"
fake_binary 1.4.0 > "$H2/.local/bin/dayflow"; chmod +x "$H2/.local/bin/dayflow"
installs_before=$(grep -c "^install$" "$STUB_LOG" || true)
env_base "$H2" \
  DAYFLOW_RELEASE_BASE="file://$SBX/nonexistent" DAYFLOW_RELEASE_API="file://$SBX/nonexistent" \
  bash "$CK/scripts/install.sh" > "$SBX/o2" 2>&1 \
  && ok "same-version re-run exits 0 without network" || { bad "idempotent re-run"; cat "$SBX/o2"; }
installs_after=$(grep -c "^install$" "$STUB_LOG" || true)
[[ $((installs_after - installs_before)) -eq 1 ]] && ok "units re-installed on idempotent run" \
  || bad "units missing on idempotent run"

# 3. Upgrade: older binary gets replaced, units re-installed, daemon restarted.
H3=$(new_home upgrade)
mkdir -p "$H3/.local/bin"
fake_binary 1.3.0 > "$H3/.local/bin/dayflow"; chmod +x "$H3/.local/bin/dayflow"
installs_before=$(grep -c "^install$" "$STUB_LOG" || true)
if env_base "$H3" bash "$CK/scripts/install.sh" > "$SBX/o3" 2>&1 \
   && [[ $($H3/.local/bin/dayflow --version) == "1.4.0" ]]; then
  ok "upgrade replaces stale binary"
else
  bad "upgrade failed"; cat "$SBX/o3"
fi
[[ $(( $(grep -c "^install$" "$STUB_LOG" || true) - installs_before )) -eq 1 ]] \
  && ok "upgrade re-ran dayflow install" || bad "upgrade skipped unit install"
grep -q "systemctl --user try-restart dayflow-capture.service" "$STUB_LOG" \
  && ok "running daemon restart attempted" || bad "no daemon restart on upgrade"

# 4. --version beats the manifest pin.
H4=$(new_home flagver)
if env_base "$H4" bash "$CK/scripts/install.sh" --version 9.9.9 > "$SBX/o4" 2>&1 \
   && [[ $($H4/.local/bin/dayflow --version) == "9.9.9" ]]; then
  ok "--version 9.9.9 beat manifest 1.4.0"
else
  bad "--version ignored"; cat "$SBX/o4"
fi

# 5. Standalone (no manifest nearby) resolves latest release.
H5=$(new_home lone)
env_base "$H5" bash "$LONE/install.sh" > "$SBX/o5" 2>&1 \
  && ok "standalone latest-release install" || { bad "standalone install"; cat "$SBX/o5"; }
[[ -x $H5/.local/bin/dayflow && $($H5/.local/bin/dayflow --version) == "8.8.8" ]] \
  && ok "latest release v8.8.8 resolved" || bad "latest resolution wrong"

# 5b. curl|bash shape: a stray manifest.json in the caller's cwd must NOT
# hijack the version pin — BASH_SOURCE isn't a real file when piped. A cwd
# file literally named `bash` exercises the `-f bash` bypass arm too.
EVIL="$SBX/evil"; mkdir -p "$EVIL"; echo '{"version":"9.9.9"}' > "$EVIL/manifest.json"
touch "$EVIL/bash"
H5B=$(new_home pipe)
if (cd "$EVIL" && env -i PATH="$STUBBIN:$PATH" HOME="$H5B" \
    DAYFLOW_RELEASE_BASE="file://$DL" DAYFLOW_RELEASE_API="file://$FIX/latest.json" \
    bash < "$INSTALLER") > "$SBX/o5b" 2>&1 \
   && [[ $($H5B/.local/bin/dayflow --version) == "8.8.8" ]]; then
  ok "piped run ignored stray cwd manifest"
else
  bad "piped run hijacked by stray manifest"; cat "$SBX/o5b"
fi

# 6. Checksum mismatch aborts before placement.
H6=$(new_home badsum)
env_base "$H6" bash "$CK/scripts/install.sh" --version 2.0.0 > "$SBX/o6" 2>&1 \
  && bad "bad checksum was accepted" || ok "checksum mismatch exits non-zero"
[[ -e $H6/.local/bin/dayflow ]] && bad "binary placed despite bad checksum" \
  || ok "nothing placed on checksum failure"

# 7. Missing SHA256SUMS fails closed.
fake_release 7.7.7; rm "$DL/v7.7.7/SHA256SUMS"
H7=$(new_home nosums)
env_base "$H7" bash "$CK/scripts/install.sh" --version 7.7.7 > "$SBX/o7" 2>&1 \
  && bad "missing SHA256SUMS was accepted" || ok "missing checksum manifest aborts"
[[ -e $H7/.local/bin/dayflow ]] && bad "binary placed without checksums" \
  || ok "nothing placed without checksums"

# 8. Unknown arch fails loudly.
cat > "$STUBBIN/uname" <<'EOF'
#!/usr/bin/env bash
echo riscv64
EOF
chmod +x "$STUBBIN/uname"
H8=$(new_home arch)
env_base "$H8" bash "$LONE/install.sh" > "$SBX/o8" 2>&1 \
  && bad "unknown arch accepted" || ok "unknown arch exits non-zero"
grep -q "unsupported architecture" "$SBX/o8" && ok "arch error message" || bad "no arch error message"
rm -f "$STUBBIN/uname"

# 9. Unknown arg rejected.
env_base "$(new_home arg)" bash "$LONE/install.sh" --bogus > "$SBX/o9" 2>&1 \
  && bad "unknown arg accepted" || ok "unknown arg rejected"

# 9b. Junk --version rejected before any download (path chars, not X.Y.Z).
env_base "$(new_home badver)" bash "$LONE/install.sh" --version "../0.0.0" > "$SBX/o9b" 2>&1 \
  && bad "junk --version accepted" || ok "junk --version rejected"

# 9c. Redirect-based latest resolution (DAYFLOW_RELEASE_API unset — the
# real curl|bash path). Stub curl emulates the HEAD redirect and serves
# the file:// download fixtures.
cat > "$STUBBIN/curl" <<'EOF'
#!/usr/bin/env bash
out=""; url=""; writefmt=""
while (( $# > 0 )); do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -w) writefmt="$2"; shift 2 ;;
    -*) shift ;;
    */*) url="$1"; shift ;;
    *) shift ;;
  esac
done
if [[ -n $writefmt && $url == */releases/latest ]]; then
  printf 'https://github.com/duketopceo/dayflow-linux/releases/tag/v8.8.8'
  exit 0
fi
if [[ -n $out && $url == file://* ]]; then
  cp "${url#file://}" "$out" 2>/dev/null || exit 22
  exit 0
fi
exit 22
EOF
chmod +x "$STUBBIN/curl"
H9C=$(new_home redirect)
env -i PATH="$STUBBIN:$PATH" HOME="$H9C" \
  DAYFLOW_RELEASE_BASE="file://$DL" \
  bash "$LONE/install.sh" > "$SBX/o9c" 2>&1 \
  && [[ $($H9C/.local/bin/dayflow --version) == "8.8.8" ]] \
  && ok "redirect-resolved latest without API" || { bad "redirect resolution failed"; cat "$SBX/o9c"; }
rm -f "$STUBBIN/curl"

# 9d. `dayflow install` failing (no user bus) must fail the script loudly.
H9D=$(new_home instfail)
env_base "$H9D" STUB_FAIL_INSTALL=1 bash "$CK/scripts/install.sh" > "$SBX/o9d" 2>&1 \
  && bad "failed dayflow install reported success" || ok "dayflow install failure exits non-zero"
grep -q "dayflow install' failed" "$SBX/o9d" && ok "install-failure diagnosis printed" \
  || bad "no failure diagnosis"

# 9e. Corrupt manifest falls through to latest instead of dying on jq.
CKB="$SBX/checkout-bad"; mkdir -p "$CKB/scripts"; cp "$INSTALLER" "$CKB/scripts/"
echo 'not json at all {{{' > "$CKB/manifest.json"
H9E=$(new_home badman)
env_base "$H9E" bash "$CKB/scripts/install.sh" > "$SBX/o9e" 2>&1 \
  && [[ $($H9E/.local/bin/dayflow --version) == "8.8.8" ]] \
  && ok "corrupt manifest degraded to latest" || { bad "corrupt manifest broke install"; cat "$SBX/o9e"; }

# 9f. SHA256SUMS present but missing the host arch entry → distinct fail.
fake_release 5.5.5
case "$(uname -m)" in
  aarch64|arm64) hostarch=arm64 ;;
  *) hostarch=amd64 ;;
esac
sed -i "/dayflow-5.5.5-$hostarch/d" "$DL/v5.5.5/SHA256SUMS"
H9F=$(new_home noentry)
env_base "$H9F" bash "$CK/scripts/install.sh" --version 5.5.5 > "$SBX/o9f" 2>&1 \
  && bad "missing arch entry accepted" || ok "missing checksum entry aborts"
grep -q "no checksum entry" "$SBX/o9f" && ok "missing-entry diagnosis" \
  || bad "wrong failure message"

# 9g. manifest.json next to the script itself (not the parent dir) pins.
LONE2="$SBX/lone2"; mkdir -p "$LONE2"; cp "$INSTALLER" "$LONE2/install.sh"
echo '{"version":"1.4.0"}' > "$LONE2/manifest.json"
H9G=$(new_home samedir)
env_base "$H9G" bash "$LONE2/install.sh" > "$SBX/o9g" 2>&1 \
  && [[ $($H9G/.local/bin/dayflow --version) == "1.4.0" ]] \
  && ok "same-dir manifest pin wins" || { bad "same-dir manifest ignored"; cat "$SBX/o9g"; }

# 9h. amd64 mapping via uname stub (host is arm64 — the other arm needs a stub).
cat > "$STUBBIN/uname" <<'EOF'
#!/usr/bin/env bash
echo x86_64
EOF
chmod +x "$STUBBIN/uname"
H9H=$(new_home amd64)
env_base "$H9H" bash "$CK/scripts/install.sh" > "$SBX/o9h" 2>&1 \
  && grep -q "arch: amd64" "$H9H/.local/bin/dayflow" \
  && ok "x86_64 maps to amd64 asset" || { bad "amd64 mapping failed"; cat "$SBX/o9h"; }
rm -f "$STUBBIN/uname"

# 9i. DAYFLOW_BUILD=local builds the checkout instead of downloading.
mkdir -p "$CK/engine"
cat > "$STUBBIN/go" <<EOF
#!/usr/bin/env bash
out=""
prev=""
for a in "\$@"; do [[ \$prev == "-o" ]] && out="\$a"; prev="\$a"; done
[[ -n \$out ]] || exit 1
printf '#!/usr/bin/env bash\ncase "\$1" in --version|-v|version) echo "1.4.0";; install) echo "install" >> "%s";; esac\n' "$STUB_LOG" > "\$out"
chmod +x "\$out"
EOF
chmod +x "$STUBBIN/go"
H9I=$(new_home build)
installs_before=$(grep -c "^install$" "$STUB_LOG" || true)
env_base "$H9I" DAYFLOW_BUILD=local bash "$CK/scripts/install.sh" > "$SBX/o9i" 2>&1 \
  && [[ $($H9I/.local/bin/dayflow --version) == "1.4.0" ]] \
  && ok "DAYFLOW_BUILD=local built and installed" || { bad "local build failed"; cat "$SBX/o9i"; }
[[ $(( $(grep -c "^install$" "$STUB_LOG" || true) - installs_before )) -eq 1 ]] \
  && ok "local build ran dayflow install" || bad "local build skipped units"
# negative: no engine/ dir → clean error
env_base "$(new_home nobuild)" DAYFLOW_BUILD=local bash "$LONE/install.sh" > "$SBX/o9i2" 2>&1 \
  && bad "local build without engine/ accepted" || ok "local build requires engine/"
rm -f "$STUBBIN/go"

echo "== uninstall.sh =="

# 10. Happy path: units removed (stub records it), binary removed, data kept.
H10=$(new_home un)
mkdir -p "$H10/.local/bin" "$H10/.local/share/dayflow" "$H10/.config/dayflow"
fake_binary 1.4.0 > "$H10/.local/bin/dayflow"; chmod +x "$H10/.local/bin/dayflow"
touch "$H10/.local/share/dayflow/dayflow.db"
env -i PATH="$STUBBIN:$PATH" HOME="$H10" bash "$UNINSTALLER" > "$SBX/o10" 2>&1 \
  && ok "uninstall exits 0" || { bad "uninstall failed"; cat "$SBX/o10"; }
[[ -e $H10/.local/bin/dayflow ]] && bad "binary still present" || ok "binary removed"
[[ -f $H10/.local/share/dayflow/dayflow.db ]] && ok "user data kept" || bad "user data deleted"
tail -1 "$STUB_LOG" | grep -q "^uninstall$" && ok "dayflow uninstall ran" || bad "units not removed"

# 11. No binary at all → still exits 0.
H11=$(new_home unempty)
env -i PATH="$STUBBIN:$PATH" HOME="$H11" bash "$UNINSTALLER" > "$SBX/o11" 2>&1 \
  && ok "uninstall with no binary exits 0" || { bad "uninstall-no-binary"; cat "$SBX/o11"; }

# 12. `dayflow uninstall` failing keeps the binary — removing it would
# orphan enabled units this script can no longer clean up.
H12=$(new_home unfail)
mkdir -p "$H12/.local/bin"
fake_binary 1.4.0 > "$H12/.local/bin/dayflow"; chmod +x "$H12/.local/bin/dayflow"
env -i PATH="$STUBBIN:$PATH" HOME="$H12" STUB_FAIL_UNINSTALL=1 \
  bash "$UNINSTALLER" > "$SBX/o12" 2>&1
[[ -x $H12/.local/bin/dayflow ]] && ok "binary kept when uninstall fails" \
  || bad "binary removed despite uninstall failure"
grep -q "keeping" "$SBX/o12" && ok "kept-binary reason printed" || bad "no reason printed"

# 13. Non-executable file at BIN: can't run its teardown, still removed.
H13=$(new_home unnoexec)
mkdir -p "$H13/.local/bin"
echo "not a binary" > "$H13/.local/bin/dayflow"
env -i PATH="$STUBBIN:$PATH" HOME="$H13" bash "$UNINSTALLER" > "$SBX/o13" 2>&1 \
  && [[ ! -e $H13/.local/bin/dayflow ]] && ok "non-executable binary removed" \
  || bad "non-executable binary left behind"

echo
echo "$PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
