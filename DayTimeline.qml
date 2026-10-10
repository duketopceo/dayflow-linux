import QtQuick
import qs.Commons

// Day view, macOS-Dayflow style: every span is a readable activity card
// pinned to a shared time gutter — never a colored sliver. Cards sit at
// their start-time offset and cascade downward when they'd overlap, so
// the title + time range is always visible. Hour ticks + a rail line run
// down the left gutter; a now-line marks the current time on today's
// view. Click selects; the caller renders the detail card.
Item {
  id: tl

  required property var pal
  property var spans: []
  property int selectedIndex: -1
  property bool showNow: false
  // "all" | "core" (productive) | "distract" | "idle"
  property string filter: "all"
  signal spanClicked(int index)

  // Minute-of-day — epoch fields are authoritative; "12:00 AM" strings
  // are the fallback for injected fixtures.
  function toMin(span, key) {
    var ts = Number(span[key + "_ts"] || 0)
    if (ts > 0) {
      var dt = new Date(ts * 1000)
      return dt.getHours() * 60 + dt.getMinutes()
    }
    var s = String(span[key] || "")
    var m = s.match(/^(\d+):(\d+)\s*(AM|PM)?$/i)
    if (!m) return 0
    var h = parseInt(m[1], 10), mn = parseInt(m[2], 10)
    if (m[3]) {
      var pm = m[3].toUpperCase() === "PM"
      if (h === 12) h = pm ? 12 : 0
      else if (pm) h += 12
    }
    return h * 60 + mn
  }

  function keep(s) {
    if (tl.filter === "core") return s.productive === true
    if (tl.filter === "distract") return s.productive === false && s.category !== "idle"
    if (tl.filter === "idle") return s.category === "idle"
    return true
  }

  // Layout: card height is duration-proportional with a readable floor;
  // y is the start-time offset unless it would overlap the previous
  // card, in which case it cascades. Recomputed when spans/filter/size
  // changes — implicitHeight tracks the last card's bottom.
  readonly property real pxPerMin: 1.0
  property var layout: []
  readonly property int cardMinH: Style.space(44)
  readonly property int gutterW: Style.space(46)

  function relayout() {
    var out = []
    var bottom = -6
    for (var i = 0; i < spans.length; i++) {
      var s = spans[i]
      if (!keep(s)) continue
      var startM = toMin(s, "start")
      var endM = Math.max(startM + 1, toMin(s, "end"))
      var h = Math.min(Math.max(cardMinH, (endM - startM) * pxPerMin), 24 * 60 * pxPerMin)
      var y = Math.max(startM * pxPerMin, bottom + 6)
      out.push({ i: i, y: y, h: h })
      bottom = y + h
    }
    layout = out
  }

  onSpansChanged: relayout()
  onFilterChanged: relayout()
  Component.onCompleted: relayout()

  implicitWidth: 100
  implicitHeight: layout.length ? (layout[layout.length - 1].y + layout[layout.length - 1].h + Style.space(8))
                                : Style.space(40)
  clip: true

  function hhmm(m) {
    var h = Math.floor(m / 60), mm = m % 60
    return (h < 10 ? "0" : "") + h + ":" + (mm < 10 ? "0" : "") + mm
  }

  // The rail — a continuous line the cards' start-dots sit on.
  Rectangle {
    x: tl.gutterW - Style.space(8)
    y: 0
    width: 1
    height: tl.height
    color: tl.pal ? tl.pal.fgFill(0.10) : Qt.rgba(1, 1, 1, 0.10)
  }

  // Hour labels + ticks in the gutter — only at the hours the mapped
  // range crosses, spaced so they don't collide.
  Repeater {
    model: layout.length ? Math.ceil((layout[layout.length - 1].y + layout[layout.length - 1].h) / tl.pxPerMin / 60) + 1 : 0
    delegate: Item {
      property int hourMin: index * 60
      width: tl.width
      height: 1
      y: hourMin * tl.pxPerMin
      visible: y >= 0 && y < tl.height && (index % 2 === 0)

      Rectangle {
        anchors.left: parent.left
        anchors.leftMargin: tl.gutterW - Style.space(8)
        width: Style.space(5)
        height: 1
        color: tl.pal ? tl.pal.fgFill(0.22) : Qt.rgba(1, 1, 1, 0.22)
      }
      Text {
        x: 0
        width: tl.gutterW - Style.space(11)
        anchors.verticalCenter: parent.verticalCenter
        anchors.verticalCenterOffset: -0.5
        horizontalAlignment: Text.AlignRight
        text: tl.hhmm(hourMin)
        textFormat: Text.PlainText
        color: tl.pal ? tl.pal.dim : "gray"
        font.family: tl.pal ? tl.pal.fontFamily : ""
        font.pixelSize: Style.font.caption
      }
    }
  }

  // Activity cards.
  Repeater {
    model: tl.layout
    delegate: Rectangle {
      id: card
      required property var modelData
      property var span: tl.spans[modelData.i]
      property bool sel: modelData.i === tl.selectedIndex
      property bool tallH: modelData.h >= Style.space(58)

      x: tl.gutterW
      y: modelData.y
      width: tl.width - tl.gutterW - Style.space(2)
      height: modelData.h
      radius: Style.cornerRadius
      color: tl.pal ? tl.pal.payloadFill(span.color, span.category, sel ? 0.30 : 0.14)
                    : Qt.rgba(1, 1, 1, 0.1)
      border.color: tl.pal
        ? (sel ? tl.pal.accentFill(0.9) : tl.pal.fgFill(0.10))
        : "transparent"
      border.width: sel ? 2 : 1
      clip: true

      // Category accent edge — the color identity, like macOS Dayflow's
      // icon chip.
      Rectangle {
        anchors.left: parent.left
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        anchors.margins: Style.space(4)
        width: Style.space(4)
        radius: Style.space(2)
        color: tl.pal ? tl.pal.payloadColor(span.color, span.category) : "white"
      }

      // Start dot on the rail.
      Rectangle {
        x: -Style.space(5)
        y: Style.space(5)
        width: Style.space(5)
        height: width
        radius: width / 2
        color: tl.pal ? tl.pal.payloadColor(span.color, span.category) : "white"
      }

      Column {
        x: Style.space(14)
        y: Style.space(5)
        width: parent.width - Style.space(18)
        spacing: 0

        Text {
          width: parent.width
          text: span.title || ""
          textFormat: Text.PlainText
          color: tl.pal ? tl.pal.foreground : "white"
          font.family: tl.pal ? tl.pal.fontFamily : ""
          font.pixelSize: Style.font.caption
          font.bold: true
          elide: Text.ElideRight
          maximumLineCount: card.tallH ? 2 : 1
          wrapMode: card.tallH ? Text.WordWrap : Text.NoWrap
        }
        Text {
          width: parent.width
          text: span.start + " – " + span.end +
                (span.appName ? "  ·  " + span.appName : "") +
                (span.minutes ? "  ·  " + (tl.pal ? tl.pal.fmtDur(span.minutes) : span.minutes + "m") : "")
          textFormat: Text.PlainText
          color: tl.pal ? tl.pal.dim : "gray"
          font.family: tl.pal ? tl.pal.fontFamily : ""
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }
      }

      MouseArea {
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: tl.spanClicked(modelData.i)
      }
    }
  }

  // Current-time line when viewing today.
  Rectangle {
    visible: tl.showNow
    readonly property int nowMin: {
      var d = new Date()
      return d.getHours() * 60 + d.getMinutes()
    }
    anchors.left: parent.left
    anchors.leftMargin: tl.gutterW - Style.space(10)
    anchors.right: parent.right
    y: Math.min(tl.height - 1, Math.max(0, nowMin * tl.pxPerMin))
    height: 2
    color: tl.pal ? tl.pal.accentFill(0.95) : "white"
    opacity: nowMin * tl.pxPerMin <= tl.height ? 1 : 0
    Timer { interval: 30000; running: tl.showNow; repeat: true; onTriggered: parent.nowMinChanged() }
  }
}
