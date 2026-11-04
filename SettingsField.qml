import QtQuick
import Quickshell
import qs.Commons
import qs.Commons as Commons

Column {
  id: root
  property var dayflow: null
  property string label: ""
  property string value: ""
  property string hint: ""
  property bool numeric: false
  property bool secret: false

  signal edited(string text)
  // Enter/focus-out — fields with dedicated write paths commit here, not
  // per keystroke like the draft-bound `edited`.
  signal committed(string text)

  spacing: Style.space(2)

  Text {
    textFormat: Text.PlainText
    visible: root.label !== ""
    width: parent.width
    text: root.label
    color: root.dayflow ? root.dayflow.dim : Commons.Color.muted
    font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
    font.pixelSize: Style.font.caption
  }

  Rectangle {
    width: parent.width
    height: input.implicitHeight + Style.space(10)
    radius: Style.cornerRadius
    color: root.dayflow
      ? root.dayflow.fgFill(0.04)
      : Qt.rgba(Commons.Color.foreground.r, Commons.Color.foreground.g, Commons.Color.foreground.b, 0.04)
    border.color: root.dayflow
      ? root.dayflow.fgFill(0.12)
      : Qt.rgba(Commons.Color.foreground.r, Commons.Color.foreground.g, Commons.Color.foreground.b, 0.12)

    Text {
      textFormat: Text.PlainText
      id: placeholder
      anchors.fill: parent
      anchors.margins: Style.space(5)
      text: root.hint
      color: root.dayflow
        ? Qt.rgba(root.dayflow.foreground.r, root.dayflow.foreground.g, root.dayflow.foreground.b, 0.35)
        : Commons.Color.muted
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
      color: root.dayflow ? root.dayflow.foreground : Commons.Color.foreground
      font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
      inputMethodHints: root.numeric ? Qt.ImhDigitsOnly : Qt.ImhNone
      echoMode: root.secret ? TextInput.Password : TextInput.Normal
      // textEdited fires on user input only — onTextChanged would also fire
      // when the `text: root.value` binding re-binds on draft refresh,
      // looping write-back into the draft.
      onTextEdited: root.edited(text)
      onEditingFinished: root.committed(text)
    }
  }
}
