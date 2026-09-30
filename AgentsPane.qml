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

  // Status chip palette — adapted to the panel theme rather than upstream's
  // fixed light palette.
  function statusColor(status) {
    switch (status) {
    case "blocked":      return Qt.rgba(0.90, 0.35, 0.30, 1.0)
    case "reviewReady":  return Qt.rgba(0.30, 0.80, 0.55, 1.0)
    case "inProgress":   return Qt.rgba(0.40, 0.70, 1.00, 1.0)
    default:             return pane.dayflow ? pane.dayflow.dim : "gray"
    }
  }
  function statusLabel(status) {
    switch (status) {
    case "blocked":      return "blocked"
    case "reviewReady":  return "review ready"
    case "inProgress":   return "in progress"
    default:             return "completed"
    }
  }
  function highlightColor(kind) {
    switch (kind) {
    case "keyDecision":    return Qt.rgba(0.62, 0.45, 0.95, 1.0)
    case "keyInfo":        return Qt.rgba(0.35, 0.60, 0.95, 1.0)
    case "readyForReview": return Qt.rgba(0.95, 0.55, 0.20, 1.0)
    default:               return "transparent"
    }
  }
  function highlightLabel(kind) {
    switch (kind) {
    case "keyDecision":    return "decision"
    case "keyInfo":        return "key info"
    case "readyForReview": return "for review"
    default:               return ""
    }
  }
  // Per-status totals across all workstreams for the legend row.
  function statusTotals() {
    var t = { blocked: 0, reviewReady: 0, inProgress: 0, completed: 0 }
    for (var i = 0; i < workstreams.length; i++) {
      var ths = workstreams[i].threads || []
      for (var j = 0; j < ths.length; j++) {
        var s = ths[j].status
        if (t[s] !== undefined) t[s]++
      }
    }
    return t
  }

  DayNavRow {
    width: parent.width
    dayflow: pane.dayflow
  }

  // Legend + refresh — visible only once a briefing exists.
  Item {
    width: parent.width
    height: legendRow.implicitHeight
    visible: pane.workstreams.length > 0

    Row {
      id: legendRow
      spacing: Style.space(10)

      Repeater {
        model: ["inProgress", "reviewReady", "blocked", "completed"]
        delegate: Row {
          spacing: Style.space(4)
          visible: pane.statusTotals()[modelData] > 0
          Rectangle {
            width: Style.space(8); height: Style.space(8)
            radius: Style.space(4)
            anchors.verticalCenter: parent.verticalCenter
            color: pane.statusColor(modelData)
          }
          Text {
            anchors.verticalCenter: parent.verticalCenter
            text: pane.statusTotals()[modelData] + " " + pane.statusLabel(modelData)
            textFormat: Text.PlainText
            color: pane.dayflow ? pane.dayflow.dim : "gray"
            font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
          }
        }
      }
    }

    Text {
      anchors.right: parent.right
      anchors.verticalCenter: legendRow.verticalCenter
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

  BusyBar {
    width: parent.width
    pal: pane.dayflow
    active: host !== null && host.agentsLoading
  }

  Text {
    visible: host !== null && host.agentsError !== ""
    text: "! " + (host ? host.agentsError : "")
    textFormat: Text.PlainText
    color: Color.urgent !== undefined ? Color.urgent : "red"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
  }

  Text {
    visible: host !== null && !host.agentsLoading && host.agentsError === "" &&
      pane.briefing !== null && pane.workstreams.length === 0
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
    width: parent.width
    color: pane.dayflow ? pane.dayflow.dim : "gray"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
  }

  Text {
    visible: host !== null && !host.agentsLoading && pane.agentSourceNote !== ""
    text: pane.agentSourceNote
    textFormat: Text.PlainText
    wrapMode: Text.WordWrap
    width: parent.width
    color: pane.dayflow ? pane.dayflow.dim : "gray"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
  }

  Flickable {
    width: parent.width
    height: parent.height - Style.space(110)
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
              border.color: Qt.rgba(statusTint.r, statusTint.g, statusTint.b, 0.35)

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
                      height: Style.space(18)
                      width: badgeText.implicitWidth + Style.space(12)
                      radius: Style.cornerRadius
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
                      height: Style.space(18)
                      width: statusText.implicitWidth + Style.space(12)
                      radius: Style.cornerRadius
                      anchors.verticalCenter: parent.verticalCenter
                      color: Qt.rgba(pane.statusColor(threadCard.modelData.status).r,
                                     pane.statusColor(threadCard.modelData.status).g,
                                     pane.statusColor(threadCard.modelData.status).b, 0.18)
                      Text {
                        id: statusText
                        anchors.centerIn: parent
                        text: pane.statusLabel(threadCard.modelData.status)
                        textFormat: Text.PlainText
                        color: pane.statusColor(threadCard.modelData.status)
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
                      width: parent.width - badgeText.width - statusText.width
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
                        text: turnRow.modelData.text +
                              (turnRow.modelData.artifact_name
                                ? "  ·  " + turnRow.modelData.artifact_name : "")
                        textFormat: Text.PlainText
                        wrapMode: Text.WordWrap
                        color: turnRow.modelData.role === "user"
                          ? (pane.dayflow ? pane.dayflow.dim : "gray")
                          : (pane.dayflow ? pane.dayflow.foreground : "white")
                        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                        font.pixelSize: Style.font.caption
                      }
                      Rectangle {
                        id: hlPill
                        visible: !!turnRow.modelData.highlight
                        height: Style.space(16)
                        width: hlText.implicitWidth + Style.space(8)
                        radius: Style.cornerRadius
                        color: Qt.rgba(pane.highlightColor(turnRow.modelData.highlight).r,
                                       pane.highlightColor(turnRow.modelData.highlight).g,
                                       pane.highlightColor(turnRow.modelData.highlight).b, 0.18)
                        Text {
                          id: hlText
                          anchors.centerIn: parent
                          text: pane.highlightLabel(turnRow.modelData.highlight)
                          textFormat: Text.PlainText
                          color: pane.highlightColor(turnRow.modelData.highlight)
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
