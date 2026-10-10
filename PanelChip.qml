import QtQuick
import qs.Commons

// Small pill/tag — status dots, category chips, compact labels. `dot`
// prepends a colored round marker; `text` is the pill label.
Rectangle {
  id: chip

  required property var pal
  property string text: ""
  property color dot: "transparent"
  property bool filled: false

  implicitHeight: Style.space(18)
  implicitWidth: row.implicitWidth + Style.space(10)
  radius: implicitHeight / 2
  color: filled
    ? (pal ? pal.accentFill(0.12) : Qt.rgba(1, 1, 1, 0.12))
    : (pal ? pal.fgFill(0.08) : Qt.rgba(1, 1, 1, 0.08))
  border.color: "transparent"

  Row {
    id: row
    anchors.centerIn: parent
    spacing: Style.space(4)

    Rectangle {
      visible: chip.dot != "transparent" && chip.dot.toString() !== ""
      width: Style.space(6)
      height: width
      radius: width / 2
      anchors.verticalCenter: parent.verticalCenter
      color: chip.dot
    }
    Text {
      anchors.verticalCenter: parent.verticalCenter
      text: chip.text
      textFormat: Text.PlainText
      color: chip.pal ? chip.pal.dim : "gray"
      font.family: chip.pal ? chip.pal.fontFamily : ""
      font.pixelSize: Style.font.caption
    }
  }
}
