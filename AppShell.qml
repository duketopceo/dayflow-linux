import QtQuick
import QtQml
import QtQuick.Window
import qs.Commons
import qs.Commons as Commons

// Desktop app shell: section rail + lazy page host (FullViewBody) over a
// service status strip. The hidden Panel is the state controller every page
// already reads (`dayflow`); it is never shown here, only its data is used.
// The bar plugin does not load this file.
Item {
  id: shell

  // Rail order. Keys Today/Week/Agents/Replay/Context are FullViewBody panes;
  // entries with `tab` embed the popup tab components until their own page
  // lands (see EmbeddedTab.qml).
  readonly property var sections: [
    { key: "today",     label: "Today" },
    { key: "week",      label: "Week" },
    { key: "standup",   label: "Standup",  tab: "StandupTab.qml", panelTab: "standup" },
    { key: "ask",       label: "Ask",      tab: "ChatTab.qml",    panelTab: "chat" },
    { key: "agents",    label: "Agents" },
    { key: "timelapse", label: "Replay" },
    { key: "context",   label: "Context" },
    { key: "settings",  label: "Settings", tab: "Settings.qml",   panelTab: "settings" }
  ]

  // The last section the user was on; the host persists it.
  readonly property string section: body.section
  // Section to open first (restored from the previous run or a dev hook).
  property string initialSection: "today"

  // Raised when something asks for the main surface (the popup's
  // "Full view" action, a tray click): the host window shows itself.
  signal raiseRequested()

  function keyAt(i) { return i >= 0 && i < sections.length ? sections[i].key : "" }
  function indexOfKey(key) {
    for (var i = 0; i < sections.length; i++) if (sections[i].key === key) return i
    return -1
  }
  function goTo(key) { if (indexOfKey(key) >= 0) body.selectSection(key) }
  function goRelative(delta) {
    var n = sections.length
    var i = indexOfKey(body.section)
    goTo(sections[((i + delta) % n + n) % n].key)
  }

  Rectangle {
    anchors.fill: parent
    color: Commons.Color.background
  }

  // State controller. Never opened: the popup surface is not part of the app.
  Panel {
    id: panel
    width: 0
    height: 0
    visible: false
    Component.onCompleted: {
      // The window is the expanded layout. Marking it loaded first keeps the
      // status reply (panel_expanded) from flipping it back to the popup size.
      panel.expandedLoaded = true
      panel.expanded = true
      panel.refreshAll()
    }
  }

  ServiceStatus { id: svc }

  Column {
    anchors.fill: parent

    FullViewBody {
      id: body
      width: parent.width
      height: parent.height - strip.height
      dayflow: panel
      sections: shell.sections
      Component.onCompleted: {
        if (shell.indexOfKey(shell.initialSection) >= 0) selectSection(shell.initialSection)
      }
    }

    ServiceStrip {
      id: strip
      width: parent.width
      pal: panel
      svc: svc
    }
  }

  // Popup's "Full view" action (Panel.fullViewOpen) means "show the main
  // surface" here: raise the window and land on Today.
  Connections {
    target: panel
    function onFullViewOpenChanged() {
      if (!panel.fullViewOpen) return
      panel.fullViewOpen = false
      shell.goTo("today")
      shell.raiseRequested()
    }
  }

  // ---- live data ----
  // The window stays open all day (unlike the popup, which refetches on every
  // open), so pages would go stale. Refetch every minute while the window is
  // shown, when it regains focus, and on Ctrl+R. Hidden to the tray: no polling.
  readonly property bool windowShown: Window.window ? Window.window.visible : false
  readonly property bool windowActive: Window.window ? Window.window.active : false
  property double lastRefreshMs: 0

  function refreshData() {
    shell.lastRefreshMs = Date.now()
    panel.refreshAll()
  }

  Timer {
    interval: 60000
    repeat: true
    running: shell.windowShown
    onTriggered: shell.refreshData()
  }

  // Focus-in refresh is throttled so alt-tabbing back and forth stays quiet.
  onWindowActiveChanged: {
    if (windowActive && Date.now() - lastRefreshMs > 15000) refreshData()
  }

  // Day navigation for the day-scoped pages (Today, Week): the pages' own
  // buttons stay; these are the keyboard equivalents.
  readonly property bool daySection: body.section === "today" || body.section === "week"
  function stepDay(delta) { if (daySection) panel.goDay(delta) }
  function jumpToday() {
    if (!daySection || panel.dayOffset === 0) return
    panel.dayOffset = 0
    panel.loadTimeline()
  }

  // Test hook (DAYFLOW_UI_SCRIPT, see app/dayflow-ui/tests/nav.sh): runs the
  // same functions the shortcuts call and returns the resulting state.
  function runStep(step) {
    var p = step.split(":")
    var arg = p.length > 1 ? p[1] : ""
    if (p[0] === "goto") goTo(arg)
    else if (p[0] === "key") goTo(keyAt(parseInt(arg) - 1))     // Ctrl+<arg>
    else if (p[0] === "rel") goRelative(parseInt(arg))          // Alt+Left/Right
    else if (p[0] === "day") stepDay(parseInt(arg))             // Left/Right
    else if (p[0] === "today") jumpToday()                      // T
    else if (p[0] === "refresh") refreshData()                  // Ctrl+R
    return "section=" + body.section + " dayOffset=" + panel.dayOffset
  }

  Shortcut { sequence: "Ctrl+R"; onActivated: shell.refreshData() }
  Shortcut { sequence: "Left";  enabled: shell.daySection; onActivated: shell.stepDay(-1) }
  Shortcut { sequence: "Right"; enabled: shell.daySection; onActivated: shell.stepDay(1) }
  Shortcut { sequence: "T";     enabled: shell.daySection; onActivated: shell.jumpToday() }

  Instantiator {
    model: shell.sections.length
    delegate: Shortcut {
      required property int index
      sequence: "Ctrl+" + (index + 1)
      onActivated: shell.goTo(shell.keyAt(index))
    }
  }
  Shortcut { sequence: "Alt+Left";  onActivated: shell.goRelative(-1) }
  Shortcut { sequence: "Alt+Right"; onActivated: shell.goRelative(1) }
}
