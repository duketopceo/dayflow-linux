import QtQuick
import Quickshell
import qs.Ui

Column {
  id: root
  property var dayflow: null
  property string label: ""
  property string value: ""
  property string hint: ""
  property bool numeric: false
  property var onEdited: function(text) {}

  spacing: Style.space(2)

  Text {
    visible: root.label !== ""
    width: parent.width
    text: root.label
    color: root.dayflow ? root.dayflow.dim : Color.dim
    font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
    font.pixelSize: Style.font.caption
  }

  Rectangle {
    width: parent.width
    height: input.implicitHeight + Style.space(10)
    radius: Style.cornerRadius
    color: root.dayflow ? root.dayflow.fgFill(0.04) : Color.fgFill(0.04)
    border.color: root.dayflow ? root.dayflow.fgFill(0.12) : Color.fgFill(0.12)

    Text {
      id: placeholder
      anchors.fill: parent
      anchors.margins: Style.space(5)
      text: root.hint
      color: root.dayflow ? Qt.rgba(root.dayflow.foreground.r, root.dayflow.foreground.g, root.dayflow.foreground.b, 0.35) : Color.dim
      font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
      visible: input.text === "" && !input.activeFocus
      elide: Text.ElideRight
    }

    TextInput {
      id: input
      anchors.fill: parent
      anchors.margins: Style.space(5)
      text: root.value
      color: root.dayflow ? root.dayflow.foreground : Color.foreground
      font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
      inputMethodHints: root.numeric ? Qt.ImhDigitsOnly : Qt.ImhNone
      onTextChanged: root.onEdited(text)
    }
  }
}
