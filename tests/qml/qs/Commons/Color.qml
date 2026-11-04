pragma Singleton
import QtQuick
import Quickshell
QtObject {
  property color foreground: "#eceff1"
  property color background: "#101315"
  property color accent: "#6b8cae"
  property color muted: "#707880"
  property color urgent: "#a55555"
  property QtObject bar: QtObject { property color active: urgent }
  property QtObject popups: QtObject {
    property color background: "#101315"
    property color border: "#2a2e33"
  }
}
