import QtQuick
import qs.Commons

// 7-day calendar grid — the week as a real calendar, not a text list.
// Columns Mon..Sun share one fixed 00:00-24:00 vertical axis so every
// hour lands in the same place on every day; today gets an expanded
// column + the now-line. Each span is a duration-sized, category-colored
// block positioned by its epoch timestamps — click jumps the Today tab
// to that day.
Item {
  id: cal

  required property var pal
  // spansFor(dayIndex) -> [{start_ts,end_ts,start,end,title,category,color,...}]
  // Provided by callers (Panel exposes weekDaySpans); kept injectable so
  // the standalone app + harness can feed the same grid.
  property var spansFor: null
  property int todayIndex: -1
  // Which column is the viewed day (wash + bold). -1 = fall back to today.
  property int selectedIndex: -1
  // Set while the pointer is over a span block — the caller renders the
  // hover readout (the grid is too dense for inline labels on slivers).
  property var hoveredSpan: null
  signal dayClicked(int dayIndex, var span)

  function daySpans(d) { return cal.spansFor ? (cal.spansFor(d) || []) : [] }

  // Minute-of-day for a span edge. start_ts/end_ts are authoritative;
  // the "12:00 AM"-style strings are the fallback for injected fixtures.
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

  readonly property int gutterW: Style.space(30)
  // Pixels per hour on the shared axis; today's column width factor.
  // The expanded whole-week layout raises hourH and equalizes columns.
  property real hourH: Style.space(16)
  property real todayScale: 1.7
  implicitWidth: 100
  // Axis starts at the first recording of the week (floored to the 2h
  // gridline) and runs to 24:00 — no dead night hours at the top.
  implicitHeight: headH + (1440 - viewStart) * pxPerMin
  clip: true

  readonly property int headH: Style.space(18)
  readonly property real pxPerMin: hourH / 60
  readonly property int viewStart: {
    var m = 1440
    for (var d = 0; d < 7; d++) {
      var ss = cal.daySpans(d)
      for (var i = 0; i < ss.length; i++) {
        var sm = cal.toMin(ss[i], "start")
        if (sm < m) m = sm
      }
    }
    return m >= 1440 ? 0 : Math.floor(m / 120) * 120
  }

  // Column x/width for dayIndex — today's column stretches by todayScale.
  function colX(d) {
    var unit = (width - gutterW) / (6 + todayScale)
    var x = gutterW
    for (var i = 0; i < d; i++) x += (i === todayIndex ? unit * todayScale : unit)
    return x
  }
  function colW(d) {
    var unit = (width - gutterW) / (6 + todayScale)
    return d === todayIndex ? unit * todayScale : unit
  }

  // Gridlines + gutter labels every 2 hours from the first recording on.
  Repeater {
    model: Math.max(1, Math.ceil((1440 - cal.viewStart) / 120))
    delegate: Item {
      property int hourMin: cal.viewStart + index * 120
      width: cal.width
      height: 1
      y: cal.headH + (hourMin - cal.viewStart) * cal.pxPerMin

      Rectangle {
        anchors.left: parent.left
        anchors.leftMargin: cal.gutterW
        anchors.right: parent.right
        height: 1
        color: cal.pal ? cal.pal.fgFill(0.06) : Qt.rgba(1, 1, 1, 0.06)
      }
      Text {
        anchors.left: parent.left
        anchors.verticalCenter: parent.verticalCenter
        text: (hourMin / 60) + ":00"
        textFormat: Text.PlainText
        color: cal.pal ? cal.pal.dim : "gray"
        font.family: cal.pal ? cal.pal.fontFamily : ""
        font.pixelSize: Style.font.caption
      }
    }
  }

  // Day columns.
  Repeater {
    model: 7
    delegate: Item {
      id: dayCol
      property int dayIndex: index
      property bool isToday: index === cal.todayIndex
      property bool isSel: index === (cal.selectedIndex >= 0 ? cal.selectedIndex : cal.todayIndex)
      property var spans: cal.daySpans(index)

      x: cal.colX(index)
      y: 0
      width: cal.colW(index)
      height: cal.height

      // The viewed day gets a faint accent wash behind the whole column.
      Rectangle {
        anchors.fill: parent
        visible: dayCol.isSel
        radius: Style.space(4)
        color: cal.pal ? cal.pal.accentFill(0.06) : "transparent"
        border.color: cal.pal ? cal.pal.accentFill(0.25) : "transparent"
        border.width: 1
      }

      // Column separator.
      Rectangle {
        anchors.left: parent.left
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        width: 1
        color: cal.pal ? cal.pal.fgFill(0.08) : Qt.rgba(1, 1, 1, 0.08)
        visible: index > 0 && !dayCol.isToday
      }

      Text {
        anchors.top: parent.top
        anchors.horizontalCenter: parent.horizontalCenter
        height: cal.headH
        text: cal.pal ? cal.pal.dayName(index) : ""
        textFormat: Text.PlainText
        color: (dayCol.isSel || dayCol.isToday) ? (cal.pal ? cal.pal.foreground : "white")
                              : (cal.pal ? cal.pal.dim : "gray")
        font.family: cal.pal ? cal.pal.fontFamily : ""
        font.pixelSize: Style.font.caption
        font.bold: dayCol.isSel || dayCol.isToday
        verticalAlignment: Text.AlignVCenter
      }

      Repeater {
        model: dayCol.spans
        delegate: Rectangle {
          id: blk
          required property var modelData
          property int startM: Math.max(cal.viewStart, cal.toMin(modelData, "start"))
          property int endM: Math.max(startM + 1, cal.toMin(modelData, "end"))
          property real rawH: (endM - startM) * cal.pxPerMin
          property bool tall: rawH >= 13

          x: 2
          y: cal.headH + (startM - cal.viewStart) * cal.pxPerMin
          width: dayCol.width - 4
          height: Math.max(4, Math.min(rawH, cal.height - y))
          radius: Style.space(3)
          color: cal.pal ? cal.pal.payloadFill(modelData.color, modelData.category,
              blkMa.containsMouse ? 0.42 : 0.26) : Qt.rgba(1, 1, 1, 0.2)
          border.color: cal.pal ? cal.pal.payloadColor(modelData.color, modelData.category) : "transparent"
          border.width: 1
          clip: true

          Text {
            visible: blk.tall && blk.width > 18
            x: Style.space(3)
            y: 1
            width: parent.width - Style.space(6)
            text: modelData.title || ""
            textFormat: Text.PlainText
            color: cal.pal ? cal.pal.foreground : "white"
            font.family: cal.pal ? cal.pal.fontFamily : ""
            font.pixelSize: Style.font.caption
            elide: Text.ElideRight
          }

          MouseArea {
            id: blkMa
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onContainsMouseChanged:
              cal.hoveredSpan = containsMouse ? modelData : null
            onClicked: cal.dayClicked(dayCol.dayIndex, modelData)
          }
        }
      }
    }
  }

  // Now-line inside today's column.
  Rectangle {
    property int nowMin: {
      var d = new Date()
      return d.getHours() * 60 + d.getMinutes()
    }
    visible: cal.todayIndex >= 0 && nowMin >= cal.viewStart
    x: cal.colX(cal.todayIndex) + 1
    width: cal.colW(cal.todayIndex) - 2
    y: cal.headH + (nowMin - cal.viewStart) * cal.pxPerMin
    height: 2
    color: cal.pal ? cal.pal.accentFill(0.95) : "white"
    Timer {
      interval: 30000
      running: cal.todayIndex >= 0
      repeat: true
      onTriggered: parent.nowMin = (new Date()).getHours() * 60 + (new Date()).getMinutes()
    }
  }
}
