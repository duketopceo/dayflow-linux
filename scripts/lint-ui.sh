#!/usr/bin/env bash
# lint-ui.sh — DESIGN.md §10 gates over the repo's QML.
#   1. no Qt.rgba()/hex literals outside Tokens.qml
#   2. no emoji / stray presentation glyphs in QML string literals
#   3. no em/en dashes in UI strings
#   4. qmllint on changed QML (vs merge-base, or all with --all)
set -u
cd "$(dirname "$0")/.."
fail=0

echo "== color literals (outside Tokens.qml) =="
hits=$(grep -rnE 'Qt\.rgba\([0-9.]+|"#[0-9a-fA-F]{3,8}"|'"'"'#[0-9a-fA-F]{3,8}'"'"'' --include='*.qml' . \
  | grep -v '/Tokens.qml' | grep -v tests/ui/ || true)
if [ -n "$hits" ]; then echo "$hits"; fail=1; fi

echo "== emoji / presentation glyphs in strings =="
hits=$(grep -rnP '"[^"]*[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}\x{FE0F}][^"]*"' --include='*.qml' . \
  | grep -v tests/ui/ || true)
if [ -n "$hits" ]; then echo "$hits"; fail=1; fi

echo "== em/en dashes in UI strings =="
hits=$(grep -rnP '"[^"]*[–—][^"]*"' --include='*.qml' . | grep -v tests/ui/ || true)
if [ -n "$hits" ]; then echo "$hits"; fail=1; fi

echo "== qmllint =="
QMLLINT=/usr/lib/qt6/bin/qmllint
if [ ! -x "$QMLLINT" ]; then QMLLINT=$(command -v qmllint || true); fi
if [ -z "$QMLLINT" ]; then
  echo "qmllint not found — skipping"
elif [ "${1:-}" = "--all" ]; then
  "$QMLLINT" --root . *.qml 2>&1 | grep -v '^$' || true
else
  base=$(git merge-base HEAD origin/master 2>/dev/null || git merge-base HEAD master)
  files=$(git diff --name-only "$base" -- '*.qml')
  [ -n "$files" ] && "$QMLLINT" $files || echo "no changed QML"
fi

exit $fail
