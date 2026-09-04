# dayflow-linux

A private, automatic work journal for Wayland — a Linux port of [Dayflow](https://www.dayflow.so/) (macOS), built for [Omarchy](https://omarchy.org)/Hyprland.

It captures a lightweight screenshot every 10 seconds, deduplicates unchanged frames, and every 15 minutes asks a vision model (Gemini via OpenRouter) to write a plain-language summary of what you were actually doing. The result is a readable timeline of your day.

- **Local-first**: frames and the SQLite database live in `~/.local/share/dayflow/`. Nothing leaves your machine except the sampled frames sent to OpenRouter for summarization.
- **Cheap**: ~30 JPEG frames per 15-min block → `google/gemini-2.5-flash` by default (any OpenRouter vision model works).
- **Light**: single static Go binary, ~25MB RAM, sub-1% CPU. No Electron, no runtime deps beyond `grim`.

## Requirements

- Wayland compositor with `grim` (wlroots/Hyprland; installed by default on Omarchy)
- `systemd --user` for the background units
- An [OpenRouter](https://openrouter.ai) API key
- Go 1.24+ to build

## Install

```sh
git clone https://github.com/lukedaduke/dayflow-linux.git
cd dayflow-linux
go build -o dayflow .
install -Dm755 dayflow ~/.local/bin/dayflow
dayflow install    # writes + enables systemd user units
systemctl --user enable --now dayflow-capture.service
```

Set your key (one of):

```sh
# option A: config file
$EDITOR ~/.config/dayflow/config.json   # "openrouter_api_key": "sk-or-..."

# option B: env (e.g. in ~/.config/environment.d/ or your shell profile)
export OPENROUTER_API_KEY=sk-or-...

# option C: if you have ~/.config/openrouter/keys.json with an "api_key" field, it just works
```

## Usage

```sh
dayflow today              # today's timeline
dayflow day 2026-09-03     # any day
dayflow status             # recording state + counts
dayflow summarize --now    # force-summarize including the current block
dayflow pause / resume / toggle
dayflow blocks             # failed summaries (retried automatically)
dayflow uninstall          # remove systemd units (data stays)
```

All query commands accept `--json` for scripting and the Omarchy plugin.

## Omarchy bar plugin

The `plugin/` folder is an Omarchy shell plugin (bar-widget + panel). It shows a recording indicator in the bar; click it for a scrollable timeline of today, right-click to pause/resume.

```sh
# local dev install
cp -r plugin ~/.config/omarchy/plugins/io.github.lukedaduke.dayflow
omarchy-shell shell rescanPlugins
omarchy plugin enable io.github.lukedaduke.dayflow
```

The plugin requires the `dayflow` binary on `PATH`.

## How it works

- **Capture**: `dayflow daemon` runs `grim -t jpeg -q 55 -` every 10s, computes a 16×16 average hash, and skips frames within a small Hamming distance of the previous one (idle screens cost nothing).
- **Summarize**: a systemd timer runs `dayflow summarize` every 15 min. For each completed wall-clock block it samples up to 30 frames, sends them to OpenRouter, and stores `{title, summary, category}` in SQLite. Processed frames are deleted unless `keep_frames` is on.
- **Privacy**: `dayflow pause` drops a flag file the daemon checks before every capture. All data under `~/.local/share/dayflow/` — delete it whenever.

## Config

`~/.config/dayflow/config.json` (created by `dayflow install`):

| key | default | notes |
|---|---|---|
| `model` | `google/gemini-2.5-flash` | any OpenRouter vision model |
| `capture_interval_sec` | 10 | frame interval |
| `block_minutes` | 15 | summary granularity |
| `frames_per_block` | 30 | frames sampled per API call |
| `jpeg_quality` | 55 | grim JPEG quality |
| `keep_frames` | false | keep raw frames after summarizing |
| `retention_days` | 7 | (reserved) |

## Not a 1:1 port

No audio capture, no menu-bar app, no onboarding UI. Just the tracking engine plus a minimal bar widget. MIT licensed, like the original.
