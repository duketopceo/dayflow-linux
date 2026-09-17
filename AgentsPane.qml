import QtQuick
import qs.Commons
import qs.Ui

// FullView "Agents" pane — Claude Code / Codex session recaps for the
// viewed day. `host` is the FullView window, which owns the session list
// and the loader process.
Column {
  id: pane

  property var host: null
  readonly property var dayflow: host ? host.dayflow : null

  width: parent ? parent.width : 0
  height: parent ? parent.height : 0
  spacing: Style.space(8)
  leftPadding: Style.space(16)
  rightPadding: Style.space(16)
  topPadding: Style.space(4)

  DayNavRow {
    width: parent.width
    dayflow: pane.dayflow
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
    visible: host !== null && !host.agentsLoading &&
      host.agentSessions.length === 0 && host.agentsError === ""
    text: "No Claude Code or Codex sessions on this day."
    textFormat: Text.PlainText
    color: pane.dayflow ? pane.dayflow.dim : "gray"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.body
  }

  Flickable {
    width: parent.width
    height: parent.height - Style.space(110)
    contentHeight: sessCol.implicitHeight
    clip: true

    Column {
      id: sessCol
      width: parent.width
      spacing: Style.space(6)

      Repeater {
        model: host ? host.agentSessions : []

        delegate: Rectangle {
          required property var modelData
          width: sessCol.width
          height: sessInner.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
          border.color: pane.dayflow ? pane.dayflow.fgFill(0.08) : "transparent"

          Column {
            id: sessInner
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(3)

            Row {
              width: parent.width
              spacing: Style.space(8)

              Rectangle {
                height: Style.space(18)
                width: badgeText.implicitWidth + Style.space(12)
                radius: Style.cornerRadius
                color: modelData.source === "claude"
                  ? Qt.rgba(0.85, 0.55, 0.25, 0.2)
                  : Qt.rgba(0.30, 0.65, 0.85, 0.2)

                Text {
                  id: badgeText
                  anchors.centerIn: parent
                  text: modelData.source
                  textFormat: Text.PlainText
                  color: modelData.source === "claude"
                    ? Qt.rgba(0.95, 0.70, 0.40, 1.0)
                    : Qt.rgba(0.45, 0.80, 1.0, 1.0)
                  font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                  font.pixelSize: Style.font.caption
                  font.bold: true
                }
              }

              Text {
                anchors.verticalCenter: parent.verticalCenter
                text: modelData.project
                textFormat: Text.PlainText
                color: pane.dayflow ? pane.dayflow.foreground : "white"
                font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
                font.bold: true
                elide: Text.ElideRight
                width: parent.width - badgeText.implicitWidth - timeText.implicitWidth - Style.space(24)
              }

              Text {
                id: timeText
                anchors.verticalCenter: parent.verticalCenter
                anchors.right: parent.right
                text: Qt.formatTime(new Date(modelData.start * 1000), "hh:mm") +
                      "–" +
                      Qt.formatTime(new Date(modelData.end * 1000), "hh:mm") +
                      " · " + modelData.messages + " msgs"
                textFormat: Text.PlainText
                color: pane.dayflow ? pane.dayflow.dim : "gray"
                font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
              }
            }

            Text {
              width: parent.width
              visible: modelData.title !== ""
              text: modelData.title
              textFormat: Text.PlainText
              color: pane.dayflow ? pane.dayflow.dim : "gray"
              font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
            }
          }
        }
      }
    }
  }
}
