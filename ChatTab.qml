import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Flickable {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null

  width: parent ? parent.width : 0
  implicitHeight: Math.min(col.implicitHeight + Style.space(12), Style.space(420))
  height: implicitHeight
  contentHeight: col.implicitHeight + Style.space(12)
  clip: true

  property var chatMessages: []
  property int chatConversation: 0
  property bool chatLoading: false
  property var conversations: []
  property string chatInput: ""

  function applyChat(raw) {
    root.chatLoading = false
    try {
      var d = JSON.parse(raw)
      root.chatMessages = d.messages || []
      root.chatConversation = Number(d.conversation_id || 0)
      if (dayflow) dayflow.notice = ""
    } catch (e) {
      if (dayflow) dayflow.notice = "chat failed"
    }
  }

  function applyConversations(raw) {
    try {
      root.conversations = JSON.parse(raw)
    } catch (e) {
      root.conversations = []
    }
  }

  function sendChat() {
    if (root.chatInput.trim() === "") return
    root.chatLoading = true
    var cmd = ["dayflow", "chat", root.chatInput, "--json"]
    if (root.chatConversation > 0) {
      cmd = ["dayflow", "chat", root.chatInput, "--conversation-id", String(root.chatConversation), "--json"]
    }
    chatProc.command = cmd
    chatProc.running = true
    root.chatInput = ""
  }

  Column {
    id: col
    width: parent.width
    spacing: Style.space(10)

    Text {
      width: parent.width
      text: "Conversations"
      color: dayflow ? dayflow.foreground : Color.foreground
      font.family: dayflow ? dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
      font.bold: true
    }

    Flow {
      width: parent.width
      spacing: Style.space(6)

      Repeater {
        model: root.conversations
        delegate: Rectangle {
          height: Style.space(26)
          width: convText.implicitWidth + Style.space(16)
          radius: Style.cornerRadius
          color: root.chatConversation === Number(modelData.id)
            ? (dayflow ? dayflow.accentFill(0.18) : "transparent")
            : (convMouse.containsMouse
                ? (dayflow ? dayflow.accentFill(0.08) : "transparent")
                : "transparent")
          border.color: dayflow ? dayflow.accentFill(0.45) : "transparent"

          Text {
            id: convText
            anchors.centerIn: parent
            text: modelData.title
            color: dayflow ? dayflow.foreground : Color.foreground
            font.family: dayflow ? dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.caption
          }

          MouseArea {
            id: convMouse
            anchors.fill: parent
            hoverEnabled: true
            onClicked: root.chatConversation = Number(modelData.id)
          }
        }
      }
    }

    Column {
      width: parent.width
      spacing: Style.space(6)

      Repeater {
        model: root.chatMessages
        delegate: Rectangle {
          width: parent.width
          height: msgText.implicitHeight + Style.space(16)
          radius: Style.cornerRadius
          color: modelData.role === "user"
            ? (dayflow ? dayflow.accentFill(0.10) : "transparent")
            : (dayflow ? dayflow.fgFill(0.04) : "transparent")
          border.color: dayflow ? dayflow.fgFill(0.08) : "transparent"

          Text {
            id: msgText
            anchors.fill: parent
            anchors.margins: Style.space(8)
            text: (modelData.role === "tool"
              ? "Tool result"
              : (modelData.role === "assistant" ? "Assistant" : "You"))
              + ":\n" + modelData.content
            color: dayflow ? dayflow.foreground : Color.foreground
            font.family: dayflow ? dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }
        }
      }
    }

    Rectangle {
      width: parent.width
      height: inputEdit.implicitHeight + Style.space(12)
      radius: Style.cornerRadius
      color: dayflow ? dayflow.fgFill(0.04) : "transparent"
      border.color: dayflow ? dayflow.fgFill(0.12) : "transparent"
      clip: true

      TextEdit {
        id: inputEdit
        anchors.fill: parent
        anchors.margins: Style.space(6)
        text: root.chatInput
        color: dayflow ? dayflow.foreground : Color.foreground
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
        wrapMode: TextEdit.Wrap
        onTextChanged: root.chatInput = text
        Keys.onReturnPressed: function(event) {
          if (!event.isAutoRepeat) {
            event.accepted = true
            root.sendChat()
          }
        }
      }
    }

    Rectangle {
      height: Style.space(32)
      width: Style.space(64)
      radius: Style.cornerRadius
      color: sendMouse.containsMouse
        ? (dayflow ? dayflow.accentFill(0.12) : "transparent")
        : "transparent"
      border.color: dayflow ? dayflow.accentFill(0.5) : "transparent"

      Text {
        anchors.centerIn: parent
        text: root.chatLoading ? "..." : "Send"
        color: dayflow ? dayflow.foreground : Color.foreground
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
      }

      MouseArea {
        id: sendMouse
        anchors.fill: parent
        hoverEnabled: true
        enabled: !root.chatLoading
        onClicked: root.sendChat()
      }
    }
  }

  Process {
    id: chatProc
    command: ["dayflow", "chat", "hello", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyChat(text)
    }
    onExited: function(exitCode) {
      if (exitCode !== 0) {
        root.chatLoading = false
        if (dayflow) dayflow.notice = "chat error"
      }
    }
  }

  Process {
    id: conversationsProc
    command: ["dayflow", "conversations", "--json"]
    running: true
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyConversations(text)
    }
  }
}
