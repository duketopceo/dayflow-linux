import QtQuick
import qs.Commons

// Shared bordered popup button. Owns the accent-fill / border / caption
// literals that footer actions, header buttons, and tab delegates each
// re-derived by hand. `frame` picks the border treatment:
//   "accent"   — accent border always (footer action chips)
//   "selected" — accent border only when selected (tab bar delegates)
//   "quiet"    — fgFill(0.12) until selected (the ⋯ toggle)
Rectangle {
  id: btn

  required property var pal
  property string text: ""
  property bool selected: false
  property bool enabled: true
  property string frame: "accent"
  property bool compact: false
  signal clicked()

  implicitHeight: compact ? Style.space(24) : Style.space(26)
  implicitWidth: label.implicitWidth + Style.space(14)
  radius: Style.cornerRadius
  color: selected
    ? (pal ? pal.accentFill(0.12) : Qt.rgba(1, 1, 1, 0.12))
    : (ma.containsMouse
        ? (pal ? pal.accentFill(frame === "selected" ? 0.06 : 0.12) : Qt.rgba(1, 1, 1, 0.08))
        : "transparent")
  border.color: frame === "accent"
    ? (pal ? pal.accentFill(0.5) : "transparent")
    : (selected
        ? (pal ? pal.accentFill(frame === "selected" ? 0.45 : 0.5) : "transparent")
        : (frame === "selected"
            ? "transparent"
            : (pal ? pal.fgFill(0.12) : Qt.rgba(1, 1, 1, 0.12))))
  opacity: enabled ? 1 : 0.45

  Text {
    id: label
    anchors.centerIn: parent
    text: btn.text
    textFormat: Text.PlainText
    color: btn.selected
      ? (btn.pal ? btn.pal.foreground : "white")
      : (btn.frame === "selected" || !btn.enabled
          ? (btn.pal ? btn.pal.dim : "gray")
          : (btn.pal ? btn.pal.foreground : "white"))
    font.bold: btn.selected
    font.family: btn.pal ? btn.pal.fontFamily : ""
    font.pixelSize: Style.font.caption
  }

  MouseArea {
    id: ma
    anchors.fill: parent
    hoverEnabled: true
    enabled: btn.enabled
    cursorShape: btn.enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
    onClicked: btn.clicked()
  }
}
