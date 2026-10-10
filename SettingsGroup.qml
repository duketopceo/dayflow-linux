import QtQuick
import qs.Commons

// Collapsible settings section — disclosure header (▸/▾ + title + optional
// caption) over a default-content body. `open` is a plain property so the
// caller hoists state (Panel keeps a per-group map on `dayflow` that
// survives the tab Loader's destroy/recreate within a session).
Column {
  id: grp

  required property var pal
  required property string groupId
  property string title: ""
  property string caption: ""
  property bool open: false
  signal toggled()

  default property alias body: bodyCol.data

  width: parent ? parent.width : 0
  spacing: Style.space(6)

  Item {
    width: parent.width
    height: Math.max(headTitle.implicitHeight, headCaption.implicitHeight)

    Text {
      id: headTitle
      anchors.left: parent.left
      anchors.verticalCenter: parent.verticalCenter
      text: (grp.open ? "▾ " : "▸ ") + grp.title
      textFormat: Text.PlainText
      color: headMa.containsMouse
        ? (grp.pal ? grp.pal.foreground : "white")
        : (grp.pal ? grp.pal.foreground : "white")
      font.family: grp.pal ? grp.pal.fontFamily : ""
      font.pixelSize: Style.font.body
      font.bold: true
    }

    Text {
      id: headCaption
      visible: grp.caption !== ""
      anchors.right: parent.right
      anchors.verticalCenter: parent.verticalCenter
      text: grp.caption
      textFormat: Text.PlainText
      color: grp.pal ? grp.pal.dim : "gray"
      font.family: grp.pal ? grp.pal.fontFamily : ""
      font.pixelSize: Style.font.caption
      elide: Text.ElideRight
      width: Math.min(implicitWidth, parent.width - headTitle.implicitWidth - Style.space(12))
    }

    MouseArea {
      id: headMa
      anchors.fill: parent
      hoverEnabled: true
      cursorShape: Qt.PointingHandCursor
      // One-way data flow: the header never flips `open` itself — callers
      // hoist state (dayflow.settingsOpenGroups) and toggle it here.
      onClicked: grp.toggled()
    }
  }

  Column {
    id: bodyCol
    visible: grp.open
    width: parent.width
    spacing: Style.space(6)
  }
}
