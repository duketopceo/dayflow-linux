import QtQuick
import Quickshell
import qs.Commons
import qs.Ui

// macOS-style daily workflow grid: rows are categories, columns are
// fixed-size time slots (15 minutes by default). Data comes from
// `dayflow day <date> --grid --json` via dayflow.workflow.
Rectangle {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null

  readonly property var workflow: dayflow && dayflow.workflow
    ? dayflow.workflow
    : ({ date: "", slot_minutes: 15, total_minutes: 0, slots: [], categories: [] })
  readonly property int slotCount: (workflow.slots || []).length

  visible: slotCount > 0
  width: parent ? parent.width : 0
  implicitHeight: slotCount > 0 ? gridCol.implicitHeight + Style.space(16) : 0
  height: implicitHeight
  radius: Style.cornerRadius
  color: dayflow ? dayflow.fgFill(0.04) : "transparent"
  border.color: dayflow ? dayflow.fgFill(0.08) : "transparent"

  function slotColor(cat) {
    if (!dayflow) return "transparent"
    return dayflow.cellColor(cat)
  }

  Column {
    id: gridCol
    width: parent.width - Style.space(16)
    anchors.centerIn: parent
    spacing: Style.space(6)

    Row {
      width: parent.width

      Text {
        text: "Daily workflow"
        color: dayflow ? dayflow.foreground : Color.foreground
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
        font.bold: true
      }

      Text {
        anchors.right: parent.right
        text: dayflow ? dayflow.fmtDur(root.workflow.total_minutes) + " tracked" : ""
        color: dayflow ? dayflow.dim : Color.dim
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
      }
    }

    Text {
      text: "1 cell = " + (root.workflow.slot_minutes || 15) + " min" +
            (root.slotCount > 0 && root.workflow.slots[0].time
              ? " · from " + root.workflow.slots[0].time : "")
      color: dayflow ? dayflow.dim : Color.dim
      font.family: dayflow ? dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.caption
    }

    Repeater {
      model: root.workflow.categories || []
      delegate: Row {
        id: catRowDelegate
        property var catRow: modelData
        width: parent.width
        spacing: Style.space(6)

        Text {
          width: Style.space(72)
          text: (catRowDelegate.catRow.display || catRowDelegate.catRow.name) + "\n" +
                (dayflow ? dayflow.fmtDur(catRowDelegate.catRow.minutes) : "")
          color: catRowDelegate.catRow.productive
            ? (dayflow ? dayflow.foreground : Color.foreground)
            : (dayflow ? dayflow.dim : Color.dim)
          font.family: dayflow ? dayflow.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
          anchors.verticalCenter: parent.verticalCenter
        }

        Row {
          id: slotRow
          width: parent.width - Style.space(72) - parent.spacing
          spacing: 1

          Repeater {
            model: root.workflow.slots || []
            delegate: Rectangle {
              width: root.slotCount > 0
                ? (slotRow.width - (root.slotCount - 1) * slotRow.spacing) / root.slotCount
                : 0
              height: Style.space(14)
              radius: 2
              color: modelData.category === catRowDelegate.catRow.name
                ? root.slotColor(modelData.category)
                : (dayflow ? dayflow.fgFill(0.04) : "transparent")
            }
          }
        }
      }
    }
  }
}
