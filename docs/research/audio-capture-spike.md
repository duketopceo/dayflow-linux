# Audio Capture Spike — go/no-go decision doc

- **Date:** 2026-09-29
- **Machine:** omarchy-max (M1 Max, aarch64, 10 cores / 62 GB, Asahi Linux, Hyprland + PipeWire + pipewire-pulse + WirePlumber)
- **Scope:** research spike only (plan U7 / R8 / KTD7). No engine code. Probe sample deleted post-measurement; no audio content quoted.
- **Verdict:** **Conditional go.** Capture is trivially viable — one subprocess, ~12 MB/min raw, zero OS friction. The blocker is not technical; it is that monitor-node capture gives *zero* OS-level consent signal and records third parties by default, so the entire feature cost is the consent architecture. Ship only if the full consent bar below is built; otherwise the feature repeats the #4992 failure shape.

## Probe results (measured)

Default sink resolution: `wpctl inspect @DEFAULT_AUDIO_SINK@` → **node id 71** (`audio_effect.j316-convolver`, `media.class = Audio/Sink` — the EasyEffects convolver that is the session default; the ALSA hardware sinks sit behind it).

Working capture command:

```bash
pw-record --target 71 out.wav          # target = node id, NOT "<name>.monitor"
```

Results from a ~58 s capture while a generated test tone played via `paplay`:

| Measurement | Value |
|---|---|
| Duration | 58.2 s |
| Format negotiated | WAV, pcm_s16le, 48 kHz stereo (1536 kb/s) |
| File size | 11,173,932 bytes → **~11.5 MB/min** (~191 KB/s) |
| Signal check | mean −45.2 dB, max −17.0 dB (tone present; not silence) |
| Portal/consent dialogs | **none** — `journalctl --user` grep for portal/consent/permission over the capture window: empty |
| Visible indicator | none anywhere in the session |
| Exit on SIGINT | code 1 (normal — pw-record reports 1 on signal termination; file is complete and well-formed) |

Fallback path (not needed here, verified present): `pactl` shows `audio_effect.j316-convolver.monitor` as source 76, so `parecord -d audio_effect.j316-convolver.monitor` works identically through pipewire-pulse. Generic form on any pipewire-pulse install: `parecord -d $(pactl get-default-sink).monitor`. The native `pw-record --target <id>` path is preferred — no pulse-compat dependency — and an alternative native form `pw-record -P '{ stream.capture.sink = true }'` records the default sink's monitor without resolving an id.

`wpctl`, `pw-record`, `pw-dump`, `parecord`, `paplay` all on PATH. No conference/call apps were running during the probe (checked `pw-link` outputs + process list first).

## Storage math

| Encoding | Rate | 12-h day |
|---|---|---|
| Raw WAV 48 kHz stereo s16 (as probed) | 11.5 MB/min | ~8.3 GB |
| Mono 16 kHz s16 (speech-sufficient) | 1.9 MB/min | ~1.4 GB |
| Opus ~32 kbps (speech-adequate) | ~0.24 MB/min | ~170 MB |

Raw PCM is fine as a ring buffer of a few hours; anything retained needs encoding (`pw-record … | opusenc` or in-process encode). Capped retention is mandatory either way — see consent bar.

## Transcription: whisper.cpp local vs provider

Measured literature numbers for this hardware class (M1 Pro/Max, CPU NEON — **no Metal/CoreML on Asahi Linux**, so CPU-only):

- M1 Pro, 8 threads (whisper.cpp bench, issue #89): encoder per 30-s window — base 220 ms, small 685 ms, medium ~1.9 s, large ~3.4 s. End-to-end on a 10-min podcast: tiny/base ≈ 10–40× realtime, large ≈ 2×.
- M1 Max (8 P + 2 E vs M1 Pro's 6 P + 2 E) lands at or above those numbers.

Practical read: `small` on CPU ≈ 5–10× realtime → a 12-h day of audio transcribes in ~1.5–2.5 h of background compute, or incrementally during the day at well under one core average. Model footprint: base 141 MB, small 465 MB, medium 1.5 GB. RAM is a non-issue at 62 GB; on smaller installs `small` (~1–2 GB RSS) is the sensible default.

Provider transcription would mean **continuous audio egress** — the heaviest data class this project has ever touched (today's worst egress is a bounded, scrubbed transcript *excerpt*). Local-first transcription with whisper.cpp is clearly the right default; provider egress, if ever offered, must be a separate explicit opt-in naming the endpoint, per R8.

## Consent surface findings

- **No portal involvement.** xdg-desktop-portal consent applies to ScreenCast/screen sharing; plain PipeWire audio streams — including sink *monitor* capture — are not mediated by it. Confirmed empirically: zero portal/consent activity in the user journal during the capture.
- **No indicator.** Nothing in Wayland/Hyprland renders a "recording" affordance for monitor capture. The OS gives the operator (and anyone else on the call) zero signal. Whatever indicator exists must be built by us.
- **Third-party speech is the load-bearing difference.** A sink monitor records *what the machine plays back* — every remote participant on a call, notification TTS, media audio. That is categorically heavier than the agent-recap excerpts that already caused the #4992 marketplace finding, and it captures people who never consented to anything. WirePlumber metadata can identify which app a stream belongs to (`media.name`/`application.name` on linked streams), which is what makes per-app exclusion feasible at capture time rather than as post-hoc scrubbing.
- **pipewire-pulse vs native:** no capability difference for this use — `parecord -d <sink>.monitor` and `pw-record --target <id>` resolve to the same monitor ports. Native avoids a pipewire-pulse dependency on systems that run bare PipeWire, so the daemon should use `pw-record`/libpipewire and treat parecord as fallback.

## Required consent bar (the shippable checklist)

Non-negotiable, all of it, or no-go:

1. **Default-off flag** — e.g. `audio_capture: false`; absent/disabled means no pw-record child is ever spawned. Upgrade must never silently enable it.
2. **Persistent recording indicator** — a visible, always-on-while-recording affordance (bar widget state + something not dismissible), because the OS shows nothing. Off-label for "paused": indicator must reflect actual capture state, not config state.
3. **Per-app exclusion** — stream metadata inspection (`application.name`, `media.name`) with an exclusion list; a bundled default set covering major call apps (zoom/meet/teams/discord/slack) is strongly advised, and exclusion must apply *before* samples hit disk.
4. **Local, capped retention** — encoded ring buffer with hard byte cap + time cap, wired into the existing `retention_days`/`max_storage_mb`/`scrub` controls so `dayflow scrub` and pruning cover audio-derived artifacts too.
5. **Local-by-default transcription** — whisper.cpp (or equivalent on-device STT); provider egress of any audio or audio-derived transcript is a *separate* opt-in naming the endpoint, never implied by enabling capture.
6. **Third-party-speech treatment** — documented plainly: sink-monitor capture records remote call participants who never consented; the exclusion defaults and indicator exist precisely because of this. Call-audio exclusion by default, not by request.
7. **PRIVACY.md row** — new entries under "What Dayflow collects" and "Controls" (and "What leaves your machine" if provider transcription exists) before merge, not after. This is the #4992 lesson applied forward.

## What a real implementation plan would scope (if go)

- **Capture unit:** daemon child process — `pw-record --target <resolved-id> --rate 16000 --channels 1 -` piped to an encoder (opus) or buffered WAV chunks; target re-resolved on sink change (dock/headphone transitions change node ids); bounded like every other tick-path exec (KTD2 applies — `CommandContext`, off the select loop).
- **Chunking:** N-minute segments (e.g. 5 min → ~9.6 MB mono PCM / ~1.2 MB opus) so transcription is incremental and a crash loses ≤ one segment.
- **Store:** `audio_segments` table `{ts_start, ts_end, path, app_tags, transcribed}`; transcripts into a table that `blocks_fts`/search can reach; segment files under the data dir subject to the same retention/prune path as frames.
- **Consent plumbing:** config keys `audio_capture` (bool, default false), `audio_exclude_apps` (list), `audio_provider_egress` (separate bool); onboarding step + Settings toggle row mirroring the recaps pattern (R5 precedent — same gate shape, heavier copy).
- **Indicator:** bar widget recording state + e.g. a persistent `notify-send`-class signal or widget badge; must track *actual* pw-record liveness.
- **Transcription worker:** whisper.cpp CLI/whisper-cpp bindings or a bundled binary; `small` model (~465 MB download) as default; runs off-tick, drains a queue.
- **Effort estimate:** this is a multi-unit plan of its own — roughly comparable to the capture+store+UI surface of the original recorder plus the consent surfaces. Do not fold it into a parity PR.

## Bottom line

Technically: **go** — proven working on this machine with a one-line command, ~12 MB/min worst-case raw, ~170 MB/day encoded, and local transcription comfortably inside CPU budget. Product-wise: **conditional** — it ships only behind the seven-item consent bar above, with per-app call exclusion and a PRIVACY.md update as merge blockers. Without those, it is an always-on recorder of third parties with no OS signal, which is exactly the consent class that blocked the marketplace listing.
