pragma Singleton
import QtQuick

// Standalone stand-in for Omarchy's qs.Commons.Style: the spacing function,
// corner radius and font scale the tab QML uses, at Omarchy's default
// 12px base size (see /usr/share/omarchy/shell/Commons/Style.qml).
QtObject {
  id: root
  property int cornerRadius: 0
  property int fontBaseSize: 12
  readonly property real fontScale: Math.max(1 / 12, fontBaseSize / 12)

  function spaceReal(px) {
    var n = Number(px)
    if (!isFinite(n) || n <= 0) return 0
    return n * fontScale
  }
  function space(px) {
    var n = spaceReal(px)
    if (n <= 0) return 0
    return Math.max(1, Math.round(n))
  }
  function fontPx(mult) { return Math.max(1, Math.round(fontBaseSize * mult)) }

  readonly property QtObject font: QtObject {
    readonly property string family: "monospace"
    readonly property int baseSize: root.fontBaseSize
    readonly property int caption: root.fontPx(0.833)
    readonly property int bodySmall: root.fontPx(0.917)
    readonly property int body: root.fontPx(1.0)
    readonly property int subtitle: root.fontPx(1.083)
    readonly property int title: root.fontPx(1.167)
    readonly property int heading: root.fontPx(1.333)
    readonly property int display: root.fontPx(2.0)
  }
}
