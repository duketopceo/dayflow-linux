import QtQuick
import qs.Commons

// The 7-day week grid + its hover readout + the click-to-jump contract,
// shared by the popup's Today tab (compact card and expanded split) and
// FullView's Week pane. A block click jumps the viewed day to that
// column and queues the span's first child ts so DayPane selects it once
// the day's timeline lands.
//
// Host-agnostic: reads only `dayflow` (the Panel root / pal), WeekCalendar
// and qs.Commons Style; no other Omarchy-only types.
Column {
  id: hero

  property var dayflow: null
  // Forwarded to WeekCalendar (same defaults as WeekCalendar's own).
  property real hourH: Style.space(16)
  property real todayScale: 1.7
  // Day columns visible at once; the rest scroll (default 3).
  property int visibleDays: 3
  // Log prefix so the uilog trail still tells popup from FullView.
  property string logPrefix: "week cal jump "
  // Which surface's DayPanes may consume the jump (popup | fullview).
  property string spanScope: "popup"

  readonly property alias calendar: cal

  function jumpToDay(dayIndex, span) {
    if (!dayflow) return
    var offset = dayIndex - dayflow.todayIndex()
    dayflow.uilog(logPrefix + offset)
    if (span) {
      var kids = span.children || []
      dayflow.pendingSpanFor = hero.spanScope
      dayflow.pendingSpanTs = Number(kids.length ? kids[0].start_ts : (span.start_ts || 0))
    } else {
      // A blank-column jump abandons any armed pending selection.
      dayflow.pendingSpanTs = 0
      dayflow.pendingSpanFor = ""
    }
    dayflow.dayOffset = offset
    dayflow.loadTimeline()
  }

  spacing: Style.space(4)

  WeekCalendar {
    id: cal
    width: parent.width
    pal: hero.dayflow
    hourH: hero.hourH
    todayScale: hero.todayScale
    visibleDays: hero.visibleDays
    todayIndex: hero.dayflow ? hero.dayflow.todayIndex() : -1
    selectedIndex: hero.dayflow ? hero.dayflow.todayIndex() + hero.dayflow.dayOffset : -1
    spansFor: function(d) { return hero.dayflow ? hero.dayflow.weekDaySpans(d) : [] }
    onDayClicked: function(dayIndex, span) { hero.jumpToDay(dayIndex, span) }
  }

  // Hover readout — slivers too thin for inline labels still get a
  // literal "what was I doing" line here.
  Text {
    width: parent.width
    height: visible ? implicitHeight : 0
    visible: cal.hoveredSpan !== null && cal.hoveredSpan !== undefined
    text: cal.hoveredSpan
      ? cal.hoveredSpan.start + "–" + cal.hoveredSpan.end +
        "  " + (cal.hoveredSpan.title || "") +
        (cal.hoveredSpan.appName ? "  ·  " + cal.hoveredSpan.appName : "") +
        (cal.hoveredSpan.minutes ? "  ·  " + hero.dayflow.fmtDur(cal.hoveredSpan.minutes) : "")
      : ""
    textFormat: Text.PlainText
    color: hero.dayflow ? hero.dayflow.foreground : "white"
    font.family: hero.dayflow ? hero.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
    elide: Text.ElideRight
  }
}
