import QtQuick
import Quickshell

// Offscreen layout verification for the classic compact popup. Renders the
// real Panel.qml (popup shell incl. footer), each tab's surface, Settings,
// Onboarding, and the shared components against inert qs.Commons/qs.Ui stubs
// and a fail-closed `dayflow` binary on PATH. Geometry is the evidence:
// out-of-bounds items, clipped lines, and error states fail the run.
ShellRoot {
  id: runner
  property int scenario: -1
  property bool capturing: false
  property string outputPath: OUTPUT_PATH
  property var cases: []
  property int failures: 0

  ListModel { id: cats }

  // dayflow-shaped stub for surfaces rendered without the real panel root.
  // Their own Processes still hit the stub `dayflow` binary on PATH.
  QtObject {
    id: model
    property color foreground: "#e8e6e3"
    property color dim: "#8b8f95"
    property color accent: "#6b8cae"
    property string fontFamily: "monospace"
    property bool configLoaded: true
    property bool configured: true
    property var config: ({ knowledge_sync: false })
    property var configDraft: ({ provider: "local", model: "gemma3:4b",
      api_base_url: "http://localhost:11434/v1",
      classification_prompt: "Classify productive work honestly." })
    property string storageText: "32 MB"
    property string notice: ""
    property string noticeTone: "neutral"
    property string errorText: ""
    property bool engineInstalling: false
    property string installErr: ""
    property string installLog: ""
    property string installScriptPath: "/nonexistent-dayflow-install.sh"
    property bool expanded: false
    property var settingsCatModel: cats
    property var spans: [
      { start_ts: 1760000000, end_ts: 1760010800, start: "8:00 AM", end: "10:30 AM",
        title: "dayflow polish work", summary: "working on the popup", category: "coding",
        productive: true, appName: "Ghostty", minutes: 150, blocks: 4 },
      { start_ts: 1760014400, end_ts: 1760018000, start: "10:30 AM", end: "11:30 AM",
        title: "standup + team review", summary: "daily sync", category: "communication",
        productive: true, appName: "Slack", minutes: 60, blocks: 2 },
      { start_ts: 1760021600, end_ts: 1760023400, start: "12:15 PM", end: "12:45 PM",
        title: "idle", summary: "", category: "idle",
        productive: false, appName: "", minutes: 30, blocks: 1 },
      { start_ts: 1760028800, end_ts: 1760032400, start: "1:20 PM", end: "2:20 PM",
        title: "hacker news rabbit hole", summary: "distraction", category: "browsing",
        productive: false, appName: "Firefox", minutes: 60, blocks: 2 }
    ]
    property string dateLabel: "Fri 10-09"
    property bool timelineLoading: false
    property var standup: ({ yesterday: { date: "", total_minutes: 0, entries: [] },
      today: { date: "", total_minutes: 0, entries: [] } })
    property var dayGoal: ({ date: "", goal: "", completed: false })
    property var draft: ({ date: "", highlights: "", tasks: "", blockers: "", priorities: "" })
    property bool draftDirty: false
    property var insights: ({ total_minutes: 0, focus_minutes: 0, distraction_minutes: 0,
      idle_minutes: 0, categories: [], apps: [], top_distractions: [], focus_blocks: [], days: 0 })
    property var weekBlocks: [{ start_ts: 1760000000, end_ts: 1760010800, category: "dev" }]
    // Multi-app children exercise the card's secondary-icon + child-seam
    // paths; app/app_name/activities feed the icon rows in SpanCard.
    property var weekCards: [{
      title: "dayflow polish", category: "dev", app: "ghostty", app_name: "Ghostty",
      productive: true, summary: "popup work", minutes: 150, blocks: 4,
      start: "8:00 AM", end: "10:30 AM",
      children: [
        { start_ts: 1760000000, end_ts: 1760005400, start: "8:00 AM", end: "9:30 AM",
          app: "ghostty", title: "polishing the popup", summary: "", category: "dev",
          activities: [{ app: "ghostty", title: "editing qml" }] },
        { start_ts: 1760005400, end_ts: 1760010800, start: "9:30 AM", end: "10:30 AM",
          app: "firefox", title: "checking docs", summary: "", category: "dev",
          activities: [{ app: "firefox", title: "qt docs" }] }
      ]
    }, {
      title: "inbox + slack", category: "communication", app: "slack", app_name: "Slack",
      productive: true, summary: "comms", minutes: 45, blocks: 2,
      start: "11:00 AM", end: "11:45 AM",
      children: [
        { start_ts: 1760026800, end_ts: 1760029500, start: "11:00 AM", end: "11:45 AM",
          app: "slack", title: "team sync", summary: "", category: "communication",
          activities: [{ app: "slack", title: "standup thread" }] }
      ]
    }]
    property var dayFrames: []
    property double pendingSpanTs: 0
    property string weekStart: "Mon 10-05"
    property string weekEnd: "Sun 10-11"
    property string weekSummary: ""
    property bool weekSummaryLoading: false
    property var weeklyPayload: ({ total_minutes: 1200, focus_minutes: 800,
      distraction_minutes: 120, idle_minutes: 60, context_shift_count: 14,
      category_donut: [{ name: "dev", display: "Development", minutes: 700, color: "" }],
      app_treemap: [{ name: "ghostty", display: "Ghostty", minutes: 500 }],
      top_distractions: [], focus_blocks: [], highlights: [], suggestions: [], heatmap: [] })
    property int dayOffset: 0
    // Settings write-path + group state surface.
    property bool keyringOpenrouter: false
    property var keySetQueue: []
    property var pendingPatches: []
    property var patchFailures: []
    property var settingsOpenGroups: ({ "provider": true })

    function fgFill(a) { return Qt.rgba(foreground.r, foreground.g, foreground.b, a) }
    function accentFill(a) { return Qt.rgba(accent.r, accent.g, accent.b, a) }
    function btnBg(hot) { return hot ? accentFill(0.12) : "transparent" }
    function uilog(m) {}
    function saveConfig() { notice = "settings saved" }
    function loadConfig() {}
    function loadTimeline() {}
    function refreshForTab(tab) {}
    function saveDraft() {}
    function saveBlockEdits() {}
    function requestEngineInstall() {}
    function goDay(d) { dayOffset += d }
    function viewDateStr() { return "2026-10-09" }
    function viewDateLabel() { return "Friday, October 9" }
    function dayName(i) { return ["Mon","Tue","Wed","Thu","Fri","Sat","Sun"][i] || "" }
    function todayIndex() { return 4 }
    function fmtDur(m) { return m + "m" }
    function fmtHours(m) { return (Number(m) / 60).toFixed(1) }
    function appDisplayName(a) { return a }
    function appIcon(a) { return "" }
    function catDisplay(c) { return c }
    function categoryColor(c) { return accent }
    function categoryForHour(h) { return "" }
    function cellColor(c) { return accentFill(0.4) }
    function payloadColor() { return accent }
    function payloadFill() { return accentFill(0.2) }
    function pillBgColor() { return fgFill(0.08) }
    function procByName(n) { return null }
    function weekDayMinutes() { return [] }
    function weekDayOrder() { return [] }
    function weekDaySpans(d) {
      return d === 4 ? weekCards.map(function(c) {
        var s = Object.assign({}, c)
        s.start_ts = c.children[0].start_ts
        s.end_ts = c.children[0].end_ts
        return s
      }) : []
    }
    function weekDayNames() { return [] }
    function queuePatch(payload, label) { pendingPatches = pendingPatches.concat([{ payload: payload, label: label }]) }
    function configDiff(draft, base) {
      var patch = {}
      var src = draft || {}
      var ref = base || {}
      for (var k in src) { if (JSON.stringify(src[k]) !== JSON.stringify(ref[k])) patch[k] = src[k] }
      return patch
    }
    function settingsGroupToggle(id) {
      var m = Object.assign({}, settingsOpenGroups)
      m[id] = !(m[id] === true)
      settingsOpenGroups = m
    }
  }

  // FullView host stand-in — the panes take `host` and read dayflow,
  // compactLayout, forecast, and the timelapse surface through it.
  QtObject {
    id: fvStub
    property var dayflow: model
    property bool compactLayout: false
    property var forecast: null
    property var tlFrames: []
    property bool tlPlaying: false
    property int tlIndex: 0
    property bool tlLoading: false
    property string tlError: ""
    property bool tlPlaybackOn: false
    function tlLoad() {}
    function tlSeek(i) {}
    function enablePlayback() {}
  }

  // Bar stand-in for the real panel root — it reads bar.foreground /
  // bar.fontFamily / bar.switchPanelFrom.
  QtObject {
    id: barStub
    property color foreground: model.foreground
    property color barForeground: model.foreground
    property string fontFamily: model.fontFamily
    property string position: "top"
    function switchPanelFrom(w, d) { return false }
  }

  FloatingWindow {
    id: window
    implicitWidth: 1280
    implicitHeight: 800
    color: "#1c2128"
    title: "Dayflow classic popup — isolated layout verification"

    Rectangle {
      id: stage
      color: window.color
      x: 40; y: 40
      width: window.width - 80
      height: window.height - 80

      // The real popup — root type is Panel (stub qs.Ui base), a 0-size
      // controller Item; its KeyboardPanel child renders at contentWidth.
      // Anchored top-left so the child lands inside the stage.
      Panel {
        id: popup
        visible: runner.kindIs("panel")
        bar: barStub
        anchors.left: parent.left
        anchors.top: parent.top
      }

      Item {
        id: settingsHost
        anchors.fill: parent
        visible: runner.kindIs("settings")
        property var panel: model
        Settings { anchors.fill: parent }
      }

      Item {
        id: obHost
        width: Math.min(stage.width, 620)
        height: stage.height
        visible: runner.kindIs("onboarding")
        property var panel: model
        Onboarding { anchors.fill: parent }
      }

      // FullView panes rendered standalone against the host stub — the
      // week hero grid and the day-detail pane exercise their real code.
      Item {
        id: fvHost
        anchors.fill: parent
        visible: runner.kindIs("fullview")
        Loader {
          anchors.fill: parent
          source: {
            var t = runner.scenario >= 0 && runner.scenario < runner.cases.length
              ? runner.cases[runner.scenario] : null
            if (!t || t.kind !== "fullview") return ""
            // fit.qml runs as root/shell.qml — repo QML is symlinked flat
            // beside it, so pane paths are bare filenames.
            return t.fvSection === "today" ? "TodayPane.qml" : "WeekPane.qml"
          }
          onLoaded: item.host = fvStub
        }
      }

      // Shared-component spot renders.
      Column {
        id: componentsHost
        visible: runner.kindIs("components")
        spacing: 8
        PanelCard { id: cardProbe; pal: model; width: 200; height: 60 }
        PanelCard { id: wellProbe; pal: model; well: true; width: 200; height: 60 }
        PanelButton { id: btnProbe; pal: model; text: "Enabled" }
        PanelButton { id: btnDisProbe; pal: model; text: "Disabled"; enabled: false }
        PanelButton { id: btnUnselProbe; pal: model; text: "Tab"; frame: "selected" }
        PanelButton { id: btnSelProbe; pal: model; text: "Selected"; selected: true; frame: "selected" }
        PanelChip { id: chipProbe; pal: model; text: "dev" }
        StatRow { id: statProbe; pal: model; label: "Storage"; value: "32 MB"; width: 200 }
      }
    }
  }

  function kindIs(k) {
    return runner.scenario >= 0 && runner.scenario < runner.cases.length &&
      runner.cases[runner.scenario].kind === k
  }

  function fail(message) { failures++; console.error("FIT_FAIL " + message) }

  function findByName(item, name) {
    if (item.objectName === name) return item
    if (!item.children) return null
    for (var i = 0; i < item.children.length; i++) {
      var hit = findByName(item.children[i], name)
      if (hit) return hit
    }
    return null
  }

  function checkTree(item, label, clipped) {
    if (!item || !item.visible || item.opacity === 0) return
    if (item.clip === true) clipped = true
    var point = item.mapToItem(stage, 0, 0)
    if (!clipped && (point.x < -1 || point.y < -1 || point.x + item.width > stage.width + 1 || point.y + item.height > stage.height + 1))
      fail(label + " bounds: " + item + " at " + point.x + "," + point.y + " size " + item.width + "x" + item.height)
    if (item.font !== undefined && item.font.pixelSize > 0 && item.font.pixelSize < 9)
      fail(label + " unreadable font: " + item.font.pixelSize)
    if (item.children) for (var i = 0; i < item.children.length; i++) checkTree(item.children[i], label, clipped)
  }

  // Component literal assertions — the whole point of the shared set is
  // that these values come from one place.
  function checkComponents() {
    if (String(wellProbe.border.color) === String(cardProbe.border.color))
      fail("PanelCard well border should differ from card border")
    if (btnDisProbe.opacity !== 0.45) fail("disabled PanelButton opacity " + btnDisProbe.opacity)
    if (String(btnSelProbe.border.color) === String(btnUnselProbe.border.color)) fail("selected PanelButton border should differ")
    if (btnProbe.implicitHeight <= 0 || chipProbe.implicitHeight <= 0 || statProbe.implicitHeight <= 0)
      fail("component implicit size collapsed")
  }

  // Footer contract: exactly one permanent row — status + ⋯ when closed,
  // action chips + ⋯ when open; never both, never a second row.
  function checkFooter(test) {
    var row = findByName(popup, "footerRow")
    if (!row) { fail("footerRow not found"); return }
    if (row.height > 30) fail("footer row too tall: " + row.height)
    if (!test.actionsOpen) {
      var vis = 0
      for (var i = 0; i < row.children.length; i++) if (row.children[i].visible) vis++
      if (vis > 2) fail("footer row shows " + vis + " children")
    }
  }

  function targetFor(test) {
    if (test.kind === "panel") return popup
    if (test.kind === "settings") return settingsHost
    if (test.kind === "onboarding") return obHost
    if (test.kind === "components") return componentsHost
    if (test.kind === "fullview") return fvHost
    return stage
  }

  function applyCase(test) {
    window.implicitWidth = test.width
    window.implicitHeight = test.height
    stage.width = test.width - 80
    stage.height = test.height - 80
    if (test.kind === "panel" && test.tab) popup.currentTab = test.tab
    if (test.kind === "panel") model.expanded = test.expanded === true
    if (test.kind === "panel" && test.standup !== undefined) model.standup = test.standup
    if (test.kind === "panel" && test.actionsOpen !== undefined) popup.actionsOpen = test.actionsOpen
    if (test.kind === "panel" && test.notice !== undefined) { popup.notice = test.notice; popup.noticeAt = Date.now() }
    if (test.kind === "onboarding") onboardingHostApply(test)
    if (test.kind === "fullview") fvStub.compactLayout = test.compact === true
  }

  function onboardingHostApply(test) {
    var ob = obHost.children[0]
    if (!ob) return
    ob.detected = { ollama: true, ollama_models: ["gemma3:4b"], lmstudio: true,
      lmstudio_models: [], agents: { codex: true },
      presets: [{ id: "local", name: "Local", slug: "local/gemma3:4b", model: "gemma3:4b" }] }
    if (test.step !== undefined) ob.step = test.step
  }

  Timer {
    interval: 250
    running: true
    repeat: true
    onTriggered: {
      if (runner.capturing) return
      if (runner.scenario >= 0 && runner.scenario < runner.cases.length) {
        var test = runner.cases[runner.scenario]
        var target = targetFor(test)
        runner.checkTree(target, test.name)
        if (test.kind === "components") runner.checkComponents()
        if (test.kind === "panel") runner.checkFooter(test)
        console.log("FIT_RESULT " + JSON.stringify({ name: test.name, width: test.width, height: test.height, failures: runner.failures }))
        runner.capturing = true
        stage.grabToImage(function(result) {
          result.saveToFile(runner.outputPath + "/" + test.name + ".png")
          runner.capturing = false
          runner.nextCase()
        })
      } else runner.nextCase()
    }
  }

  function nextCase() {
    scenario++
    if (scenario >= cases.length) { console.log("FIT_COMPLETE failures=" + failures); Qt.quit(); return }
    applyCase(cases[scenario])
  }

  Component.onCompleted: {
    for (var i = 0; i < 40; i++) cats.append({ name: "Category " + i, description: "desc", color: "" })
    var list = []
    // Popup shell: compact + expanded widths, every tab, footer states.
    for (var w of [540, 780]) {
      for (var tab of ["today", "standup", "chat", "agents", "settings"])
        list.push({ name: "panel-" + w + "-" + tab, kind: "panel", width: w + 80, height: 800, tab: tab })
      list.push({ name: "panel-" + w + "-actions-open", kind: "panel", width: w + 80, height: 800, tab: "today", actionsOpen: true })
      if (w === 780) {
        list.push({ name: "panel-780-expanded-today", kind: "panel", width: w + 80, height: 800, tab: "today", expanded: true })
        list.push({ name: "panel-780-expanded-chat", kind: "panel", width: w + 80, height: 800, tab: "chat", expanded: true })
        list.push({ name: "panel-780-expanded-standup", kind: "panel", width: w + 80, height: 800, tab: "standup", expanded: true })
        list.push({ name: "panel-780-expanded-agents", kind: "panel", width: w + 80, height: 800, tab: "agents", expanded: true })
        // Recap with a populated today — exercises the entry rows + the
        // Copy recap action (no Blockers/draft surfaces must render).
        list.push({ name: "panel-780-standup-entries", kind: "panel", width: w + 80, height: 800, tab: "standup",
          standup: { yesterday: { date: "", total_minutes: 0, entries: [] },
            today: { date: "Wed 10-14", total_minutes: 210, entries: [
              { title: "dayflow polish work", app: "Ghostty", category: "coding",
                productive: true, minutes: 150, span: "8:00–10:30" },
              { title: "standup + team review", app: "Slack", category: "communication",
                productive: true, minutes: 60, span: "10:30–11:30" }
            ] } } })
      }
      list.push({ name: "panel-" + w + "-notice", kind: "panel", width: w + 80, height: 800, tab: "today", notice: "settings saved" })
    }
    // Settings + onboarding at three viewport sizes, dark theme.
    for (var size of [{ width: 1280, height: 800 }, { width: 1280, height: 720 }, { width: 620, height: 800 }]) {
      list.push({ name: "settings-" + size.width + "x" + size.height, kind: "settings", width: size.width, height: size.height })
      for (var step = 0; step < 5; step++)
        list.push({ name: "onboarding-" + size.width + "x" + size.height + "-" + step, kind: "onboarding", width: size.width, height: size.height, step: step })
    }
    list.push({ name: "components", kind: "components", width: 620, height: 800 })
    // FullView panes: the week hero grid at full + compact widths, and the
    // day-detail pane parity check.
    list.push({ name: "fullview-week", kind: "fullview", width: 1280, height: 800, fvSection: "week" })
    list.push({ name: "fullview-week-compact", kind: "fullview", width: 900, height: 620, fvSection: "week", compact: true })
    list.push({ name: "fullview-today", kind: "fullview", width: 1280, height: 800, fvSection: "today" })
    cases = list
  }
}
