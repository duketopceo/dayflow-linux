import QtQuick
import qs.Commons
import qs.Commons as Commons

// First-run wizard for the app: the existing Onboarding steps in a centered
// card over the shell. Shown while the engine reports the install
// unconfigured and the user has not skipped. "Skip" is always available.
Rectangle {
  id: overlay

  required property var panel

  color: Qt.alpha(Commons.Color.background, 0.8)

  // Swallow clicks so the page behind cannot be used half-configured by accident.
  MouseArea { anchors.fill: parent; hoverEnabled: true }

  Rectangle {
    id: card
    anchors.centerIn: parent
    width: Math.min(overlay.width - Style.space(48), Style.space(680))
    height: Math.min(overlay.height - Style.space(48), loader.height + Style.space(32))
    radius: Style.cornerRadius
    color: Commons.Color.background
    border.color: overlay.panel ? overlay.panel.fgFill(0.14) : "transparent"

    Loader {
      id: loader
      x: Style.space(16)
      y: Style.space(16)
      width: parent.width - Style.space(32)
      // Onboarding reads its panel as parent.panel (PopupBody's Loader contract).
      property var panel: overlay.panel
      source: "Onboarding.qml"
      onLoaded: {
        if (!item) return
        if (item.maxHeight !== undefined)
          item.maxHeight = Qt.binding(function() { return overlay.height - Style.space(80) })
        item.dismissed.connect(function() { overlay.panel.onboardingSkipped = true })
      }
    }
  }
}
