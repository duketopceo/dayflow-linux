import QtQuick
import qs.Commons
import qs.Ui

// FullView "Week" pane — the week calendar is the hero: a full-density
// 7-day time grid sized to fill the pane, with the clicked day's card
// timeline + detail beside it. Analytics (forecast, stats, heatmap,
// breakdowns, summary) follow below.
// `host` is the FullView window; `host.forecast` holds the forecast payload.
Flickable {
  id: pane

  property var host: null
  readonly property var dayflow: host ? host.dayflow : null
  readonly property var forecast: host ? host.forecast : null
  // Day detail state for the hero's right column.
  property int selSpan: -1
  property string spanFilter: "all"

  width: parent ? parent.width : 0
  height: parent ? parent.height : 0
  contentHeight: weekCol.implicitHeight
  clip: true

  Column {
    id: weekCol
    width: pane.width - Style.space(32)
    x: Style.space(16)
    spacing: Style.space(10)

    // ---- hero: the week grid + clicked day's detail ----
    Row {
      width: parent.width
      height: Math.max(heroCal.implicitHeight, heroDay.implicitHeight)
      spacing: Style.space(10)

      WeekHero {
        id: heroCal
        width: Math.floor(parent.width * 0.62)
        dayflow: pane.dayflow
        logPrefix: "fullview week cal jump "
        spanScope: "fullview"
        // Fill the pane's visible height with the day's active span —
        // the "no wasted space" tier. Clamped into the full-density
        // band so every card carries icon + wrapped title + time + app.
        hourH: Math.min(80, Math.max(40,
          (pane.height - Style.space(48)) * 60 / Math.max(60, 1440 - heroCal.calendar.viewStart)))
        todayScale: 1.0
      }

      DayPane {
        id: heroDay
        paneScope: "fullview"
        width: parent.width - heroCal.width - Style.space(10)
        dayflow: pane.dayflow
        selectedSpan: pane.selSpan
        spanFilter: pane.spanFilter
        onSpanSelected: function(i) { pane.selSpan = i; heroDay.detailCard.flash() }
        onFilterPicked: function(f) { pane.spanFilter = f }
      }
    }

    // tomorrow forecast strip
    Rectangle {
      visible: pane.forecast !== null && (pane.forecast.items || []).length > 0
      width: parent.width
      height: fcRow.implicitHeight + Style.space(14)
      radius: Style.cornerRadius
      color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
      border.color: pane.dayflow ? pane.dayflow.accentFill(0.25) : "transparent"

      Flow {
        id: fcRow
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(8)

        Text {
          text: "Tomorrow · " + (pane.forecast ? pane.forecast.weekday : "") +
                " · " + (pane.forecast ? pane.forecast.confidence : "") + " conf" +
                (pane.forecast && pane.forecast.confidence_score !== undefined
                  ? " · jev " + Math.round(pane.forecast.confidence_score * 100) + "%" : "")
          textFormat: Text.PlainText
          color: pane.dayflow ? pane.dayflow.dim : "gray"
          font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
          font.pixelSize: Style.font.caption
        }

        Repeater {
          model: pane.forecast ? (pane.forecast.items || []).slice(0, 5) : []

          delegate: Rectangle {
            required property var modelData
            height: Style.space(20)
            width: fcChipText.implicitWidth + Style.space(14)
            radius: Style.cornerRadius
            color: pane.dayflow
              ? pane.dayflow.payloadFill("", modelData.category, 0.15)
              : "transparent"
            border.color: pane.dayflow
              ? pane.dayflow.payloadFill("", modelData.category, 0.4)
              : "transparent"

            Text {
              id: fcChipText
              anchors.centerIn: parent
              text: (pane.dayflow ? pane.dayflow.appDisplayName(modelData.category) : modelData.category) +
                    " " + Math.round(modelData.pct) + "%"
              textFormat: Text.PlainText
              color: pane.dayflow ? pane.dayflow.foreground : "white"
              font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
            }
          }
        }
      }
    }

    // stat strip
    Flow {
      width: parent.width
      spacing: Style.space(8)

      Repeater {
        model: pane.dayflow ? [
          { label: "Tracked",     val: pane.dayflow.fmtDur(pane.dayflow.weeklyPayload.total_minutes) },
          { label: "Focus",       val: pane.dayflow.fmtDur(pane.dayflow.weeklyPayload.focus_minutes) },
          { label: "Distraction", val: pane.dayflow.fmtDur(pane.dayflow.weeklyPayload.distraction_minutes) },
          { label: "Shifts",      val: String(pane.dayflow.weeklyPayload.context_shift_count) }
        ] : []

        delegate: PanelCard {
          pal: pane.dayflow
          required property var modelData
          height: Style.space(52)
          width: host && host.compactLayout
            ? (weekCol.width - Style.space(8)) / 2
            : (weekCol.width - Style.space(24)) / 4

          Column {
            anchors.centerIn: parent
            spacing: Style.space(2)

            Text {
              anchors.horizontalCenter: parent.horizontalCenter
              text: modelData.val
              color: pane.dayflow ? pane.dayflow.foreground : "white"
              font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
              font.pixelSize: Style.font.subtitle
              font.bold: true
            }
            Text {
              anchors.horizontalCenter: parent.horizontalCenter
              text: modelData.label
              textFormat: Text.PlainText
              color: pane.dayflow ? pane.dayflow.dim : "gray"
              font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
            }
          }
        }
      }
    }

    // week-over-week trends
    PanelCard {
      id: trendCard
      pal: pane.dayflow
      visible: pane.dayflow !== null && pane.dayflow.weeklyPayload.trends !== undefined &&
        pane.dayflow.weeklyPayload.trends !== null && pane.dayflow.weeklyPayload.trends.has_prev
      width: parent.width
      height: trendCol.implicitHeight + Style.space(16)

      property var trends: pane.dayflow && pane.dayflow.weeklyPayload.trends
        ? pane.dayflow.weeklyPayload.trends : null

      function deltaText(mins) {
        var sign = mins > 0 ? "+" : mins < 0 ? "-" : "±"
        return sign + (pane.dayflow ? pane.dayflow.fmtDur(Math.abs(mins)) : String(Math.abs(mins)))
      }

      Column {
        id: trendCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(4)

        Text {
          text: "vs last week" +
            (trendCard.trends ? "  ·  prev " +
              (pane.dayflow ? pane.dayflow.fmtDur(trendCard.trends.prev_total_minutes) : "") : "")
          color: pane.dayflow ? pane.dayflow.foreground : "white"
          font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
          font.bold: true
          bottomPadding: Style.space(4)
        }

        // headline metric deltas
        Flow {
          width: parent.width
          spacing: Style.space(6)

          Repeater {
            model: trendCard.trends ? [
              { label: "Tracked",     delta: trendCard.trends.total_delta_minutes },
              { label: "Focus",       delta: trendCard.trends.focus_delta_minutes },
              { label: "Distraction", delta: trendCard.trends.distraction_delta_minutes },
              { label: "Shifts",      delta: trendCard.trends.shift_delta_count }
            ] : []

            delegate: Rectangle {
              required property var modelData
              height: Style.space(22)
              width: deltaChip.implicitWidth + Style.space(16)
              radius: Style.cornerRadius
              color: pane.dayflow ? pane.dayflow.fgFill(0.05) : "transparent"
              border.color: pane.dayflow ? pane.dayflow.fgFill(0.10) : "transparent"

              Text {
                id: deltaChip
                anchors.centerIn: parent
                text: modelData.label + " " +
                  (modelData.label === "Shifts"
                    ? (modelData.delta > 0 ? "+" : "") + modelData.delta
                    : trendCard.deltaText(modelData.delta))
                textFormat: Text.PlainText
                color: modelData.delta > 0
                  ? (pane.dayflow ? pane.dayflow.foreground : "white")
                  : (pane.dayflow ? pane.dayflow.dim : "gray")
                font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
              }
            }
          }
        }

        // per-category deltas
        Repeater {
          model: trendCard.trends ? trendCard.trends.categories.slice(0, 6) : []

          delegate: Row {
            required property var modelData
            width: trendCol.width
            spacing: Style.space(8)

            Text {
              width: parent.width - Style.space(140)
              text: modelData.display || modelData.name
              textFormat: Text.PlainText
              color: pane.dayflow ? pane.dayflow.foreground : "white"
              font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
              anchors.verticalCenter: parent.verticalCenter
            }

            Text {
              width: Style.space(132) - parent.spacing
              horizontalAlignment: Text.AlignRight
              anchors.verticalCenter: parent.verticalCenter
              text: trendCard.deltaText(modelData.delta_minutes) +
                "  (" + (modelData.delta_share > 0 ? "+" : "") +
                Math.round(modelData.delta_share) + "pt)"
              textFormat: Text.PlainText
              color: modelData.delta_minutes > 0
                ? (pane.dayflow ? pane.dayflow.foreground : "white")
                : (pane.dayflow ? pane.dayflow.dim : "gray")
              font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
            }
          }
        }
      }
    }

    // heatmap — 7 days x 24 hours
    PanelCard {
      pal: pane.dayflow
      width: parent.width
      height: heatCol.implicitHeight + Style.space(16)

      Column {
        id: heatCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(3)

        Text {
          text: "Focus heatmap"
          color: pane.dayflow ? pane.dayflow.foreground : "white"
          font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
          font.bold: true
          bottomPadding: Style.space(4)
        }

        Repeater {
          model: 7

          delegate: Row {
            required property int index
            spacing: Style.space(2)

            Text {
              width: Style.space(30)
              text: pane.dayflow ? pane.dayflow.dayName(index) : ""
              color: index === (pane.dayflow ? pane.dayflow.todayIndex() : -1)
                ? (pane.dayflow ? pane.dayflow.foreground : "white")
                : (pane.dayflow ? pane.dayflow.dim : "gray")
              font.bold: index === (pane.dayflow ? pane.dayflow.todayIndex() : -1)
              font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
              anchors.verticalCenter: parent.verticalCenter
            }

            Repeater {
              model: 24

              delegate: Rectangle {
                required property int index
                property int dayIndex: parent ? parent.index : 0
                width: Style.space(16)
                height: Style.space(12)
                radius: Style.space(2)
                color: pane.dayflow
                  ? pane.dayflow.cellColor(pane.dayflow.categoryForHour(dayIndex, index))
                  : "transparent"
              }
            }
          }
        }
      }
    }

    // two columns: category donut legend + top apps
    Flow {
      width: parent.width
      spacing: Style.space(10)

      PanelCard {
        pal: pane.dayflow
        width: host && host.compactLayout
          ? parent.width
          : (parent.width - Style.space(10)) / 2
        height: catCol.implicitHeight + Style.space(16)

        Column {
          id: catCol
          width: parent.width - Style.space(16)
          anchors.centerIn: parent
          spacing: Style.space(4)

          Text {
            text: "Categories"
            color: pane.dayflow ? pane.dayflow.foreground : "white"
            font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
            font.bold: true
            bottomPadding: Style.space(4)
          }

          Repeater {
            model: pane.dayflow ? pane.dayflow.weeklyPayload.category_donut : []

            delegate: Row {
              required property var modelData
              width: catCol.width
              spacing: Style.space(8)

              Rectangle {
                width: Style.space(10)
                height: Style.space(10)
                radius: Style.space(5)
                anchors.verticalCenter: parent.verticalCenter
                color: pane.dayflow
                  ? pane.dayflow.payloadColor(modelData.color, modelData.name)
                  : "gray"
              }

              Text {
                width: parent.width - Style.space(80)
                text: pane.dayflow ? pane.dayflow.appDisplayName(modelData.display || modelData.name) : modelData.name
                textFormat: Text.PlainText
                color: pane.dayflow ? pane.dayflow.foreground : "white"
                font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
                elide: Text.ElideRight
              }

              Text {
                width: Style.space(72) - parent.spacing
                horizontalAlignment: Text.AlignRight
                text: pane.dayflow ? pane.dayflow.fmtDur(modelData.minutes) : ""
                color: pane.dayflow ? pane.dayflow.dim : "gray"
                font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
              }
            }
          }
        }
      }

      PanelCard {
        pal: pane.dayflow
        width: host && host.compactLayout
          ? parent.width
          : (parent.width - Style.space(10)) / 2
        height: appCol.implicitHeight + Style.space(16)

        Column {
          id: appCol
          width: parent.width - Style.space(16)
          anchors.centerIn: parent
          spacing: Style.space(4)

          Text {
            text: "Top apps"
            color: pane.dayflow ? pane.dayflow.foreground : "white"
            font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
            font.bold: true
            bottomPadding: Style.space(4)
          }

          Repeater {
            model: pane.dayflow ? pane.dayflow.weeklyPayload.app_treemap : []

            delegate: Row {
              required property var modelData
              width: appCol.width
              spacing: Style.space(8)

              Text {
                width: parent.width - Style.space(70)
                text: pane.dayflow ? pane.dayflow.appDisplayName(modelData.display || modelData.name) : modelData.name
                textFormat: Text.PlainText
                color: pane.dayflow ? pane.dayflow.foreground : "white"
                font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
                elide: Text.ElideRight
              }

              Text {
                width: Style.space(62) - parent.spacing
                horizontalAlignment: Text.AlignRight
                text: pane.dayflow ? pane.dayflow.fmtDur(modelData.minutes) : ""
                color: pane.dayflow ? pane.dayflow.dim : "gray"
                font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
              }
            }
          }
        }
      }
    }

    // week summary
    PanelCard {
      pal: pane.dayflow
      visible: pane.dayflow !== null && pane.dayflow.weekSummary !== ""
      width: parent.width
      height: sumText.implicitHeight + Style.space(20)

      Text {
        id: sumText
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        text: pane.dayflow ? pane.dayflow.weekSummary : ""
        textFormat: Text.PlainText
        color: pane.dayflow ? pane.dayflow.foreground : "white"
        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
        font.pixelSize: Style.font.body
        wrapMode: Text.WordWrap
      }
    }

    Item { width: 1; height: Style.space(8) }
  }
}
