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
curl -fsSL https://github.com/duketopceo/dayflow-linux/releases/latest/download/install.sh | bash
dayflow setup          # pick a provider (OpenRouter key, Ollama/LM Studio, MCP, or a CLI provider)
dayflow doctor         # verifies session, capture backend, key, model, agent stores, bus
dayflow tui            # timeline in the terminal
```

`install.sh` maps `uname -m` to the `amd64`/`arm64` release asset,
downloads it plus the release's `SHA256SUMS`, verifies the checksum
before anything lands in `~/.local/bin`, then runs `dayflow install` for
the systemd user units (capture daemon + summarize/backup/export
timers). It's idempotent — re-running upgrades to the target version and
re-installs units. Options: `--version X.Y.Z` pins a release;
`DAYFLOW_BUILD=local` builds the checked-out repo instead of
downloading.

No Omarchy, no Quickshell, no `hyprctl` required — those paths degrade
gracefully (`output:"auto"` falls back to composite capture, `ignore --active`
reports Hyprland-only).

## Uninstall

```sh
bash scripts/uninstall.sh   # from a checkout — units + ~/.local/bin/dayflow, data kept
# or without a checkout: dayflow uninstall && rm ~/.local/bin/dayflow
```

`~/.local/share/dayflow` (journal + frames) and `~/.config/dayflow`
survive on purpose; delete them by hand if wanted.

## Manual install

```sh
# from a release binary (amd64/arm64) downloaded from a release page:
install -Dm755 dayflow ~/.local/bin/dayflow

# or from source:
cd engine && go build -o dayflow . && install -Dm755 dayflow ~/.local/bin/dayflow

dayflow install        # systemd user units
dayflow setup          # provider
dayflow doctor         # sanity check
```

## Verify

```sh
dayflow status         # capture_state: recording, frame count climbing
dayflow today          # after ~15 min: your first summarized block
dayflow events -n 10   # audit log if anything looks off
```

Notifications use `notify-send`; the units pass `DBUS_SESSION_BUS_ADDRESS`
through, and `doctor` warns if the bus looks unreachable.
