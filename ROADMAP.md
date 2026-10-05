# Dayflow for Linux — Roadmap

A private, automatic work journal for Linux (Omarchy/Hyprland, any wlroots
compositor). Port of [Dayflow](https://www.dayflow.so/) (macOS). Current
release: **v1.5.0** (master is ahead of the tag; v1.6.0 pending).

Last updated: 2026-10-05.

## Shipped

- 10s capture → dedupe → 15-min vision-model summaries (OpenRouter, ~$0.09/M tokens), local-first storage.
- Jev classification for category + productive flag; opt-in agent-session recaps (Claude Code, Codex, OpenCode, Devin, Cursor).
- Multi-provider routing: OpenRouter / custom / local / MCP / CLI providers (`kind: "cli"` shells out to subscription-auth agent CLIs).
- OpenRouter app attribution (`HTTP-Referer` + `X-Title`) on every call, including model listing (#46).
- macOS-parity v1.3; v1.4 facelift; completion program (focus capture, notifications, FTS5 search, usage stats, drift-watch) merged.
- Omarchy marketplace listing (`io.github.duketopceo.dayflow`) with automated install: `scripts/install.sh` pins the engine binary to the plugin's manifest version, verifies SHA256SUMS before placing it in `~/.local/bin`, and enables the systemd user units; the widget surfaces a one-click install when the binary is missing.
- Agents pane (FullView) + Agents tab (compact panel); per-source conversation stats — sessions, active time, turns (#42, #44).
- Agents briefing watchdog raised 75s→300s with a "still working" hint — fixes the pane never loading while live sessions invalidate the fingerprint mid-build (#43).
- Complete local agent-chat indexing: FTS5 over all five harnesses, bounded retrieval feeding `ask`/chat; runaway safeguards (deadlines, byte budgets, watermarks, partial-write safety).
- Keyring-outage hardening: pre-flight key check + auto-heal so a locked omaseal can't silently starve summarization (#41).
- Opt-in knowledge-brain sync: `dayflow sync` pushes one distilled markdown doc per day (journal + agent workstreams) to a configured Kurultai `/ingest` endpoint; content-hash dedup; no frames/transcripts/raw turns (#45).
- Genericized sync transport: `knowledge_transport` picks `http` (direct POST to `knowledge_url`) or `ssh` (docker-exec relay for loopback-only brains); all endpoint/secret fields required in user config — no personal infra in source (#48).
- Kurultai-side `remote_ingest` feature flag (duketopceo/kurultai#406): secret-authenticated non-loopback `/ingest`, default-off, audit-logged; enabled on the personal deployment service.
- Batch agent recaps: `agent_recap_batch` submits uncached sessions as one OpenRouter Batch API job (~50% off, async) and collects results on a later pass once the job completes; per-call-site routing lets `agent_recap`/`agent_briefing` run on a different model than chat.

## Next

0. **Ribbon Log UI, refreshed 2026-10-05** ([plan](docs/plans/2026-10-02-2315-feat-ribbon-log-ui-redesign-plan.md)): after the Pulse dashboard (#58) was reverted (#60), build one tab set once. Full View tabs: Today, Week (with the category flow folded in), Standup, Ask, Agents, Replay, Settings; Search is a Ctrl+K overlay; onboarding is a 3-step overlay. The popup becomes a Week glance (modeled on upstream's week grid) with pause, copy standup, and open Full View. Order: P0 identity and theming (U1-U6), then P1 shared components and lazy tab host (U7, U21), Today/Week/Ask/Settings tabs (U18, U11, U20, U22, U9), Ctrl+K search (U12), Week-glance popup (U10), delete duplicate Today/Week code (U19, about 1,500 lines); then P2 Replay, Agents polish, onboarding, TUI ribbon, marketing assets (U13-U17). Lightweight rule: lazy per-tab data, hidden tabs unloaded, no LLM call on tab open, frames only in Replay. Next up: U1 (fixture day and preview harness).

1. ~~Deploy + flip~~ — done: kurultai redeployed with `remote_ingest`, local
   `knowledge_transport: http`, sync verified landing `lane: "trusted"` in
   default search.
2. Per-agent-conversation playback ranges (upstream `AgentPlaybackView` parity; stats half shipped in #44).
3. v1.6.0 tag — everything on the list except the playback item above is shipped; cut when ready.
4. Windows capture adapter research (grabscreen equivalents) — parked until asked for.

Resolved dead-ends: Flow parity (upstream Flow is a hosted waitlist product, nothing local to port); Fable integration (declined).

## Principles

Local-first by default; everything that leaves the machine is opt-in and
configurable. No silent transcript egress. No personal infrastructure in
source — endpoints, hosts, and secret references are user config. Port, not
clone: upstream (Jerry, dayflow.so) owns the brand and the product direction;
this repo is the Linux distribution lane.
