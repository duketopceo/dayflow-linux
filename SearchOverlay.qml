import QtQuick
import Quickshell.Io
import qs.Commons
import qs.Commons as Commons

// Ctrl+K search over the journal (`dayflow search <query> --json`, FTS5).
// Enter or a click on a result jumps to that day on Today. The query is one
// argv element; leading dashes are stripped because the engine drops
// --flag-looking arguments from the query.
Rectangle {
  id: overlay

  required property var panel
  // Called with a day offset (<= 0) when a result is picked.
  signal pickedDay(int offset)
  signal closeRequested()

  property string query: ""
  property var results: []
  property bool searching: false
  property string error: ""
  property int current: 0
  readonly property int resultCount: results.length

  color: Qt.alpha(Commons.Color.background, 0.8)

  function cleanQuery(q) { return q.trim().replace(/^-+/, "").trim() }

  function open() {
    queryInput.text = ""
    overlay.query = ""
    overlay.results = []
    overlay.error = ""
    overlay.current = 0
    Qt.callLater(function() { queryInput.forceActiveFocus() })
  }

  function setQuery(q) {
    queryInput.text = q
    overlay.query = q
    debounce.stop()
    overlay.runSearch()
  }

  function runSearch() {
    var q = cleanQuery(overlay.query)
    if (q === "") { overlay.results = []; overlay.error = ""; overlay.searching = false; return }
    if (searchProc.running) { overlay.queued = true; return }
    overlay.queued = false
    overlay.error = ""
    overlay.searching = true
    searchProc.command = ["dayflow", "search", q, "--json"]
    searchProc.running = true
  }
  property bool queued: false

  // Days between local midnight today and the result's date (always <= 0).
  function dayOffsetOf(start) {
    var m = /^(\d{4})-(\d{2})-(\d{2})/.exec(start || "")
    if (!m) return 0
    var d = new Date(parseInt(m[1]), parseInt(m[2]) - 1, parseInt(m[3]))
    var t = new Date()
    t = new Date(t.getFullYear(), t.getMonth(), t.getDate())
    return Math.min(0, Math.round((d.getTime() - t.getTime()) / 86400000))
  }

  function pick(i) {
    if (i < 0 || i >= results.length) return
    overlay.pickedDay(overlay.dayOffsetOf(results[i].Start))
  }

  Timer { id: debounce; interval: 250; onTriggered: overlay.runSearch() }

  Process {
    id: searchProc
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try {
          var d = JSON.parse(text)
          overlay.results = Array.isArray(d) ? d : []
          overlay.current = 0
          overlay.error = ""
        } catch (e) {
          overlay.results = []
          overlay.error = "search failed"
        }
      }
    }
    onExited: function(code) {
      overlay.searching = false
      if (code !== 0) { overlay.results = []; overlay.error = "search failed" }
      if (overlay.queued) overlay.runSearch()
    }
  }

  // Click outside the card closes.
  MouseArea { anchors.fill: parent; onClicked: overlay.closeRequested() }

  Rectangle {
    id: card
    anchors.horizontalCenter: parent.horizontalCenter
    y: Style.space(48)
    width: Math.min(overlay.width - Style.space(48), Style.space(640))
    height: Math.min(overlay.height - Style.space(96), box.implicitHeight + Style.space(24))
    radius: Style.cornerRadius
    color: Commons.Color.background
    border.color: overlay.panel ? overlay.panel.fgFill(0.14) : "transparent"
    MouseArea { anchors.fill: parent }   // keep clicks inside the card

    Column {
      id: box
      x: Style.space(12)
      y: Style.space(12)
      width: parent.width - Style.space(24)
      spacing: Style.space(8)

      Rectangle {
        width: parent.width
        height: queryInput.implicitHeight + Style.space(14)
        radius: Style.cornerRadius
        color: overlay.panel ? overlay.panel.fgFill(0.05) : "transparent"
        border.color: overlay.panel ? overlay.panel.accentFill(0.4) : "transparent"

        TextInput {
          id: queryInput
          anchors.fill: parent
          anchors.margins: Style.space(7)
          color: overlay.panel ? overlay.panel.foreground : "white"
          font.family: overlay.panel ? overlay.panel.fontFamily : ""
          font.pixelSize: Style.font.body
          clip: true
          selectByMouse: true
          onTextChanged: { overlay.query = text; debounce.restart() }
          Keys.onPressed: function(ev) {
            if (ev.key === Qt.Key_Escape) { overlay.closeRequested(); ev.accepted = true }
            else if (ev.key === Qt.Key_Down) { overlay.current = Math.min(overlay.results.length - 1, overlay.current + 1); ev.accepted = true }
            else if (ev.key === Qt.Key_Up) { overlay.current = Math.max(0, overlay.current - 1); ev.accepted = true }
            else if (ev.key === Qt.Key_Return || ev.key === Qt.Key_Enter) { overlay.pick(overlay.current); ev.accepted = true }
          }

          Text {
            visible: queryInput.text === ""
            anchors.verticalCenter: parent.verticalCenter
            text: "Search your journal"
            textFormat: Text.PlainText
            color: overlay.panel ? overlay.panel.dim : "gray"
            font.family: queryInput.font.family
            font.pixelSize: queryInput.font.pixelSize
          }
        }
      }

      Text {
        visible: overlay.error !== "" || (!overlay.searching && overlay.cleanQuery(overlay.query) !== "" && overlay.results.length === 0)
        width: parent.width
        text: overlay.error !== "" ? overlay.error : "No matches"
        textFormat: Text.PlainText
        color: overlay.panel ? overlay.panel.dim : "gray"
        font.family: overlay.panel ? overlay.panel.fontFamily : ""
        font.pixelSize: Style.font.caption
      }

      ListView {
        id: list
        width: parent.width
        height: Math.min(contentHeight, overlay.height - Style.space(200))
        visible: overlay.results.length > 0
        clip: true
        model: overlay.results
        currentIndex: overlay.current
        onCurrentIndexChanged: positionViewAtIndex(currentIndex, ListView.Contain)
        spacing: Style.space(2)

        delegate: Rectangle {
          required property var modelData
          required property int index
          width: list.width
          height: rowCol.implicitHeight + Style.space(10)
          radius: Style.cornerRadius
          color: index === overlay.current
            ? (overlay.panel ? overlay.panel.accentFill(0.14) : "transparent")
            : (rowMa.containsMouse && overlay.panel ? overlay.panel.fgFill(0.06) : "transparent")

          Column {
            id: rowCol
            x: Style.space(8)
            y: Style.space(5)
            width: parent.width - Style.space(16)
            spacing: Style.space(2)
            Text {
              width: parent.width
              text: modelData.Title
              textFormat: Text.PlainText
              elide: Text.ElideRight
              color: overlay.panel ? overlay.panel.foreground : "white"
              font.family: overlay.panel ? overlay.panel.fontFamily : ""
              font.pixelSize: Style.font.body
            }
            Text {
              width: parent.width
              text: modelData.Start + (modelData.Category ? " · " + modelData.Category : "")
                + (modelData.App ? " · " + modelData.App : "")
              textFormat: Text.PlainText
              elide: Text.ElideRight
              color: overlay.panel ? overlay.panel.dim : "gray"
              font.family: overlay.panel ? overlay.panel.fontFamily : ""
              font.pixelSize: Style.font.caption
            }
          }

          MouseArea {
            id: rowMa
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: overlay.pick(index)
          }
        }
      }
    }
  }
}
