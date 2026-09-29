# Dayflow engine on any Linux

The Omarchy plugin is a panel skin over the engine — the engine itself is a
standalone Go binary with a CLI, TUI (`dayflow tui`), and MCP server. It runs
anywhere `grim` (or a substitute `capture_command`) and `systemd --user` exist.

## Capture backends

| Compositor | Works? |
|---|---|
| Hyprland, sway, river, labwc, other wlroots | `grim` out of the box; `output: "auto"` and `ignore --active` need `hyprctl` (Hyprland only) |
| GNOME/KDE Wayland, X11 | set `capture_command` to a tool that writes an image to stdout (e.g. `gnome-screenshot -f /dev/stdout`, `spectacle -bno /dev/stdout`, `import -window root png:-` on X11). Whitespace-separated argv, no quoting — wrap complex commands in a script |
| Headless / no session | daemon idles cleanly and resumes when a session appears |

## Install

```sh
# from a release binary (amd64/arm64):
install -Dm755 dayflow ~/.local/bin/dayflow

# or from source:
cd engine && go build -o dayflow . && install -Dm755 dayflow ~/.local/bin/dayflow

dayflow install        # systemd user units: capture daemon, summarize/backup/export timers
dayflow setup          # pick a provider (OpenRouter key, Ollama/LM Studio, MCP, or a CLI provider)
dayflow doctor         # verifies session, capture backend, key, model, agent stores, bus
dayflow tui            # timeline in the terminal
```

No Omarchy, no Quickshell, no `hyprctl` required — those paths degrade
gracefully (`output:"auto"` falls back to composite capture, `ignore --active`
reports Hyprland-only).

## Verify

```sh
dayflow status         # capture_state: recording, frame count climbing
dayflow today          # after ~15 min: your first summarized block
dayflow events -n 10   # audit log if anything looks off
```

Notifications use `notify-send`; the units pass `DBUS_SESSION_BUS_ADDRESS`
through, and `doctor` warns if the bus looks unreachable.
