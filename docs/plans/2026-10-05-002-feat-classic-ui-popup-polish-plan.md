# Plan: classic-UI popup polish — "tight and neat"

**Created:** 2026-10-05
**Origin:** `docs/brainstorms/2026-10-05-classic-ui-polish-requirements.md`
(U3 entry gate of `docs/plans/2026-10-05-001-feat-classic-ui-v160-release-plan.md`,
referred to below as **the 001 plan**; this plan's own units are "U1–U5".)

## Problem frame

The classic popup is the v1.6.0 baseline and the daily surface. It reads loose:
~39 hand-rolled card literals and ~95 `radius: Style.cornerRadius` sites
re-derive the same visuals per screen; a four-row chrome stack sits under every
tab; Settings is a single 809-line scroll that overflows small panels and
fields ~16 of ~44 json-tagged config keys (the origin doc's "~30" was an
undercount; this recount supersedes it). The user rejected the Pulse dashboard
as "huge and empty and overdone" — this pass compresses what exists, adds no
new surfaces, and keeps the popup/full-window architecture untouched.

## Key technical decisions

- **KTD1 — Shared components before per-screen tightening.** Introduce
  `PanelCard.qml` (card + `well` variant for input boxes), `PanelButton.qml`
  (bordered button with `selected`/`enabled` states), `PanelChip.qml`
  (pill/dot), `StatRow.qml` (label + dim value row). All take `required
  property var dayflow` and wrap `qs.Commons`/`qs.Ui` tokens — matching the
  existing shared-component convention (`BusyBar`, `CopyButton`). Flat files,
  implicit directory import, no `qmldir`. Names avoid `QtQuick.Controls`
  collisions.
- **KTD2 — Footer = one permanent row + transient rows.** The permanent row is
  the status line (`N frames · M pending · storage · ignoring a,b`) plus a `⋯`
  overflow button holding the three quick actions. Layout: status `Text` gets
  `width: row.width - overflowBtn.width - spacing` with `elide:
  Text.ElideRight`; `⋯` is fixed-width, never compressed. The version-skew
  banner (already `visible`-gated) and the notice text render as transient
  rows only when set — "one row by default" is satisfied.
  - **Notice lifecycle.** Notices currently never auto-clear (`dayflow.notice`
    is only cleared by `applyConfig`) — that would pin the footer at two rows
    after the first action. Success/info notices auto-clear after ~4s (Timer)
    or on panel close; error notices persist until the next action. The save
    confirmation must survive the `configProc` reload it triggers — `applyConfig`
    skips `notice = ""` when a save-originated notice is fresher than the reload.
  - **In-flight feedback.** While `summarizeProc.running`, the `⋯` button shows
    a busy state and the status row suffixes `summarizing…`; on exit a
    completion notice ("summaries done"/"summarize failed") closes the loop —
    today the footer button label *is* the progress indicator, so the menu
    alone would show nothing.
  - **Menu mechanics.** Upward-opening overlay anchored to `⋯`, inside panel
    bounds (the window is exactly content-height — a downward popup clips).
    Escape/outside-click/item-select closes the menu first; the panel's
    `PanelKeyCatcher` close-request fires only when the menu is closed. Menu is
    mouse-driven this pass (parity with today's buttons); keyboard nav is
    deferred. Fallback if the overlay fights `KeyboardPanel`: inline compact
    chip row — spec'd as one row (chips replace the status text while open),
    not a second footer row.
- **KTD3 — No engine changes; patch writes go over stdin.** Patch-only keys
  (`routing`, `knowledge_*`, `pricing`, `request_timeout_sec`) write via fresh
  single-key `config patch -` payloads piped to the process's stdin
  (`stdinEnabled` + `write()`), mirroring `patchProc` at Panel.qml:184 and
  Onboarding.qml:64 — the codebase's convention keeps secrets out of argv
  (`knowledge_secret_ref` can hold a literal secret). `provider`/`routing`
  options write via `provider set` or a fresh `{"routing":{...}}` patch.
  `ignore_apps` never round-trips a list: the field is a comma-separated list;
  on commit the UI reads live `config --json`, diffs against the field, and
  emits `dayflow ignore <class>` / `dayflow unignore <class>` per delta —
  `config set ignore_apps` replaces the whole list and would clobber apps
  ignored via `--active` since load.
- **KTD4 — saveConfig sends a dirty-key diff, delete-guard stays as belt.**
  `saveConfig` builds the patch from keys where `configDraft` deep-differs
  from the last-loaded `config --json` — untouched keys are never sent, which
  closes the stale-snapshot class structurally (external writers of
  `ignore_apps`, `panel_expanded`, provider/routing state can't be reverted
  by an old draft) and drops masked keys for free (draft sentinel equals
  loaded sentinel). The literal `delete patch.providers` /
  `delete patch.routing` lines stay as defense-in-depth for keys that *did*
  change but own dedicated paths — `engine/panel_carry_test.go` greps for
  them. This replaces the planned enumeration-only widening; the blacklist
  stays correct only while the external-writer inventory does.
- **KTD5 — Settings = collapsible groups, scroll-capable.** ~40+ fields cannot
  all fit the 460px cap at once; groups collapse by disclosure. Groups toggle
  independently (no auto-collapse/accordion); the existing `Flickable` is
  retained so expanded content scrolls — "no clipping" means no mid-line cut,
  not no-scroll. "AI provider" is open by default, all others closed. Collapsed
  state is hoisted to `dayflow` properties so it survives the tab `Loader`'s
  destroy/recreate within a session; cross-session persistence stays out of
  scope. R4's no-scroll rule applies to the four content tabs at typical data
  volumes; Settings' contract is "no clipping, groups collapsible" per its
  acceptance example, and ChatTab's transcript scrolls inherently ("primary
  content" = chrome + latest exchange) — both boundary clarifications, not
  contract changes.
- **KTD6 — Resurrect the deleted harness mechanics, port the scenario set.**
  `tests/ui/fit.qml` and `tests/ui/verify-fit.py` are recoverable at
  `3990660~1` (the tree before the 001 plan's U1 deleted them).
  `fit-completions.qml`/`verify-completions.py` stay deleted — they exercise
  post-`91ee9fe` surfaces that do not exist on the classic tree. The original
  36 scenarios were authored against the new UI (PagedText pagination, the
  1160px three-column Settings), so they are **ported**, not run unmodified:
  drop new-UI-only cases, re-baseline Settings/onboarding scenarios against
  classic surfaces, then extend with footer-row and Settings-group scenarios.
  Mechanics: symlink `*.qml` into a temp root, stub `qs.Commons`/`qs.Ui`
  singletons + a canned-JSON `dayflow` on PATH, render via `quickshell --path`
  with `QT_QPA_PLATFORM=offscreen`, assert rendered geometry. The stub
  `dayflow` exits non-zero on any invocation it doesn't explicitly record,
  and the harness exports `DAYFLOW_CONFIG`/`DAYFLOW_DATA_DIR` to the temp
  root — a pass-through can never touch the live config or keyring. Rendered
  geometry is the only fit evidence — never source-string checks.
- **KTD7 — `knowledge_secret_ref` renders cleartext with hint copy.** Engine
  masks only the two `*_api_key` fields and `providers[].api_key`; the ref is
  designed to be an `omaseal://service/account` reference, not a secret. The
  field shows its value and hints at the omaseal-ref form. (Flagged: a literal
  secret pasted here stays cleartext — in the field, in config.json, and in
  every `config --json` read path; accepted, noted in PR, engine masking stays
  deferred.)
- **KTD8 — Keyring hint, scoped to fields with a real keyring path.** Only
  `openrouter_api_key` (and `providers[].api_key`, out of scope this pass)
  have engine keyring reads — `decisions_api_key` never does, so it gets no
  hint. `dayflow key status` runs alongside `configProc` in
  `refreshForTab("settings")` and re-runs after a successful save; on failure
  or missing omaseal the hint simply doesn't render (it fatals when omaseal
  is absent — `main.go:1157`). When the keyring reports the account present:
  the field shows a "stored in system keyring" caption under the masked
  sentinel, stays editable, and an edit routes through `dayflow key set
  <account>` on stdin with the key deleted from the draft patch — a
  config.json copy would silently win over keyring otherwise
  (`config.go:199`). When keyring is absent the field behaves exactly as
  today (draft patch + sentinel strip).

## Scope boundaries

**In scope:** popup shell + all five tabs + `Onboarding.qml`/`InstallPrompt.qml`
(literal normalization only — no onboarding layout rework), `tests/ui`
resurrection, `Panel.qml` footer, `Settings.qml` rework.

**Deferred to follow-up work:** full-window pane polish (`FullView.qml` +
`TodayPane`/`WeekPane`/`TimelapsePane`/`ContextPane`/`AgentsPane` — same shared
components apply there unchanged, per the `host.dayflow` prop pattern); "still
working" Agents hint; stdin transport for prompt overrides; CI QML compile
check; engine masking for `knowledge_secret_ref`; stale `config set`
supported-keys doc comment in `engine/config.go`; keyboard navigation for the
overflow menu (mouse-driven parity this pass); providers[] `api_key` editing
surface; routing/pricing entry removal (see U4 note).

**Non-goals:** new tabs or navigation, wholesale surface replacement, Pulse
dashboard revival, new engine features or call-site changes, Flow parity.

## High-level technical design

```
Panel.qml bottom chrome, before → after
─────────────────────────────────────────────────────
 PanelSeparator                                     │  PanelSeparator
 Flow [Ignore app][Summarize(N)][Full view]         │  ┌─ permanent row ─┐
 skew banner Column (conditional)                   │  │ status ····· ⋯  │
 status Text                                        │  └─────│──────────┘
 notice Text (conditional)                          │        └─ overflow:
                                                       Ignore app (dim if
                                                       none) / Summarize
                                                       (N pending) /
                                                       Full view
                                              transient: skew banner,
                                              notice — visible-gated
```

Settings group order (collapsed unless noted): **AI provider** (open —
provider, model, presets, key, api_base_url, decisions_url/model/api_key,
site_name) → **Capture** → **Classification** (jev_classification,
classification_model, classification_prompt, categories) → **Agents**
(agent_recaps, agent_recap_batch, agent_completions) → **Sync** (knowledge_sync
+ transport-conditional subkeys, decisions covered above) → **Behavior**
(ignore_apps, auto_pause_locked, filter_inappropriate, notifications) →
**Advanced** (request_timeout_sec, keep_frames, output, capture_command, debug,
pricing note) → **Prompt overrides** (per-provider blocks, "saves on Enter"
caption).

## Implementation units

### U1. Shared components + harness resurrection

**Goal:** `PanelCard`, `PanelButton`, `PanelChip`, `StatRow` exist and render
under the inert stub harness; `tests/ui` fit harness restored with the
scenario set ported to classic surfaces, green.

**Requirements:** R2, R8. **Dependencies:** none.

**Files:** create `PanelCard.qml`, `PanelButton.qml`, `PanelChip.qml`,
`StatRow.qml`, `tests/ui/fit.qml`, `tests/ui/verify-fit.py` (restored from
`3990660~1` then extended); no existing files modified.

**Approach:** components own the exact literals the codebase repeats —
PanelCard `fgFill(0.04)`/`fgFill(0.08)` border, `well` variant `0.04`/`0.12`;
PanelButton height 24/26 variants, hover `btnBg`, `selected` `accentFill`,
border `accentFill(0.5)`, `enabled` drives `opacity 0.45` + MouseArea gate
(matching today's dimmed-not-hidden behavior). Harness stubs get new stub
model props (`expanded`, `engineMissing`, `activeApp`, `blocksPending`,
`notice`, versions) mirroring Panel.qml's usage so components load under it.

**Test scenarios:**
- Harness renders each new component at 540px and asserts implicit size > 0
  and border/radius equal to the canonical literal.
- `verify-fit.py` runs the ported battery against the classic tree — new-UI
  scenarios (PagedText pagination, 1160px three-column Settings) dropped and
  re-baselined to classic surfaces; green before polish work starts.
- Component with `enabled: false` reports opacity 0.45; `selected` variant
  shows accentFill fill.
- Stub `dayflow` exits non-zero on an unrecorded invocation (fail-closed);
  harness env points `DAYFLOW_CONFIG`/`DAYFLOW_DATA_DIR` at the temp root.

**Verification:** `python tests/ui/verify-fit.py` exits 0 on the pre-polish
tree (ported battery, classic surfaces).

### U2. Footer collapse (Panel.qml)

**Goal:** bottom chrome is one permanent row (status + `⋯` overflow); skew
banner and notice stay transient; quick actions keep live state inside the
menu.

**Requirements:** R3, R7. **Dependencies:** U1.

**Files:** `Panel.qml`, `Settings.qml` (notice-row removal only),
`tests/ui/verify-fit.py` (new scenarios).

**Approach:** reuse `PanelChip`/`PanelButton` for the overflow affordance; menu
mechanics, notice lifecycle, and in-flight feedback per KTD2. Menu items
mirror today's gates verbatim — "Ignore current app" disabled when
`activeApp === ""`; "Summarize now"/"(N pending)"/"Summarizing…" label and
disable logic copied from the Flow (Panel.qml:1426-1444); "Full view" always
enabled. Menu suppressed under `engineMissing`/`Onboarding` surfaces — status
row still renders. The in-tab Settings notice duplicate (Settings.qml:789-797)
is removed; the transient footer notice row carries it.

**Test scenarios:**
- Rendered: footer contributes exactly one row-height when no notice/skew —
  panel `contentHeight` shrinks by the Flow height vs the 91ee9fe baseline.
- `blocksPending=3` → menu label reads "Summarize now (3 pending)".
- `activeApp=""` → "Ignore current app" entry disabled (opacity 0.45).
- Skew banner appears when versions differ, dismissed by "Not now", returns
  for a new version pair (existing `skewDismissedFor` logic untouched).
- `notice="copied"` → transient row renders, clears on the ~4s Timer (stub
  advances the timer or asserts the Timer's interval/repeat bindings).
- `summarizeProc.running` stub → `⋯` busy state + `summarizing…` suffix;
  exit sets a completion notice.
- Menu overlay geometry stays inside panel bounds (rendered y-extent ≥ 0,
  ≤ panel height) at 540px.

**Verification:** harness geometry assert + manual popup open on the live
shell (menu dismissal and Escape ordering can't be exercised offscreen).

### U3. Per-tab tightening via shared components

**Goal:** every popup surface composes shared components; filler copy cut;
identical information density requirement met.

**Requirements:** R1, R2, R4. **Dependencies:** U1 (U2 independent but same
file, sequence anyway to keep the diff readable).

**Files:** `Panel.qml` (header literals, tab-bar buttons), `TodayTab.qml`,
`StandupTab.qml`, `ChatTab.qml`, `WeekTab.qml`, `Onboarding.qml`,
`InstallPrompt.qml`, `tests/ui/verify-fit.py`.

**Approach:** mechanical substitution of the ~39 card literals and bordered-
button literals for the shared components; then compression passes: drop
redundant explainer notes (keep the agent-recaps consent copy — it is
consent-relevant, not filler), collapse stat tiles that wrap a single stat,
tighten `spacing`/`padding` where a row repeats chrome. ChatTab's transcript
scrolls inherently — "primary content" for R4 is chrome + latest exchange.
Preserve the three documented carries and every engine call site verbatim.

**Test scenarios:**
- TodayTab with a 12-span fixture renders ≥6 spans inside the 360px cap.
- Each tab's rendered height ≤ existing cap at typical fixtures; no content
  clipped mid-line.
- `git diff 91ee9fe -- Panel.qml` still shows only carries plus polish edits
  (carries intact — `panel_carry_test.go` green).
- Onboarding renders all 5 steps without layout regressions under the
  harness's stubbed `detect`/`doctor`.

**Verification:** harness scenarios + `cd engine && go test ./...`.

### U4. Settings rework — dense groups + all engine-gap fields

**Goal:** Settings is grouped/collapsible, fits the 460px cap without
clipping, and exposes every engine-only key through its correct write path.

**Requirements:** R5, R6, R4-as-modified. **Dependencies:** U1, U2.

**Files:** `Settings.qml`, `SettingsGroup.qml` (new), `Panel.qml` (saveConfig
diff construction + delete-guard), `tests/ui/verify-fit.py`,
`engine/panel_carry_test.go` (extend pins: Settings.qml prompt-override display
reads `spec.key`, alongside the delete-guard literals), `README.md` (config
table gains the UI-surfaced note).

**Approach:** `SettingsGroup.qml` (disclosure header + collapsible body) added
to the shared set; group order per HTD; expansion policy per KTD5.

**Write timing:** fields with dedicated/patch-only paths commit on
Enter/focus-out, matching `prompt_overrides`' "saves on Enter" model — the
Save button covers the `configDraft`-diff keys only. Dedicated-path writes
queue through one Process per key (the `pendingProviderWrites` pattern);
partial failures surface as an aggregated sticky notice ("saved except:
<keys>") rather than letting the last writer's result overwrite an earlier
error.

Field→write-path map (explicit — every surfaced key named; the full list is
checked against `Config`'s json tags, ~44 keys):

| Keys | Write path |
|---|---|
| provider, model, api_base_url, site_name, capture_interval_sec, block_minutes, frames_per_block, jpeg_quality, frame_max_dim, retention_days, max_frames_mb, max_db_mb, max_storage_mb, keep_frames, auto_pause_locked, filter_inappropriate, debug, output, capture_command, jev_classification, classification_model, classification_prompt, agent_recaps, agent_recap_batch, agent_completions, decisions_url, decisions_model, categories | `configDraft` dirty-diff patch (KTD4 — unchanged semantics, no external writers) |
| decisions_api_key | draft-diff patch, `"***redacted***"` sentinel strip (existing guard, R6); no keyring path exists for it |
| openrouter_api_key | keyring present → `dayflow key set` on stdin + key removed from draft; keyring absent → draft-diff + sentinel strip (KTD8) |
| ignore_apps | comma-separated field; commit diffs against live `config --json` and emits `dayflow ignore`/`unignore` per delta; deleted from draft patch |
| notifications | dedicated `config set notifications.enabled`; deleted from draft patch |
| knowledge_* (all 7) | fresh single-key `config patch -` stdin per field; deleted from draft patch |
| routing fields | fresh `{"routing":{...}}` `config patch -` stdin; deleted from draft patch; add/overwrite-only this pass (patch merge cannot delete map entries) |
| prompt_overrides | existing `provider set` queue, unchanged (argv exposure already a deferred finding); `providers[].api_key` is **not** surfaced — `provider set` passes argv verbatim with no sentinel strip |
| request_timeout_sec, pricing | fresh `config patch -` stdin payloads; deleted from draft patch; pricing add/overwrite-only like routing |
| panel_expanded | already external (header button) — never in the draft-diff |

`decisions_url` field carries hint copy: "URL shape picks transport — `/v1`
base or `…/chat/completions` selects chat transport". `knowledge_*` subkeys
show only under their transport branch (ssh host/container/port vs http url).
`knowledge_secret_ref` is cleartext + `omaseal://` hint (KTD7). The
`knowledge_sync` toggle carries consent copy mirroring `agent_recaps` —
`config.go:69` documents enabling it as consent to journal atoms leaving the
machine.

**Test scenarios:**
- Harness: Settings at 1280×720 viewport renders with no clipping; each group
  expands/collapses; provider group open by default; collapsed state survives
  a tab switch in-session.
- `decisions_url` field commit produces a `config patch -` call with the
  payload on stdin (stub records argv + stdin); a `/v1`-less value shows the
  transport hint.
- Saving with an untouched masked key sends no `*_api_key` in the payload
  (diff excludes it); editing to a new value sends it — never the
  `"***redacted***"` literal.
- `dayflow ignore --active` then field commit → the live-added app survives:
  emitted calls are `ignore`/`unignore` deltas only, never a list rewrite
  (KTD3 regression scenario).
- `key status` reports set → `openrouter_api_key` shows the keyring caption
  and its edit routes `key set`; `key status` absent/failing → no hint, no
  crash; `decisions_api_key` shows no keyring caption ever.
- Saving with only `panel_expanded`-style externally-owned keys untouched
  emits an empty/no-op draft patch.

**Verification:** harness scenarios + `go test ./...` (carry test pins the
delete-guard literals + `spec.key` display read) + live popup smoke at
compact and expanded widths.

### U5. Docs + ship

**Goal:** PR carries the polish rationale; roadmap and deferred findings
updated.

**Requirements:** all. **Dependencies:** U1–U4.

**Files:** `ROADMAP.md`, `docs/brainstorms/2026-10-05-classic-ui-polish-requirements.md`
(link back), PR body.

**Approach:** mark the classic-UI polish track done in ROADMAP's Next list;
PR body lists the deferred findings carried over from the 001 plan's U1
review (masked-key edit footgun, argv prompt overrides, wider stale-snapshot
class) and which ones this pass resolved — the diff-based `saveConfig` (KTD4)
closes the stale-snapshot class structurally.

**Test scenarios:** `Test expectation: none — docs and PR mechanics only.`

**Verification:** `go test ./...` green, harness green, live popup verified.

## Verification contract

- `cd engine && go build ./... && go vet ./... && go test ./...` — includes
  `panel_carry_test.go` (carries + widened delete-guard) and
  `version_test.go`.
- `python tests/ui/verify-fit.py` — resurrected battery + new footer/Settings
  scenarios; rendered geometry only.
- `bash scripts/test-install.sh` — unchanged surfaces must still pass.
- Live check on the plugin checkout: hot-reload with zero QML errors in the
  journal; `dayflow status` unaffected.

## Risks

- **QML regressions hide in un-referenced literals** — mitigated by R8's
  rendered checks and the harness baseline run before edits land.
- **`panel_carry_test.go` greps literal lines** — KTD4's diff construction
  restructures `saveConfig`; the pinned `delete` strings and the Settings
  `spec.key` read stay verbatim so the test stays green.
- **Menu mechanics under `KeyboardPanel`/`PanelKeyCatcher`** — overflow popup
  focus is the least-tested piece and cannot be exercised offscreen; fallback
  is the single-row inline chip strip spec'd in KTD2.
- **Harness stub tokens can drift from the live shell** — `qs.Commons`/`qs.Ui`
  stub constants are hand-copied, so the harness proves component/runtime
  geometry, not host acceptance. Mitigation: stub values are generated
  against the installed shell's tokens at harness setup (not hand-typed), and
  U5's live-popup verification includes a fit check at the 460px cap, not
  just zero-QML-errors.
- **Settings group state persistence** — collapsed state lives on `dayflow`
  (survives tab switches in-session); cross-session persistence is out of
  scope.
