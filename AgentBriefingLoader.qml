import QtQuick
import Quickshell.Io

// Shared briefing loader — owns the `dayflow briefing --json` process,
// pending-queue, watchdog, and payload state for whichever host embeds it
// (FullView's section rail, the compact panel's Agents tab). Hosts set
// `panel` to their panel root (needs uilog + viewDateStr + dayOffset).
Item {
  id: loader

  property var panel: null

  // The day's briefing payload from `dayflow briefing --json`:
  // {day, mode, recaps_enabled, workstreams[], sources[]} — null until the
  // first load so callers can distinguish "not loaded" from "empty day".
  property var briefing: null
  // Per-source scan status ({source, sessions, status, note?, drift?}) —
  // surfaced so an unavailable or drifted store doesn't read as a quiet day.
  property var sources: []
  // recaps_enabled from briefing --json. Defaults true so a pre-field
  // binary doesn't flash the opt-in hint; false only when the engine says.
  property bool recapsEnabled: true
  property bool loading: false
  property string error: ""
  // Determinate progress: -1 while no events have arrived (binaries
  // predating progress emission stay indeterminate), 0..1 otherwise.
  property real progress: -1
  // Machine phase key ("scan"|"decode"|"polish") and the display label.
  property string phase: ""
  property string phaseLabel: ""
  // Queued reload kind while a run is mid-flight ("" | "load" | "refresh").
  property string pending: ""
  property int run: 0
  property bool timedOut: false
  // Seconds since the current run started — the heartbeat that keeps a
  // long decode/polish phase reading as working, not frozen.
  property int elapsedSec: 0
  // A refresh was requested and its result hasn't landed yet — the
  // "refresh" affordance gets a visible ack instead of a dead press.
  // Cleared unconditionally wherever the current run settles: a drained
  // plain load after invalidate() must release the ack too, not just
  // refresh-flagged runs.
  property bool refreshing: false

  // One JSON progress line per stderr write — see agentProgress in the
  // engine. Non-JSON stderr noise is ignored.
  function handleProgress(line) {
    // Buffered lines from a killed/superseded run must not restamp phase
    // or creep the bar backward onto the fresh run (FullView's parser
    // carries the same guard).
    if (agentsProc.runId !== loader.run || loader.timedOut) return
    var ev
    try {
      ev = JSON.parse(line)
    } catch (e) {
      return
    }
    if (!ev || typeof ev.phase !== "string") return
    var n = ev.n || 0
    var i = ev.i || 0
    loader.phase = ev.phase
    if (ev.phase === "scan") {
      loader.phaseLabel = "scanning " + (ev.source || "stores")
      // A tick event is the engine's in-slot heartbeat — creep toward the
      // slot's end instead of snapping back to its start.
      var scanEnd = n > 0 ? 0.05 + 0.10 * ((i + 1) / n) : 0.05
      loader.progress = ev.tick
        ? Math.min(Math.max(loader.progress, 0.05) + 0.003, scanEnd)
        : (n > 0 ? 0.05 + 0.10 * (i / n) : 0.05)
    } else if (ev.phase === "decode") {
      loader.phaseLabel = "reading session " + i + " of " + n
      var decEnd = n > 0 ? 0.15 + 0.60 * (i / n) : 0.15
      loader.progress = ev.tick
        ? Math.min(Math.max(loader.progress, 0.15) + 0.006, decEnd)
        : decEnd
    } else if (ev.phase === "polish") {
      // One long model call — ticks creep 0.8 → 0.97 while it runs.
      loader.phaseLabel = "writing briefing"
      loader.progress = ev.tick
        ? Math.min(Math.max(loader.progress, 0.8) + 0.005, 0.97)
        : Math.max(loader.progress, 0.8)
    }
  }

  function load(refresh) {
    if (!loader.panel) return
    loader.panel.uilog("agents load " + loader.panel.viewDateStr() + (refresh ? " (refresh)" : ""))
    loader.progress = -1
    loader.phase = ""
    loader.phaseLabel = ""
    loader.run = loader.run + 1
    loader.timedOut = false
    loader.elapsedSec = 0
    if (refresh) loader.refreshing = true
    var cmd = ["dayflow", "briefing", loader.panel.viewDateStr(), "--json"]
    if (refresh) cmd.push("--refresh")
    agentsProc.command = cmd
    loader.loading = true
    if (agentsProc.running) {
      if (refresh === true || loader.pending !== "refresh")
        loader.pending = refresh === true ? "refresh" : "load"
    } else {
      // Stamp the run on the process only at actual start — callbacks from a
      // still-terminating previous run then fail the runId check.
      agentsProc.runId = loader.run
      agentsProc.running = true
    }
  }

  // Runs the pending reload/refresh recorded by load when a process was
  // mid-flight. A queued refresh outranks a queued load.
  function drainPending() {
    var p = loader.pending
    loader.pending = ""
    if (p !== "") loader.load(p === "refresh")
  }

  // Day changed — the loaded payload and any in-flight run are stale.
  // Bumping run unconditionally stale-marks even a just-started run whose
  // callbacks haven't fired; a queued reload is only needed mid-flight.
  function invalidate() {
    loader.briefing = null
    loader.sources = []
    loader.error = ""
    loader.progress = -1
    loader.phase = ""
    loader.phaseLabel = ""
    // The ack'd request's context is gone — a queued refresh still runs,
    // but an in-flight refresh's press must not outlive its day.
    if (loader.pending !== "refresh") loader.refreshing = false
    loader.run = loader.run + 1
    if (loader.loading && loader.pending !== "refresh") loader.pending = "load"
  }

  // Hosts own the reload decision (visible section/tab only they know);
  // the loader just drops stale state.
  Connections {
    target: loader.panel
    function onDayOffsetChanged() { loader.invalidate() }
  }

  Process {
    id: agentsProc
    property int runId: 0
    command: ["dayflow", "briefing", "--json"]
    // stderr carries one JSON progress event per line — drives the
    // determinate bar in AgentsPane.
    stderr: SplitParser {
      onRead: function(line) { loader.handleProgress(line) }
    }
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        // Stale callbacks from a previous process (or a watchdog-killed run)
        // carry old results — ignore them so a fresh run's state and the
        // timeout message survive. (streamFinished can fire on exit.)
        if (agentsProc.runId !== loader.run || loader.timedOut) return
        loader.loading = false
        // Any current-run settle resolves the press — a drained plain load
        // after invalidate() would otherwise leave "refreshing…" + a dead
        // button for the rest of the session.
        if (loader.pending !== "refresh") loader.refreshing = false
        try {
          var d = JSON.parse(text)
          // Day changed mid-run — this payload is for the old day; the
          // queued reload (drained on exit) owns state now.
          if (loader.panel && d.day !== loader.panel.viewDateStr()) return
          loader.briefing = d
          loader.sources = d.sources || []
          loader.recapsEnabled = d.recaps_enabled !== false
          loader.error = ""
          loader.progress = 1
          loader.phase = ""
          loader.phaseLabel = ""
        } catch (e) {
          loader.briefing = null
          loader.sources = []
          loader.error = "could not load agent briefing"
        }
      }
    }
    onExited: function(exitCode) {
      if (agentsProc.runId !== loader.run) {
        // A previous process's exit — never touch the new run's state, but
        // still service the queued reload it was waiting on.
        Qt.callLater(loader.drainPending)
        return
      }
      loader.loading = false
      if (loader.pending !== "refresh") loader.refreshing = false
      if (exitCode !== 0 && !loader.timedOut) {
        loader.briefing = null
        loader.sources = []
        loader.error = "agent briefing failed"
      }
      // Defer the drain until every callback for this dead run has fired —
      // restarting here would restamp runId and let the old run's remaining
      // callbacks (exited/runningChanged/streamFinished) misattribute to it.
      Qt.callLater(loader.drainPending)
    }
    // FailedToStart emits neither exited nor streamFinished — clear the
    // flag and drain the queue so the bar never sticks.
    onRunningChanged: {
      if (!agentsProc.running) {
        if (agentsProc.runId !== loader.run) {
          Qt.callLater(loader.drainPending)
          return
        }
        loader.loading = false
        if (loader.pending !== "refresh") loader.refreshing = false
        // If neither exited nor streamFinished ran (FailedToStart — e.g.
        // dayflow missing from PATH), no callback sets an error: check
        // after they settle so the pane reports failure, not blank.
        Qt.callLater(function() {
          if (agentsProc.runId === loader.run && !agentsProc.running
              && loader.briefing === null && loader.error === ""
              && loader.pending === "")
            loader.error = "agent briefing failed"
        })
        Qt.callLater(loader.drainPending)
      }
    }
  }

  // Watchdog: the engine bounds recap generation internally (~45s), but a
  // wedged provider or hung scan could still outlive it — kill the process
  // and surface the metadata-only state rather than spinning forever.
  // pending is cleared BEFORE stopping so onRunningChanged doesn't see a
  // queued reload and restart the proc it just killed; timedOut makes
  // onExited keep the timeout message instead of overwriting it.
  // The polish model call is one indeterminate stretch (~10–55s under
  // polishDeadline) — creep the bar so it doesn't look frozen.
  Timer {
    interval: 700
    repeat: true
    running: loader.loading && loader.phase === "polish"
    onTriggered: loader.progress = Math.min(0.97, loader.progress + 0.01)
  }

  Timer {
    id: agentsWatchdog
    // A cold first build decodes every session's turns plus the polish
    // call — measured ~75s on a 7GB Devin store, so 75s killed real runs.
    interval: 180000
    running: agentsProc.running
    repeat: false
    onTriggered: {
      if (loader.panel) loader.panel.uilog("agents watchdog: killing hung agentsProc")
      loader.pending = ""
      loader.timedOut = true
      agentsProc.running = false
      loader.loading = false
      if (loader.pending !== "refresh") loader.refreshing = false
      loader.briefing = null
      loader.sources = []
      loader.error = "agent briefing timed out"
    }
  }

  // Elapsed heartbeat — ticks only while a run is live so a minutes-long
  // decode/polish shows "Ns elapsed" instead of a parked bar.
  Timer {
    interval: 1000
    repeat: true
    running: loader.loading
    onTriggered: loader.elapsedSec += 1
  }
}
