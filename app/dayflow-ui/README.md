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

## Build and run

    cmake -S app/dayflow-ui -B build -G Ninja
    cmake --build build          # also builds the Go engine to build/dayflow
    build/dayflow-ui             # --background starts hidden in the tray
    ctest --test-dir build       # offscreen smoke

Engine resolution: `DAYFLOW_ENGINE`, then `dayflow` beside `dayflow-ui`, then
PATH. Dev hooks: `DAYFLOW_UI_TAB=settings`, `DAYFLOW_UI_SCREENSHOT=out.png`.
