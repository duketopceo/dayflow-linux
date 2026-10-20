# Preview sandbox

Render any Dayflow surface on any Omarchy theme without touching the live
session. Foundation for every Ribbon Log UI unit (KTD5, plan U1).

    scripts/preview/run.sh <surface> <theme> [scale] [--state <S>]

- **surface** — `popup`, `fullview-today`, `week` (maps in `shell.qml`)
- **theme** — name under `/usr/share/omarchy/themes` or `~/.config/omarchy/themes`
- **scale** — display scale, default 2
- **--state** — `recording|paused|locked|down|missing` (shim's canned `status --json`)

Writes `shots/<surface>-<theme>-<scale>.png` (gitignored).

## Safety (why it can't hurt)

- Scratch `HOME` via `mktemp`; theme is staged into it and rendered by
  Omarchy's own `omarchy-theme-set-templates`.
- `OPENROUTER_API_KEY` unset; `omaseal` is a failing stub on PATH.
- `dayflow` is the shim: canned `status`, everything else passes to the
  real binary against the seeded `DAYFLOW_DATA_DIR` scratch dir.
- Hosts extracted body components in a `FloatingWindow`, never the
  layer-shell `KeyboardPanel` — the live bar/panel/theme are untouched.

## Layout

    run.sh            scratch HOME + env + launch + shot
    shell.qml         surface -> Loader in FloatingWindow + grabToImage
    dayflow-shim      canned status --json, passthrough otherwise
    omaseal           failing secrets stub
    shots/            output (gitignored)
