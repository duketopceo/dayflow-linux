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

  // Minute-of-day + midnight-crossing rules live in SpanTime — shared
  // with DayTimeline so both surfaces interpret spans identically.
  SpanTime { id: spanTime }

  readonly property int gutterW: Style.space(26)
  // Pixels per hour on the shared axis; today's column width factor.
  // The expanded whole-week layout raises hourH and equalizes columns.
  property real hourH: Style.space(20)
  property real todayScale: 1.7
  // How many day columns are visible at once; the rest scroll
  // horizontally. 7 = the whole week (no scroll).
  property int visibleDays: 7
  // Density tier from the hour scale — drives how much a block renders:
  // "low" icon+1-line, "high" wrapped title+time, "full" +app+seams.
  readonly property string density: hourH >= 40 ? "full" : (hourH >= 24 ? "high" : "low")
  implicitWidth: 100
  // Axis spans the recorded week: first activity floored to the 2h
  // gridline through the last activity ceiled to it (8h floor) — no
  // dead night hours at either end.
  implicitHeight: headH + (viewEnd - viewStart) * pxPerMin
  clip: true

  readonly property int headH: Style.space(18)
  readonly property real pxPerMin: hourH / 60
  // Materialize each day's span list once per invalidation — viewStart,
  // viewEnd, and the seven columns each scanned all weekCards otherwise
  // (3× full materialization per change, per instance).
  readonly property var perDay: {
    var out = []
    for (var d = 0; d < 7; d++) out.push(daySpans(d))
    return out
  }

  readonly property int viewStart: {
    var m = 1440
    for (var d = 0; d < 7; d++) {
      var ss = cal.perDay[d]
      for (var i = 0; i < ss.length; i++) {
        var sm = spanTime.toMin(ss[i], "start")
        if (sm < m) m = sm
      }
    }
    return m >= 1440 ? 0 : Math.floor(m / 120) * 120
  }

  readonly property int viewEnd: {
    var m = 0
    for (var d = 0; d < 7; d++) {
      var ss = cal.perDay[d]
      for (var i = 0; i < ss.length; i++) {
        var em = spanTime.endMin(ss[i], spanTime.toMin(ss[i], "start"))
        if (em > m) m = em
      }
    }
    var e = m <= 0 ? 1440 : Math.min(1440, Math.ceil(m / 120) * 120)
    // The 8h floor stays inside the day — an evening-only week must not
    // push the axis past 24:00 (gutter would print "26:00").
    return Math.max(Math.min(1440, viewStart + 480), e)
  }

  // Content width of the scrollable column band: full week when
  // visibleDays >= 7, otherwise stretched so visibleDays fit the viewport.
  readonly property real daysViewW: Math.max(0, width - gutterW)
  readonly property real daysContentW: Math.max(0, visibleDays >= 7 ? daysViewW : daysViewW * 7 / visibleDays)
  // Column x/width for dayIndex inside the scroll band — today's column
  // stretches by todayScale.
  function colX(d) {
    var unit = daysContentW / (6 + todayScale)
    var x = 0
    for (var i = 0; i < d; i++) x += (i === todayIndex ? unit * todayScale : unit)
    return x
  }
  function colW(d) {
    var unit = daysContentW / (6 + todayScale)
    return d === todayIndex ? unit * todayScale : unit
  }
  // Bring the viewed day into the scrolled viewport (no-op at 7 days).
  function scrollToDay(d) {
    if (visibleDays >= 7 || d < 0) return
    var cx = colX(d) + colW(d) / 2 - colFlick.width / 2
    colFlick.contentX = Math.max(0, Math.min(daysContentW - colFlick.width, cx))
  }
  onSelectedIndexChanged: scrollToDay(selectedIndex >= 0 ? selectedIndex : todayIndex)
  onVisibleDaysChanged: scrollToDay(selectedIndex >= 0 ? selectedIndex : todayIndex)
  Component.onCompleted: scrollToDay(selectedIndex >= 0 ? selectedIndex : todayIndex)

  // Gridlines + gutter labels every 2 hours across the recorded span.
  Repeater {
    model: Math.max(1, Math.floor((cal.viewEnd - cal.viewStart) / 120) + 1)
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
  // Day columns — inside a horizontal scroll band when visibleDays < 7;
  // the gutter and gridlines stay fixed while the week pans underneath.
  Flickable {
    id: colFlick
    x: cal.gutterW
    y: 0
    width: Math.max(0, cal.daysViewW)
    height: cal.height
    contentWidth: cal.daysContentW
    contentHeight: cal.height
    clip: true
    interactive: cal.visibleDays < 7
    onWidthChanged: cal.scrollToDay(cal.selectedIndex >= 0 ? cal.selectedIndex : cal.todayIndex)
    WheelHandler {
      target: colFlick
      enabled: cal.visibleDays < 7
      onWheel: function(e) {
        var dx = e.pixelDelta.x !== 0 ? -e.pixelDelta.x : -e.angleDelta.y / 2
        var nx = Math.max(0, Math.min(cal.daysContentW - colFlick.width,
                                      colFlick.contentX + dx))
        e.accepted = nx !== colFlick.contentX
        colFlick.contentX = nx
      }
    }
    Repeater {
      model: 7
      delegate: Item {
        id: dayCol
        property int dayIndex: index
        property bool isToday: index === cal.todayIndex
        property bool isSel: index === (cal.selectedIndex >= 0 ? cal.selectedIndex : cal.todayIndex)
        property var spans: cal.perDay[index]

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
            property int startM: Math.max(cal.viewStart, spanTime.toMin(modelData, "start"))
            property int endM: Math.max(startM + 1, Math.min(cal.viewEnd, spanTime.endMin(modelData, startM)))
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

            // Title type scale — a point bigger once the density can show
            // wrapped text; naturalMeasure must share it or the expand
            // target mismeasures.
            readonly property real titlePx: cal.density === "low"
              ? Style.font.caption : Style.font.body
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
                  // Icon-only columns are the reading surface at compact
                  // density — they get the largest glyph.
                  width: visible ? Style.space(cal.density === "low" ? 12 : 10) : 0
                  height: Style.space(cal.density === "low" ? 12 : 10)
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
                  font.pixelSize: blk.titlePx
                  font.weight: Font.Medium
                  // Wrapped line count from the card's measured room — the
                  // reference formula: (height - pads - reserved) / lineH.
                  property int lineH: Math.ceil(blk.titlePx * 1.25)
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
              font.pixelSize: blk.titlePx
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
                property int cm: spanTime.toMin(modelData, "start")
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
      visible: cal.todayIndex >= 0 && nowMin >= cal.viewStart && nowMin <= cal.viewEnd
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

}
