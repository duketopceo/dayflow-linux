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

  // Click feedback: the detail card must be on screen when it selects —
  // in compact it can sit below the Flickable fold, so scroll it into
  // view; in both layouts it flashes so the click has a visible result.
  NumberAnimation {
    id: scrollAnim
    property: "contentY"
    duration: 160
    easing.type: Easing.OutCubic
  }
  // One anim per Flickable — retargeting a running NumberAnimation cancels
  // the in-flight scroll.
  NumberAnimation {
    id: outerScrollAnim
    property: "contentY"
    duration: 160
    easing.type: Easing.OutCubic
  }
  // Minimal-reveal scroll: `flick` scrolls until `card`'s bounds (already
  // mapped into `host` content coordinates) are inside the viewport.
  function scrollCardIntoView(flick, host, card, anim) {
    var p = card.mapToItem(host, 0, 0)
    if (!p) return
    var top = p.y, bottom = p.y + card.height
    anim.target = flick
    if (top < flick.contentY) {
      anim.to = Math.max(0, top - Style.space(4))
      anim.restart()
    } else if (bottom > flick.contentY + flick.height) {
      anim.to = Math.min(Math.max(0, flick.contentHeight - flick.height),
        bottom - flick.height + Style.space(4))
      anim.restart()
    }
  }
  function revealDetail(pane) {
    if (!pane || !pane.detailCard) return
    var card = pane.detailCard
    Qt.callLater(function() {
      if (!card.visible) return
      card.flash()
      if (!root.visible) return
      // Expanded puts the card in the rail's own Flickable — but the rail
      // itself can sit below the outer viewport's fold, so scroll the
      // outer page to the card first, then the rail within it.
      if (dayflow.expanded) {
        scrollCardIntoView(root, col, card, outerScrollAnim)
        scrollCardIntoView(expRail, pane, card, scrollAnim)
        return
      }
      scrollCardIntoView(root, col, card, outerScrollAnim)
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

        WeekHero {
          width: parent.width
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
    PanelCard {
      pal: dayflow
      visible: !dayflow.expanded
        && (dayflow.weekBlocks.length > 0 || (dayflow.weekCards && dayflow.weekCards.length > 0))
      width: parent.width
      height: visible ? weekWrapCol.implicitHeight + Style.space(12) : 0

      Column {
        id: weekWrapCol
        width: parent.width - Style.space(12)
        anchors.centerIn: parent
        spacing: Style.space(4)

        WeekHero {
          width: parent.width
          dayflow: root.dayflow
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
