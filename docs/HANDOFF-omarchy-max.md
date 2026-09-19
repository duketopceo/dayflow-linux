# Handoff — omarchy-macbook-m1 → omarchy-max (2026-09-19)

State of the world at machine wipe. Repo is clean; everything below is
on `master` and pushed.

## What's shipped

- **v1.1.0** (`e323a92`, tag `v1.1.0`) — FullView, timelapse, context
  sankey, agent recaps, forecast. PR #20.
- **Maintenance tranche** (`2709d39`) — PR #21: MCP bounded framing,
  transcript day-bucketing, log sanitization, usage breakdown, QML tab
  extraction, dead-block flagging (`Recording failed` rows), daemon
  `capture_state` health + `Restart=always`, FullView ownership fix
  (moved loader to `BarWidget.qml`), opaque FullView surface.
- **Jev judgment integration** (`fee3c86`) — PR #23: TypeSafe Jev
  decisions (`/api/alpha/decisions`) own category, merge continuity,
  quality gate, dead-block triage, standup worthiness, forecast
  confidence, sankey salience. Calibrated scores persist on blocks
  (`category_confidence`, `quality_confidence`, `same_as_prev`,
  `low_confidence`, `triaged`). Chat model keeps all generation.
  PR #22's parallel `engine/jev.go` was unified into `decisions.go` —
  config is `jev_classification` (bool off-switch) +
  `classification_model` (default `typesafe/jev-1.13`); judge keys
  resolve through the provider chain (classification→chat→vision).
  Do NOT resurrect `engine/jev.go` — it was deliberately deleted.

## Deployment (on the new machine)

- Binary: `~/.local/bin/dayflow` — rebuild with `cd engine && go build
  -o ~/.local/bin/dayflow .`
- Daemon: `systemctl --user restart dayflow-capture.service`
- Plugin: `~/.config/omarchy/plugins/io.github.duketopceo.dayflow/` —
  rsync the `*.qml` + `manifest.json` from repo root.
- **QML changes need `omarchy-restart-shell`** — hot reload does NOT
  recreate bar widgets (learned the hard way twice).
- Data: `~/.local/share/dayflow/` (dayflow.db + frames). Restoring the
  old db carries all history; the schema patches are idempotent.

## Deferred / next work

- **Omarchy marketplace**: `io.github.duketopceo.dayflow` is still NOT
  in `plugins.omarchy.org/catalog.json` (3,487 plugins, no entry).
  Submission is the obvious next milestone.
- **FullView "build on it a ton"**: user wants more panes/depth there.
  Architecture is solid: `host.dayflow` injection, section rail, all
  five panes (Today/Week/Timelapse/Context/Agents) wired.
- P3 residuals from the Jev review (documented in code):
  `worthyBlocks`/`salientShifts` share a judge-filter shape (dedup
  candidate); `llm_calls` rows carry no block↔judge correlation;
  `prevBlock` has no dedicated test; no QML test harness.
- Original motivation to keep in mind: forecast was "guess percentage
  of next day" as a fun addon — Jev made it a real calibrated number
  (`jev NN%` in `dayflow forecast`).

## Gotchas

- Asahi: `sudo` needs a password, no TTY auth — hand sudo to the user.
- Never edit `/usr/share/omarchy/` — user config only.
- `kurultai connect` device flow: use `https://api-knowledge.shippedit.dev`
  (api- host; UI host 302s on the device endpoint). devin@omarchy is
  on the Hey board — post presence from the new seat after connect.
- `.codebase-memory/` is a local MCP artifact — gitignored, never
  commit it (it slipped in twice).
- Tests must never egress: `testEnv`/`benchEnv` stub `decisionsURL`
  to a dead endpoint — keep that when adding test envs.
- CI is `.github/workflows/ci.yml` (build/vet/gofmt/test on merge refs —
  unformatted files on master poison EVERY PR's check).
