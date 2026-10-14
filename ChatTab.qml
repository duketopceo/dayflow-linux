import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Flickable {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null

  width: parent ? parent.width : 0
  implicitHeight: Math.min(col.implicitHeight + Style.space(12),
    dayflow && dayflow.expanded ? Style.space(560) : Style.space(420))
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

  property bool historyOpen: false

  // Title of the currently loaded conversation for the compact header.
  function currentConvTitle() {
    if (root.chatConversation <= 0) return "new chat"
    for (var i = 0; i < root.conversations.length; i++)
      if (Number(root.conversations[i].id) === root.chatConversation)
        return root.conversations[i].title || "conversation"
    return "conversation"
  }

  Column {
    id: col
    // Expanded: the thread keeps a readable measure and the rail fills
    // the right side — the full-width stretch read as empty.
    width: dayflow && dayflow.expanded
      ? Math.floor(parent.width * 0.62) : parent.width
    spacing: Style.space(6)

    LoadingBar {
      width: parent.width
      pal: root.dayflow
      active: root.chatLoading
      estimate: 10
    }

    // Compact header: current conversation + history toggle + new. The
    // conversation list stays collapsed unless asked for — a cloud of
    // pills is the busy part.
    Row {
      width: parent.width
      spacing: Style.space(8)

      Text {
        text: root.currentConvTitle()
        textFormat: Text.PlainText
        width: parent.width - (histBtn.visible ? histBtn.width + Style.space(8) : 0)
               - newBtn.width - Style.space(8)
        color: dayflow ? dayflow.foreground : Color.foreground
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
        font.bold: true
        elide: Text.ElideRight
        anchors.verticalCenter: parent.verticalCenter
      }

      Text {
        id: histBtn
        visible: root.conversations.length > 0
        text: root.historyOpen ? "hide" : "history"
        textFormat: Text.PlainText
        color: histMa.containsMouse ? (dayflow ? dayflow.foreground : Color.foreground)
                                    : (dayflow ? dayflow.dim : Color.dim)
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        font.underline: histMa.containsMouse
        anchors.verticalCenter: parent.verticalCenter
        MouseArea {
          id: histMa
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: root.historyOpen = !root.historyOpen
        }
      }

      Text {
        id: newBtn
        text: "+ new"
        textFormat: Text.PlainText
        color: newMa.containsMouse ? (dayflow ? dayflow.foreground : Color.foreground)
                                   : (dayflow ? dayflow.dim : Color.dim)
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        font.underline: newMa.containsMouse
        anchors.verticalCenter: parent.verticalCenter
        MouseArea {
          id: newMa
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: { root.newConversation(); root.historyOpen = false }
        }
      }
    }

    // Conversation history — plain rows, collapsed by default.
    Column {
      visible: root.historyOpen
      width: parent.width
      spacing: 0

      Repeater {
        model: root.conversations
        delegate: Rectangle {
          height: Style.space(24)
          width: parent.width
          radius: Style.space(4)
          color: convMouse.containsMouse
            ? (dayflow ? dayflow.accentFill(0.08) : "transparent")
            : "transparent"

          Text {
            anchors.left: parent.left
            anchors.leftMargin: Style.space(6)
            anchors.verticalCenter: parent.verticalCenter
            width: parent.width - Style.space(12)
            text: (root.chatConversation === Number(modelData.id) ? "● " : "")
                  + (modelData.title || "conversation")
            textFormat: Text.PlainText
            color: root.chatConversation === Number(modelData.id)
              ? (dayflow ? dayflow.foreground : Color.foreground)
              : (dayflow ? dayflow.dim : Color.dim)
            font.family: dayflow ? dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.caption
            elide: Text.ElideRight
          }

          MouseArea {
            id: convMouse
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: { root.loadConversation(Number(modelData.id)); root.historyOpen = false }
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
          property bool isUser: modelData.role === "user"

          visible: showThis
          // Chat convention instead of speaker labels: user right-aligned
          // accent bubble, assistant left-aligned plain card.
          width: isUser ? Math.min(parent.width * 0.82, msgText.implicitWidth + Style.space(16))
                        : parent.width
          x: isUser ? parent.width - width : 0
          height: showThis ? msgText.implicitHeight + Style.space(14) : 0
          radius: Style.cornerRadius
          color: isUser
            ? (dayflow ? dayflow.accentFill(0.14) : "transparent")
            : (dayflow ? dayflow.fgFill(0.04) : "transparent")

          Text {
            id: msgText
            visible: parent.showThis
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.margins: Style.space(7)
            textFormat: Text.RichText
            text: showThis ? root.fmtMsg(modelData.content) : ""
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
        textFormat: Text.PlainText
        color: dayflow ? dayflow.foreground : Color.foreground
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
    }

    Flow {
      visible: root.chatMessages.length === 0 && !root.chatLoading
      width: parent.width
      spacing: Style.space(6)

      Repeater {
        model: [
          "What did I work on yesterday?",
          "Draft my recap",
          "Summarize today"
        ]
        delegate: Rectangle {
          height: Style.space(24)
          width: chipText.implicitWidth + Style.space(14)
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

    // Input row: send lives inside the box — no separate button row.
    Rectangle {
      width: parent.width
      height: inputRow.implicitHeight + Style.space(10)
      radius: Style.cornerRadius
      color: dayflow ? dayflow.fgFill(0.04) : "transparent"
      border.color: dayflow ? dayflow.fgFill(0.12) : "transparent"
      clip: true

      Row {
        id: inputRow
        width: parent.width - Style.space(10)
        anchors.centerIn: parent
        spacing: Style.space(6)

        TextEdit {
          id: inputEdit
          width: parent.width - sendLbl.width - Style.space(6)
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

        Text {
          id: sendLbl
          anchors.verticalCenter: parent.verticalCenter
          text: "➤"
          textFormat: Text.PlainText
          color: (sendMa.containsMouse && !root.chatLoading)
            ? (dayflow ? dayflow.foreground : Color.foreground)
            : (dayflow ? dayflow.dim : Color.dim)
          font.family: dayflow ? dayflow.fontFamily : Style.font.family
          font.pixelSize: Style.font.body
          MouseArea {
            id: sendMa
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            enabled: !root.chatLoading
            onClicked: root.sendChat()
          }
        }
      }
    }
  }

  // Expanded context rail — the right ~38% of the tab: today at a glance
  // plus the conversation list the compact header hides behind "history".
  Column {
    id: rail
    visible: dayflow && dayflow.expanded
    x: col.width + Style.space(10)
    width: parent.width - col.width - Style.space(10)
    spacing: Style.space(8)

    Text {
      text: "Today"
      textFormat: Text.PlainText
      color: dayflow ? dayflow.foreground : Color.foreground
      font.family: dayflow ? dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
      font.bold: true
    }

    Rectangle {
      visible: root.recapText() !== ""
      width: parent.width
      height: railRecap.implicitHeight + Style.space(14)
      radius: Style.cornerRadius
      color: dayflow ? dayflow.fgFill(0.04) : "transparent"
      border.color: dayflow ? dayflow.accentFill(0.25) : "transparent"

      Text {
        id: railRecap
        anchors.fill: parent
        anchors.margins: Style.space(7)
        text: root.recapText()
        textFormat: Text.PlainText
        color: dayflow ? dayflow.foreground : Color.foreground
        font.family: dayflow ? dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
    }

    Text {
      visible: root.conversations.length > 0
      text: "Conversations"
      textFormat: Text.PlainText
      color: dayflow ? dayflow.foreground : Color.foreground
      font.family: dayflow ? dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
      font.bold: true
    }

    Column {
      width: parent.width
      spacing: 0

      Repeater {
        model: root.conversations
        delegate: Rectangle {
          height: Style.space(24)
          width: parent.width
          radius: Style.space(4)
          color: railConvMouse.containsMouse
            ? (dayflow ? dayflow.accentFill(0.08) : "transparent")
            : "transparent"

          Text {
            anchors.left: parent.left
            anchors.leftMargin: Style.space(6)
            anchors.verticalCenter: parent.verticalCenter
            width: parent.width - Style.space(12)
            text: (root.chatConversation === Number(modelData.id) ? "● " : "")
                  + (modelData.title || "conversation")
            textFormat: Text.PlainText
            color: root.chatConversation === Number(modelData.id)
              ? (dayflow ? dayflow.foreground : Color.foreground)
              : (dayflow ? dayflow.dim : Color.dim)
            font.family: dayflow ? dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.caption
            elide: Text.ElideRight
          }

          MouseArea {
            id: railConvMouse
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: root.loadConversation(Number(modelData.id))
          }
        }
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
    // FailedToStart emits neither exited nor streamFinished — clear the
    // flag so the busy bar can't stick.
    onRunningChanged: {
      if (!chatProc.running) root.chatLoading = false
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
    onRunningChanged: {
      if (!convProc.running) root.chatLoading = false
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
