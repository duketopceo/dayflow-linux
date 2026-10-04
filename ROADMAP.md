# Dayflow for Linux — Roadmap

A private, automatic work journal for Linux (Omarchy/Hyprland, any wlroots
compositor). Port of [Dayflow](https://www.dayflow.so/) (macOS). Current
release: **v1.5.0** (master is ahead of the tag; v1.6.0 pending).

Last updated: 2026-10-03.

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

## Next

1. Remote-authenticated ingest — kurultai-private PR opens `/ingest` to
   secret-authenticated non-loopback callers behind an env flag; dayflow swaps
   the SSH-relay transport for direct HTTPS and conforms the payload to the
   quality gate (trusted lane). Plan: `docs/plans/2026-10-02-001-feat-remote-ingest-trusted-sync-plan.md`
   (kept out of this public repo — lives in kurultai-private).
2. Per-agent-conversation playback ranges (upstream `AgentPlaybackView` parity; stats half shipped in #44).
3. Scrub personal deployment identifiers from code/docs/UI — knowledge sync is generic infrastructure; personal endpoints belong in user config, not source.
4. v1.6.0 tag — cut when items 1–3 land.
5. Windows capture adapter research (grabscreen equivalents) — parked until asked for.

Resolved dead-ends: Flow parity (upstream Flow is a hosted waitlist product, nothing local to port); Fable integration (declined).

## Principles

Local-first by default; everything that leaves the machine is opt-in and
configurable. No silent transcript egress. No personal infrastructure in
source — endpoints, hosts, and secret references are user config. Port, not
clone: upstream (Jerry, dayflow.so) owns the brand and the product direction;
this repo is the Linux distribution lane.
