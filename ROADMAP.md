# Dayflow for Linux — Roadmap

A private, automatic work journal for Linux (Omarchy/Hyprland, any wlroots
compositor). Port of [Dayflow](https://www.dayflow.so/) (macOS). Current
release: **v1.6.0** (tag pending; master = release content).

Last updated: 2026-10-05.

## Shipped — v1.6.0

- 10s capture → dedupe → 15-min vision-model summaries (OpenRouter, ~$0.09/M tokens), local-first storage.
- Jev classification for category + productive flag; opt-in agent-session recaps (Claude Code, Codex, OpenCode, Devin, Cursor).
- Multi-provider routing: OpenRouter / custom / local / MCP / CLI providers (`kind: "cli"` shells out to subscription-auth agent CLIs).
- OpenRouter app attribution (`HTTP-Referer` + `X-Title`) on every call, including model listing (#46).
- macOS-parity v1.3; v1.4 facelift; completion program (focus capture, notifications, FTS5 search, usage stats, drift-watch) merged.
- Omarchy marketplace listing (`io.github.duketopceo.dayflow`) with automated install: `scripts/install.sh` pins the engine binary to the plugin's manifest version, verifies SHA256SUMS before placing it in `~/.local/bin`, and enables the systemd user units; the widget surfaces a one-click install when the binary is missing.
- Agents pane (FullView) with per-source drift/unavailable status (#42). Per-source stats strip (#44) was removed with the classic-UI restore; data remains via `dayflow briefing --json`.
- Agents briefing watchdog raised 75s→300s — fixes the pane never loading while live sessions invalidate the fingerprint mid-build (#43).
- Complete local agent-chat indexing: FTS5 over all five harnesses, bounded retrieval feeding `ask`/chat; runaway safeguards (deadlines, byte budgets, watermarks, partial-write safety).
- Keyring-outage hardening: pre-flight key check + auto-heal so a locked omaseal can't silently starve summarization (#41).
- Opt-in knowledge-brain sync: `dayflow sync` pushes one distilled markdown doc per day (journal + agent workstreams) to a configured Kurultai `/ingest` endpoint; content-hash dedup; no frames/transcripts/raw turns (#45).
- Genericized sync transport: `knowledge_transport` picks `http` (direct POST to `knowledge_url`) or `ssh` (docker-exec relay for loopback-only brains); all endpoint/secret fields required in user config — no personal infra in source (#48).
- Kurultai-side `remote_ingest` feature flag (duketopceo/kurultai#406): secret-authenticated non-loopback `/ingest`, default-off, audit-logged; enabled on the personal deployment service.
- Batch agent recaps: `agent_recap_batch` submits uncached sessions as one OpenRouter Batch API job (~50% off, async) and collects results on a later pass once the job completes; per-call-site routing lets `agent_recap`/`agent_briefing` run on a different model than chat.

## Next

1. ~~Deploy + flip~~ — done: kurultai redeployed with `remote_ingest`, local
   `knowledge_transport: http`, sync verified landing `lane: "trusted"` in
   default search.
2. Per-agent-conversation playback ranges (upstream `AgentPlaybackView` parity; stats half shipped in #44).
3. Classic-UI polish/redesign — gated track (U3 of the v1.6.0 plan): dense, specific, not bloated; design brief first, incremental on the classic architecture.
4. Open issues: #2 morning/evening reflections UI, #3 summary quality ratings, #4 streaming chat.
5. Windows capture adapter research (grabscreen equivalents) — parked until asked for.

Resolved dead-ends: Flow parity (upstream Flow is a hosted waitlist product, nothing local to port); Fable integration (declined).

## Principles

Local-first by default; everything that leaves the machine is opt-in and
configurable. No silent transcript egress. No personal infrastructure in
source — endpoints, hosts, and secret references are user config. Port, not
clone: upstream (Jerry, dayflow.so) owns the brand and the product direction;
this repo is the Linux distribution lane.
