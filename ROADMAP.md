# Dayflow for Linux — Roadmap

A private, automatic work journal for Linux (Omarchy/Hyprland, any wlroots
compositor). Port of [Dayflow](https://www.dayflow.so/) (macOS). Current
release: **v1.5.0**.

Last updated: 2026-09-30.

## Shipped

- 10s capture → dedupe → 15-min vision-model summaries (OpenRouter, ~$0.09/M tokens), local-first storage.
- Jev classification for category + productive flag; opt-in agent-session recaps (Claude Code, Codex, OpenCode, Devin, Cursor).
- Multi-provider routing: OpenRouter / custom / local / MCP / CLI providers (`kind: "cli"` shells out to subscription-auth agent CLIs).
- macOS-parity v1.3; v1.4 facelift; completion program (focus capture, notifications, FTS5 search, usage stats, drift-watch) merged.
- Omarchy marketplace listing (`io.github.duketopceo.dayflow`) with automated install: `scripts/install.sh` pins the engine binary to the plugin's manifest version, verifies SHA256SUMS before placing it in `~/.local/bin`, and enables the systemd user units; the widget surfaces a one-click install when the binary is missing.

## Next

1. Flow feature parity — track the upstream macOS Dayflow beta and port what Linux users ask for.
2. Windows capture adapter research (grabscreen equivalents) — parked until asked for.

## Principles

Local-first by default; everything that leaves the machine is opt-in and
configurable. No silent transcript egress. Port, not clone: upstream (Jerry,
dayflow.so) owns the brand and the product direction; this repo is the Linux
distribution lane.
