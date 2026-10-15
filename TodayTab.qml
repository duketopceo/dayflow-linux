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
  // Expanded gets real room — the side-by-side week+day layout crammed
  // into the compact cap is what made the right rail read as broken.
  implicitHeight: dayflow.expanded
    ? Math.min(col.implicitHeight, Style.space(640))
    : Math.min(col.implicitHeight, Style.space(360))
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
      // cardToSpan already maps start_ts to the first child's ts.
      dayflow.pendingSpanFor = "popup"
      dayflow.pendingSpanTs = Number(span.start_ts || 0)
    }
    dayflow.dayOffset = offset
    dayflow.loadTimeline()
  }

  // Click feedback: the detail card must be on screen when it selects —
  // in compact it can sit below the Flickable fold, so scroll it into
  // view; in both layouts it flashes so the click has a visible result.
  NumberAnimation {
    id: scrollAnim
    property: "contentY"
    duration: 160
    easing.type: Easing.OutCubic
  }
  // Minimal-reveal scroll: `flick` scrolls until `card`'s bounds (already
  // mapped into `host` content coordinates) are inside the viewport.
  function scrollCardIntoView(flick, host, card) {
    var p = card.mapToItem(host, 0, 0)
    if (!p) return
    var top = p.y, bottom = p.y + card.height
    if (top < flick.contentY) {
      scrollAnim.target = flick
      scrollAnim.to = Math.max(0, top - Style.space(4))
      scrollAnim.restart()
    } else if (bottom > flick.contentY + flick.height) {
      scrollAnim.target = flick
      scrollAnim.to = Math.min(Math.max(0, flick.contentHeight - flick.height),
        bottom - flick.height + Style.space(4))
      scrollAnim.restart()
    }
  }
  function revealDetail(pane) {
    if (!pane || !pane.detailCard) return
    var card = pane.detailCard
    Qt.callLater(function() {
      if (!card.visible) return
      card.flash()
      if (!root.visible) return
      // Expanded puts the card in the rail's own Flickable — scroll that,
      // not the outer page.
      if (dayflow.expanded) {
        scrollCardIntoView(expRail, pane, card)
        return
      }
      scrollCardIntoView(root, col, card)
    })
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
      // The grid column is the bound — expDay sits inside the rail
      // Flickable (self-bounded to weekWrapExp), so asking for its full
      // implicitHeight would reopen dead space below the rail.
      height: visible ? weekWrapExp.implicitHeight : 0
      spacing: Style.space(8)

      Column {
        id: weekWrapExp
        width: Math.floor(parent.width * 0.62)
        spacing: Style.space(4)

        WeekCalendar {
          id: weekCalExp
          width: parent.width
          pal: dayflow
          hourH: Style.space(36)
          todayScale: 1.0
          todayIndex: dayflow.todayIndex()
          selectedIndex: dayflow.todayIndex() + dayflow.dayOffset
          spansFor: function(d) { return dayflow.weekDaySpans(d) }
          onDayClicked: function(dayIndex, span) { root.jumpToDay(dayIndex, span) }
        }

        Text {
          width: parent.width
          height: visible ? Math.ceil(Style.font.caption * 1.4) : 0
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

      // Detail rail — bounded to the grid's height so the right column
      // fills exactly and scrolls its overflow, never trails dead space.
      Flickable {
        id: expRail
        width: parent.width - weekWrapExp.width - Style.space(8)
        height: weekWrapExp.implicitHeight
        contentHeight: expDay.implicitHeight
        contentWidth: width
        clip: true
        interactive: contentHeight > height

        DayPane {
          id: expDay
          width: parent.width
          dayflow: root.dayflow
          selectedSpan: root.selectedSpan
          spanFilter: root.spanFilter
          onSpanSelected: function(i) { root.selectedSpan = i; root.revealDetail(expDay) }
          onFilterPicked: function(f) { root.spanFilter = f }
        }
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
          height: visible ? Math.ceil(Style.font.caption * 1.4) : 0
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
      id: cmpDay
      visible: !dayflow.expanded
      width: parent.width
      dayflow: root.dayflow
      selectedSpan: root.selectedSpan
      spanFilter: root.spanFilter
      onSpanSelected: function(i) { root.selectedSpan = i; root.revealDetail(cmpDay) }
      onFilterPicked: function(f) { root.spanFilter = f }
    }

    // ---- this week: stats, charts, review, heatmap (expanded-gated) ----
    WeekStats {
      width: parent.width
      dayflow: root.dayflow
    }

  }
}
