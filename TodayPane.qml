import QtQuick
import qs.Commons
import qs.Ui

// FullView "Today" pane — merged-span timeline for the viewed day.
// `host` is the FullView window; dayflow state is read through it.
Flickable {
  id: pane

  property var host: null
  readonly property var dayflow: host ? host.dayflow : null

  width: parent ? parent.width : 0
  height: parent ? parent.height : 0
  contentHeight: todayCol.implicitHeight
  clip: true

  Column {
    id: todayCol
    width: pane.width - Style.space(32)
    x: Style.space(16)
    spacing: Style.space(10)

    DayNavRow {
      width: parent.width
      dayflow: pane.dayflow
    }

    BusyBar {
      width: parent.width
      pal: pane.dayflow
      active: pane.dayflow ? pane.dayflow.timelineLoading : false
    }

    Text {
      visible: pane.dayflow !== null && pane.dayflow.spans.length === 0 && !pane.dayflow.timelineLoading
      text: "No activity recorded for this day."
      color: pane.dayflow ? pane.dayflow.dim : "gray"
      font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
      font.pixelSize: Style.font.body
    }

    // merged-span timeline — wide rows
    Repeater {
      model: pane.dayflow ? pane.dayflow.spans : []

      delegate: Rectangle {
        required property var modelData
        width: todayCol.width
        height: Style.space(46)
        radius: Style.cornerRadius
        color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
        border.color: pane.dayflow ? pane.dayflow.fgFill(0.08) : "transparent"

        Rectangle {
          width: Style.space(4)
          height: parent.height - Style.space(8)
          anchors.left: parent.left
          anchors.leftMargin: Style.space(6)
          anchors.verticalCenter: parent.verticalCenter
          radius: Style.space(2)
          color: pane.dayflow ? pane.dayflow.categoryColor(modelData.category) : "gray"
        }

        Text {
          anchors.left: parent.left
          anchors.leftMargin: Style.space(18)
          anchors.verticalCenter: parent.verticalCenter
          width: Style.space(120)
          text: modelData.start + "–" + modelData.end
          color: pane.dayflow ? pane.dayflow.dim : "gray"
          font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
          font.pixelSize: Style.font.caption
        }

        Column {
          anchors.left: parent.left
          anchors.leftMargin: Style.space(150)
          anchors.right: lowConfMark.visible ? lowConfMark.left : durText.left
          anchors.rightMargin: Style.space(8)
          anchors.verticalCenter: parent.verticalCenter
          spacing: Style.space(1)

          Text {
            width: parent.width
            text: modelData.title + (modelData.count > 1 ? "  ·" + modelData.count : "")
            textFormat: Text.PlainText
            color: pane.dayflow ? pane.dayflow.foreground : "white"
            font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
            font.bold: true
            elide: Text.ElideRight
          }

          Text {
            width: parent.width
            text: (modelData.summary !== "" ? modelData.summary + " · " : "") +
                  modelData.appName + " · " +
                  (pane.dayflow ? pane.dayflow.appDisplayName(modelData.category) : modelData.category)
            textFormat: Text.PlainText
            color: pane.dayflow ? pane.dayflow.dim : "gray"
            font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
            elide: Text.ElideRight
          }
        }

        Text {
          // Jev scored a judgment on this card below the confidence
          // threshold — treat the label/summary as suspect.
          id: lowConfMark
          visible: modelData.low_confidence === true
          anchors.right: durText.left
          anchors.rightMargin: Style.space(6)
          anchors.verticalCenter: parent.verticalCenter
          text: "?"
          textFormat: Text.PlainText
          color: Qt.rgba(0.95, 0.70, 0.15, 1.0)
          font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
          font.pixelSize: Style.font.caption
          font.bold: true
        }

        Text {
          id: durText
          anchors.right: parent.right
          anchors.rightMargin: Style.space(12)
          anchors.verticalCenter: parent.verticalCenter
          text: pane.dayflow ? pane.dayflow.fmtDur(modelData.minutes) : ""
          color: pane.dayflow ? pane.dayflow.foreground : "white"
          font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }
      }
    }
  }
}
