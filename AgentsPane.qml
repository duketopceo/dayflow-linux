import QtQuick
import qs.Commons
import qs.Ui

// FullView "Agents" pane — the day's agent briefing: sessions grouped into
// workstreams (LLM-named when recaps are on, project-grouped otherwise),
// each thread carrying a derived status and a condensed turn narrative.
// `host` is the FullView window, which owns the briefing payload and the
// loader process (`dayflow briefing <day> --json`).
Column {
  id: pane

  property var host: null
  readonly property var dayflow: host ? host.dayflow : null
  // The briefing payload, or null until first load / after an error.
  readonly property var briefing: host ? host.agentBriefing : null
  readonly property var workstreams: briefing ? (briefing.workstreams || []) : []

  width: parent ? parent.width : 0
  height: parent ? parent.height : 0
  spacing: Style.space(8)
  leftPadding: Style.space(16)
  rightPadding: Style.space(16)
  topPadding: Style.space(4)

  // Column padding positions children but doesn't shrink parent.width —
  // direct children must bind innerW or they overflow the padding on the
  // right (masked in FullView, clipped in the compact popup).
  readonly property real innerW: width - leftPadding - rightPadding

  // Status/highlight palettes — adapted to the panel theme rather than
  // upstream's fixed light palette.
  readonly property var statusMeta: ({
    "inProgress":  { label: "in progress",  color: Qt.rgba(0.40, 0.70, 1.00, 1.0) },
    "reviewReady": { label: "review ready", color: Qt.rgba(0.30, 0.80, 0.55, 1.0) },
    "blocked":     { label: "blocked",      color: Qt.rgba(0.90, 0.35, 0.30, 1.0) },
    "completed":   { label: "completed",    color: "gray" }
  })
  readonly property var highlightMeta: ({
    "keyDecision":    { label: "decision",   color: Qt.rgba(0.62, 0.45, 0.95, 1.0) },
    "keyInfo":        { label: "key info",   color: Qt.rgba(0.35, 0.60, 0.95, 1.0) },
    "readyForReview": { label: "for review", color: Qt.rgba(0.95, 0.55, 0.20, 1.0) }
  })
  function statusColor(status) {
    var m = statusMeta[status]
    if (!m || status === "completed") return pane.dayflow ? pane.dayflow.dim : "gray"
    return m.color
  }
  function statusLabel(status) {
    return statusMeta[status] ? statusMeta[status].label : "completed"
  }
  function highlightColor(kind) {
    return highlightMeta[kind] ? highlightMeta[kind].color : "transparent"
  }
  // Re-alpha a theme/status color for fills and borders.
  function tint(c, a) { return Qt.rgba(c.r, c.g, c.b, a) }

  // Per-status totals across all workstreams — bound expression evaluates
  // once per briefing load, not per legend delegate.
  readonly property var statusTotals: statusTotalsOf(workstreams)
  function statusTotalsOf(ws) {
    var t = { blocked: 0, reviewReady: 0, inProgress: 0, completed: 0 }
    for (var i = 0; i < ws.length; i++) {
      var ths = ws[i].threads || []
      for (var j = 0; j < ths.length; j++) {
        var s = ths[j].status
        if (t[s] !== undefined) t[s]++
      }
    }
    return t
  }

  DayNavRow {
    width: pane.innerW
    dayflow: pane.dayflow
  }

  // Legend + refresh — refresh stays available whenever the pane is hosted
  // so empty and error days can be retried; the legend itself still needs
  // a briefing to describe.
  Item {
    width: pane.innerW
    height: Math.max(legendRow.implicitHeight, refreshText.implicitHeight)
    visible: pane.host !== null

    Row {
      id: legendRow
      spacing: Style.space(10)
      visible: pane.workstreams.length > 0

      Repeater {
        model: ["inProgress", "reviewReady", "blocked", "completed"]
        delegate: Row {
          spacing: Style.space(4)
          visible: pane.statusTotals[modelData] > 0
          Rectangle {
            width: Style.space(8); height: Style.space(8)
            radius: Style.space(4)
            anchors.verticalCenter: parent.verticalCenter
            color: pane.statusColor(modelData)
          }
          Text {
            anchors.verticalCenter: parent.verticalCenter
            text: pane.statusTotals[modelData] + " " + pane.statusLabel(modelData)
            textFormat: Text.PlainText
            color: pane.dayflow ? pane.dayflow.dim : "gray"
            font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
          }
        }
      }
    }

    Text {
      id: refreshText
      anchors.right: parent.right
      anchors.verticalCenter: parent.verticalCenter
      text: "refresh"
      textFormat: Text.PlainText
      color: refreshMa.containsMouse
        ? (pane.dayflow ? pane.dayflow.foreground : "white")
        : (pane.dayflow ? pane.dayflow.dim : "gray")
      font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
      font.pixelSize: Style.font.caption
      font.underline: refreshMa.containsMouse
      MouseArea {
        id: refreshMa
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        enabled: pane.host !== null && !pane.host.agentsLoading
        onClicked: pane.host.agentsLoad(true)
      }
    }
  }

  // Progress surface — hosts that don't expose it (older loaders) fall
  // back to the indeterminate BusyBar.
  readonly property real agentProgress: {
    var p = host ? host.agentsProgress : undefined
    return p === undefined || p === null ? -1 : p
  }
  readonly property string agentPhase: host && host.agentsPhase ? host.agentsPhase : ""

  Column {
    width: pane.innerW
    spacing: Style.space(2)
    visible: host !== null && host.agentsLoading

    BusyBar {
      width: parent.width
      pal: pane.dayflow
      active: parent.visible && pane.agentProgress < 0
      visible: pane.agentProgress < 0
    }
    Rectangle {
      visible: pane.agentProgress >= 0
      width: parent.width
      height: 3
      radius: 1
      color: pane.dayflow ? pane.dayflow.fgFill(0.08) : Qt.rgba(1, 1, 1, 0.08)
      clip: true
      Rectangle {
        width: Math.max(Style.space(6), parent.width * pane.agentProgress)
        height: parent.height
        radius: parent.radius
        color: pane.dayflow ? pane.dayflow.accentFill(0.7) : Qt.rgba(1, 1, 1, 0.5)
        Behavior on width {
          NumberAnimation { duration: 250; easing.type: Easing.OutQuad }
        }
      }
    }
    Text {
      visible: pane.agentPhase !== ""
      text: pane.agentPhase + (pane.agentProgress >= 0 ? " · " + Math.round(pane.agentProgress * 100) + "%" : "")
      textFormat: Text.PlainText
      color: pane.dayflow ? pane.dayflow.dim : "gray"
      font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
      font.pixelSize: Style.font.caption
    }
  }

  Text {
    visible: host !== null && host.agentsError !== ""
    width: pane.innerW
    wrapMode: Text.WordWrap
    text: "! " + (host ? host.agentsError : "")
    textFormat: Text.PlainText
    color: Color.urgent !== undefined ? Color.urgent : "red"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
  }

  Text {
    visible: host !== null && !host.agentsLoading && host.agentsError === "" &&
      pane.briefing !== null && pane.workstreams.length === 0
    width: pane.innerW
    wrapMode: Text.WordWrap
    text: "No Claude Code, Codex, OpenCode, Devin, or Cursor sessions on this day."
    textFormat: Text.PlainText
    color: pane.dayflow ? pane.dayflow.dim : "gray"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.body
  }

  // One dim line per store that scanned unavailable or drifted — an empty
  // briefing caused by a broken/moved store must not read as a quiet day.
  readonly property string agentSourceNote: {
    var srcs = host && host.agentSources ? host.agentSources : []
    var lines = []
    for (var i = 0; i < srcs.length; i++) {
      var s = srcs[i]
      if (s.status === "unavailable" || s.drift === true) {
        var note = (s.note && s.note !== "") ? s.note : "store unavailable"
        lines.push(s.source + ": " + note + (s.drift === true ? " — may have drifted" : ""))
      }
    }
    return lines.join("\n")
  }

  // Opt-in hint: the model prose pass sends a bounded, scrubbed transcript
  // skeleton to the configured provider; the deterministic briefing always
  // renders either way.
  Text {
    visible: host !== null && host.agentRecapsEnabled === false && !host.agentsLoading
    text: "Recaps are off — transcripts stay local and this briefing stays deterministic.\nEnable model-written prose with: dayflow config set agent_recaps true"
    textFormat: Text.PlainText
    wrapMode: Text.WordWrap
    width: pane.innerW
    color: pane.dayflow ? pane.dayflow.dim : "gray"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
  }

  Text {
    visible: host !== null && !host.agentsLoading && pane.agentSourceNote !== ""
    text: pane.agentSourceNote
    textFormat: Text.PlainText
    wrapMode: Text.WordWrap
    width: pane.innerW
    color: pane.dayflow ? pane.dayflow.dim : "gray"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
  }

  Flickable {
    width: pane.innerW
    // Size from space actually consumed above (nav, legend, error, notes)
    // rather than a constant — optional rows vary in count and wrap.
    height: Math.max(0, parent.height - y)
    contentHeight: wsCol.implicitHeight
    clip: true

    Column {
      id: wsCol
      width: parent.width
      spacing: Style.space(14)

      Repeater {
        model: pane.workstreams

        delegate: Column {
          id: wsSection
          required property var modelData
          width: wsCol.width
          spacing: Style.space(6)

          // Workstream header: name + summary + accomplishment bullets.
          Text {
            text: wsSection.modelData.name
            textFormat: Text.PlainText
            color: pane.dayflow ? pane.dayflow.foreground : "white"
            font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
            font.pixelSize: Style.font.title
            font.bold: true
            elide: Text.ElideRight
            width: parent.width
          }
          Text {
            visible: !!wsSection.modelData.summary
            text: wsSection.modelData.summary || ""
            textFormat: Text.PlainText
            color: pane.dayflow ? pane.dayflow.dim : "gray"
            font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
            width: parent.width
          }
          Repeater {
            model: wsSection.modelData.bullets || []
            delegate: Text {
              required property var modelData
              text: "• " + modelData
              textFormat: Text.PlainText
              color: pane.dayflow ? pane.dayflow.foreground : "white"
              font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
              wrapMode: Text.WordWrap
              width: wsSection.width
              opacity: 0.85
            }
          }

          // Thread cards.
          Repeater {
            model: wsSection.modelData.threads || []

            delegate: Rectangle {
              id: threadCard
              required property var modelData
              width: wsSection.width
              height: cardInner.implicitHeight + Style.space(16)
              radius: Style.cornerRadius
              property color statusTint: pane.statusColor(modelData.status)
              color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
              border.color: pane.tint(statusTint, 0.35)

              Column {
                id: cardInner
                width: parent.width - Style.space(16)
                anchors.centerIn: parent
                spacing: Style.space(3)

                Item {
                  width: parent.width
                  height: headerRow.implicitHeight

                  Row {
                    id: headerRow
                    spacing: Style.space(8)
                    width: parent.width - timeText.implicitWidth - Style.space(12)

                    Rectangle {
                      id: sourceBadge
                      height: Style.space(18)
                      width: badgeText.implicitWidth + Style.space(12)
                      radius: Style.cornerRadius
                      anchors.verticalCenter: parent.verticalCenter
                      color: threadCard.modelData.source === "claude"
                        ? Qt.rgba(0.85, 0.55, 0.25, 0.2)
                        : Qt.rgba(0.30, 0.65, 0.85, 0.2)
                      Text {
                        id: badgeText
                        anchors.centerIn: parent
                        text: threadCard.modelData.source
                        textFormat: Text.PlainText
                        color: threadCard.modelData.source === "claude"
                          ? Qt.rgba(0.95, 0.70, 0.40, 1.0)
                          : Qt.rgba(0.45, 0.80, 1.0, 1.0)
                        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                        font.pixelSize: Style.font.caption
                        font.bold: true
                      }
                    }

                    Rectangle {
                      id: statusChip
                      height: Style.space(18)
                      width: statusText.implicitWidth + Style.space(12)
                      radius: Style.cornerRadius
                      anchors.verticalCenter: parent.verticalCenter
                      color: pane.tint(threadCard.statusTint, 0.18)
                      Text {
                        id: statusText
                        anchors.centerIn: parent
                        text: pane.statusLabel(threadCard.modelData.status)
                        textFormat: Text.PlainText
                        color: threadCard.statusTint
                        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                        font.pixelSize: Style.font.caption
                      }
                    }

                    Text {
                      anchors.verticalCenter: parent.verticalCenter
                      text: threadCard.modelData.title
                      textFormat: Text.PlainText
                      color: pane.dayflow ? pane.dayflow.foreground : "white"
                      font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                      font.pixelSize: Style.font.body
                      font.bold: true
                      elide: Text.ElideRight
                      width: parent.width - sourceBadge.width - statusChip.width
                             - Style.space(24)
                    }
                  }

                  Text {
                    id: timeText
                    anchors.right: parent.right
                    anchors.verticalCenter: headerRow.verticalCenter
                    text: Qt.formatTime(new Date(threadCard.modelData.started_at * 1000), "hh:mm") +
                          "–" +
                          Qt.formatTime(new Date(threadCard.modelData.ended_at * 1000), "hh:mm")
                    textFormat: Text.PlainText
                    color: pane.dayflow ? pane.dayflow.dim : "gray"
                    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                  }
                }

                Text {
                  width: parent.width
                  visible: !!threadCard.modelData.latest_outcome
                  text: threadCard.modelData.latest_outcome || ""
                  textFormat: Text.PlainText
                  wrapMode: Text.WordWrap
                  color: pane.dayflow ? pane.dayflow.foreground : "white"
                  font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                  font.pixelSize: Style.font.caption
                  font.italic: true
                  opacity: 0.85
                }

                // Condensed turn narrative with highlight pills.
                Column {
                  width: parent.width
                  spacing: Style.space(2)
                  visible: (threadCard.modelData.turns || []).length > 0

                  Repeater {
                    model: threadCard.modelData.turns || []
                    delegate: Row {
                      id: turnRow
                      required property var modelData
                      property color hlTint: pane.highlightColor(modelData.highlight)
                      width: parent.width
                      spacing: Style.space(6)

                      Text {
                        width: Style.space(28)
                        anchors.top: parent.top
                        text: turnRow.modelData.role === "user" ? "you" : "agent"
                        textFormat: Text.PlainText
                        color: pane.dayflow ? pane.dayflow.dim : "gray"
                        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                        font.pixelSize: Style.font.caption
                        opacity: 0.7
                      }
                      Text {
                        width: parent.width - Style.space(34)
                             - (hlPill.visible ? hlPill.width + Style.space(6) : 0)
                             - (artifactLink.visible ? artifactLink.width + Style.space(6) : 0)
                        text: turnRow.modelData.text
                        textFormat: Text.PlainText
                        wrapMode: Text.WordWrap
                        color: turnRow.modelData.role === "user"
                          ? (pane.dayflow ? pane.dayflow.dim : "gray")
                          : (pane.dayflow ? pane.dayflow.foreground : "white")
                        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                        font.pixelSize: Style.font.caption
                      }
                      Text {
                        id: artifactLink
                        visible: !!turnRow.modelData.artifact_path
                        anchors.top: parent.top
                        text: turnRow.modelData.artifact_name
                              || turnRow.modelData.artifact_path || ""
                        textFormat: Text.PlainText
                        color: pane.dayflow ? pane.dayflow.dim : "gray"
                        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                        font.pixelSize: Style.font.caption
                        font.underline: artifactMouse.containsMouse
                        MouseArea {
                          id: artifactMouse
                          anchors.fill: parent
                          hoverEnabled: true
                          cursorShape: Qt.PointingHandCursor
                          // Open the containing directory, not the file —
                          // transcript-derived paths could point at
                          // executables/.desktop files. Engine stores
                          // absolute paths only.
                          onClicked: {
                            var p = turnRow.modelData.artifact_path || ""
                            if (p[0] !== "/") return
                            var dir = p.substring(0, p.lastIndexOf("/")) || "/"
                            Qt.openUrlExternally("file://" + encodeURI(dir))
                          }
                        }
                      }
                      Rectangle {
                        id: hlPill
                        visible: !!pane.highlightMeta[turnRow.modelData.highlight]
                        height: Style.space(16)
                        width: hlText.implicitWidth + Style.space(8)
                        radius: Style.cornerRadius
                        color: pane.tint(turnRow.hlTint, 0.18)
                        Text {
                          id: hlText
                          anchors.centerIn: parent
                          text: pane.highlightMeta[turnRow.modelData.highlight]
                                ? pane.highlightMeta[turnRow.modelData.highlight].label : ""
                          textFormat: Text.PlainText
                          color: turnRow.hlTint
                          font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                          font.pixelSize: Style.font.caption
                        }
                      }
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}
