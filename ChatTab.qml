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
  property string lastSent: ""
  property string chatAttribution: ""
  property bool recapFired: false

  // Instant local recap from the already-loaded timeline spans — no LLM call.
  function recapText() {
    var spans = (dayflow && dayflow.spans) ? dayflow.spans : []
    if (spans.length === 0) return ""
    var byCat = {}
    var total = 0
    for (var i = 0; i < spans.length; i++) {
      var m = Number(spans[i].minutes || 0)
      total += m
      var c = spans[i].category || "other"
      byCat[c] = (byCat[c] || 0) + m
    }
    if (total <= 0) return ""
    var cats = Object.keys(byCat).sort(function(a, b) { return byCat[b] - byCat[a] }).slice(0, 3)
    var parts = []
    for (i = 0; i < cats.length; i++)
      parts.push(dayflow.catDisplay(cats[i]) + " " + dayflow.fmtDur(byCat[cats[i]]))
    var last = spans[spans.length - 1]
    var head = (dayflow.dateLabel || "Today") + " · " + dayflow.fmtDur(total) + " tracked — " + parts.join(" · ")
    if (last && last.title) head += "\nLatest: " + last.title
    return head
  }

  // Lightweight markdown-ish formatting for assistant replies: bold, inline
  // code, and "- " bullets. Input is HTML-escaped first.
  function fmtMsg(s) {
    var esc = String(s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    esc = esc.replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>")
    esc = esc.replace(/`([^`]+)`/g, "<font face=\"monospace\">$1</font>")
    esc = esc.replace(/\n- /g, "<br/>• ").replace(/^- /g, "• ")
    esc = esc.replace(/\n/g, "<br/>")
    return esc
  }

  function applyChat(raw) {
    root.chatLoading = false
    try {
      var d = JSON.parse(raw)
      root.chatMessages = d.messages || []
      root.chatConversation = Number(d.conversation_id || 0)
      root.chatAttribution = d.provider ? d.provider + " · " + (d.model || "") : ""
      if (dayflow) dayflow.notice = ""
      conversationsProc.running = true // a new conversation may have been created
    } catch (e) {
      root.chatInput = root.lastSent
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

  function applyConversation(raw) {
    root.chatLoading = false
    try {
      var d = JSON.parse(raw)
      root.chatMessages = d.messages || []
      if (dayflow) dayflow.notice = ""
    } catch (e) {
      if (dayflow) dayflow.notice = "could not load conversation"
    }
  }

  function loadConversation(id) {
    if (dayflow) dayflow.uilog("chat load conv " + id)
    // chatProc and convProc both write chatConversation/chatMessages —
    // switching while either is active lets completions land out of order.
    if (chatProc.running || convProc.running) return
    root.chatConversation = id
    root.chatMessages = []
    root.chatAttribution = ""
    if (id <= 0) return
    root.chatLoading = true
    convProc.command = ["dayflow", "conversation", String(id), "--json"]
    convProc.running = true
  }

  function newConversation() {
    if (dayflow) dayflow.uilog("chat new")
    if (chatProc.running || convProc.running) return
    root.chatConversation = 0
    root.chatMessages = []
    root.chatAttribution = ""
  }

  function sendChat() {
    if (root.chatInput.trim() === "") return
    if (dayflow) dayflow.uilog("chat send")
    root.chatLoading = true
    root.lastSent = root.chatInput
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

    BusyBar {
      width: parent.width
      dayflow: root.dayflow
      active: root.chatLoading
    }

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

      Rectangle {
        height: Style.space(26)
        width: newConvText.implicitWidth + Style.space(16)
        radius: Style.cornerRadius
        color: root.chatConversation === 0
          ? (dayflow ? dayflow.accentFill(0.18) : "transparent")
          : (newConvMouse.containsMouse
              ? (dayflow ? dayflow.accentFill(0.08) : "transparent")
              : "transparent")
        border.color: dayflow ? dayflow.accentFill(0.45) : "transparent"

        Text {
          id: newConvText
          anchors.centerIn: parent
          text: "+ New"
          color: dayflow ? dayflow.foreground : Color.foreground
          font.family: dayflow ? dayflow.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
        }

        MouseArea {
          id: newConvMouse
          anchors.fill: parent
          hoverEnabled: true
          onClicked: root.newConversation()
        }
      }

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
            onClicked: root.loadConversation(Number(modelData.id))
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
          property bool showThis: modelData.role !== "tool" && !(modelData.role === "assistant" && modelData.tool_calls && modelData.tool_calls.length > 0)

          visible: showThis
          width: parent.width
          height: showThis ? msgText.implicitHeight + Style.space(16) : 0
          radius: Style.cornerRadius
          color: modelData.role === "user"
            ? (dayflow ? dayflow.accentFill(0.10) : "transparent")
            : (dayflow ? dayflow.fgFill(0.04) : "transparent")
          border.color: dayflow ? dayflow.fgFill(0.08) : "transparent"

          Text {
            id: msgText
            visible: parent.showThis
            anchors.fill: parent
            anchors.margins: Style.space(8)
            textFormat: Text.RichText
            text: showThis
              ? (modelData.role === "assistant"
                  ? "<b>Assistant</b>:<br/>" + root.fmtMsg(modelData.content)
                  : "<b>You</b>:<br/>" + root.fmtMsg(modelData.content))
              : ""
            color: dayflow ? dayflow.foreground : Color.foreground
            font.family: dayflow ? dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }
        }
      }
    }

    Text {
      visible: root.chatAttribution !== ""
      width: parent.width
      text: "via " + root.chatAttribution
      color: dayflow ? dayflow.dim : Color.dim
      font.family: dayflow ? dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.caption
    }

    // Preloaded mini-recap: local timeline data, shown instantly in the
    // empty state so the tab isn't blank while the LLM recap warms up.
    Rectangle {
      visible: root.chatMessages.length === 0 && root.recapText() !== ""
      width: parent.width
      height: recapLabel.implicitHeight + Style.space(14)
      radius: Style.cornerRadius
      color: dayflow ? dayflow.fgFill(0.04) : "transparent"
      border.color: dayflow ? dayflow.accentFill(0.25) : "transparent"

      Text {
        id: recapLabel
        anchors.fill: parent
        anchors.margins: Style.space(7)
        text: root.recapText()
        color: dayflow ? dayflow.foreground : Color.foreground
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
    }

    Text {
      visible: autoRecapTimer.running
      width: parent.width
      text: "auto-recap in a few seconds…"
      color: dayflow ? dayflow.dim : Color.dim
      font.family: dayflow ? dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.caption
    }

    Flow {
      visible: root.chatMessages.length === 0 && !root.chatLoading
      width: parent.width
      spacing: Style.space(6)

      Repeater {
        model: [
          "What did I work on yesterday?",
          "Draft my standup",
          "Where did I lose focus this week?",
          "Summarize today"
        ]
        delegate: Rectangle {
          height: Style.space(26)
          width: chipText.implicitWidth + Style.space(16)
          radius: Style.cornerRadius
          color: chipMouse.containsMouse
            ? (dayflow ? dayflow.accentFill(0.10) : "transparent")
            : (dayflow ? dayflow.fgFill(0.04) : "transparent")
          border.color: dayflow ? dayflow.fgFill(0.12) : "transparent"

          Text {
            id: chipText
            anchors.centerIn: parent
            text: modelData
            color: dayflow ? dayflow.dim : Color.dim
            font.family: dayflow ? dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.caption
          }

          MouseArea {
            id: chipMouse
            anchors.fill: parent
            hoverEnabled: true
            onClicked: {
              root.chatInput = modelData
              root.sendChat()
            }
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
        enabled: !root.chatLoading
        Keys.onReturnPressed: function(event) {
          if (event.modifiers & Qt.ShiftModifier) {
            event.accepted = false // newline
            return
          }
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

  // Auto-recap: this tab instance is destroyed by the tab Loader when the
  // user clicks away, so the timer simply dying with it implements
  // "fires 5s after opening, unless you click off". Skips if the user is
  // typing, a conversation/messages are already loaded, or a proc is busy.
  Timer {
    id: autoRecapTimer
    interval: 5000
    repeat: false
    running: true
    onTriggered: {
      if (root.recapFired) return
      root.recapFired = true
      if (!dayflow || dayflow.configured === false) return
      if (root.chatMessages.length !== 0 || root.chatConversation !== 0) return
      if (chatProc.running || convProc.running || root.chatLoading) return
      if (root.chatInput.trim() !== "") return
      root.chatInput = "Summarize my day so far"
      root.sendChat()
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
        root.chatInput = root.lastSent // restore for retry
        if (dayflow) dayflow.notice = "chat error"
      }
    }
  }

  Process {
    id: convProc
    command: ["dayflow", "conversation", "0", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyConversation(text)
    }
    onExited: function(exitCode) {
      if (exitCode !== 0) {
        root.chatLoading = false
        if (dayflow) dayflow.notice = "could not load conversation"
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
