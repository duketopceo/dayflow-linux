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
| `search_agent_sessions` | `agent_msgs_fts` index of all five agent stores (may lazily ingest new turns into the index) | — |
| `get_events` | events (may include local paths, provider error strings) | — |
| `get_log` | debug.log tail (UI actions, debug lines) | — |
| `get_frames` | frames index + frame file existence | — |
| `get_usage` | api_calls + llm_calls (all providers, all tasks; `breakdown` groups by day/task/provider/model; optional `days` bounds the window in local days; `data_since` reports the coverage floor; dollar estimates appear when `pricing` config is set) | — |
| `get_stats` | db stats, storage, config (no secrets) | — |
| `get_standup` | blocks | — |
| `get_insights` | blocks | — |
| `get_agent_sessions` | Agent transcripts: Claude Code / Codex JSONL under `~/.claude/projects/` and `~/.codex/sessions/`, OpenCode `~/.local/share/opencode/opencode*.db`, Devin `~/.local/share/devin/cli/` (sessions.db + ATIF transcripts), Cursor `state.vscdb` — all stores opened read-only | meta/events drift bookkeeping only (`agent_source_seen`/`agent_source_drifted` markers + one `agent_source_drift` event per outage; no-ops under `--read-only`) |
| `get_forecast` | blocks (same-weekday history blend) | — |
| `chat` | blocks + journal context | **writes chat_conversations/chat_messages and sends journal-derived content to the configured AI provider** — a cli-routed provider can take minutes (`cli_timeout_sec`, default 180s); give the client a generous timeout |

Every tool except `chat` is read-only with respect to journal state —
`get_agent_sessions` also reads agent transcript stores on disk (JSONL +
sqlite) and performs the drift bookkeeping described above, and
`search_agent_sessions` may lazily ingest turns into the derived
`agent_msgs_fts` index (internal bookkeeping, not journal mutation; under
`--read-only` the ro handle skips ingest entirely and an unindexed DB
returns empty hits). `chat` is the only tool that mutates journal state
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
  A summarize sweep first pre-flights the provider key: unresolvable → the
  sweep aborts without touching blocks (a locked keyring can't burn retry
  attempts). Once a key resolves again, blocks that died with a
  `no API key` error are resurrected automatically on the next sweep;
  `dayflow summarize --retry` manually resets all failed/dead blocks.
- Jev judgment fields on blocks (all nullable — absent means "no opinion",
  never conflate with zero): `category_confidence` (0-1, Jev's argmax
  category score), `quality_confidence` (0-1, title/summary quality gate),
  `same_as_prev` (bool, merge continuity). `low_confidence` on blocks is
  true when any judgment scored < 0.55; on timeline cards it aggregates
  the children. Read-only MCP sessions never egress to Jev — the fields
  are simply absent there.
- `cards` is the merged activity-card array emitted — in identical shape
  and chronological order — by `today`/`day`/`timeline`/`week`/`month
  --json`, `insights --json`, and MCP `get_timeline`/`get_insights`.
  Fields: `start`/`end` (local-time strings), `start_ts`/`end_ts` (Unix
  seconds), `app`, `app_name`, `title`, `summary`, `category`,
  `productive` (bool), `blocks` (child count), `minutes`,
  `low_confidence`, `children` (the raw block objects, same shape as
  `blocks[]`). Day attribution: a card belongs to the day containing its
  first child — a card spanning midnight is one card in the payload, and
  consumers grouping by day should bucket `children` themselves (the QML
  week view renders a per-day continuation span this way). Consecutive
  blocks fold into one card when `same_as_prev` judged continuity against
  the previous *done* block, or titles match, or app+category match with
  a non-empty app. There is no separate per-surface merge — the panel and
  all JSON surfaces consume this one array.
- `get_forecast` / `dayflow forecast --json`: `confidence_score` is Jev's
  calibrated probability (0-1) that the predicted mix is plausible;
  `confidence` remains the sample-count heuristic. Jev unreachable → field
  absent.
- Judge calls are recorded in `llm_calls` with `task='judge:<kind>'`,
  `provider='typesafe'` (kinds: block, triage, standup, forecast, shifts,
  agent_recap).
- `dayflow agents --json`: `file` is an opaque, stable per-session key —
  the transcript path for claude/codex, a `<tool>://<store>/<id>` URI for
  opencode/devin/cursor. `recap` is a generated one-line summary cached in
  `agent_recaps` keyed on `file`; cache invalidation is file mtime/size
  for the JSONL sources and a content hash of message ids + update times
  for the DB sources. `recap_confidence` is Jev's quality score. Both
  fields are `omitempty` —
  they are *absent* (not `""`/`null`) when a session has no recap or no
  quality score; consumers should treat a missing `recap` as "no recap",
  which includes sessions judged unworthy for that transcript version. `--no-recaps` skips all model calls; `dayflow mcp` never
  generates recaps (judges disabled) but would serve cached ones.
  `agent_recaps` defaults to `false` — recap generation is opt-in
  (`dayflow config set agent_recaps true`); cached rows are still served
  either way, and with the flag off nothing new is generated and no
  transcript excerpt leaves the machine. Generation egress is a bounded
  excerpt (≤2 KB, first/last user message + last assistant reply) scrubbed
  of home paths and common token shapes; generation calls log as
  `task='agent_recap'` in `llm_calls`.
  The payload carries `recaps_enabled` (bool) so UIs can show the opt-in
  hint instead of an unexplained absence of recaps.
  The payload also carries `sources` — one entry per store:
  `{source, sessions, status: "ok"|"empty"|"unavailable", note?, drift?}`.
  `drift` means a previously-productive store now scans unavailable;
  `note` carries store-level detail (identifiers/sizes only, never
  content). Store locations can be overridden for tests/portable
  installs: `DAYFLOW_CLAUDE_DIR`, `DAYFLOW_CODEX_DIR`,
  `DAYFLOW_OPENCODE_DB`, `DAYFLOW_DEVIN_DIR`, `DAYFLOW_CURSOR_DB`,
  `DAYFLOW_CURSOR_WORKSPACES`.
- `dayflow briefing [day] --json` emits the Agents-pane payload: the day's
  sessions grouped into workstreams with derived statuses and condensed
  turn narratives. Top level: `day`, `generated_at`, `mode`
  (`"model"`|`"fallback"`), `recaps_enabled`, `workstreams`, `sources`
  (same shape as `agents --json`). Each workstream: `id` (stable),
  `name`, `summary`?, `bullets`?, `threads`. Each thread: `id`, `title`,
  `source`, `project`?, `session_path`, `status`
  (`blocked`|`reviewReady`|`inProgress`|`completed`), `started_at`,
  `ended_at` (Unix seconds), `latest_outcome`?, `turns`. Each turn:
  `role` (`user`|`agent`), `text`, `highlight`?
  (`keyDecision`|`keyInfo`|`readyForReview`), `ts`?, `artifact_name`?,
  `artifact_path`?. Status is derived deterministically: `inProgress`
  when the session ended within ~15 min of now, `blocked` when the last
  usable turn is an unanswered user turn, `completed` otherwise; the
  model pass may only upgrade `completed` to `reviewReady`, never invent
  `blocked`. With `agent_recaps` off (or no provider configured) the
  payload is fully populated by the deterministic fallback — `mode` is
  `"fallback"` and no transcript content leaves the machine; with recaps
  on, one bounded scrubbed skeleton goes to the configured provider —
  containing the day, workstream `id`+`name`, and per-thread `id`,
  `source`, `status`, `title` (transcript-derived first message), and
  condensed turn `role`+`text`. `session_path` and absolute project paths
  are never sent. The model rewrites prose fields (`name`/`summary`/
  `bullets`/`title`/`latest_outcome`, turn `highlight`, artifact picks) —
  ids, ordering, and statuses are engine-owned and survive malformed model
  output. Results are cached in `agent_briefings` keyed on day + content
  fingerprint; a cached `"fallback"` briefing is regenerated when consent
  is enabled later, and a cached `inProgress` status re-derives against
  the clock on every serve. `--refresh` bypasses the cache. An empty day
  returns `workstreams: []` with `sources` still populated.
  `artifact_path`/`artifact_name` are accepted from the model only when
  the path is path-shaped and literally appears in the source turn text —
  the model cannot fabricate files.
- **Agent chat index** (`dayflow ingest`, `dayflow search-agents <q>`,
  `dayflow ask <q>`, MCP `search_agent_sessions`, chat tool
  `searchAgentSessions`): every usable turn (real user input + assistant
  replies) from all five harnesses is scrubbed, bounded (4 KB), and stored
  in a content-stored FTS5 table `agent_msgs_fts` with `source`, `session`
  (the stable `file` key), `project`, `role`, `ts` citation columns.
  `agent_ingest` deduplicates turns by `sha(source|session|idx|ts)`;
  `agent_sess_fp` skips sessions whose adapter fingerprint is unchanged so
  unchanged transcripts are never re-decoded. `meta.agent_ingest_day` is
  the rolling watermark — first `ingest` backfills ~30 days, later runs
  cover watermark..today. Ingestion is fully local (no provider calls) and
  runs on demand, on `searchAgentSessions` when the watermark is stale,
  and hourly alongside daemon retention; indexed rows prune with
  `retention_days`. `dayflow ingest --reindex` wipes and rebuilds the
  index; `dayflow scrub` does NOT touch it (blocks only). FTS retrieval
  is local-only — `hits` are `{session, source, project, role, ts,
  snippet}` (max 20 matches, 8 sessions, snippet() ellipsized; session and
  project are scrubbed like the text) — only the already-scrubbed
  snippets can reach a provider, via the normal consent-gated chat path.
  Derived agent text in `agent_recaps`, `agent_briefings`, and
  `chat_messages` (tool results quoting snippets) does not currently age
  out with `retention_days`.

  **Runaway safeguards**: every ingest pass runs under a wall-clock
  deadline (90 s daemon/lazy, 10 min `dayflow ingest`) and a 256 MB
  decoded-byte ceiling; transcript files over 64 MB are skipped
  unread (same guard covers `agents`/`briefing` scans, which show the
  session absent), and sessions cap at 4000 indexed turns. A budgeted
  stop is not an error: `ingest` prints/reports `pending`, the daemon
  logs a pause line, and the per-day watermark resumes the backlog on
  the next pass. A mid-session abort writes its partial turns but
  withholds the fingerprint so the next pass re-decodes and completes
  coverage.
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
