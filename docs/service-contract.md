# Service contract

Dayflow is three parts that share one background service:

| Part | Role | Talks to the service through |
|---|---|---|
| Background service | Captures frames, summarizes blocks, backs up, exports. Headless. | systemd user units around the `dayflow` engine binary |
| Desktop app (`dayflow-ui`) | The main experience: every page of the product. | the CLI commands below, plus `systemctl --user` for unit health |
| Omarchy bar plugin | Assists: status icon, pause/resume, opens the app. | the same CLI commands |

There is no daemon API, socket or IPC layer. The interface is the engine's
CLI and its on-disk state, and every UI part is a client of it. This file is
the list of what clients may rely on. Changing or removing anything listed
here is a breaking change for the app and the plugin; add keys, never repurpose
them. `engine/status_contract_test.go` pins the `status --json` keys.

## State on disk

| Path | Meaning |
|---|---|
| `~/.local/share/dayflow/dayflow.db` | SQLite journal (frames, blocks, events, chat). Override: `DAYFLOW_DATA_DIR`. |
| `~/.config/dayflow/config.json` | Config; the daemon hot-reloads on mtime change. Override: `DAYFLOW_CONFIG`. |
| `<data dir>/PAUSED` | Pause flag. Present means capture is paused. Written by `pause`, `resume`, `toggle`. |

UI-only state (window size, last page) is not part of the contract and never
goes in `config.json`; the app keeps it in `~/.config/dayflow-ui/`.

## Status: `dayflow status --json`

Exit 0 with one JSON object on stdout. Non-zero means the engine could not
open its database; clients treat that as "engine not answering".

| Key | Type | Meaning |
|---|---|---|
| `capture_state` | string | `recording`, `paused`, `locked` or `down`. `down` means no capture heartbeat for more than 60 s (or 3 capture intervals). |
| `paused` | bool | The pause flag is set. |
| `last_frame` | string | Local time of the newest frame, `3:04 PM`; empty when there is none. For display only. |
| `last_frame_ts` | number | Epoch seconds of the newest frame; `0` when there is none. Use this for age. |
| `frames_today` | number | Frames captured today. |
| `blocks_done`, `blocks_pending`, `blocks_total` | number | Summarized blocks today, blocks waiting for a summary, all summarized blocks. |
| `configured` | bool | A vision provider is usable. |
| `model` | string | Configured vision model. |
| `active_app`, `ignored_apps` | string, array | Focused window class and the ignore list. |
| `storage_bytes`, `storage_text` | number, string | Journal size. |
| `panel_expanded` | bool | Legacy popup layout flag. |
| `version` | string | Engine version. |

## Actions

| Command | Effect | Output |
|---|---|---|
| `dayflow pause` | Set the pause flag. | `paused` |
| `dayflow resume` | Clear the pause flag. | `resumed` |
| `dayflow toggle` | Flip the pause flag. | `paused` or `resumed` |
| `dayflow summarize` | Summarize pending blocks now (the timer does this every 15 minutes). | progress on stderr |
| `dayflow doctor [--json] [--deep]` | Health checks: capture backend, config, provider, schema, integrity. | checks list; exit non-zero when a check fails |

Pause and resume take effect on the daemon's next tick; clients re-read
`status --json` afterwards rather than assuming the state.

## Unit health: systemd user units

`dayflow install` writes and enables these units; `systemctl --user` is the
authority on whether they run. Clients read them with:

    systemctl --user show dayflow-capture.service dayflow-summarize.service \
      dayflow-summarize.timer dayflow-backup.timer dayflow-export.timer \
      -p Id -p ActiveState -p SubState -p UnitFileState

One property block per unit, blank-line separated, property order not fixed.

| Unit | Healthy state | Notes |
|---|---|---|
| `dayflow-capture.service` | `active (running)` | The capture daemon. Restart: `systemctl --user restart dayflow-capture.service`. |
| `dayflow-summarize.timer` | `active (waiting)` | Fires `dayflow-summarize.service` every 15 minutes. The service is `active` only while a run is in progress. |
| `dayflow-backup.timer` | `active (waiting)` | Daily snapshot. |
| `dayflow-export.timer` | `active (waiting)` | Daily markdown export. |

Rollup used by the app's status strip (`ServiceStatus.qml`):

| Health | Condition |
|---|---|
| `ok` | engine answers, capture is not `down`, every watched unit is `active` |
| `degraded` | a watched unit is not `active`, or `capture_state` is `down` |
| `unmanaged` | `systemctl --user` cannot be queried (no user manager, sandbox); capture may still run under the app |
| `unknown` | the engine did not answer |

Start a stopped unit with `systemctl --user start <unit>`. Clients never
enable, disable or edit units; that stays with `dayflow install` and
`dayflow uninstall`.

## Compatibility

- The app bundles its own engine (`dayflow` beside `dayflow-ui`) and prefers it
  over `PATH` (`DAYFLOW_ENGINE` overrides), so app and engine versions match.
- The plugin pins its engine through `manifest.json`; a mismatch shows the
  existing version-skew notice.
- Older engines lack `last_frame_ts`; clients treat a missing key as `0`.
