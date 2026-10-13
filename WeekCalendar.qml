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

  readonly property int gutterW: Style.space(26)
  // Pixels per hour on the shared axis; today's column width factor.
  // The expanded whole-week layout raises hourH and equalizes columns.
  property real hourH: Style.space(16)
  property real todayScale: 1.7
  // Density tier from the hour scale — drives how much a block renders:
  // "low" icon+1-line, "high" wrapped title+time, "full" +app+seams.
  readonly property string density: hourH >= 40 ? "full" : (hourH >= 24 ? "high" : "low")
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
          property real restH: Math.max(4,
            Math.min((endM - startM) * cal.pxPerMin, cal.height - y))
          // Second-most-common child app — the reference's primary/secondary
          // icon pair, applied to app icons. "" when the card is single-app.
          property string secondApp: {
            var kids = modelData.children || []
            var counts = {}
            for (var i = 0; i < kids.length; i++) {
              var a = kids[i].app || ""
              if (a && a !== modelData.app) counts[a] = (counts[a] || 0) + 1
            }
            var best = "", n = 0
            for (var k in counts) if (counts[k] > n) { n = counts[k]; best = k }
            return best
          }
          // Natural content height drives the hover-expand target (KTD2):
          // the unbounded wrapped title + caption inside card padding.
          property real naturalH: Math.max(restH,
            Style.space(6) + naturalMeasure.implicitHeight +
            (timeTxt.visible ? timeTxt.implicitHeight : 0))
          property bool expanded: false

          x: 2
          y: cal.headH + (startM - cal.viewStart) * cal.pxPerMin
          width: dayCol.width - 4
          height: expanded ? Math.min(naturalH, cal.height - y - 2) : restH
          z: expanded ? 10 : 0
          radius: Style.space(3)
          color: cal.pal ? cal.pal.payloadFill(modelData.color, modelData.category,
              blkMa.containsMouse ? 0.50 : 0.34) : Qt.rgba(1, 1, 1, 0.2)
          border.color: cal.pal ? cal.pal.payloadColor(modelData.color, modelData.category) : "transparent"
          border.width: 1
          clip: true

          Behavior on height { NumberAnimation { duration: 110 } }

          // Expand carries a short intent delay — a fast pointer sweep
          // across the grid shouldn't strobe cards (reference parity).
          Timer {
            id: expandTimer
            interval: 140
            onTriggered: blk.expanded = true
          }

          // Category accent edge — the color identity.
          Rectangle {
            anchors.left: parent.left
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            width: Style.space(3)
            radius: Style.space(2)
            color: cal.pal ? cal.pal.payloadColor(modelData.color, modelData.category) : "white"
          }

          Column {
            x: Style.space(6)
            y: 1
            width: parent.width - Style.space(8)
            spacing: 0

            Row {
              width: parent.width
              spacing: Style.space(2)

              Image {
                id: appImg
                visible: status === Image.Ready
                width: visible ? Style.space(9) : 0
                height: Style.space(9)
                y: Style.space(1)
                source: cal.pal ? cal.pal.appIcon(modelData.app) : ""
              }

              Text {
                id: titleWrap
                width: parent.width - (appImg.visible ? appImg.width + parent.spacing : 0)
                text: modelData.title || ""
                textFormat: Text.PlainText
                color: cal.pal ? cal.pal.foreground : "white"
                font.family: cal.pal ? cal.pal.fontFamily : ""
                font.pixelSize: Style.font.caption
                font.weight: Font.Medium
                // Wrapped line count from the card's measured room — the
                // reference formula: (height - pads - reserved) / lineH.
                property int lineH: Math.ceil(Style.font.caption * 1.25)
                property int avail: blk.height - Style.space(6) -
                  (timeTxt.visible ? timeTxt.implicitHeight : 0)
                maximumLineCount: Math.max(1, Math.floor(avail / lineH))
                wrapMode: cal.density === "low" || maximumLineCount <= 1
                  ? Text.NoWrap : Text.WordWrap
                elide: Text.ElideRight
                // Below ~64px wide a title is illegible fragments — at
                // compact density drop to icon-only and let hover speak.
                visible: blk.height >= 12 &&
                  (blk.width > 18) &&
                  (cal.density !== "low" || blk.width >= Style.space(64) || dayCol.isSel || dayCol.isToday)
              }
            }

            // Second app icon — full density only, top-right feels cramped;
            // it rides the caption line instead.
            Text {
              id: timeTxt
              visible: cal.density !== "low" && blk.height >= Style.space(30) && blk.width > 26
              width: parent.width
              text: (modelData.start || "") + "–" + (modelData.end || "") +
                (cal.density === "full" && modelData.appName ? " · " + modelData.appName : "")
              textFormat: Text.PlainText
              color: cal.pal ? cal.pal.dim : "gray"
              font.family: cal.pal ? cal.pal.fontFamily : ""
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
            }
          }

          // Unbounded wrap measurement — the hover-expand height target.
          Text {
            id: naturalMeasure
            visible: false
            width: parent.width - Style.space(14)
            text: modelData.title || ""
            textFormat: Text.PlainText
            font.family: cal.pal ? cal.pal.fontFamily : ""
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }

          // Secondary app icon — full density, pinned top-right so the
          // title still gets the row.
          Image {
            visible: cal.density === "full" && blk.secondApp !== "" &&
              blk.width > Style.space(30) && status === Image.Ready
            anchors.right: parent.right
            anchors.top: parent.top
            anchors.margins: Style.space(3)
            width: Style.space(8)
            height: Style.space(8)
            opacity: 0.8
            source: cal.pal && blk.secondApp !== "" ? cal.pal.appIcon(blk.secondApp) : ""
          }

          // Child seams — full density hairlines at each child block's
          // start, so a merged card still reveals where the app changed.
          Repeater {
            model: cal.density === "full" && blk.height >= Style.space(48)
              ? (modelData.children || []) : []
            delegate: Rectangle {
              required property int index
              required property var modelData
              visible: index > 0
              property int cm: cal.toMin(modelData, "start")
              y: Math.max(0, (cm - blk.startM) * cal.pxPerMin)
              x: Style.space(5)
              width: parent.width - Style.space(6)
              height: 1
              color: cal.pal ? cal.pal.fgFill(0.18) : Qt.rgba(1,1,1,0.18)
            }
          }

          MouseArea {
            id: blkMa
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onContainsMouseChanged: {
              cal.hoveredSpan = containsMouse ? modelData : null
              if (containsMouse) expandTimer.restart()
              else { expandTimer.stop(); blk.expanded = false }
            }
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
