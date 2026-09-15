# Agent access to Dayflow

`dayflow mcp` is a **stdio** MCP server. It has no network listener, no auth
layer, and no remote transport — a client launches it as a child process and
talks JSON-RPC over stdin/stdout. It reads the local journal at
`~/.local/share/dayflow/dayflow.db`.

```sh
claude mcp add dayflow -- ~/.local/bin/dayflow mcp
```

## Tools

| tool | reads | writes / side effects |
|---|---|---|
| `get_timeline` | blocks | — |
| `get_status` | frames, blocks, pause flag | — |
| `search_journal` | blocks | — |
| `get_events` | events (may include local paths, provider error strings) | — |
| `get_usage` | api_calls | — |
| `get_stats` | db stats, storage, config (no secrets) | — |
| `get_standup` | blocks | — |
| `get_insights` | blocks | — |
| `chat` | blocks + journal context | **writes chat_conversations/chat_messages and sends journal-derived content to the configured AI provider** |

Every tool except `chat` is a pure database read. `chat` is the only tool
that mutates state or sends data to an external provider.

## Read-only mode

Run the server read-only when the agent should observe but never write or
spend tokens:

```sh
dayflow mcp --read-only
# or
DAYFLOW_MCP_READONLY=1 dayflow mcp
```

In read-only mode `chat` is removed from `tools/list` and rejected with an
error if called anyway.

## Remote access (Tailscale)

The intended remote path is **Tailscale SSH** — the journal is sensitive, do
not expose it with Tailscale Funnel or a public listener.

```sh
# ad-hoc CLI over Tailscale SSH
ssh <host>.<tailnet>.ts.net dayflow today
ssh <host>.<tailnet>.ts.net dayflow status
```

Pull a markdown week/day timeline over SSH (good for agents on other
devices, e.g. feeding a work journal to another machine):

```sh
ssh <host>.<tailnet>.ts.net dayflow export week          # on-demand, always fresh
ssh <host>.<tailnet>.ts.net cat ~/.local/share/dayflow/exports/week.md   # cached, refreshed daily by dayflow-export.timer
```

The export file is written `0600` under `0700` `exports/` and contains
journal text — treat it with the same care as the database.

MCP over SSH stdio (launches `dayflow mcp` on the remote host; recommend
`--read-only` for untrusted or automation clients):

```sh
claude mcp add dayflow-remote -- ssh <host>.<tailnet>.ts.net ~/.local/bin/dayflow mcp --read-only
```

Notes:

- `dayflow` is a **user** service. On a headless SSH session the user systemd
  manager must be running (`loginctl enable-linger` if needed); `dayflow`
  CLI/MCP reads the data dir directly and works regardless, but
  `systemctl --user` commands need the user manager.
- The `PAUSED` flag file is respected by the capture daemon only; CLI reads
  still work while paused.
- After an engine upgrade, restart long-lived MCP clients — they hold the
  schema version of the build they launched with.

## Data contract for agents

- Times are Unix seconds (UTC) in the db; CLI/MCP output is local time.
- `blocks.status`: `done` (summarized), `failed` (retryable), `dead` (gave up).
- Frame files under `frames/` exist only until their block is summarized
  unless `keep_frames` is on; files under `quarantine/` are untracked
  orphans awaiting review, not live data.
- Do not write to the db directly — use the CLI (`edit`, `scrub`, `retry`,
  `reconcile`) or MCP tools. Direct writes bypass the event log.
