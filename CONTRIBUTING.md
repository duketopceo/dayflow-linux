# Contributing

## Build & test

```sh
cd engine
go build .                 # module root is engine/
go test -count=1 .         # full suite — stubbed providers, no network
cd .. && scripts/smoke-install.sh   # sandboxed install smoke (from repo root)
```

## Layout

- `engine/` — Go CLI/daemon/TUI/MCP. `capture.go` (daemon loop), `store.go` (schema + migrations), `agents_*.go` (per-source transcript adapters), `search.go` (FTS5), `notify.go`, `setup.go` (doctor/detect/setup).
- Repo root `*.qml` — the Omarchy/Quickshell panel. `Panel.qml` is the host; `*Pane`/`Tab` files are views.

## Invariants worth knowing before you change code

- **Consent defaults are load-bearing.** Anything that egresses data off this machine defaults off and is opt-in — `agent_recaps` is the canonical example (see `docs/plans/` and PRIVACY.md). New egress surfaces must ship default-off with honest copy naming every destination.
- **The FTS index must follow deletes.** Any new `DELETE FROM blocks` path needs to keep `blocks_fts` and `block_edits` consistent — see `deleteBlocksWhere` and the triggers in `search.go`.
- **Every subprocess on the capture tick is `CommandContext`-bounded.** An unbounded exec wedges the single-goroutine daemon loop and defeats stall reporting.
- **Versions live in three places**: `engine/main.go` `version`, `manifest.json`, `Panel.qml` `pluginVersion` — `version_test.go` pins them equal.
- **Agent-store changes need fixture updates** — `dayflow fixtures capture <source>` regenerates the contract fixtures; see `docs/maintenance.md`.

## Workflow

Feature work goes through `docs/plans/` (structured plans) → implementation → review → PR. Keep commits conventional (`feat:`, `fix:`, `docs:`). Run the suite before pushing.
