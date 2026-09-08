# Dayflow Privacy Notice

Dayflow is a **local-first** automatic work journal. This notice describes what data is collected, how it is used, and the controls available to you.

## What Dayflow collects

- **Screenshots**: a lightweight JPEG frame is captured at the interval you configure (default every 10 seconds) while Dayflow is recording and your session is not paused or locked.
- **Focused window information**: the active window class/app name is recorded alongside each frame so the journal can attribute activity to apps.
- **Journal entries**: the SQLite database in `~/.local/share/dayflow/` stores `{title, summary, category, app, timestamps}` for each summarized block.
- **Audit metadata**: `events`, `api_calls`, and usage logs are stored locally for diagnostics and token accounting.

## What leaves your machine

Only the **sampled frames** sent to your chosen AI provider for summarization leave your machine. By default this is OpenRouter; you may also configure a local endpoint such as Ollama or LM Studio. Dayflow does not send screenshots or journal data anywhere else.

Dayflow does **not** include telemetry, analytics, crash reporting, or cloud synchronization.

## Where your data lives

All local data is stored in `~/.local/share/dayflow/` and configuration in `~/.config/dayflow/`. You can wipe everything at any time:

```sh
dayflow pause
rm -rf ~/.local/share/dayflow ~/.config/dayflow
```

## Controls

- **Pause / resume**: `dayflow pause` or right-click the bar widget. Pausing creates a flag file the daemon checks before every capture.
- **Auto-pause on lock**: `auto_pause_locked` is enabled by default; the daemon pauses while your session is locked.
- **Ignore apps**: add window classes to `ignore_apps` so their frames are never captured.
- **Retention**: `retention_days` and `max_storage_mb` prune old frames, events, and API logs automatically.
- **Scrub**: `dayflow scrub <query>` deletes existing blocks matching a title or summary.
- **No idle tracking**: empty blocks (no frames) are not stored, so idle or screen-off time does not appear in the journal.

## Sensitive content

Dayflow can see whatever is on your screen. If you handle passwords, payment cards, government IDs, or other sensitive information, **pause capture** or **add the app to `ignore_apps`** before viewing it. Dayflow includes a sensitive-content filter, but it is a best-effort aid, not a guarantee. You are responsible for ensuring sensitive information is not captured.

## Changes

This notice may be updated in the repository. The version shipped with your installed release applies unless you pull a newer one.

## License

Dayflow is MIT licensed. This privacy notice is provided for transparency and is not a legal contract or warranty.
