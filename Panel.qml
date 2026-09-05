import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Panel {
  id: dayflow
  moduleName: "io.github.duketopceo.dayflow"
  manageIpc: false

  signal statusChanged()

  property var anchorItem: null
  property var hostWidget: null
  property var blocks: []
  property string dateLabel: ""
  property bool paused: false
  property bool configured: true
  property string errorText: ""
  property string modelName: ""
  property string activeApp: ""
  property var ignoredApps: []
  property int framesToday: 0
  property int blocksPending: 0
  property string storageText: ""
  property string notice: ""
  property string currentTab: "today"
  property var config: ({})
  property var configDraft: ({})
  property bool configLoaded: false
  property var standup: ({ yesterday: { date: "", total_minutes: 0, entries: [] }, today: { date: "", total_minutes: 0, entries: [] } })
  property var draft: ({ date: "", highlights: "", tasks: "", blockers: "", priorities: "" })
  property bool draftDirty: false
  property var workflow: ({ date: "", slot_minutes: 15, total_minutes: 0, slots: [], categories: [] })
  property var insights: ({ total_minutes: 0, focus_minutes: 0, distraction_minutes: 0, idle_minutes: 0, categories: [], apps: [], top_distractions: [], focus_blocks: [], days: 0 })
  property var weekBlocks: []
  property string weekStart: ""
  property string weekEnd: ""
  property string weekSummary: ""
  property bool weekSummaryLoading: false
  property var spans: []
  property int dayOffset: 0
  property bool expanded: false

  readonly property color foreground: dayflow.bar ? dayflow.bar.foreground : Color.foreground
  readonly property color dim: Qt.darker(dayflow.foreground, 1.5)
  readonly property string fontFamily: dayflow.bar ? dayflow.bar.fontFamily : Style.font.family

  function open() {
    dayflow.controller.show()
    refreshAll()
  }

  function close() {
    dayflow.controller.hide()
  }

  function switchPanel(direction) {
    if (dayflow.bar && typeof dayflow.bar.switchPanelFrom === "function")
      return dayflow.bar.switchPanelFrom(dayflow.hostWidget || dayflow, direction)
    return false
  }

  function refreshAll() {
    dayflow.loadTimeline()
    if (!statusProc.running) statusProc.running = true
    if (!standupFetchProc.running) standupFetchProc.running = true
    if (!insightsFetchProc.running) insightsFetchProc.running = true
    if (!weekTimelineProc.running) weekTimelineProc.running = true
    if (!configProc.running) configProc.running = true
  }

  function cloneConfig(obj) {
    return JSON.parse(JSON.stringify(obj || {}))
  }

  function applyConfig(raw) {
    try {
      var data = JSON.parse(raw)
      var cfg = data.config || data
      dayflow.config = cfg
      dayflow.configDraft = dayflow.cloneConfig(cfg)
      settingsCatModel.clear()
      var cats = cfg.categories || []
      for (var i = 0; i < cats.length; i++) {
        var c = cats[i]
        settingsCatModel.append({ name: c.name || "", description: c.description || "", color: c.color || "" })
      }
      dayflow.configLoaded = true
      dayflow.notice = ""
    } catch (e) {
      dayflow.notice = "config load failed"
    }
  }

  function saveConfig() {
    var patch = dayflow.cloneConfig(dayflow.configDraft)
    if (patch.openrouter_api_key === "***redacted***") delete patch.openrouter_api_key
    patch.categories = []
    for (var i = 0; i < settingsCatModel.count; i++) {
      var item = settingsCatModel.get(i)
      if (item.name) patch.categories.push({ name: item.name, description: item.description, color: item.color || "" })
    }
    patchProc.command = ["dayflow", "config", "patch", JSON.stringify(patch)]
    patchProc.running = true
  }

  function loadConfig() {
    if (!configProc.running) configProc.running = true
  }

  function viewDate() {
    var d = new Date()
    d.setDate(d.getDate() + dayflow.dayOffset)
    return d
  }

  function viewDateStr() {
    var d = dayflow.viewDate()
    var m = ("0" + (d.getMonth() + 1)).slice(-2)
    var dd = ("0" + d.getDate()).slice(-2)
    return d.getFullYear() + "-" + m + "-" + dd
  }

  function viewDateLabel() {
    if (dayflow.dayOffset === 0) return "Today"
    if (dayflow.dayOffset === -1) return "Yesterday"
    var names = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
    var d = dayflow.viewDate()
    return names[d.getDay()] + " " + dayflow.viewDateStr().substring(5)
  }

  function goDay(delta) {
    var next = dayflow.dayOffset + delta
    if (next > 0) return
    dayflow.dayOffset = next
    dayflow.blocks = []
    dayflow.spans = []
    dayflow.loadTimeline()
  }

  function loadTimeline() {
    timelineProc.command = ["dayflow", "timeline", "--json", dayflow.viewDateStr()]
    if (!timelineProc.running) timelineProc.running = true
    dayflow.loadWorkflow()
  }

  // Merge consecutive blocks about the same thing (same title, or same
  // app+category) into longer "blocked out" spans.
  function mergeSpans(blocks) {
    var list = (blocks || []).slice()
    list.sort(function(a, b) { return Number(a.start_ts) - Number(b.start_ts) })
    var spans = []
    for (var i = 0; i < list.length; i++) {
      var b = list[i]
      var prev = spans.length ? spans[spans.length - 1] : null
      var same = prev && (b.title === prev.title ||
        (b.app === prev.app && b.category === prev.category))
      if (same) {
        prev.children.push(b)
        prev.end = b.end
        prev.end_ts = b.end_ts
        prev.minutes += Math.round((Number(b.end_ts) - Number(b.start_ts)) / 60)
        prev.count++
        prev.title = b.title
        prev.summary = b.summary
      } else {
        spans.push({
          start: b.start, end: b.end,
          start_ts: b.start_ts, end_ts: b.end_ts,
          title: b.title, summary: b.summary,
          category: b.category, app: b.app,
          appName: b.app_name || dayflow.appDisplayName(b.app),
          minutes: Math.round((Number(b.end_ts) - Number(b.start_ts)) / 60),
          count: 1,
          children: [b]
        })
      }
    }
    spans.reverse()
    return spans
  }

  function fmtDur(mins) {
    mins = Math.round(Number(mins) || 0)
    if (mins < 60) return mins + "m"
    var h = Math.floor(mins / 60)
    var m = mins % 60
    return m ? h + "h " + m + "m" : h + "h"
  }

  function appIcon(cls) {
    if (!cls || cls === "") return ""
    var tail = String(cls).split(".").pop()
    var names = [cls, String(cls).toLowerCase(), tail, tail.toLowerCase()]
    for (var i = 0; i < names.length; i++) {
      try {
        var p = Quickshell.iconPath(names[i], true)
        if (p && String(p).length > 0) return p
      } catch (e) {}
    }
    return ""
  }

  function applyTimeline(raw) {
    try {
      var d = JSON.parse(raw)
      dayflow.blocks = d.blocks || []
      dayflow.spans = dayflow.mergeSpans(dayflow.blocks)
      dayflow.dateLabel = d.date || ""
      dayflow.errorText = ""
    } catch (e) {
      dayflow.blocks = []
      dayflow.spans = []
      dayflow.errorText = "could not read timeline"
    }
  }

  function applyStatus(raw) {
    try {
      var s = JSON.parse(raw)
      dayflow.paused = s.paused === true
      dayflow.configured = s.configured !== false
      dayflow.modelName = s.model || ""
      dayflow.activeApp = s.active_app || ""
      dayflow.ignoredApps = s.ignored_apps || []
      dayflow.framesToday = Number(s.frames_today || 0)
      dayflow.blocksPending = Number(s.blocks_pending || 0)
      dayflow.storageText = s.storage_text || ""
    } catch (e) {}
  }

  function applyStandup(raw) {
    try {
      var d = JSON.parse(raw)
      dayflow.standup = d
      if (d.draft && !dayflow.draftDirty) {
        dayflow.draft = d.draft
      }
    } catch (e) {}
  }

  function applyWorkflow(raw) {
    try {
      var d = JSON.parse(raw)
      dayflow.workflow = d
    } catch (e) {
      dayflow.workflow = ({ date: "", slot_minutes: 15, total_minutes: 0, slots: [], categories: [] })
    }
  }

  function todayStr() {
    var d = new Date()
    var m = ("0" + (d.getMonth() + 1)).slice(-2)
    var dd = ("0" + d.getDate()).slice(-2)
    return d.getFullYear() + "-" + m + "-" + dd
  }

  function saveDraft() {
    var d = dayflow.draft || {}
    draftSaveProc.command = ["dayflow", "standup", "save",
      "--date", d.date || dayflow.todayStr(),
      "--highlights", String(d.highlights || ""),
      "--tasks", String(d.tasks || ""),
      "--blockers", String(d.blockers || ""),
      "--priorities", String(d.priorities || "")]
    if (!draftSaveProc.running) draftSaveProc.running = true
  }

  function loadWorkflow() {
    gridProc.command = ["dayflow", "day", dayflow.viewDateStr(), "--grid", "--json"]
    if (!gridProc.running) gridProc.running = true
  }

  function applyInsights(raw) {
    try {
      var d = JSON.parse(raw)
      dayflow.insights = d
    } catch (e) {}
  }

  function applyWeekTimeline(raw) {
    try {
      var d = JSON.parse(raw)
      dayflow.weekBlocks = d.blocks || []
      dayflow.weekStart = d.start || ""
      dayflow.weekEnd = d.end || ""
    } catch (e) {
      dayflow.weekBlocks = []
    }
  }

  function applyWeekReview(raw) {
    dayflow.weekSummaryLoading = false
    try {
      var d = JSON.parse(raw)
      dayflow.weekSummary = d.review || ""
    } catch (e) {
      dayflow.weekSummary = "Could not load weekly review."
    }
  }

  function weekStartDate() {
    if (dayflow.weekStart === "") return new Date()
    return new Date(dayflow.weekStart + "T00:00:00")
  }

  function categoryForHour(dayIndex, hour) {
    if (!dayflow.weekBlocks.length) return ""
    var base = dayflow.weekStartDate().getTime() + dayIndex * 86400000 + hour * 3600000
    for (var i = 0; i < dayflow.weekBlocks.length; i++) {
      var b = dayflow.weekBlocks[i]
      var s = Number(b.start_ts || 0) * 1000
      var e = Number(b.end_ts || 0) * 1000
      if (s < base + 3600000 && e > base) {
        return b.category
      }
    }
    return ""
  }

  function dayName(index) {
    var names = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
    return names[index] || ""
  }

  function weekDayBlocks(dayIndex) {
    if (!dayflow.weekBlocks.length) return []
    var dayStart = dayflow.weekStartDate().getTime() + dayIndex * 86400000
    var dayEnd = dayStart + 86400000
    var out = []
    for (var i = 0; i < dayflow.weekBlocks.length; i++) {
      var s = Number(dayflow.weekBlocks[i].start_ts || 0) * 1000
      if (s >= dayStart && s < dayEnd) out.push(dayflow.weekBlocks[i])
    }
    return out
  }

  function weekDaySpans(dayIndex) {
    return dayflow.mergeSpans(dayflow.weekDayBlocks(dayIndex))
  }

  function weekDayMinutes(dayIndex) {
    var list = dayflow.weekDayBlocks(dayIndex)
    var total = 0
    for (var i = 0; i < list.length; i++)
      total += (Number(list[i].end_ts) - Number(list[i].start_ts)) / 60
    return Math.round(total)
  }

  function catDisplay(cat) {
    if (!cat || cat === "") return ""
    return cat.charAt(0).toUpperCase() + cat.slice(1)
  }

  function modelShort() {
    var m = dayflow.modelName
    return m.indexOf("/") >= 0 ? m.split("/").pop() : m
  }

  // Resolve a window class or app name to a proper display name.
  // Prefers the desktop entry's real Name, then a cleaned-up tail.
  function appDisplayName(cls) {
    if (!cls || cls === "") return ""
    try {
      var entries = DesktopEntries.applications.values || []
      var lc = String(cls).toLowerCase()
      var tail = lc.split(".").pop()
      for (var i = 0; i < entries.length; i++) {
        var e = entries[i]
        var eid = String(e.id || "").toLowerCase()
        if (eid === lc || eid === tail ||
            eid === lc + ".desktop" || eid === tail + ".desktop") {
          return e.name
        }
      }
    } catch (e2) {}
    return dayflow.titleize(cls)
  }

  function titleize(cls) {
    var s = String(cls)
    var i = s.indexOf("__")
    if (i >= 0) s = s.substring(0, i)
    var tail = s
    var j = tail.lastIndexOf(".")
    if (j >= 0) tail = tail.substring(j + 1)
    var lt = tail.toLowerCase()
    if (tail === "" || lt === "com" || lt === "org" || lt === "net" ||
        lt === "default" || tail.length <= 2) {
      var k = s.indexOf(".")
      tail = k > 0 ? s.substring(0, k) : s
      lt = tail.toLowerCase()
    }
    var junk = ["-default", "-browser", "-bin", ".bin"]
    for (var m = 0; m < junk.length; m++) {
      var suf = junk[m]
      if (lt.length >= suf.length &&
          lt.substring(lt.length - suf.length) === suf) {
        tail = tail.substring(0, tail.length - suf.length)
        lt = tail.toLowerCase()
      }
    }
    tail = tail.replace(/^[_\-. ]+|[_\-. ]+$/g, "")
    if (tail === "") tail = s
    var words = tail.split(/[\s_\-]+/)
    for (var w = 0; w < words.length; w++) {
      if (words[w] !== "")
        words[w] = words[w].charAt(0).toUpperCase() + words[w].substring(1)
    }
    return words.join(" ")
  }

  function categoryColor(cat) {
    switch (cat) {
      case "coding":        return Qt.rgba(0.22, 0.55, 0.95, 1.0)
      case "communication": return Qt.rgba(0.95, 0.45, 0.15, 1.0)
      case "browsing":      return Qt.rgba(0.55, 0.35, 0.95, 1.0)
      case "writing":       return Qt.rgba(0.20, 0.75, 0.55, 1.0)
      case "meetings":      return Qt.rgba(0.95, 0.70, 0.15, 1.0)
      case "design":        return Qt.rgba(0.95, 0.25, 0.55, 1.0)
      case "media":         return Qt.rgba(0.95, 0.25, 0.25, 1.0)
      case "system":        return Qt.rgba(0.50, 0.50, 0.55, 1.0)
      case "idle":          return dayflow.dim
      case "personal":      return Color.urgent !== undefined ? Color.urgent : Qt.rgba(0.95, 0.25, 0.35, 1.0)
      case "other":         return Qt.darker(dayflow.foreground, 1.4)
      default:              return dayflow.foreground
    }
  }

  function cellColor(cat) {
    if (!cat || cat === "") return dayflow.fgFill(0.06)
    var c = dayflow.categoryColor(cat)
    return Qt.rgba(c.r, c.g, c.b, 0.85)
  }

  function pillBgColor(cat) {
    var c = dayflow.categoryColor(cat)
    return Qt.rgba(c.r, c.g, c.b, 0.15)
  }

  function _rgb(c) {
    if (typeof c === "string") {
      var h = c.charAt(0) === "#" ? c.substring(1) : c
      if (h.length === 8) h = h.substring(0, 6)
      if (h.length === 3) {
        h = h.charAt(0) + h.charAt(0) + h.charAt(1) + h.charAt(1) + h.charAt(2) + h.charAt(2)
      }
      if (h.length === 6) {
        return [parseInt(h.substring(0, 2), 16) / 255,
                parseInt(h.substring(2, 4), 16) / 255,
                parseInt(h.substring(4, 6), 16) / 255]
      }
      return [1, 1, 1]
    }
    if (c === undefined || c === null) return [1, 1, 1]
    return [c.r, c.g, c.b]
  }

  function themeFill(alpha) {
    var rgb = dayflow._rgb(dayflow.foreground)
    return Qt.rgba(rgb[0], rgb[1], rgb[2], alpha)
  }

  function accentFill(alpha) {
    var c = (Color.accent === undefined || Color.accent === null)
      ? dayflow.foreground : Color.accent
    var rgb = dayflow._rgb(c)
    return Qt.rgba(rgb[0], rgb[1], rgb[2], alpha)
  }

  function fgFill(alpha) {
    var rgb = dayflow._rgb(dayflow.foreground)
    return Qt.rgba(rgb[0], rgb[1], rgb[2], alpha)
  }

  function btnBg(hot) {
    return hot
      ? dayflow.accentFill(0.12)
      : "transparent"
  }

  function fmtHours(mins) {
    if (mins === undefined || isNaN(mins)) return "0.0"
    return (Number(mins) / 60).toFixed(1)
  }

  Process {
    id: timelineProc
    command: ["dayflow", "timeline", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyTimeline(text)
    }
    onExited: function(exitCode) {
      if (exitCode !== 0) dayflow.errorText = "dayflow CLI not found on PATH"
    }
  }

  Process {
    id: statusProc
    command: ["dayflow", "status", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyStatus(text)
    }
  }

  Process {
    id: standupFetchProc
    command: ["dayflow", "standup", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyStandup(text)
    }
  }

  Process {
    id: gridProc
    command: ["dayflow", "day", "--grid", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyWorkflow(text)
    }
  }

  Process {
    id: draftSaveProc
    command: ["dayflow", "standup", "save"]
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: { if (text.trim() !== "") dayflow.notice = text.trim() }
    }
    onExited: function(exitCode) {
      if (exitCode === 0) {
        dayflow.draftDirty = false
        dayflow.notice = "draft saved"
        if (!standupFetchProc.running) standupFetchProc.running = true
      } else {
        dayflow.notice = "draft save failed"
      }
    }
  }

  Process {
    id: insightsFetchProc
    command: ["dayflow", "insights", "week", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyInsights(text)
    }
  }

  Process {
    id: weekTimelineProc
    command: ["dayflow", "week", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyWeekTimeline(text)
    }
  }

  Process {
    id: reviewProc
    command: ["dayflow", "review", "week", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyWeekReview(text)
    }
  }

  Process {
    id: toggleProc
    command: ["dayflow", "toggle"]
    onExited: {
      dayflow.statusChanged()
      Qt.callLater(dayflow.refreshAll)
    }
  }

  Process {
    id: ignoreProc
    command: ["dayflow", "ignore", "--active"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.notice = text.trim()
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: { if (text.trim() !== "") dayflow.notice = text.trim() }
    }
    onExited: {
      dayflow.statusChanged()
      Qt.callLater(dayflow.refreshAll)
    }
  }

  Process {
    id: summarizeProc
    command: ["dayflow", "summarize", "--now"]
    onExited: Qt.callLater(dayflow.refreshAll)
  }

  Process {
    id: copyProc
    command: ["bash", "-c", "dayflow export | wl-copy"]
    onExited: function(exitCode) {
      dayflow.notice = exitCode === 0 ? "copied today's journal" : "copy failed"
    }
  }

  Process {
    id: standupProc
    command: ["bash", "-c", "dayflow standup | wl-copy"]
    onExited: function(exitCode) {
      dayflow.notice = exitCode === 0 ? "copied standup" : "standup copy failed"
    }
  }

  Process {
    id: insightsProc
    command: ["bash", "-c", "dayflow insights | wl-copy"]
    onExited: function(exitCode) {
      dayflow.notice = exitCode === 0 ? "copied daily insights" : "insights copy failed"
    }
  }

  Process {
    id: configProc
    command: ["dayflow", "config", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: dayflow.applyConfig(text)
    }
  }

  Process {
    id: patchProc
    command: ["dayflow", "config", "patch", "{}"]
    onExited: function(exitCode) {
      if (exitCode === 0) {
        dayflow.notice = "settings saved"
        configProc.running = true
      } else {
        dayflow.notice = "settings save failed"
      }
    }
  }

  // ---- tab components ----
  Component {
    id: todayTab
    Flickable {
      width: parent.width
      implicitHeight: Math.min(col.implicitHeight, Style.space(360))
      height: implicitHeight
      contentHeight: col.implicitHeight
      clip: true

      Column {
        id: col
        width: parent.width
        spacing: Style.space(10)

        // ---- day switcher ----
        Row {
          width: parent.width
          spacing: Style.space(6)

          Rectangle {
            height: Style.space(28)
            width: Style.space(28)
            radius: Style.cornerRadius
            color: dPrev.containsMouse ? dayflow.accentFill(0.12) : dayflow.fgFill(0.04)
            border.color: dayflow.accentFill(0.4)
            Text {
              anchors.centerIn: parent
              text: "‹"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
            }
            MouseArea {
              id: dPrev
              anchors.fill: parent
              hoverEnabled: true
              cursorShape: Qt.PointingHandCursor
              onClicked: dayflow.goDay(-1)
            }
          }

          Text {
            text: dayflow.viewDateLabel()
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
            anchors.verticalCenter: parent.verticalCenter
          }

          Rectangle {
            height: Style.space(28)
            width: Style.space(28)
            radius: Style.cornerRadius
            color: dNext.containsMouse ? dayflow.accentFill(0.12) : dayflow.fgFill(0.04)
            border.color: dayflow.accentFill(0.4)
            opacity: dayflow.dayOffset < 0 ? 1 : 0.4
            Text {
              anchors.centerIn: parent
              text: "›"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
            }
            MouseArea {
              id: dNext
              anchors.fill: parent
              hoverEnabled: true
              cursorShape: Qt.PointingHandCursor
              enabled: dayflow.dayOffset < 0
              onClicked: dayflow.goDay(1)
            }
          }

          Text {
            visible: dayflow.dayOffset !== 0
            text: "back to today"
            color: backToday.containsMouse ? dayflow.foreground : dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            font.underline: backToday.containsMouse
            anchors.verticalCenter: parent.verticalCenter
            MouseArea {
              id: backToday
              anchors.fill: parent
              hoverEnabled: true
              cursorShape: Qt.PointingHandCursor
              onClicked: { dayflow.dayOffset = 0; dayflow.loadTimeline() }
            }
          }
        }

        Text {
          visible: dayflow.spans.length === 0 && dayflow.errorText === "" && dayflow.configured
          width: parent.width
          text: "Nothing summarized yet — blocks land every 15 minutes."
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: Text.WordWrap
        }

        Repeater {
          model: dayflow.spans

          delegate: Rectangle {
            width: col.width
            height: cardCol.implicitHeight + Style.space(16)
            radius: Style.cornerRadius
            color: dayflow.fgFill(0.04)
            border.color: dayflow.fgFill(0.08)
            clip: true

            // Category-colored edge so spans scan by activity type.
            Rectangle {
              anchors.left: parent.left
              anchors.top: parent.top
              anchors.bottom: parent.bottom
              width: Style.space(3)
              color: dayflow.categoryColor(modelData.category)
            }

            Column {
              id: cardCol
              width: parent.width - Style.space(22)
              anchors.centerIn: parent
              anchors.horizontalCenterOffset: Style.space(3)
              spacing: Style.space(4)

              Row {
                width: parent.width
                spacing: Style.space(8)

                Image {
                  width: Style.space(14)
                  height: Style.space(14)
                  source: dayflow.appIcon(modelData.app)
                  visible: status === Image.Ready
                  anchors.verticalCenter: parent.verticalCenter
                }

                Text {
                  text: modelData.start + "–" + modelData.end +
                        " · " + dayflow.fmtDur(modelData.minutes) +
                        (modelData.count > 1 ? " · " + modelData.count + " blocks" : "") +
                        (modelData.appName ? " · " + modelData.appName : "")
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  anchors.verticalCenter: parent.verticalCenter
                }

                Rectangle {
                  height: catText.implicitHeight + Style.space(4)
                  width: catText.implicitWidth + Style.space(10)
                  radius: height / 2
                  color: dayflow.pillBgColor(modelData.category)

                  Text {
                    id: catText
                    anchors.centerIn: parent
                    text: dayflow.catDisplay(modelData.category)
                    color: dayflow.foreground
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.caption
                  }
                }
              }

              Text {
                width: parent.width
                text: modelData.title
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                font.bold: true
                wrapMode: Text.WordWrap
              }

              Text {
                width: parent.width
                text: modelData.summary
                color: dayflow.foreground
                opacity: 0.75
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                wrapMode: Text.WordWrap
                maximumLineCount: 4
                elide: Text.ElideRight
              }

              // Merged span: one row per underlying 15-min block.
              Repeater {
                model: modelData.count > 1 ? modelData.children : []

                delegate: Text {
                  width: parent.width
                  text: "· " + modelData.start + " " + modelData.title
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  elide: Text.ElideRight
                }
              }

              // Single block: per-app segments as before.
              Repeater {
                model: modelData.count === 1 ? (modelData.children[0].activities || []) : []

                delegate: Text {
                  width: parent.width
                  text: dayflow.appDisplayName(modelData.app) + " · " + modelData.title
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  elide: Text.ElideRight
                }
              }
            }
          }
        }
      }
    }
  }

  Component {
    id: standupTab
    Flickable {
      width: parent.width
      implicitHeight: Math.min(suCol.implicitHeight, Style.space(420))
      height: implicitHeight
      contentHeight: suCol.implicitHeight
      clip: true

      Column {
        id: suCol
        width: parent.width
        spacing: Style.space(10)

      // ---- editable standup draft ----
      Component {
        id: draftField
        Column {
          property string label: ""
          property string field: ""
          width: parent ? parent.width : 0
          spacing: Style.space(2)

          Text {
            text: label
            color: dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
          }

          Rectangle {
            width: parent.width
            height: Math.min(fEdit.implicitHeight + Style.space(8), Style.space(72))
            radius: Style.cornerRadius
            color: dayflow.fgFill(0.04)
            border.color: dayflow.fgFill(0.12)
            clip: true

            TextEdit {
              id: fEdit
              anchors.fill: parent
              anchors.margins: Style.space(4)
              text: dayflow.draft[field] || ""
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              wrapMode: TextEdit.Wrap
              // Only user edits (focused field) mark the draft dirty — this
              // keeps applyStandup() refreshes from clobbering in-progress text.
              onTextChanged: {
                if (fEdit.activeFocus) {
                  dayflow.draft[field] = text
                  dayflow.draftDirty = true
                }
              }
            }
          }
        }
      }

      Rectangle {
        width: parent.width
        height: draftCol.implicitHeight + Style.space(16)
        radius: Style.cornerRadius
        color: dayflow.fgFill(0.04)
        border.color: dayflow.fgFill(0.08)

        Column {
          id: draftCol
          width: parent.width - Style.space(16)
          anchors.centerIn: parent
          spacing: Style.space(6)

          Row {
            width: parent.width

            Text {
              text: "Standup draft" + (dayflow.draft.date ? " · " + dayflow.draft.date : "")
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }

            Text {
              anchors.right: parent.right
              visible: dayflow.draftDirty
              text: "unsaved changes"
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
          }

          Loader {
            width: parent.width
            height: item ? item.implicitHeight : 0
            sourceComponent: draftField
            onLoaded: { item.label = "Highlights"; item.field = "highlights" }
          }

          Loader {
            width: parent.width
            height: item ? item.implicitHeight : 0
            sourceComponent: draftField
            onLoaded: { item.label = "Tasks"; item.field = "tasks" }
          }

          Loader {
            width: parent.width
            height: item ? item.implicitHeight : 0
            sourceComponent: draftField
            onLoaded: { item.label = "Blockers"; item.field = "blockers" }
          }

          Loader {
            width: parent.width
            height: item ? item.implicitHeight : 0
            sourceComponent: draftField
            onLoaded: { item.label = "Priorities"; item.field = "priorities" }
          }

          Rectangle {
            height: Style.space(28)
            width: saveDraftText.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: mSaveDraft.containsMouse
              ? dayflow.accentFill(0.12)
              : "transparent"
            border.color: dayflow.accentFill(0.5)

            Text {
              id: saveDraftText
              anchors.centerIn: parent
              text: draftSaveProc.running ? "Saving..." : "Save draft"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: mSaveDraft
              anchors.fill: parent
              hoverEnabled: true
              cursorShape: Qt.PointingHandCursor
              onClicked: dayflow.saveDraft()
            }
          }
        }
      }

      // ---- daily workflow grid ----
      Loader {
        width: parent.width
        height: item ? item.implicitHeight : 0
        source: "DailyWorkflowGrid.qml"
        property var panel: dayflow
      }

      Text {
        visible: dayflow.standup.yesterday.entries.length === 0 && dayflow.standup.today.entries.length === 0
        width: parent.width
        text: "No standup data yet — need a few summarized blocks."
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        wrapMode: Text.WordWrap
      }

      Component {
        id: standupDay

        Rectangle {
          property string label: ""
          property var day: ({ date: "", total_minutes: 0, entries: [] })

          width: parent.width
          height: sdCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: dayflow.fgFill(0.04)
          border.color: dayflow.fgFill(0.08)

          Column {
            id: sdCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(8)

            Row {
              width: parent.width

              Text {
                text: label
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }

              Text {
                anchors.right: parent.right
                text: dayflow.fmtDur(day.total_minutes) + " tracked"
                color: dayflow.dim
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
              }
            }

            Repeater {
              model: day.entries || []
              delegate: Row {
                width: parent.width
                spacing: Style.space(8)

                Rectangle {
                  width: durText.implicitWidth + Style.space(10)
                  height: durText.implicitHeight + Style.space(4)
                  radius: Style.cornerRadius
                  color: (modelData.productive === true)
                    ? dayflow.accentFill(0.12)
                    : dayflow.fgFill(0.05)

                  Text {
                    id: durText
                    anchors.centerIn: parent
                    text: dayflow.fmtDur(modelData.minutes)
                    color: (modelData.productive === true) ? dayflow.foreground : dayflow.dim
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.caption
                    font.bold: true
                  }
                }

                Column {
                  width: parent.width - parent.spacing - (durText.implicitWidth + Style.space(10))
                  spacing: Style.space(1)

                  Text {
                    width: parent.width
                    text: modelData.title
                    color: (modelData.productive === true) ? dayflow.foreground : dayflow.dim
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.body
                    wrapMode: Text.WordWrap
                  }

                  Text {
                    width: parent.width
                    text: (modelData.app ? modelData.app + " · " : "")
                          + dayflow.catDisplay(modelData.category)
                          + (modelData.span ? " · " + modelData.span : "")
                    color: dayflow.dim
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.caption
                    wrapMode: Text.WordWrap
                  }
                }
              }
            }
          }
        }
      }

      Loader {
        width: parent.width
        visible: dayflow.standup.yesterday.entries.length > 0
        sourceComponent: standupDay
        onLoaded: {
          item.label = "Yesterday"
          item.day = dayflow.standup.yesterday
        }
      }

      Loader {
        width: parent.width
        visible: dayflow.standup.today.entries.length > 0
        sourceComponent: standupDay
        onLoaded: {
          item.label = "Today"
          item.day = dayflow.standup.today
        }
      }

      Rectangle {
        visible: dayflow.standup.today.entries.length > 0 || dayflow.standup.yesterday.entries.length > 0
        width: parent.width
        height: bCol.implicitHeight + Style.space(16)
        radius: Style.cornerRadius
        color: dayflow.fgFill(0.04)
        border.color: dayflow.fgFill(0.08)

        Column {
          id: bCol
          width: parent.width - Style.space(16)
          anchors.centerIn: parent
          spacing: Style.space(6)

          Text {
            text: "Blockers"
            color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Text {
            width: parent.width
            text: dayflow.draft.blockers !== ""
              ? dayflow.draft.blockers
              : "Nothing flagged — fill this in when you write your update."
            color: dayflow.draft.blockers !== "" ? dayflow.foreground : dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Text {
            visible: dayflow.draft.priorities !== ""
            width: parent.width
            text: "Priorities: " + dayflow.draft.priorities
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Rectangle {
            id: copyBtn
            height: Style.space(28)
            width: cpy.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: mcp.containsMouse
              ? dayflow.accentFill(0.12)
              : "transparent"
            border.color: dayflow.accentFill(0.25)

            Text {
              id: cpy
              anchors.centerIn: parent
              text: "Copy standup"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: mcp
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { if (!standupProc.running) standupProc.running = true }
            }
          }
        }
      }
    }
    }
  }

  Component {
    id: weekTab
    Flickable {
      width: parent.width
      implicitHeight: Math.min(wcol.implicitHeight, Style.space(360))
      height: implicitHeight
      contentHeight: wcol.implicitHeight
      clip: true

      Column {
        id: wcol
        width: parent.width
        spacing: Style.space(10)

        Text {
          visible: dayflow.insights.total_minutes === 0
          width: parent.width
          text: "No weekly data yet."
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: Text.WordWrap
        }

        Rectangle {
          visible: dayflow.insights.total_minutes > 0
          width: parent.width
          height: statCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: dayflow.fgFill(0.04)
          border.color: dayflow.fgFill(0.08)

          Column {
            id: statCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(6)

            Text {
              text: "This week" + (dayflow.insights.days ? " · " + dayflow.insights.days + " days" : "")
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }

            Row {
              width: parent.width
              spacing: Style.space(6)

              Repeater {
                model: [
                  { label: "Tracked", value: dayflow.fmtHours(dayflow.insights.total_minutes) + " hr", dimmed: false },
                  { label: "Focus", value: dayflow.fmtHours(dayflow.insights.focus_minutes) + " hr", dimmed: false },
                  { label: "Distracted / idle", value: dayflow.fmtHours(dayflow.insights.distraction_minutes + dayflow.insights.idle_minutes) + " hr", dimmed: true }
                ]

                delegate: Rectangle {
                  width: (parent.width - 2 * parent.spacing) / 3
                  height: tileCol.implicitHeight + Style.space(10)
                  radius: Style.cornerRadius
                  color: dayflow.fgFill(0.04)
                  border.color: dayflow.fgFill(0.08)

                  Column {
                    id: tileCol
                    anchors.centerIn: parent
                    width: parent.width - Style.space(12)
                    spacing: Style.space(1)

                    Text {
                      width: parent.width
                      text: modelData.label
                      color: dayflow.dim
                      font.family: dayflow.fontFamily
                      font.pixelSize: Style.font.caption
                      elide: Text.ElideRight
                    }

                    Text {
                      width: parent.width
                      text: modelData.value
                      color: modelData.dimmed ? dayflow.dim : dayflow.foreground
                      font.family: dayflow.fontFamily
                      font.pixelSize: Style.font.body
                      font.bold: true
                      elide: Text.ElideRight
                    }
                  }
                }
              }
            }
          }
        }

        Rectangle {
          visible: dayflow.insights.total_minutes > 0
          width: parent.width
          height: reviewCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: dayflow.fgFill(0.04)
          border.color: dayflow.fgFill(0.08)

          Column {
            id: reviewCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(8)

            Row {
              width: parent.width
              spacing: Style.space(8)

              Text {
                text: "Weekly review"
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }

              Rectangle {
                width: genText.implicitWidth + Style.space(12)
                height: genText.implicitHeight + Style.space(6)
                radius: Style.cornerRadius
                color: dayflow.btnBg(genMa.containsMouse)
                border.color: dayflow.accentFill(0.5)
                visible: !dayflow.weekSummaryLoading && dayflow.weekSummary === ""

                Text {
                  id: genText
                  anchors.centerIn: parent
                  text: "Generate"
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                }
                MouseArea {
                  id: genMa
                  anchors.fill: parent
                  hoverEnabled: true
                  onClicked: { dayflow.weekSummaryLoading = true; if (!reviewProc.running) reviewProc.running = true }
                }
              }

              Rectangle {
                width: regText.implicitWidth + Style.space(12)
                height: regText.implicitHeight + Style.space(6)
                radius: Style.cornerRadius
                color: dayflow.btnBg(regMa.containsMouse)
                border.color: dayflow.accentFill(0.5)
                visible: !dayflow.weekSummaryLoading && dayflow.weekSummary !== ""

                Text {
                  id: regText
                  anchors.centerIn: parent
                  text: "Regenerate"
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                }
                MouseArea {
                  id: regMa
                  anchors.fill: parent
                  hoverEnabled: true
                  onClicked: { dayflow.weekSummaryLoading = true; if (!reviewProc.running) reviewProc.running = true }
                }
              }
            }

            Text {
              width: parent.width
              visible: dayflow.weekSummary === "" && !dayflow.weekSummaryLoading
              text: "Generate a weekly review to get AI advice, corrections, and suggestions."
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              wrapMode: Text.WordWrap
            }

            Text {
              width: parent.width
              visible: dayflow.weekSummaryLoading
              text: "Generating weekly review..."
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              wrapMode: Text.WordWrap
            }

            Text {
              width: parent.width
              visible: !dayflow.weekSummaryLoading && dayflow.weekSummary !== ""
              text: dayflow.weekSummary
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              wrapMode: Text.WordWrap
              textFormat: Text.MarkdownText
            }
          }
        }

        Rectangle {
          visible: dayflow.weekBlocks.length > 0
          width: parent.width
          height: heatCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: dayflow.fgFill(0.04)
          border.color: dayflow.fgFill(0.08)

          Column {
            id: heatCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(8)

            Text {
              text: "Week heat map"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }

            Text {
              text: "1 cell = 1 hour"
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            Repeater {
              model: 7
              delegate: Row {
                width: parent.width
                spacing: Style.space(2)

                Text {
                  width: Style.space(28)
                  text: dayflow.dayName(index)
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  anchors.verticalCenter: parent.verticalCenter
                }

                Row {
                  id: hourRow
                  width: parent.width - Style.space(28) - Style.space(2)
                  spacing: 1

                  Repeater {
                    model: 24
                    delegate: Rectangle {
                      width: (parent.width - 23 * hourRow.spacing) / 24
                      height: Style.space(10)
                      radius: 2
                      color: dayflow.cellColor(dayflow.categoryForHour(index, modelData))
                    }
                  }
                }
              }
            }

            // Hour tick labels under the grid (aligned to cell boundaries).
            Row {
              width: parent.width
              spacing: Style.space(2)

              Item { width: Style.space(28); height: 1 }

              Item {
                width: parent.width - Style.space(28) - Style.space(2)
                height: Style.space(11)

                Repeater {
                  model: [0, 6, 12, 18]
                  delegate: Text {
                    x: (parent.width / 24) * modelData
                    text: modelData + "h"
                    color: dayflow.dim
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.caption
                  }
                }
              }
            }
          }
        }

        // ---- full week timeline (merged spans per day) ----
        Rectangle {
          visible: dayflow.weekBlocks.length > 0
          width: parent.width
          height: weekCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: dayflow.fgFill(0.04)
          border.color: dayflow.fgFill(0.08)

          Column {
            id: weekCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(8)

            Text {
              text: "Week timeline" +
                    (dayflow.weekStart ? "  " + dayflow.weekStart.substring(5) + " – " + dayflow.weekEnd.substring(5) : "")
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }

            Repeater {
              model: 7
              delegate: Column {
                property var daySpans: dayflow.weekDaySpans(index)
                visible: daySpans.length > 0
                width: parent.width
                spacing: Style.space(2)

                Row {
                  width: parent.width
                  spacing: Style.space(6)
                  Text {
                    text: dayflow.dayName(index)
                    color: dayflow.foreground
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.caption
                    font.bold: true
                    anchors.verticalCenter: parent.verticalCenter
                  }
                  Text {
                    text: dayflow.fmtDur(dayflow.weekDayMinutes(index))
                    color: dayflow.dim
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.caption
                    anchors.verticalCenter: parent.verticalCenter
                  }
                }

                Repeater {
                  model: parent.daySpans
                  delegate: Row {
                    width: parent.width
                    spacing: Style.space(6)

                    Rectangle {
                      width: Style.space(6)
                      height: wTxt.implicitHeight
                      radius: width / 2
                      color: dayflow.cellColor(modelData.category)
                      anchors.verticalCenter: parent.verticalCenter
                    }

                    Text {
                      id: wTxt
                      width: parent.width - Style.space(6) - parent.spacing
                      text: modelData.start + "–" + modelData.end +
                            "  " + modelData.title +
                            (modelData.count > 1 ? "  (" + dayflow.fmtDur(modelData.minutes) + ")" : "")
                      color: dayflow.foreground
                      font.family: dayflow.fontFamily
                      font.pixelSize: Style.font.caption
                      elide: Text.ElideRight
                    }
                  }
                }
              }
            }
          }
        }

        Loader {
          id: catLoader
          width: parent.width
          height: item ? item.implicitHeight : 0
          sourceComponent: sectionList
          onLoaded: { item.title = "Categories" }
        }
        Binding { target: catLoader.item; property: "items"; value: dayflow.insights.categories; when: catLoader.status === Loader.Ready }

        Loader {
          id: appLoader
          width: parent.width
          height: item ? item.implicitHeight : 0
          sourceComponent: sectionList
          onLoaded: { item.title = "Top apps" }
        }
        Binding { target: appLoader.item; property: "items"; value: dayflow.insights.apps; when: appLoader.status === Loader.Ready }

        Loader {
          id: distLoader
          width: parent.width
          height: item ? item.implicitHeight : 0
          sourceComponent: sectionList
          onLoaded: { item.title = "Distractions to watch" }
        }
        Binding { target: distLoader.item; property: "items"; value: dayflow.insights.top_distractions; when: distLoader.status === Loader.Ready }

        Rectangle {
          visible: dayflow.insights.focus_blocks.length > 0
          width: parent.width
          height: fCol.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: dayflow.fgFill(0.04)
          border.color: dayflow.fgFill(0.08)

          Column {
            id: fCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(6)

            Text {
              text: "Longest focus blocks"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }

            Repeater {
              model: dayflow.insights.focus_blocks
              delegate: Text {
                width: parent.width
                text: "• " + (modelData.start_str || modelData.start) + "–" + (modelData.end_str || modelData.end) + " " + modelData.title
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
                wrapMode: Text.WordWrap
              }
            }
          }
        }
      }
    }
  }

  ListModel {
    id: settingsCatModel
  }

  Component {
    id: settingsTab
    Flickable {
      width: parent.width
      implicitHeight: Math.min(scol.implicitHeight + Style.space(10), Style.space(420))
      height: implicitHeight
      contentHeight: scol.implicitHeight + Style.space(10)
      clip: true

      Column {
        id: scol
        width: parent.width
        spacing: Style.space(12)

        Text {
          visible: !dayflow.configLoaded
          width: parent.width
          text: "Loading settings..."
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
        }

        Column {
          visible: dayflow.configLoaded
          width: parent.width
          spacing: Style.space(10)

          Text {
            width: parent.width
            text: "AI provider"
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Rectangle {
            width: parent.width
            height: pInput.implicitHeight + Style.space(8)
            radius: Style.cornerRadius
            color: dayflow.fgFill(0.04)
            border.color: dayflow.fgFill(0.12)

            TextInput {
              id: pInput
              anchors.fill: parent
              anchors.margins: Style.space(6)
              text: dayflow.configDraft.provider || "openrouter"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              onTextChanged: dayflow.configDraft.provider = text
            }
          }

          Rectangle {
            width: parent.width
            height: mInput.implicitHeight + Style.space(8)
            radius: Style.cornerRadius
            color: dayflow.fgFill(0.04)
            border.color: dayflow.fgFill(0.12)

            TextInput {
              id: mInput
              anchors.fill: parent
              anchors.margins: Style.space(6)
              text: dayflow.configDraft.model || ""
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              onTextChanged: dayflow.configDraft.model = text
            }
          }

          Rectangle {
            width: parent.width
            height: keyInput.implicitHeight + Style.space(8)
            radius: Style.cornerRadius
            color: dayflow.fgFill(0.04)
            border.color: dayflow.fgFill(0.12)

            TextInput {
              id: keyInput
              anchors.fill: parent
              anchors.margins: Style.space(6)
              text: dayflow.configDraft.openrouter_api_key || ""
              echoMode: TextInput.Password
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              onTextChanged: dayflow.configDraft.openrouter_api_key = text
            }
          }

          Rectangle {
            width: parent.width
            height: urlInput.implicitHeight + Style.space(8)
            radius: Style.cornerRadius
            color: dayflow.fgFill(0.04)
            border.color: dayflow.fgFill(0.12)

            TextInput {
              id: urlInput
              anchors.fill: parent
              anchors.margins: Style.space(6)
              text: dayflow.configDraft.api_base_url || ""
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              onTextChanged: dayflow.configDraft.api_base_url = text
            }
          }

          Text {
            width: parent.width
            text: "Capture"
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Row {
            width: parent.width
            spacing: Style.space(6)

            Column {
              width: (parent.width - 2 * parent.spacing) / 3
              spacing: Style.space(2)
              Text { text: "Interval (s)"; color: dayflow.dim; font.family: dayflow.fontFamily; font.pixelSize: Style.font.caption }
              Rectangle {
                width: parent.width
                height: ciInput.implicitHeight + Style.space(6)
                radius: Style.cornerRadius
                color: dayflow.fgFill(0.04)
                border.color: dayflow.fgFill(0.12)
                TextInput {
                  id: ciInput
                  anchors.fill: parent
                  anchors.margins: Style.space(4)
                  text: String(dayflow.configDraft.capture_interval_sec || 10)
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.body
                  inputMethodHints: Qt.ImhDigitsOnly
                  onTextChanged: dayflow.configDraft.capture_interval_sec = parseInt(text, 10) || 0
                }
              }
            }
            Column {
              width: (parent.width - 2 * parent.spacing) / 3
              spacing: Style.space(2)
              Text { text: "Block (min)"; color: dayflow.dim; font.family: dayflow.fontFamily; font.pixelSize: Style.font.caption }
              Rectangle {
                width: parent.width
                height: bmInput.implicitHeight + Style.space(6)
                radius: Style.cornerRadius
                color: dayflow.fgFill(0.04)
                border.color: dayflow.fgFill(0.12)
                TextInput {
                  id: bmInput
                  anchors.fill: parent
                  anchors.margins: Style.space(4)
                  text: String(dayflow.configDraft.block_minutes || 15)
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.body
                  inputMethodHints: Qt.ImhDigitsOnly
                  onTextChanged: dayflow.configDraft.block_minutes = parseInt(text, 10) || 0
                }
              }
            }
            Column {
              width: (parent.width - 2 * parent.spacing) / 3
              spacing: Style.space(2)
              Text { text: "Frames/block"; color: dayflow.dim; font.family: dayflow.fontFamily; font.pixelSize: Style.font.caption }
              Rectangle {
                width: parent.width
                height: fpbInput.implicitHeight + Style.space(6)
                radius: Style.cornerRadius
                color: dayflow.fgFill(0.04)
                border.color: dayflow.fgFill(0.12)
                TextInput {
                  id: fpbInput
                  anchors.fill: parent
                  anchors.margins: Style.space(4)
                  text: String(dayflow.configDraft.frames_per_block || 30)
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.body
                  inputMethodHints: Qt.ImhDigitsOnly
                  onTextChanged: dayflow.configDraft.frames_per_block = parseInt(text, 10) || 0
                }
              }
            }
          }

          Row {
            width: parent.width
            spacing: Style.space(6)
            Column {
              width: (parent.width - 2 * parent.spacing) / 3
              spacing: Style.space(2)
              Text { text: "JPEG quality"; color: dayflow.dim; font.family: dayflow.fontFamily; font.pixelSize: Style.font.caption }
              Rectangle {
                width: parent.width
                height: jqInput.implicitHeight + Style.space(6)
                radius: Style.cornerRadius
                color: dayflow.fgFill(0.04)
                border.color: dayflow.fgFill(0.12)
                TextInput {
                  id: jqInput
                  anchors.fill: parent
                  anchors.margins: Style.space(4)
                  text: String(dayflow.configDraft.jpeg_quality || 55)
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.body
                  inputMethodHints: Qt.ImhDigitsOnly
                  onTextChanged: dayflow.configDraft.jpeg_quality = parseInt(text, 10) || 0
                }
              }
            }
            Column {
              width: (parent.width - 2 * parent.spacing) / 3
              spacing: Style.space(2)
              Text { text: "Retention (days)"; color: dayflow.dim; font.family: dayflow.fontFamily; font.pixelSize: Style.font.caption }
              Rectangle {
                width: parent.width
                height: rdInput.implicitHeight + Style.space(6)
                radius: Style.cornerRadius
                color: dayflow.fgFill(0.04)
                border.color: dayflow.fgFill(0.12)
                TextInput {
                  id: rdInput
                  anchors.fill: parent
                  anchors.margins: Style.space(4)
                  text: String(dayflow.configDraft.retention_days || 7)
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.body
                  inputMethodHints: Qt.ImhDigitsOnly
                  onTextChanged: dayflow.configDraft.retention_days = parseInt(text, 10) || 0
                }
              }
            }
            Column {
              width: (parent.width - 2 * parent.spacing) / 3
              spacing: Style.space(2)
              Text { text: "Max storage (MB)"; color: dayflow.dim; font.family: dayflow.fontFamily; font.pixelSize: Style.font.caption }
              Rectangle {
                width: parent.width
                height: msInput.implicitHeight + Style.space(6)
                radius: Style.cornerRadius
                color: dayflow.fgFill(0.04)
                border.color: dayflow.fgFill(0.12)
                TextInput {
                  id: msInput
                  anchors.fill: parent
                  anchors.margins: Style.space(4)
                  text: String(dayflow.configDraft.max_storage_mb || 10240)
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.body
                  inputMethodHints: Qt.ImhDigitsOnly
                  onTextChanged: dayflow.configDraft.max_storage_mb = parseInt(text, 10) || 0
                }
              }
            }
          }

          Text {
            width: parent.width
            text: "Category buckets"
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Text {
            width: parent.width
            text: "The model uses these descriptions to classify activity."
            color: dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }

          Repeater {
            model: settingsCatModel
            delegate: Column {
              width: parent.width
              spacing: Style.space(4)

              Row {
                width: parent.width
                spacing: Style.space(6)

                Rectangle {
                  width: parent.width * 0.28
                  height: catName.implicitHeight + Style.space(6)
                  radius: Style.cornerRadius
                  color: dayflow.fgFill(0.04)
                  border.color: dayflow.fgFill(0.12)
                  TextInput {
                    id: catName
                    anchors.fill: parent
                    anchors.margins: Style.space(4)
                    text: model.name
                    color: dayflow.foreground
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.body
                    onEditingFinished: settingsCatModel.setProperty(model.index, "name", text)
                  }
                }

                Rectangle {
                  width: parent.width - parent.width * 0.28 - remBtn.width - 2 * parent.spacing
                  height: catDesc.implicitHeight + Style.space(6)
                  radius: Style.cornerRadius
                  color: dayflow.fgFill(0.04)
                  border.color: dayflow.fgFill(0.12)
                  TextInput {
                    id: catDesc
                    anchors.fill: parent
                    anchors.margins: Style.space(4)
                    text: model.description
                    color: dayflow.foreground
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.body
                    onEditingFinished: settingsCatModel.setProperty(model.index, "description", text)
                  }
                }

                Rectangle {
                  id: remBtn
                  width: delText.implicitWidth + Style.space(12)
                  height: catName.height
                  radius: Style.cornerRadius
                  color: dayflow.btnBg(maDel.containsMouse)
                  border.color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground

                  Text {
                    id: delText
                    anchors.centerIn: parent
                    text: "×"
                    color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.body
                  }
                  MouseArea {
                    id: maDel
                    anchors.fill: parent
                    hoverEnabled: true
                    onClicked: settingsCatModel.remove(model.index)
                  }
                }
              }
            }
          }

          Rectangle {
            width: addBtnText.implicitWidth + Style.space(16)
            height: addBtnText.implicitHeight + Style.space(8)
            radius: Style.cornerRadius
            color: dayflow.btnBg(addMa.containsMouse)
            border.color: dayflow.accentFill(0.5)

            Text {
              id: addBtnText
              anchors.centerIn: parent
              text: "+ Add category"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
            }
            MouseArea {
              id: addMa
              anchors.fill: parent
              hoverEnabled: true
              onClicked: settingsCatModel.append({ name: "", description: "" })
            }
          }

          Text {
            width: parent.width
            text: "LLM classification instructions"
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Text {
            width: parent.width
            text: "Extra instructions appended to every summarization prompt."
            color: dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }

          Rectangle {
            width: parent.width
            height: Math.min(promptBox.implicitHeight + Style.space(10), Style.space(120))
            radius: Style.cornerRadius
            color: dayflow.fgFill(0.04)
            border.color: dayflow.fgFill(0.12)

            TextEdit {
              id: promptBox
              anchors.fill: parent
              anchors.margins: Style.space(6)
              text: dayflow.configDraft.classification_prompt || ""
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              wrapMode: TextEdit.Wrap
              onTextChanged: dayflow.configDraft.classification_prompt = text
            }
          }

          Row {
            width: parent.width
            spacing: Style.space(6)

            Rectangle {
              width: saveText.implicitWidth + Style.space(16)
              height: saveText.implicitHeight + Style.space(8)
              radius: Style.cornerRadius
              color: dayflow.accentFill(0.16)
              border.color: dayflow.accentFill(0.5)

              Text {
                id: saveText
                anchors.centerIn: parent
                text: "Save settings"
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }
              MouseArea {
                anchors.fill: parent
                onClicked: dayflow.saveConfig()
              }
            }

            Rectangle {
              width: resetText.implicitWidth + Style.space(16)
              height: resetText.implicitHeight + Style.space(8)
              radius: Style.cornerRadius
              color: dayflow.btnBg(resetMa.containsMouse)
              border.color: dayflow.fgFill(0.12)

              Text {
                id: resetText
                anchors.centerIn: parent
                text: "Reload"
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
              }
              MouseArea {
                id: resetMa
                anchors.fill: parent
                hoverEnabled: true
                onClicked: {
                  dayflow.configDraft = dayflow.cloneConfig(dayflow.config)
                  settingsCatModel.clear()
                  for (var i = 0; i < (dayflow.config.categories || []).length; i++) {
                    var c = dayflow.config.categories[i]
                    settingsCatModel.append({ name: c.name || "", description: c.description || "", color: c.color || "" })
                  }
                }
              }
            }
          }

          Text {
            width: parent.width
            text: dayflow.notice
            color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
            visible: text !== ""
          }
        }
      }
    }
  }

  Component {
    id: chatTab
    Loader {
      width: parent.width
      height: item ? item.implicitHeight : Style.space(460)
      source: "ChatTab.qml"
      property var panel: dayflow
    }
  }

  Component {
    id: settingsTabNew
    Loader {
      width: parent.width
      height: item ? item.implicitHeight : Style.space(460)
      source: "Settings.qml"
      property var panel: dayflow
    }
  }

  // ---- helpers ----
  Component {
    id: sectionList
    Rectangle {
      visible: items.length > 0
      width: parent ? parent.width : 0
      implicitHeight: sCol.implicitHeight + Style.space(16)
      height: implicitHeight
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)
      property var items: []
      property string title: ""

      Column {
        id: sCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(6)

        Text {
          text: title
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        Repeater {
          model: items
          delegate: Row {
            width: parent.width
            spacing: Style.space(8)

            Text {
              width: parent.width - mins.implicitWidth - parent.spacing
              text: modelData.display || modelData.name
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
              anchors.verticalCenter: parent.verticalCenter
            }

            Text {
              id: mins
              text: dayflow.fmtHours(modelData.minutes) + " hr"
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              anchors.verticalCenter: parent.verticalCenter
            }
          }
        }
      }
    }
  }

  KeyboardPanel {
    id: panel
    anchorItem: dayflow.anchorItem
    owner: dayflow.hostWidget || dayflow
    bar: dayflow.bar
    open: dayflow.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(dayflow.expanded ? Style.space(560) : Style.space(340))
    contentHeight: panel.fittedContentHeight(content.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onCloseRequested: dayflow.close()
      onTabRequested: function(direction) { dayflow.switchPanel(direction) }

      Column {
        id: content
        width: parent.width
        leftPadding: Style.space(12)
        rightPadding: Style.space(12)
        topPadding: Style.space(12)
        bottomPadding: Style.space(12)
        spacing: Style.space(10)

        // ---- header ----
        Row {
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(8)

          Column {
            width: parent.width - toggleBtn.width - expandBtn.width - Style.space(6) - parent.spacing
            spacing: Style.space(2)

            Row {
              spacing: Style.space(6)

              Rectangle {
                width: Style.space(7)
                height: Style.space(7)
                radius: width / 2
                anchors.verticalCenter: parent.verticalCenter
                color: dayflow.paused ? Color.urgent : Color.accent
              }

              Text {
                text: "Dayflow"
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.subtitle
                font.bold: true
              }
            }

            Text {
              text: (dayflow.paused ? "paused" : "recording") + " · " + dayflow.modelShort()
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
              width: parent.width
            }
          }

          Rectangle {
            id: expandBtn
            height: Style.space(28)
            width: exg.implicitWidth + Style.space(14)
            radius: Style.cornerRadius
            color: mexg.containsMouse ? dayflow.accentFill(0.12) : "transparent"
            border.color: dayflow.accentFill(0.5)

            Text {
              id: exg
              anchors.centerIn: parent
              text: dayflow.expanded ? "Shrink" : "Expand"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: mexg
              anchors.fill: parent
              hoverEnabled: true
              onClicked: dayflow.expanded = !dayflow.expanded
            }
          }

          Rectangle {
            id: toggleBtn
            height: Style.space(28)
            width: tgl.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: dayflow.paused
              ? dayflow.accentFill(0.15)
              : (mtgl.containsMouse ? dayflow.accentFill(0.12) : "transparent")
            border.color: dayflow.accentFill(0.5)

            Text {
              id: tgl
              anchors.centerIn: parent
              text: dayflow.paused ? "Resume capture" : "Pause"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: mtgl
              anchors.fill: parent
              hoverEnabled: true
              onClicked: toggleProc.running = true
            }
          }
        }

        // ---- tab bar ----
        Row {
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(6)

          Repeater {
            model: ["today", "standup", "chat", "week", "settings"]

            delegate: Rectangle {
              height: Style.space(28)
              width: tabLabel.implicitWidth + Style.space(16)
              radius: Style.cornerRadius
              color: dayflow.currentTab === modelData
                ? dayflow.accentFill(0.12)
                : (tabMouse.containsMouse
                    ? dayflow.accentFill(0.06)
                    : "transparent")
              border.color: dayflow.currentTab === modelData
                ? dayflow.accentFill(0.45)
                : "transparent"

              Text {
                id: tabLabel
                anchors.centerIn: parent
                text: modelData.charAt(0).toUpperCase() + modelData.slice(1)
                color: dayflow.currentTab === modelData ? dayflow.foreground : dayflow.dim
                font.bold: dayflow.currentTab === modelData
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
              }

              MouseArea {
                id: tabMouse
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: dayflow.currentTab = modelData
              }
            }
          }
        }

        PanelSeparator { foreground: dayflow.foreground }

        // ---- error / not-configured states ----
        Column {
          visible: dayflow.errorText !== "" || !dayflow.configured
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(6)

          Text {
            visible: dayflow.errorText !== ""
            width: parent.width
            text: "! " + dayflow.errorText
            color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Text {
            visible: !dayflow.configured
            width: parent.width
            text: "Not configured yet. Run `dayflow setup` in a terminal."
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }
        }

        // ---- content ----
        Loader {
          id: tabLoader
          width: parent.width - content.leftPadding - content.rightPadding
          height: item ? item.implicitHeight : Style.space(120)
          sourceComponent: dayflow.currentTab === "today" ? todayTab
            : dayflow.currentTab === "standup" ? standupTab
            : dayflow.currentTab === "chat" ? chatTab
            : dayflow.currentTab === "week" ? weekTab
            : settingsTabNew
        }

        PanelSeparator { foreground: dayflow.foreground }

        // ---- quick actions (pause/resume lives in the header) ----
        Flow {
          width: parent.width - content.leftPadding - content.rightPadding
          height: implicitHeight
          spacing: Style.space(6)

          Rectangle {
            height: Style.space(26)
            width: a2.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: dayflow.btnBg(m2.containsMouse)
            border.color: dayflow.accentFill(0.5)
            opacity: dayflow.activeApp !== "" ? 1 : 0.45
            Text {
              id: a2
              anchors.centerIn: parent
              text: "Ignore current app"
              color: dayflow.activeApp !== "" ? dayflow.foreground : dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: m2
              anchors.fill: parent
              hoverEnabled: true
              enabled: dayflow.activeApp !== ""
              onClicked: { if (!ignoreProc.running) ignoreProc.running = true }
            }
          }

          Rectangle {
            height: Style.space(26)
            width: a3.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: dayflow.btnBg(m3.containsMouse)
            border.color: dayflow.accentFill(0.5)
            opacity: (dayflow.blocksPending > 0 || summarizeProc.running) ? 1 : 0.45
            Text {
              id: a3
              anchors.centerIn: parent
              text: summarizeProc.running
                ? "Summarizing..."
                : (dayflow.blocksPending > 0
                    ? "Summarize now (" + dayflow.blocksPending + " pending)"
                    : "Summarize now")
              color: (dayflow.blocksPending > 0 || summarizeProc.running) ? dayflow.foreground : dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: m3
              anchors.fill: parent
              hoverEnabled: true
              enabled: dayflow.blocksPending > 0 && !summarizeProc.running
              onClicked: { if (!summarizeProc.running) summarizeProc.running = true }
            }
          }

          Rectangle {
            height: Style.space(26)
            width: a4.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: dayflow.btnBg(m4.containsMouse)
            border.color: dayflow.accentFill(0.5)
            Text {
              id: a4
              anchors.centerIn: parent
              text: "Copy today's journal"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: m4
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { if (!copyProc.running) copyProc.running = true }
            }
          }
        }

        // ---- status ----
        Text {
          width: parent.width - content.leftPadding - content.rightPadding
          text: dayflow.framesToday + " frames · " + dayflow.blocksPending + " pending" +
                (dayflow.storageText !== "" ? " · " + dayflow.storageText : "") +
                (dayflow.ignoredApps.length ? " · ignoring " + dayflow.ignoredApps.map(function(a) { return dayflow.appDisplayName(a) }).join(", ") : "")
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }

        Text {
          visible: dayflow.notice !== ""
          width: parent.width - content.leftPadding - content.rightPadding
          text: dayflow.notice
          color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          wrapMode: Text.WordWrap
        }
      }
    }
  }
}
