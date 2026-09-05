# dayflow — agent access guide

This file is the contract for agents (Claude, Codex, MCP clients)
that read or operate the dayflow work journal on this machine.

## What this is

A local-first activity journal. A daemon (`dayflow-capture.service`) captures
screen frames, and a summarizer turns each 15-minute block into a
`{title, summary, category}` record via an OpenRouter vision model.

**Data lives only on this machine** at `~/.local/share/dayflow/`:

```
~/.local/share/dayflow/
├── dayflow.db     # SQLite (WAL). The journal.
├── frames/        # raw JPEG frames awaiting summarization (usually empty)
├── debug.log      # daemon log, written when config `debug` is true
└── PAUSED         # flag file; its existence means "do not capture"
```

## Read access rules

1. **Prefer the CLI or MCP over raw SQL.** The CLI is stable interface; the schema may change.
2. Raw SQLite reads are allowed: open `dayflow.db` **read-only** (`mode=ro`, WAL is safe for concurrent readers). Never write to it — writes go through the daemon/CLI.
3. Do not read files under `frames/` unless the task explicitly needs raw screenshots — they are the most sensitive data here.
4. Respect the pause flag: if `~/.local/share/dayflow/PAUSED` exists, do not attempt to summarize or inspect frames.

## Interfaces

### CLI (preferred)

```sh
dayflow today|day <date>|timeline [--json] [date]
dayflow day <date> --grid [--json]              # macOS-style daily workflow grid
dayflow week|month [--json]
dayflow weekly [--json]                         # weekly analytics, charts, highlights
dayflow standup [--json]                         # yesterday/today standup update
dayflow standup save --date YYYY-MM-DD --highlights "..." --tasks "..." --blockers "..." --priorities "..."
dayflow standup draft|load [--date YYYY-MM-DD]   # saved standup draft
dayflow insights [day|week|month] [--json]      # focus, category, app analytics
dayflow export [today|week|month|YYYY-MM-DD]   # markdown
dayflow status [--json]
dayflow events [--json] [-n N]
dayflow usage [--json]
dayflow stats [--json]                          # storage, block counts, coverage, API usage
dayflow blocks [--json]                        # failed summaries
dayflow search <query>                         # search titles/summaries/apps
dayflow chat [message] [--conversation-id N] [--json]
dayflow conversations [--json]                   # list chat conversations
dayflow edit <start_ts|YYYY-MM-DD HH:MM> <field> <value>  # correct title/category/summary/productive
dayflow edits <start_ts|YYYY-MM-DD HH:MM>        # audit history for a block
dayflow provider list|add|set|remove|test        # multi-provider routing
dayflow retry                                  # reset failed blocks
dayflow scrub <query>                          # delete blocks matching <query>
dayflow pause|resume|toggle
dayflow ignore <class> | ignore --active | unignore <class>
dayflow config [--json] [set <k> <v>] [patch <json>]
dayflow models [--json]                               # recommended vision model presets
dayflow doctor
```

### MCP (stdio)

`dayflow mcp` exposes tools: `get_timeline(date)`, `get_status`,
`search_journal(query)`, `get_events(limit)`, `get_usage`, `get_stats()`,
`get_standup()`, `get_insights(range)`, `chat(message, conversation_id)`.

```sh
claude mcp add dayflow -- ~/.local/bin/dayflow mcp
```

### Schema (for reference / read-only SQL)

```sql
frames(id, ts, path)                                     -- pending raw frames
blocks(start_ts PK, end_ts, title, summary, category,    -- the journal
       frame_count, status, error, created_at,
       productive, activities, attempts, app)             -- app is dominant window class
block_edits(id, start_ts, field, old_value, new_value, edited_at)  -- user corrections overlay
chat_conversations(id, title, created_at, updated_at)
chat_messages(id, conversation_id, role, content, tool_calls, created_at)
daily_standup_entries(id, date, highlights, tasks, blockers, priorities, ai_summary, created_at, updated_at)
journal_entries(id, date, entry_type, content, created_at, updated_at)
day_goals(id, date, description, created_at, updated_at)
day_goal_categories(id, goal_id, category_name, minutes)
llm_calls(id, ts, call_type, provider, model, prompt_tokens, completion_tokens, status, error)
events(id, ts, type, detail)                             -- audit log
api_calls(id, ts, block_start, model, frames_sent,       -- cost log
          prompt_tokens, completion_tokens, latency_ms, status, error)
```

`blocks.activities` is a JSON array of per-app segments `{app,title,summary,category}`;
`blocks.app` is the dominant window class; `blocks.attempts` counts summarize retries
(cap 3, then `status='dead'`). `frames.app` records the focused window per frame.

`category` is one of the user-defined `categories` in config (defaults: coding,
browsing, communication, writing, design, media, meetings, system, idle, personal, other).
`productive` is an LLM-judged boolean for focus/distraction analytics; older rows fall
back to the category/app heuristic.

## Behavior rules for agents

- **Do not** call `dayflow pause`/`resume`/`ignore`/`config set` without the
  user asking — those change capture state.
- `dayflow summarize` costs API tokens; batch calls, don't loop it.
- Treat summaries as ground truth about *what was on screen*, not intent.
  Cross-check with git/file evidence before attributing work.
- When reporting "what did I do", cite block times so the user can verify.
- Frames older than `retention_days` and blocks are pruned automatically —
  don't rely on old raw frames existing.

## Config reference

`~/.config/dayflow/config.json` — keys: `provider` (`openrouter`|`local`|`custom`|`mcp`),
`model`, `api_base_url`, `capture_interval_sec`, `block_minutes`, `frames_per_block`,
`jpeg_quality`, `keep_frames`, `retention_days`, `max_storage_mb`,
`auto_pause_locked`, `filter_inappropriate`, `ignore_apps`, `output`,
`capture_command`, `openrouter_api_key`, `site_name`, `debug`,
`categories` (array of `{name, description, color?}`), and `classification_prompt`
(extra free-form instructions prepended to every summarization prompt).
Also `providers` (array of `{id,name,kind,api_base_url,api_key,model,vision,chat,enabled,prompt_overrides}`)
and `routing` (`{primary,secondary,task_provider}`) for multi-provider/failover routing.

`dayflow config` prints the config with the API key masked; `dayflow config set`
still writes the real key. `dayflow config --json` prints machine-readable JSON
(with the key redacted). `dayflow config patch '<json-object>'` merges multiple
keys atomically and is used by the Settings tab.
`provider` is a legacy hint for the endpoint style; the new `providers`/`routing`
fields control which endpoint each task uses. API keys are only sent to providers
that are not `local` or `mcp` and have a key configured.

`debug true` writes a verbose engine log to `~/.local/share/dayflow/debug.log`
(captures, dedup skips, summarize calls with tokens/latency, retention runs,
config reloads, auto-pause events). Turn it off when not needed.

`dayflow stats` shows storage usage (data dir, database, WAL, frames awaiting
summary), journal block counts and date coverage, event log size, and API
call/token/latency totals. The panel status line shows total storage too.
