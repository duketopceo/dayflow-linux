import QtQuick
import qs.Commons

// Shared popup card — owns the fgFill(0.04)/fgFill(0.08) card literal and
// the deeper-bordered `well` variant used for input boxes (0.04/0.12).
// Children land in the card's default content area.
Rectangle {
  id: card

  required property var pal
  // well = input/inner surface — same fill, stronger border.
  property bool well: false

  radius: Style.cornerRadius
  color: pal ? pal.fgFill(0.04) : Qt.rgba(1, 1, 1, 0.04)
  border.color: pal ? pal.fgFill(well ? 0.12 : 0.08) : Qt.rgba(1, 1, 1, 0.08)
}
