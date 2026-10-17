import QtQuick
import qs.Commons
import qs.Ui

// FullView "Today" pane — the viewed day's card timeline: day switcher
// (DayPane's own — no separate DayNavRow, it would be a second nav), filter chips,
// app-icon blocks on a time gutter, click-to-detail (SpanCard with the
// per-app breakdown and frame thumbnail). Same surface as the popup's
// DayPane — `host` is the FullView window; dayflow state is read through it.
Flickable {
  id: pane

  property var host: null
  readonly property var dayflow: host ? host.dayflow : null
  property int selSpan: -1
  property string spanFilter: "all"

  width: parent ? parent.width : 0
  height: parent ? parent.height : 0
  contentHeight: todayCol.implicitHeight
  clip: true

  Column {
    id: todayCol
    width: pane.width - Style.space(32)
    x: Style.space(16)
    spacing: Style.space(10)

    LoadingBar {
      width: parent.width
      pal: pane.dayflow
      active: pane.dayflow ? pane.dayflow.timelineLoading : false
      estimate: 2
    }

    DayPane {
      id: todayDay
      paneScope: "fullview"
      width: parent.width
      dayflow: pane.dayflow
      selectedSpan: pane.selSpan
      spanFilter: pane.spanFilter
      onSpanSelected: function(i) { pane.selSpan = i; todayDay.detailCard.flash() }
      onFilterPicked: function(f) { pane.spanFilter = f }
    }
  }
}
