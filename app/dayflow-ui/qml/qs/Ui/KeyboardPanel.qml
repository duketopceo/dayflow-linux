import QtQuick

// Window-host version of Omarchy's layer-shell KeyboardPanel: instead of a
// popup surface it is the window's content area. Content is laid out at
// contentWidth x contentHeight; the host window sizes itself from those.
Item {
  id: root
  property Item anchorItem: null
  property var owner: null
  property QtObject bar: null
  property bool open: false
  property Item focusTarget: null
  property int contentWidth: 540
  property int contentHeight: 400
  property int maxWidth: 1400
  property int maxHeight: 1000
  default property alias contentItem: holder.children

  function fittedContentWidth(w) { return Math.min(w, maxWidth) }
  function fittedContentHeight(h) { return Math.min(Math.max(h, 200), maxHeight) }

  visible: open
  onContentWidthChanged: HostSizing.width = contentWidth
  onContentHeightChanged: HostSizing.height = contentHeight
  implicitWidth: contentWidth
  implicitHeight: contentHeight

  Item {
    id: holder
    anchors.fill: parent
  }

  onOpenChanged: if (open && focusTarget) Qt.callLater(function() { focusTarget.forceActiveFocus() })
  Component.onCompleted: {
    HostSizing.width = contentWidth
    HostSizing.height = contentHeight
    if (open && focusTarget) focusTarget.forceActiveFocus()
  }
}
