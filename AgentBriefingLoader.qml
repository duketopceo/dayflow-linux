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
  // Queued reload kind while a run is mid-flight ("" | "load" | "refresh").
  property string pending: ""
  property int run: 0
  property bool timedOut: false

  function load(refresh) {
    if (!loader.panel) return
    loader.panel.uilog("agents load " + loader.panel.viewDateStr() + (refresh ? " (refresh)" : ""))
    loader.run = loader.run + 1
    loader.timedOut = false
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
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        // Stale callbacks from a previous process (or a watchdog-killed run)
        // carry old results — ignore them so a fresh run's state and the
        // timeout message survive. (streamFinished can fire on exit.)
        if (agentsProc.runId !== loader.run || loader.timedOut) return
        loader.loading = false
        try {
          var d = JSON.parse(text)
          // Day changed mid-run — this payload is for the old day; the
          // queued reload (drained on exit) owns state now.
          if (loader.panel && d.day !== loader.panel.viewDateStr()) return
          loader.briefing = d
          loader.sources = d.sources || []
          loader.recapsEnabled = d.recaps_enabled !== false
          loader.error = ""
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
  Timer {
    id: agentsWatchdog
    interval: 300000
    running: agentsProc.running
    repeat: false
    onTriggered: {
      if (loader.panel) loader.panel.uilog("agents watchdog: killing hung agentsProc")
      loader.pending = ""
      loader.timedOut = true
      agentsProc.running = false
      loader.loading = false
      loader.briefing = null
      loader.sources = []
      loader.error = "agent briefing timed out"
    }
  }
}
