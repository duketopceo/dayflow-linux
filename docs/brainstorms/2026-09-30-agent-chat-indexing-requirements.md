---
title: "Agent Chat Indexing — All Harness Chats Into Dayflow"
date: 2026-09-30
type: feat
status: requirements-ready
---

# Agent Chat Indexing — All Harness Chats Into Dayflow

## Summary

Index every coding-agent conversation Dayflow can reach — the five
installed harnesses, CLI and desktop alike (Claude Code, Codex, OpenCode,
Devin, Cursor) — into the local journal store, so all agent chats are
searchable and chat-queryable through Dayflow's existing surfaces.

User's phrasing: "the agents that I use in cli or desktop — basically all
chats should make it to dayflow."

## Problem Frame

Dayflow already scans the five harness stores for sessions, recaps, and
the new Agents-pane briefing — but only metadata and condensed turns land
in `agent_briefings`/`agent_recaps`. The full conversation text stays in
each tool's own silo. A user asking "what did Codex do about the auth
bug?" today has to grep five different transcript formats by hand.

This feature ingests the conversation text itself (scrubbed) into a
queryable index and wires a hybrid retrieval path — FTS5 keyword
retrieval plus model re-rank — into the existing chat surfaces.

## Requirements

### R1 — Full-fidelity ingestion

Index the usable turn text of every discovered session across all five
adapters — not just the 12-turn condensed narrative. Same `Turns()` seam
the briefing uses; same usability rules (usableUser, isEnvelopeText).
Text is scrubbed (`scrubText`) at ingest, matching the briefing's
condense-time scrubbing precedent.

### R2 — Hybrid retrieval (OpenRouter-powered)

- FTS5 keyword index over ingested turn text (new FTS table alongside
  `blocks_fts`, following search.go conventions — external content,
  failure backoff via meta stamp, `--reindex` support).
- Query-time model re-rank of FTS hits via the configured chat provider
  (OpenRouter) — bounded hits only, scrubbed text only, gated by
  `agent_recaps` consent and `DisableJudges` like the briefing path.
- No index-time embeddings — egress stays per-query and bounded.

### R3 — Chat surfaces

A shared "search agent sessions" capability reaches:

- The existing chat engine as a new tool (`searchAgentSessions`) so the
  pane Chat tab can answer over agent history.
- A CLI entry point (`dayflow ask <question>` or equivalent) reusing the
  chat orchestrator.
- MCP: a `search_agent_sessions` tool for external agents (read-only).

### R4 — Incremental + consistent

Indexing is incremental — keyed on the same fingerprint seam as recaps,
so unchanged sessions are never re-indexed and new turns pick up on the
next daemon tick / scan. Store metadata (source, session id, project,
role, ts) beside the text so results can cite and group by session.

### R5 — Privacy posture

Scrub before store. No egress when `agent_recaps` is off — FTS retrieval
still works fully offline; only the re-rank step drops. Retention follows
existing `retention_days`/`dayflow scrub` coverage where applicable, or
the contract documents the exception.

## Non-Goals

- No new UI tab — reuse the existing Chat tab and briefing pane.
- No embedding index / vector store this phase.
- No AgentPlayback visualizations, no Flow, no Windows adapter, no Fable.
- Backfill of historical sessions is allowed but bounded by each
  adapter's existing discovery window.

## Success Criteria

- `dayflow ask "what did codex do about the auth bug"` returns a grounded
  answer citing session context, with recaps enabled.
- Chat tab answers agent-history questions without a new surface.
- Re-running ingestion on an unchanged day performs no re-index work.
- With `agent_recaps` off, keyword search still works and zero egress
  occurs; `llm_calls` gains no rows.

## Work Relationships

- Stacks on `feat/agents-pane-briefing` (PR #37): reuses `Turns()`,
  `chatEgressOK`, `scanAgentDay`, fingerprint plumbing.
- Follows the briefing's consent model and the search.go FTS5/derived-
  index conventions.
- Pre-existing adapter schema drift (opencode/devin `s.title`, cursor
  `createdAt`) — a separate maintenance tranche; ingestion degrades to
  whatever adapters currently yield.

## Open Questions for Planning — resolved

- Retention: FTS rows prune with `retention_days` via a `ts < cutoff`
  delete in the existing prune pass.
- Re-rank budget: top ~8 sessions / ~20 hits per query, snippet-bound;
  synthesis = the existing tool-result answer pass.
- CLI verb: `dayflow ask` (one-shot) + `dayflow ingest` (manual index).

## Key Technical Decisions

- **Content-stored FTS5, not external-content** — `blocks_fts` is
  external-content because `blocks` exists; agent messages have no other
  home, so `agent_msgs_fts` stores text + `UNINDEXED` metadata columns
  (source/session/project/role/ts) directly. No triggers needed.
- **Dedup via `agent_ingest` tracker table** — key =
  sha(source|session_key|turn_idx|ts); INSERT OR IGNORE skips seen turns.
  Session fingerprint changes don't force reindex of unchanged turns.
- **Rolling watermark ingestion** — meta `agent_ingest_day` tracks the
  oldest unscanned day; each run scans watermark-1d → today (backfill to
  adapters' discovery window on first run) so in-flight sessions catch up.
- **Hybrid = FTS retrieval + chat-model synthesis** — the tool returns
  bounded ranked snippets; the answering model re-ranks/synthesizes in the
  tool-result pass that already exists. No separate re-rank call.
- **Re-rank egress gate = `chatEgressOK`** — same consent + DisableJudges
  + non-CLI-provider rule as recaps/briefing. FTS alone needs nothing.
- **Ingestion trigger points**: `dayflow ingest` (manual), daemon
  maintenance cadence (~30 min), and lazily inside `searchAgentSessions`
  when the watermark is stale (>1h) — all dedup-cheap.

## High-Level Technical Design

```
engine/
  agent_index.go     # agent_msgs_fts + agent_ingest schema, derived
                     #   migration, prune hook
  agent_index.go     # ingestAgentChats, searchAgentSessions
  chat.go            # searchAgentSessions tool + prompt entry
  mcp.go             # search_agent_sessions tool (read-only)
  main.go            # dayflow ask / dayflow ingest commands
  capture.go         # daemon periodic ingest hook (~30 min)
```

## Implementation Units

### IU1 — Index schema + derived migration

`agent_msgs_fts USING fts5(text, source UNINDEXED, session UNINDEXED,
project UNINDEXED, role UNINDEXED, ts UNINDEXED)` + `agent_ingest(k TEXT
PRIMARY KEY)`. Migration follows `applyDerivedIndexMigrations`: meta
backoff marker, failure degrades search to a no-op rather than wedging
openDB; `search --reindex` clears it. Prune hook: `ts < cutoff` delete
inside the retention pass. Tests: schema test entry, prune coverage.

### IU2 — Ingestion

`ingestAgentChats(db, cfg)` — for each day in watermark range: scanAdapter
sources → `Turns(sess)` → `turnRole`-filter → `scrubText` → dedup insert.
Update watermark. Dedup-cheap on repeat. Tests: fixture session indexes,
re-run inserts nothing, scrubbed text lands (secret gone), watermark
advances, empty day no-op.

### IU3 — Retrieval + surfaces

`searchAgentSessions(db, query)` → MATCH + `snippet()` + bm25 order,
grouped per session, bounded. Wire: `executeTool` case + system-prompt
tool entry; MCP `search_agent_sessions` (read-only OK — zero egress);
lazy ingest when watermark stale. Tests: keyword hit ranks, group/citation
shape, MCP tool listed + callable read-only.

### IU4 — CLI + daemon hook + docs

`dayflow ask <q>` = one-shot `chatWithJournal` on a fresh conversation,
prints answer (`--json` for the response object). `dayflow ingest
[--json]` forces a pass. Daemon calls `ingestAgentChats` on a ~30-min
cadence inside its maintenance path (non-fatal on error). Docs:
agent-contract.md new commands + index contract, README feature line.

