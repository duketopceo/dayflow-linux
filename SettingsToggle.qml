import QtQuick
import qs.Commons

// Label + On/Off chip row — the Settings consent/flag control repeated for
// every boolean key. `on` mirrors the current value; `toggled()` is the
// caller's write hook (draft-bound keys flip the draft, dedicated-path keys
// emit their own write).
Item {
  id: tgl

  required property var pal
  property string label: ""
  property bool on: false
  property bool enabled: true
  signal toggled()

  width: parent ? parent.width : 0
  height: Math.max(lbl.implicitHeight, chip.implicitHeight)

  Text {
    id: lbl
    anchors.left: parent.left
    anchors.right: chip.left
    anchors.rightMargin: Style.space(8)
    anchors.verticalCenter: parent.verticalCenter
    text: tgl.label
    textFormat: Text.PlainText
    color: tgl.pal ? tgl.pal.foreground : "white"
    font.family: tgl.pal ? tgl.pal.fontFamily : ""
    font.pixelSize: Style.font.body
    wrapMode: Text.WordWrap
  }

  PanelButton {
    id: chip
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
    pal: tgl.pal
    frame: "quiet"
    compact: true
    selected: tgl.on
    text: tgl.on ? "On" : "Off"
    enabled: tgl.enabled
    onClicked: tgl.toggled()
  }
}
