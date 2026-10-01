# Omarchy Plugin Marketplace Submission — Dayflow

## Issue Title

[Plugin]: Dayflow

## Repository URL

https://github.com/duketopceo/dayflow-linux

## Category

Productivity

## Tags

AI, Bar, Quickshell

## Suggested missing tag (optional)

work-journal

## Maintainer notes

**What it does:**
Dayflow is a private, automatic work journal for Omarchy / Hyprland. It captures a lightweight screenshot every 10 seconds, deduplicates unchanged frames, and every 15 minutes sends ~30 sampled frames to a vision model to generate a plain-language summary of what you were doing. The result is a readable timeline in the bar panel plus standup updates, focus/distraction analytics, multi-provider routing, chat with your journal, inline block editing, daily workflow grid, and weekly analytics charts.

**Installation:**
```sh
omarchy plugin add https://github.com/duketopceo/dayflow-linux.git --enable
```

Then open the panel and click **Install engine** — the widget runs the
plugin's `scripts/install.sh`, which downloads the release binary pinned
to the plugin's version (amd64/arm64), verifies it against the release's
`SHA256SUMS`, places it in `~/.local/bin`, and enables the systemd user
units. Provider setup is the one remaining interactive step — the panel's
onboarding wizard handles it, or `dayflow setup` in a terminal.

Equivalent terminal path (also works without Omarchy):
```sh
bash ~/.config/omarchy/plugins/io.github.duketopceo.dayflow/scripts/install.sh
# or standalone: curl -fsSL https://github.com/duketopceo/dayflow-linux/releases/latest/download/install.sh | bash
```

**Removal:**
```sh
# engine teardown first (plugin dir must still exist):
bash ~/.config/omarchy/plugins/io.github.duketopceo.dayflow/scripts/uninstall.sh
omarchy plugin disable io.github.duketopceo.dayflow
omarchy plugin remove io.github.duketopceo.dayflow
# captured data + config are kept; to also wipe them:
rm -rf ~/.local/share/dayflow
rm -rf ~/.config/dayflow
```

**Permissions / dependencies:**
- Requires `grim` (wlroots compositors) or a custom `capture_command` for other Wayland compositors.
- Uses `systemd --user` for the capture daemon and summarize timer.
- Optional `hyprctl` for per-app ignore list on Hyprland.
- Requires an OpenRouter API key or a local OpenAI-compatible endpoint (Ollama, LM Studio).
- Screen-lock auto-pause uses `loginctl` (systemd). Falls back to continuing capture if `loginctl` is unavailable.

**Privacy / consent:**
- All frames and the SQLite journal live in `~/.local/share/dayflow/`.
- Capture can be paused/resumed from the bar widget, CLI, or automatically when the session is locked.
- Ignored apps are never captured.
- Raw frames are deleted after summarization unless `keep_frames` is enabled.

**License:**
MIT
