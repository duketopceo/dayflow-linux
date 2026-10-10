import QtQuick
import qs.Commons

// Label + dim value row — the "key: value" stat line repeated across
// Today/Week/Settings. Label takes the dim color, value stays foreground
// and elides to the remaining width.
Row {
  id: stat

  required property var pal
  property string label: ""
  property string value: ""

  spacing: Style.space(6)

  Text {
    id: labelText
    anchors.verticalCenter: parent.verticalCenter
    text: stat.label
    textFormat: Text.PlainText
    color: stat.pal ? stat.pal.dim : "gray"
    font.family: stat.pal ? stat.pal.fontFamily : ""
    font.pixelSize: Style.font.body
  }
  Text {
    anchors.verticalCenter: parent.verticalCenter
    width: Math.max(0, stat.width - labelText.implicitWidth - stat.spacing)
    text: stat.value
    textFormat: Text.PlainText
    elide: Text.ElideRight
    color: stat.pal ? stat.pal.foreground : "white"
    font.family: stat.pal ? stat.pal.fontFamily : ""
    font.pixelSize: Style.font.body
  }
}
