# Omarchy Plugin Marketplace Submission — Dayflow

## Issue Title

[Plugin]: Dayflow

## Repository URL

https://github.com/duketopceo/dayflow-linux

## Category

Productivity

## Tags

productivity, time-tracking, ai

## Suggested missing tag (optional)

work-journal

## Maintainer notes

**What it does:**
Dayflow is a private, automatic work journal for Omarchy / Hyprland. It captures a lightweight screenshot every 10 seconds, deduplicates unchanged frames, and every 15 minutes sends ~30 sampled frames to a vision model to generate a plain-language summary of what you were doing. The result is a readable timeline in the bar panel plus standup updates and focus/distraction analytics.

**Installation:**
```sh
# Build and install the Go engine
cd engine
go build -o dayflow .
install -Dm755 dayflow ~/.local/bin/dayflow
dayflow install
systemctl --user enable --now dayflow-capture.service
dayflow setup   # interactive key + model selection

# Install the Omarchy bar plugin
omarchy plugin add https://github.com/duketopceo/dayflow-linux.git --enable
```

**Removal:**
```sh
omarchy plugin disable io.github.duketopceo.dayflow
omarchy plugin remove io.github.duketopceo.dayflow
dayflow uninstall
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
