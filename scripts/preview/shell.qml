// Preview sandbox shell — hosts one extracted body component in a
// FloatingWindow (never the layer-shell KeyboardPanel), grabs a shot to
// $DAYFLOW_SHOT, then quits. Surface comes from $DAYFLOW_SURFACE.
import QtQuick
import Quickshell
import qs.Commons

  FloatingWindow {
    id: win
    property string surface: Quickshell.env("DAYFLOW_SURFACE")
    property string shotPath: Quickshell.env("DAYFLOW_SHOT")
    property real uiScale: parseFloat(Quickshell.env("DAYFLOW_SCALE")) || 2
    implicitWidth: 860; implicitHeight: 720
    color: Color.background

    QtObject { id: summarizeProc; property bool running: false }

    QtObject {
        id: dayflow
        property color foreground: Color.foreground
        property color dim: Color.muted
        property color accent: Color.accent
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
        property var settingsCatModel: [{name:"coding"},{name:"browsing"},{name:"communication"}]
        // Production spans arrive newest-first (Panel.applyTimeline reverses
        // engine cards) — the stub matches so "Latest" resolves correctly.
        property var spans: [
          { start_ts: 1760028800, end_ts: 1760032400, start: "1:20 PM", end: "2:20 PM",
            title: "hacker news rabbit hole", summary: "distraction", category: "browsing",
            productive: false, appName: "Firefox", minutes: 60, blocks: 2 },
          { start_ts: 1760021600, end_ts: 1760023400, start: "12:15 PM", end: "12:45 PM",
            title: "idle", summary: "", category: "idle",
            productive: false, appName: "", minutes: 30, blocks: 1 },
          { start_ts: 1760014400, end_ts: 1760018000, start: "10:30 AM", end: "11:30 AM",
            title: "standup + team review", summary: "daily sync", category: "communication",
            productive: true, appName: "Slack", minutes: 60, blocks: 2 },
          { start_ts: 1760000000, end_ts: 1760010800, start: "8:00 AM", end: "10:30 AM",
            title: "dayflow polish work", summary: "working on the popup", category: "coding",
            productive: true, appName: "Ghostty", minutes: 150, blocks: 4 }
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
            property bool actionsOpen: false
        property string activeApp: "Ghostty"
        property int blocksPending: 0
        property bool engineMissing: false
        property string engineVersion: "dev"
        property int framesToday: 42
        property bool fullViewOpen: false
        property var ignoredApps: []
        property bool onboardingSkipped: true
        property bool paused: false
        property string pluginVersion: "dev"
        property var skewDismissedFor: ({})
        property string summarizePhase: ""
    function versionNewer(a, b) { return false }
    }

    Loader {
        id: body
        anchors.fill: parent
        source: {
            switch (win.surface) {
            case "popup":           return "PopupBody.qml";
            case "fullview-today":  return "TodayPane.qml";
            case "week":            return "WeekPane.qml";
            default:                return "";
            }
        }
    }

    Component.onCompleted: { win.visible = true }
    Timer {
        interval: 2500; running: true; repeat: false
        onTriggered: body.grabToImage(function(res) {
            res.saveToFile(win.shotPath);
            Qt.quit();
        })
    }
    // hard cap — never hang the harness
    Timer { interval: 15000; running: true; repeat: false; onTriggered: Qt.quit() }
}
