#!/usr/bin/env bash
# lint-ui.sh — DESIGN.md §10 gates as a ratchet: flags literals/glyphs on
# NET-ADDED lines only (added minus removed across the diff vs merge
# base), so moved code (e.g. PopupBody) and the pre-existing U2 purge
# backlog don't permanently fail the gate. --all scans the whole tree.
set -u
cd "$(dirname "$0")/.."
fail=0
PAT_COLOR='Qt\.rgba\([0-9.]+|"#[0-9a-fA-F]{3,8}"|'"'"'#[0-9a-fA-F]{3,8}'"'"''
PAT_GLYPH='[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}\x{FE0F}]'
PAT_DASH='[–—]'

base=$(git merge-base HEAD origin/master 2>/dev/null || git merge-base HEAD master 2>/dev/null)
if [ -z "$base" ]; then echo "no merge base with master — cannot ratchet" >&2; exit 1; fi

if [ "${1:-}" = "--all" ]; then
  # whole-tree report (the U2 purge checklist) — every literal counts
  added=$(grep -rnE "$PAT_COLOR" --include='*.qml' . | grep -v '/Tokens.qml' | grep -v tests/ui/ || true)
  added_g=$(grep -rnP "$PAT_GLYPH" --include='*.qml' . | grep -v tests/ui/ || true)
  added_d=$(grep -rnP "$PAT_DASH" --include='*.qml' . | grep -v tests/ui/ || true)
else
  diff=$(git diff "$base" -- '*.qml'; git status --porcelain | awk '$1=="??" && $2 ~ /\.qml$/ {print $2}' | while read -r f; do cat "$f" | sed "s|^|+$f: |"; done)
  plus=$(echo "$diff" | grep '^+' | grep -v '^+++' || true)
  minus=$(echo "$diff" | grep '^-' | grep -v '^---' | sed 's/^-//' | sort || true)
  # net-added = + lines whose content isn't also a - line (moved code nets
  # out); counts preserved so one removal cancels only one addition.
  net=$(echo "$plus" | sed 's/^+//' | sort | comm -23 - <(echo "$minus") || true)
  added=$(echo "$net" | grep -nE "$PAT_COLOR" | grep -v '/Tokens.qml' || true)
  added_g=$(echo "$net" | grep -nP "$PAT_GLYPH" || true)
  added_d=$(echo "$net" | grep -nP "$PAT_DASH" || true)
fi

echo "== color literals (outside Tokens.qml) =="
[ -n "$added" ] && { echo "$added"; fail=1; }
echo "== emoji / presentation glyphs in strings =="
[ -n "$added_g" ] && { echo "$added_g"; fail=1; }
echo "== em/en dashes in UI strings =="
[ -n "$added_d" ] && { echo "$added_d"; fail=1; }

echo "== qmllint =="
QMLLINT=/usr/lib/qt6/bin/qmllint
[ -x "$QMLLINT" ] || QMLLINT=$(command -v qmllint || true)
files=$(git diff --name-only "$base" -- '*.qml' 2>/dev/null; git status --porcelain | awk '$1=="??" && $2 ~ /\.qml$/ {print $2}')
if [ -z "$QMLLINT" ]; then echo "qmllint not found — skipping"
elif [ -n "$files" ]; then "$QMLLINT" $files || true   # advisory: pervasive pre-existing unqualified-access noise
else echo "no changed QML"; fi

exit $fail
