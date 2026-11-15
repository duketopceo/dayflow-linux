# dayflow-ui

Standalone Qt6/QML window + tray host for Dayflow (plan U4/U5,
`docs/plans/2026-10-06-001-feat-standalone-desktop-app-plan.md`).

The tab QML at the repo root (Panel, Today, Settings, ...) is embedded
**unmodified** as module `Dayflow.App`. The Omarchy/Quickshell-only things it
imports are shimmed here:

| import | shim |
|---|---|
| `Quickshell.Io` Process / StdioCollector / SplitParser | `process.{h,cpp}` (KTD11 contract; `dayflow` argv0 and `bash -c "dayflow … \| wl-copy"` are routed to the bundled engine / Qt clipboard) |
| `Quickshell` (`iconPath`, `FloatingWindow`) | `quickshellshim.*`, `qml/Quickshell/` |
| `qs.Commons` Color / Style | `theme.h` (C++, see note), `qml/qs/Commons/Style.qml` |
| `qs.Ui` Panel / KeyboardPanel / PanelKeyCatcher / PanelSeparator | `qml/qs/Ui/` |

Note: Qt 6.12 ships a built-in `QtQuick.Color` singleton that shadows a
QML-file `Color` under the tab files' unversioned `import QtQuick`; the shim
`Color` is therefore a C++ singleton.

## Shell

`qml/host/Main.qml` is only the native window (size and last-page persistence
in `~/.config/dayflow-ui/`, tray hooks, close-to-tray). The UI is the shared
root QML:

| File | Role |
|---|---|
| `AppShell.qml` | Rail (Today, Week, Standup, Ask, Agents, Replay, Context, Settings), Ctrl+1..8 and Alt+Left/Right navigation, owns the hidden `Panel` state controller |
| `FullViewBody.qml` | Rail rendering and lazy pane host, shared with the plugin's `FullView.qml` window |
| `EmbeddedTab.qml` | Temporary page for Standup, Ask and Settings: hosts the popup tab components until each gets its own page |
| `ServiceStatus.qml`, `ServiceStrip.qml` | Bottom strip: capture state, last frame age, summarizer, systemd unit health, pause/restart/start actions ([contract](../../docs/service-contract.md)) |

`Panel.fullViewOpen` set to true (the popup's "Full view" action) raises the
window on Today.

## Build and run

    cmake -S app/dayflow-ui -B build -G Ninja
    cmake --build build          # also builds the Go engine to build/dayflow
    build/dayflow-ui             # --background starts hidden in the tray
    ctest --test-dir build       # smoke, every section, install layout

On some Arch/Asahi installs CMake cannot find OpenGL although libOpenGL and
libGLX are installed; configure with
`-DOPENGL_opengl_LIBRARY=/usr/lib/libOpenGL.so -DOPENGL_glx_LIBRARY=/usr/lib/libGLX.so`
(`scripts/install-app.sh` does this automatically).

Engine resolution: `DAYFLOW_ENGINE`, then `dayflow` beside `dayflow-ui`, then
PATH. Dev hooks: `DAYFLOW_UI_TAB=<section>` (`today`, `week`, `standup`, `ask`,
`agents`, `timelapse`, `context`, `settings`), `DAYFLOW_UI_SCREENSHOT=out.png`.

## Install

    scripts/install-app.sh [--prefix DIR] [--autostart] [--dry-run]

Installs `dayflow-ui`, the bundled `dayflow` engine, `dayflow.desktop` (with
Pause/Resume actions), the icon, and an autostart template under
`share/dayflow/autostart/`; `--autostart` copies it to
`~/.config/autostart/`. The capture units are separate: `dayflow install`.
