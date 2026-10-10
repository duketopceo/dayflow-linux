import QtQuick
import qs.Commons

// One loading bar for the whole popup. Two modes:
//   progress >= 0  → determinate fill (engine streams real events on stderr)
//   progress <  0  → elapsed estimate — fills asymptotically toward 95% over
//                    `estimate` seconds, so a slow call still reads as work
//                    and never pins at 100% pretending it's done.
// `label` is the phase text ("reading session 7 of 23", "summarizing 3/8").
Column {
  id: bar

  required property var pal
  property bool active: false
  property real progress: -1   // 0..1 determinate; <0 = estimate mode
  property real estimate: 5    // expected seconds in estimate mode
  property string label: ""

  // visible:false removes the column from layout — no explicit height needed.
  visible: active
  spacing: Style.space(3)
  clip: true

  onActiveChanged: if (active) elapsedBase = Date.now()
  property real elapsedBase: 0
  property real elapsedSec: 0

  Timer {
    interval: 120
    running: bar.active
    repeat: true
    onTriggered: bar.elapsedSec = (Date.now() - bar.elapsedBase) / 1000
  }

  Item {
    width: bar.width
    height: Style.space(4)

    Rectangle {  // track
      anchors.fill: parent
      radius: height / 2
      color: bar.pal ? bar.pal.fgFill(0.10) : Qt.rgba(1, 1, 1, 0.1)
    }
    Rectangle {  // fill
      id: fill
      anchors.left: parent.left
      anchors.verticalCenter: parent.verticalCenter
      height: parent.height
      radius: height / 2
      color: bar.pal ? bar.pal.accentFill(0.9) : Qt.rgba(1, 1, 1, 0.9)
      width: bar.progress >= 0
        ? parent.width * Math.min(1, bar.progress)
        : parent.width * Math.min(0.95, 1 - Math.exp(-bar.elapsedSec / Math.max(0.5, bar.estimate)))
      Behavior on width { NumberAnimation { duration: 150 } }
    }
  }

  Text {
    visible: text !== ""
    text: bar.label !== "" ? bar.label
        : (bar.elapsedSec >= 2 ? Math.round(bar.elapsedSec) + "s" : "")
    textFormat: Text.PlainText
    color: bar.pal ? bar.pal.dim : "gray"
    font.family: bar.pal ? bar.pal.fontFamily : ""
    font.pixelSize: Style.font.caption
    elide: Text.ElideRight
    width: bar.width
  }
}
