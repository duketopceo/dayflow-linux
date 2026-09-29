# Maintenance runbook — drift-watch & upgrade safety

Operational guide for keeping dayflow healthy as upstream tools evolve.
Plan context: U6a/U6b of `docs/plans/2026-09-28-001-feat-completion-and-maintenance-plan.md`.

## Agent-store drift-watch

The agent sources (Claude Code, Codex, OpenCode, Devin, Cursor) read
undocumented, version-fragile on-disk stores. Drift is caught by **semantic
contracts**, not schema assertions: for each source, a committed fixture in
`engine/testdata/stores/` pins the known store shape, and
`engine/store_contracts_test.go` asserts the adapter's own `Scan` — the
same path `dayflow agents` runs — extracts ≥1 sentinel session from it.

A schema-only check is deliberately avoided: it false-positives on Cursor's
two legal layouts (`composerHeaders`+`cursorDiskKV` vs `composerData:`-blob
fallback) and on OpenCode's two generations (`session_message` vs
`message`+`part`), and it can't see payload-shape drift.

| Source | Fixture files |
|--------|---------------|
| claude | `claude.jsonl` (synthetic transcript) |
| codex  | `codex.jsonl` (synthetic transcript) |
| opencode | `opencode.sql` (next gen), `opencode-old.sql` (legacy gen) |
| devin  | `devin.sql` (`sessions.db` shape; ATIF `transcripts/*.json` are pinned in-test) |
| cursor | `cursor.sql` (headers layout), `cursor-fallback.sql` (KV-only layout) |

### When a contract test fails

The failure message names the source. Two possible causes:

1. **Upstream drift** — the tool changed its store. Regenerate the fixture
   from a real store (below), review the `.sql` diff to see what moved,
   then update the adapter (`engine/agents_*.go`) until the regenerated
   fixture passes.
2. **Fixture rot** — the fixture file was hand-edited wrong. Fix or
   regenerate it; never weaken the extraction assertion to make it pass.

Negative cases are pinned too: renamed columns must surface `unavailable`
(naming the column where the adapter's SQL sees it), and an absent store
must report `empty` — never drift.

### Regenerating a fixture

```bash
cd engine
go build -o /tmp/dayflow .
/tmp/dayflow fixtures capture <source>          # reads the default store path
/tmp/dayflow fixtures capture opencode --db /path/to/opencode.db
/tmp/dayflow fixtures capture cursor  --db /path/to/state.vscdb --out testdata/stores/cursor-fallback.sql
```

- Sources: `claude codex opencode devin cursor`. Default output is
  `testdata/stores/<source>.sql` (`.jsonl` for transcript sources) —
  run from `engine/`.
- Capture is **schema-only**: it reads `sqlite_master` DDL and
  `pragma_table_info`, then synthesizes sentinel rows (`__fixture_*__`).
  It never reads row payloads — verify the output contains no real strings
  before committing (the suite enforces this on synthetic stores too).
- Capture opens the store `mode=ro`+`immutable` — no WAL copy, no writes.
- `--db` is guarded: the path must look like that source's store
  (`opencode*.db`, `sessions.db`, `*.vscdb`).
- Layout variants: point `--db` at a store of the other generation/layout
  and use `--out` (e.g. an old-style OpenCode store → `opencode-old.sql`).
- `claude`/`codex` have no schema to capture — their fixture is a
  canonical synthetic transcript; regenerate to refresh it, or hand-edit
  the sentinel lines when the transcript line shape changes. Runtime drift
  for these sources surfaces via `dayflow agents` scan notes/drift flags.

Commit `.sql`/`.jsonl` text fixtures only — **never** a binary `.db`
(undiffable, and the sqlite freelist leaks deleted content).

### Doctor-side check (setup.go, sibling change)

The per-source `agent store <name>` doctor checks open each present store
through `openStoreProbe` (`engine/fixtures.go`) — `mode=ro`+`immutable`,
**not** `openROStore`, whose WAL temp-copy fallback would duplicate
hundreds of MB on Cursor's `state.vscdb`. `agentStoreDBs()` (same file)
resolves each DB-backed source's path(s) including test overrides;
`agentStoresDetected()` (setup.go) covers presence for all five sources
and feeds the `agents` map in `detect --json`. Each probe
(`probeAgentStoreExtraction`, setup.go) runs the adapter's extraction
reads over a bounded tail of the store — `probeTailRows` newest rows —
because Devin's `message_nodes` has no `created_at` index and Cursor's
`cursorDiskKV`/`composerHeaders` values are large blobs, so the full
windowed extraction is too expensive for doctor. Results use the doctor
check vocabulary: `info` when no store file exists (an uninstalled tool
is not drift), `ok` when the probe reads cleanly, and `warn` carrying the
query error when it doesn't. Agent-store checks never count toward
`failures` — upstream drift is a warning, not a broken install.

## Dependency cadence

- **Monthly:** `cd engine && go get -u ./... && go mod tidy`, then
  `go build . && go test . -count=1`. Pay attention to
  `modernc.org/sqlite` bumps — the store probes depend on `mode=ro`,
  `immutable`, `query_only`, and `pragma_table_info` semantics.
- After any dep bump that touches sqlite or the agents path, run the
  focused contract tests: `go test . -run 'Contract|Fixture' -count=1`.
- If a bumped dep changes behavior a contract depends on, the fixture is
  the alarm — don't patch the fixture to match a regression.

## Upgrade smoke checklist

Run after upgrading the plugin/engine on a live machine:

1. `cd engine && go build .` — binary compiles clean.
2. `go test . -count=1` — full suite green.
3. `dayflow doctor` — all checks ok; specifically:
   - `schema version`: database v ≤ binary v (a downgrade fails).
   - `schema migration` warn means the DB was just migrated — restart
     `dayflow-capture` and any `dayflow mcp` clients.
   - `plugin manifest`: binary version == installed
     `~/.config/omarchy/plugins/io.github.duketopceo.dayflow/manifest.json`
     version. `engine-only install` (info) is fine; a version gap = a
     half-applied upgrade — fix before continuing.
   - `agent store <source>` checks: `ok` = store readable, `info` = the
     tool isn't installed; `warn` = the extraction probe errored — the
     store shape moved or the DB is unreadable, see drift-watch above.
4. `systemctl --user restart dayflow-capture.service` then
   `systemctl --user status dayflow-capture.service`.
5. Frames flowing: `dayflow status` shows captures advancing;
   `dayflow frames` lists new frames for today.
6. `dayflow version` — banner matches the release you intended to run.
