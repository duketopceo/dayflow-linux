# Dayflow for Omarchy / Wayland

A private, automatic work journal for Linux — a port of [Dayflow](https://www.dayflow.so/) (macOS) built for [Omarchy](https://omarchy.org)/Hyprland and other wlroots compositors.

It captures a lightweight screenshot every 10 seconds, deduplicates unchanged frames, and every 15 minutes asks a vision model (Gemini via [OpenRouter](https://openrouter.ai) by default) to write a plain-language summary of what you were actually doing. The result is a readable timeline of your day — shown in a bar panel or from the CLI.

- **Local-first**: frames and the SQLite database live in `~/.local/share/dayflow/`. Nothing leaves your machine except the sampled frames sent for summarization.
- **Cheap**: ~30 JPEG frames per 15-min block → `google/gemini-2.5-flash` by default. Any OpenRouter vision model works.
- **Light**: single static Go binary, ~25MB RAM, sub-1% CPU.
- **Private controls**: pause toggle, per-app ignore list, automatic frame deletion, retention pruning.

## Repository layout

This repo is both the Omarchy plugin and the engine:

```
manifest.json      # Omarchy plugin manifest (repo root, per marketplace rules)
BarWidget.qml      # bar indicator: recording state, click for panel
Panel.qml          # timeline panel + settings footer
engine/            # Go CLI/daemon (the tracking engine)
scripts/stress.sh  # live stress test
```

## Requirements

- Wayland compositor where `grim` works (Hyprland, sway, river, … — any wlroots-based compositor, any GPU vendor). On non-wlroots compositors set `capture_command` to a tool that writes an image to stdout.
- `systemd --user` for the background units
- `hyprctl` (optional) for the app ignore list — Hyprland only
- An OpenRouter API key
- Go to build the engine (prebuilt binaries: see Releases)

## Install the engine

```sh
cd engine
go build -o dayflow .
install -Dm755 dayflow ~/.local/bin/dayflow
dayflow install    # writes + enables systemd user units
systemctl --user enable --now dayflow-capture.service
```

Set your key — any one of:

```sh
dayflow config set openrouter_api_key sk-or-...
# or export OPENROUTER_API_KEY=sk-or-...
# or ~/.config/openrouter/keys.json with an "api_key" field (auto-detected)
```

## Install the plugin

```sh
omarchy plugin add https://github.com/lukedaduke/dayflow-linux.git --enable
```

or for local development:

```sh
cp -r . ~/.config/omarchy/plugins/io.github.lukedaduke.dayflow   # excludes .git
omarchy-shell shell rescanPlugins
omarchy plugin enable io.github.lukedaduke.dayflow
```

Bar widget: recording indicator; left-click opens the timeline panel, right-click pauses/resumes. The panel shows today's blocks, engine stats, the ignore list, and pause / ignore-focused-app / summarize-now controls.

## CLI

```sh
dayflow today                  # today's timeline
dayflow day 2026-09-03         # any day
dayflow status                 # state, counts, model
dayflow summarize --now        # force summarization including the current block
dayflow pause | resume | toggle
dayflow ignore <class>         # never capture while this app is focused
dayflow ignore --active        # ignore the currently focused app (Hyprland)
dayflow unignore <class>
dayflow events -n 20           # full audit log: captures, skips, errors
dayflow usage                  # token totals across all API calls
dayflow blocks                 # failed summaries (auto-retried)
dayflow config set <k> <v>     # live settings
dayflow uninstall              # remove systemd units (data stays)
```

All query commands accept `--json`.

## Config

`~/.config/dayflow/config.json`:

| key | default | notes |
|---|---|---|
| `model` | `google/gemini-2.5-flash` | any OpenRouter vision model |
| `capture_interval_sec` | 10 | frame interval |
| `block_minutes` | 15 | summary granularity |
| `frames_per_block` | 30 | frames sampled per API call |
| `jpeg_quality` | 55 | grim JPEG quality |
| `keep_frames` | false | keep raw frames after summarizing |
| `retention_days` | 7 | prunes frames, events, and api logs |
| `ignore_apps` | `[]` | window classes never captured (Hyprland) |
| `output` | `""` | restrict capture to one monitor (`grim -o`) |
| `capture_command` | `""` | custom screenshot command (writes image to stdout) |
| `openrouter_api_key` | `""` | API key |

## Privacy

- `dayflow pause` (or right-click the bar widget) drops a flag file the daemon checks before every capture.
- Ignored apps are skipped at capture time — their frames are never written to disk.
- All frames are deleted after summarization unless `keep_frames` is on; retention pruning removes anything older than `retention_days`.
- Everything lives in `~/.local/share/dayflow/` — `rm -rf` it to wipe all data.

## Cross-hardware / portability

Capture goes through `grim` → the compositor's screencopy protocol, which is hardware-agnostic (Intel, AMD, NVIDIA, ARM). The Go binary is pure-Go + `modernc.org/sqlite` (no cgo) and builds for `amd64`, `arm64`, etc. AI runs on OpenRouter, so no local GPU/NPU is required. On non-wlroots compositors (KDE, GNOME), set `capture_command` — e.g. `"gnome-screenshot -f /dev/stdout"` or a small wrapper.

## Testing

```sh
cd engine && go test ./...   # unit + end-to-end tests with a stubbed API
scripts/stress.sh            # live stress test against a sandboxed data dir
```

## Not a 1:1 port

No audio capture, no menu-bar app, no onboarding wizard. Just the tracking engine plus a minimal bar widget. MIT licensed, like the original.
