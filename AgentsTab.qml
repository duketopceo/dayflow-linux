import QtQuick
import qs.Commons
import qs.Ui

// Compact-panel "Agents" tab — embeds the same AgentsPane the Full view
// rail shows. AgentsPane talks to a `host` carrying the panel plus the
// briefing surface; this shim forwards both from the loader on Panel.
Item {
  id: tab

  property var dayflow: parent && parent.panel ? parent.panel : null

  // host.* contract AgentsPane binds to.
  readonly property var agentBriefing: dayflow ? dayflow.agentBriefing : null
  readonly property var agentSources: dayflow ? dayflow.agentSources : []
  readonly property bool agentRecapsEnabled: dayflow ? dayflow.agentRecapsEnabled : true
  readonly property bool agentsLoading: dayflow ? dayflow.agentsLoading : false
  readonly property string agentsError: dayflow ? dayflow.agentsError : ""
  readonly property real agentsProgress: dayflow ? dayflow.agentProgress : -1
  readonly property string agentsPhase: dayflow ? dayflow.agentPhase : ""
  function agentsLoad(refresh) { if (dayflow) dayflow.agentsLoad(refresh) }

  width: parent ? parent.width : 0
  implicitHeight: Style.space(400)
  height: implicitHeight

  AgentsPane {
    width: parent.width
    height: parent.height
    host: tab
  }
}
