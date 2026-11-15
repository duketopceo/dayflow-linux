import QtQuick
import QtQml
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
