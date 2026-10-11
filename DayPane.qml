import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// The selected-day detail surface: day switcher, copy actions, calendar
// picker, filter chips, the macOS-style card timeline, and the detail
// card. Lives inside TodayTab — once in the compact stack, once in the
// expanded side-by-side layout.
Column {
  id: daypane
  required property var dayflow
  // Two-way via signals: the parent owns selection so it survives the
  // compact/expanded layout swap.
  property int selectedSpan: -1
  property string spanFilter: "all"
  signal spanSelected(int i)
  signal filterPicked(string f)

  width: parent.width
  spacing: Style.space(8)

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
        dayflow: daypane.dayflow
        label: modelData.label
        proc: daypane.dayflow.procByName(modelData.proc)
        onActivated: {
          daypane.dayflow.uilog(modelData.logName + " " + daypane.dayflow.viewDateStr())
          proc.command = ["bash", "-c",
            "dayflow export " + daypane.dayflow.viewDateStr() + modelData.extra + " | wl-copy"]
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
    dayflow: daypane.dayflow
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
          pal: daypane.dayflow
          frame: "quiet"
          compact: true
          selected: daypane.spanFilter === modelData.id
          text: modelData.label
          onClicked: daypane.filterPicked(modelData.id)
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
    function onSpansChanged() { if (daypane.selectedSpan >= dayflow.spans.length) daypane.spanSelected(-1) }
  }

  // Day calendar — proportional time blocks; click selects and the
  // detail card renders below.
  DayTimeline {
    visible: dayflow.spans.length > 0
    width: parent.width
    pal: dayflow
    spans: dayflow.spans
    filter: daypane.spanFilter
    selectedIndex: daypane.selectedSpan
    showNow: dayflow.dayOffset === 0
    onSpanClicked: function(i) { daypane.spanSelected(i) }
  }

  SpanCard {
    visible: daypane.selectedSpan >= 0 && daypane.selectedSpan < dayflow.spans.length
    width: parent.width
    pal: dayflow
    span: daypane.selectedSpan >= 0 && daypane.selectedSpan < dayflow.spans.length
      ? dayflow.spans[daypane.selectedSpan] : ({})
  }
}
