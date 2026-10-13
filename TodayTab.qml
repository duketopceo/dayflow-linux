import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Flickable {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null
  // Day calendar selection — index into dayflow.spans; -1 = no detail card.
  property int selectedSpan: -1
  // Filter chips: all | core | distract | idle (DayTimeline.keep()).
  property string spanFilter: "all"
  width: parent.width
  implicitHeight: Math.min(col.implicitHeight, Style.space(360))
  height: implicitHeight
  contentHeight: col.implicitHeight
  clip: true

  // Shared click-through for both calendar placements: jump the day
  // detail to the clicked column's date, and when a specific block was
  // clicked mark its child ts so DayPane selects it once the day loads.
  function jumpToDay(dayIndex, span) {
    var offset = dayIndex - dayflow.todayIndex()
    dayflow.uilog("week cal jump " + offset)
    if (span) {
      var kids = span.children || []
      dayflow.pendingSpanTs = Number(kids.length ? kids[0].start_ts : (span.start_ts || 0))
    }
    dayflow.dayOffset = offset
    dayflow.loadTimeline()
  }

  Column {
    id: col
    width: parent.width
    spacing: Style.space(8)

    LoadingBar {
      width: parent.width
      pal: dayflow
      active: dayflow.timelineLoading
        || dayflow.procByName("weeklyProc").running
        || dayflow.procByName("weekTimelineProc").running
        || dayflow.procByName("insightsFetchProc").running
      estimate: 2
    }

    // ---- expanded: the whole week, like macOS Dayflow's main window —
    // a big equal-column 7-day grid on the left, the selected day's
    // card timeline on the right.
    Row {
      visible: dayflow.expanded
      width: parent.width
      height: visible ? Math.max(weekWrapExp.implicitHeight, expDay.implicitHeight) : 0
      spacing: Style.space(8)

      Column {
        id: weekWrapExp
        width: Math.floor(parent.width * 0.62)
        spacing: Style.space(4)

        WeekCalendar {
          id: weekCalExp
          width: parent.width
          pal: dayflow
          hourH: Style.space(30)
          todayScale: 1.0
          todayIndex: dayflow.todayIndex()
          selectedIndex: dayflow.todayIndex() + dayflow.dayOffset
          spansFor: function(d) { return dayflow.weekDaySpans(d) }
          onDayClicked: function(dayIndex, span) { root.jumpToDay(dayIndex, span) }
        }

        Text {
          width: parent.width
          height: visible ? implicitHeight : 0
          visible: weekCalExp.hoveredSpan !== null && weekCalExp.hoveredSpan !== undefined
          text: weekCalExp.hoveredSpan
            ? weekCalExp.hoveredSpan.start + "–" + weekCalExp.hoveredSpan.end +
              "  " + (weekCalExp.hoveredSpan.title || "") +
              (weekCalExp.hoveredSpan.appName ? "  ·  " + weekCalExp.hoveredSpan.appName : "") +
              (weekCalExp.hoveredSpan.minutes ? "  ·  " + dayflow.fmtDur(weekCalExp.hoveredSpan.minutes) : "")
            : ""
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }
      }

      DayPane {
        id: expDay
        width: parent.width - weekWrapExp.width - Style.space(8)
        dayflow: root.dayflow
        selectedSpan: root.selectedSpan
        spanFilter: root.spanFilter
        onSpanSelected: function(i) { root.selectedSpan = i }
        onFilterPicked: function(f) { root.spanFilter = f }
      }
    }

    // ---- compact: week calendar card on top, day detail below ----
    Rectangle {
      visible: !dayflow.expanded
        && (dayflow.weekBlocks.length > 0 || (dayflow.weekCards && dayflow.weekCards.length > 0))
      width: parent.width
      height: visible ? weekWrapCol.implicitHeight + Style.space(12) : 0
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)

      Column {
        id: weekWrapCol
        width: parent.width - Style.space(12)
        anchors.centerIn: parent
        spacing: Style.space(4)

        WeekCalendar {
          id: weekCal
          width: parent.width
          pal: dayflow
          todayIndex: dayflow.todayIndex()
          selectedIndex: dayflow.todayIndex() + dayflow.dayOffset
          spansFor: function(d) { return dayflow.weekDaySpans(d) }
          onDayClicked: function(dayIndex, span) { root.jumpToDay(dayIndex, span) }
        }

        // Hover readout — slivers too thin for inline labels still get a
        // literal "what was I doing" line here.
        Text {
          width: parent.width
          height: visible ? implicitHeight : 0
          visible: weekCal.hoveredSpan !== null && weekCal.hoveredSpan !== undefined
          text: weekCal.hoveredSpan
            ? weekCal.hoveredSpan.start + "–" + weekCal.hoveredSpan.end +
              "  " + (weekCal.hoveredSpan.title || "") +
              (weekCal.hoveredSpan.appName ? "  ·  " + weekCal.hoveredSpan.appName : "") +
              (weekCal.hoveredSpan.minutes ? "  ·  " + dayflow.fmtDur(weekCal.hoveredSpan.minutes) : "")
            : ""
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }
      }
    }

    DayPane {
      visible: !dayflow.expanded
      width: parent.width
      dayflow: root.dayflow
      selectedSpan: root.selectedSpan
      spanFilter: root.spanFilter
      onSpanSelected: function(i) { root.selectedSpan = i }
      onFilterPicked: function(f) { root.spanFilter = f }
    }

    // ---- this week: stats, charts, review, heatmap (expanded-gated) ----
    WeekStats {
      width: parent.width
      dayflow: root.dayflow
    }

  }
}
