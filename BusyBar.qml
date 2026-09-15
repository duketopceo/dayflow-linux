import QtQuick
import Quickshell
import qs.Commons

// Thin indeterminate progress indicator shown while a fetch is in flight.
Rectangle {
  id: root
  property var dayflow: null
  property bool active: true

  height: 2
  radius: 1
  color: dayflow ? dayflow.fgFill(0.08) : Qt.rgba(1, 1, 1, 0.08)
  clip: true
  visible: active

  Rectangle {
    id: slide
    width: Math.max(Style.space(20), root.width * 0.25)
    height: root.height
    radius: 1
    color: dayflow ? dayflow.accentFill(0.7) : Qt.rgba(1, 1, 1, 0.5)

    NumberAnimation on x {
      running: root.visible && root.active
      loops: Animation.Infinite
      from: -slide.width
      to: root.width
      duration: 1100
      easing.type: Easing.InOutQuad
    }
  }
}
