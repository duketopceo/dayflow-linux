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

    // ---- day switcher ----
    Row {
      width: parent.width
      spacing: Style.space(4)

      Rectangle {
        height: Style.space(28)
        width: Style.space(28)
        radius: Style.cornerRadius
        color: dPrev.containsMouse ? dayflow.accentFill(0.12) : dayflow.fgFill(0.04)
        border.color: dayflow.accentFill(0.4)
        Text {
          anchors.centerIn: parent
          text: "‹"
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
        }
        MouseArea {
          id: dPrev
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: dayflow.goDay(-1)
        }
      }

      Text {
        text: dayflow.viewDateLabel()
        textFormat: Text.PlainText
        color: dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        font.bold: true
        anchors.verticalCenter: parent.verticalCenter
      }

      Rectangle {
        height: Style.space(28)
        width: Style.space(28)
        radius: Style.cornerRadius
        color: dNext.containsMouse ? dayflow.accentFill(0.12) : dayflow.fgFill(0.04)
        border.color: dayflow.accentFill(0.4)
        opacity: dayflow.dayOffset < 0 ? 1 : 0.4
        Text {
          anchors.centerIn: parent
          text: "›"
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
        }
        MouseArea {
          id: dNext
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          enabled: dayflow.dayOffset < 0
          onClicked: dayflow.goDay(1)
        }
      }

      Text {
        visible: dayflow.dayOffset !== 0
        text: "back to today"
        textFormat: Text.PlainText
        color: backToday.containsMouse ? dayflow.foreground : dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        font.underline: backToday.containsMouse
        anchors.verticalCenter: parent.verticalCenter
        MouseArea {
          id: backToday
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: { dayflow.uilog("back to today"); dayflow.dayOffset = 0; dayflow.loadTimeline() }
        }
      }

      Rectangle {
        height: Style.space(28)
        width: calLbl.implicitWidth + Style.space(14)
        radius: Style.cornerRadius
        color: calPicker.visible ? dayflow.accentFill(0.15) : dayflow.fgFill(0.04)
        border.color: dayflow.accentFill(0.4)
        Text {
          id: calLbl
          anchors.centerIn: parent
          text: "Cal"
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
        }
        MouseArea {
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: { calPicker.visible = !calPicker.visible; dayflow.uilog("calendar " + (calPicker.visible ? "open" : "close")) }
        }
      }
    }

    // ---- copy actions for the viewed day ----
    Flow {
      width: parent.width
      spacing: Style.space(4)

      Repeater {
        // Copies the currently viewed day, not always today.
        model: [
          { label: "Copy day · md", proc: "copyProc", logName: "copy day md", extra: "" },
          { label: "Copy day · mini", proc: "copyMiniProc", logName: "copy day mini", extra: " --brief" }
        ]
        delegate: CopyButton {
          required property var modelData
          dayflow: root.dayflow
          label: modelData.label
          proc: root.dayflow.procByName(modelData.proc)
          onActivated: {
            root.dayflow.uilog(modelData.logName + " " + root.dayflow.viewDateStr())
            proc.command = ["bash", "-c",
              "dayflow export " + root.dayflow.viewDateStr() + modelData.extra + " | wl-copy"]
            proc.running = true
          }
        }
      }
    }

    // ---- calendar picker ----
    CalendarPicker {
      id: calPicker
      visible: false
      width: parent.width
      dayflow: root.dayflow
    }

    // ---- week calendar: the day selector + week overview in one grid ----
    Rectangle {
      visible: dayflow.weekBlocks.length > 0 || (dayflow.weekCards && dayflow.weekCards.length > 0)
      width: parent.width
      height: weekWrapCol.implicitHeight + Style.space(12)
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
          onDayClicked: function(dayIndex, span) {
            var offset = dayIndex - dayflow.todayIndex()
            dayflow.uilog("week cal jump " + offset)
            dayflow.dayOffset = offset
            dayflow.loadTimeline()
          }
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

    // ---- span filter chips — macOS Dayflow style ----
    Flow {
      visible: dayflow.spans.length > 0
      width: parent.width
      spacing: Style.space(4)

      Repeater {
        model: [
          { id: "all", label: "All tasks" },
          { id: "core", label: "Core tasks" },
          { id: "distract", label: "Distractions" },
          { id: "idle", label: "Idle time" }
        ]
        delegate: Item {
          required property var modelData
          width: fchip.implicitWidth
          height: fchip.implicitHeight
          PanelButton {
            id: fchip
            pal: root.dayflow
            frame: "quiet"
            compact: true
            selected: root.spanFilter === modelData.id
            text: modelData.label
            onClicked: root.spanFilter = modelData.id
          }
        }
      }
    }

    Text {
      visible: dayflow.spans.length === 0 && dayflow.errorText === "" && dayflow.configured
      width: parent.width
      text: "Nothing summarized yet — blocks land once per block interval (15 min by default)."
      textFormat: Text.PlainText
      color: dayflow.dim
      font.family: dayflow.fontFamily
      font.pixelSize: Style.font.body
      wrapMode: Text.WordWrap
    }

    Connections {
      target: dayflow
      function onSpansChanged() { if (root.selectedSpan >= dayflow.spans.length) root.selectedSpan = -1 }
    }

    // Day calendar — proportional time blocks; click selects and the
    // detail card renders below.
    DayTimeline {
      visible: dayflow.spans.length > 0
      width: parent.width
      pal: dayflow
      spans: dayflow.spans
      filter: root.spanFilter
      selectedIndex: root.selectedSpan
      showNow: dayflow.dayOffset === 0
      onSpanClicked: function(i) { root.selectedSpan = i }
    }

    SpanCard {
      visible: root.selectedSpan >= 0 && root.selectedSpan < dayflow.spans.length
      width: parent.width
      pal: dayflow
      span: root.selectedSpan >= 0 && root.selectedSpan < dayflow.spans.length
        ? dayflow.spans[root.selectedSpan] : ({})
    }

    // ---- this week: stats, charts, review, heatmap (expanded-gated) ----
    WeekStats {
      width: parent.width
      dayflow: root.dayflow
    }

  }
}
