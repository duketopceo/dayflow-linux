# Agent access to Dayflow

`dayflow mcp` is a **stdio** MCP server. It has no network listener, no auth
layer, and no remote transport — a client launches it as a child process and
talks JSON-RPC over stdin/stdout. It reads the local journal at
`~/.local/share/dayflow/dayflow.db`.

Requests are newline-delimited JSON-RPC. A request line larger than **8 MiB**
is rejected with a `-32600` error and the server keeps serving — malformed or
oversized input never kills the process.

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
| `get_log` | debug.log tail (UI actions, debug lines) | — |
| `get_frames` | frames index + frame file existence | — |
| `get_usage` | api_calls + llm_calls (all providers, all tasks; `breakdown` groups by task/provider/model) | — |
| `get_stats` | db stats, storage, config (no secrets) | — |
| `get_standup` | blocks | — |
| `get_insights` | blocks | — |
| `get_agent_sessions` | Claude Code / Codex JSONL transcripts under `~/.claude/projects/` and `~/.codex/sessions/` (read-only) | — |
| `get_forecast` | blocks (same-weekday history blend) | — |
| `chat` | blocks + journal context | **writes chat_conversations/chat_messages and sends journal-derived content to the configured AI provider** |

Every tool except `chat` is read-only — `get_agent_sessions` also reads
agent JSONL transcripts on disk. `chat` is the only tool that mutates state
or sends data to an external provider.

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

- Times are Unix seconds (UTC) in the db. CLI/MCP JSON outputs carry Unix
  seconds in `*_ts` fields (and `AgentSession.start`/`end`, frame `ts`);
  human-readable string fields (`start`, `end`, `time`) are local time.
- `blocks.status`: `done` (summarized), `failed` (retryable), `dead` (gave up).
- Jev judgment fields on blocks (all nullable — absent means "no opinion",
  never conflate with zero): `category_confidence` (0-1, Jev's argmax
  category score), `quality_confidence` (0-1, title/summary quality gate),
  `same_as_prev` (bool, merge continuity). `low_confidence` on blocks is
  true when any judgment scored < 0.55; on timeline cards it aggregates
  the children. Read-only MCP sessions never egress to Jev — the fields
  are simply absent there.
- `get_forecast` / `dayflow forecast --json`: `confidence_score` is Jev's
  calibrated probability (0-1) that the predicted mix is plausible;
  `confidence` remains the sample-count heuristic. Jev unreachable → field
  absent.
- Judge calls are recorded in `llm_calls` with `task='judge:<kind>'`,
  `provider='typesafe'` (kinds: block, triage, standup, forecast, shifts,
  agent_recap).
- `dayflow agents --json`: `recap` is a generated one-line summary cached in
  `agent_recaps` per transcript path (invalidated on file mtime/size);
  `recap_confidence` is Jev's quality score. Both fields are `omitempty` —
  they are *absent* (not `""`/`null`) when a session has no recap or no
  quality score; consumers should treat a missing `recap` as "no recap",
  which includes sessions judged unworthy for that transcript version. `--no-recaps` skips all model calls; `dayflow mcp` never
  generates recaps (judges disabled) but would serve cached ones.
  `agent_recaps: false` in config is the durable opt-out — cached rows are
  still served, nothing new is generated. Generation egress is a bounded
  excerpt (≤2 KB, first/last user message + last assistant reply) scrubbed
  of home paths and common token shapes; generation calls log as
  `task='agent_recap'` in `llm_calls`.
- `dayflow goal --json`: `streak` = `{current, best, total}` consecutive-day
  completion counts. Viewed day pending → `current` counts back from
  yesterday; any past day without a completed goal breaks a run.
- `dayflow weekly --json` `trends`: `has_prev` false means no prior-week
  data — render nothing. `categories[]` covers the union of both weeks
  (dropped categories have `curr_minutes: 0`), sorted by |Δ minutes|;
  `delta_share` is share-of-week change in percentage points.
- Frame files under `frames/` exist only until their block is summarized
  unless `keep_frames` is on; files under `quarantine/` are untracked
  orphans awaiting review, not live data.
- Do not write to the db directly. MCP exposes no journal-mutation tools;
  corrections require the CLI (`edit`, `scrub`, `retry`, `reconcile`),
  e.g. over SSH. Direct writes bypass the event log.
