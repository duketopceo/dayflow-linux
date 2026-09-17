import QtQuick
import qs.Commons
import qs.Ui

// FullView "Week" pane — forecast strip, stat strip, focus heatmap,
// category/app breakdowns, and the weekly summary.
// `host` is the FullView window; `host.forecast` holds the forecast payload.
Flickable {
  id: pane

  property var host: null
  readonly property var dayflow: host ? host.dayflow : null
  readonly property var forecast: host ? host.forecast : null

  width: parent ? parent.width : 0
  height: parent ? parent.height : 0
  contentHeight: weekCol.implicitHeight
  clip: true

  Column {
    id: weekCol
    width: pane.width - Style.space(32)
    x: Style.space(16)
    spacing: Style.space(12)

    // tomorrow forecast strip
    Rectangle {
      visible: pane.forecast !== null && (pane.forecast.items || []).length > 0
      width: parent.width
      height: fcRow.implicitHeight + Style.space(14)
      radius: Style.cornerRadius
      color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
      border.color: pane.dayflow ? pane.dayflow.accentFill(0.25) : "transparent"

      Row {
        id: fcRow
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(8)

        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: "Tomorrow · " + (pane.forecast ? pane.forecast.weekday : "") +
                " · " + (pane.forecast ? pane.forecast.confidence : "") + " conf"
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
    Row {
      width: parent.width
      spacing: Style.space(8)

      Repeater {
        model: pane.dayflow ? [
          { label: "Tracked",     val: pane.dayflow.fmtDur(pane.dayflow.weeklyPayload.total_minutes) },
          { label: "Focus",       val: pane.dayflow.fmtDur(pane.dayflow.weeklyPayload.focus_minutes) },
          { label: "Distraction", val: pane.dayflow.fmtDur(pane.dayflow.weeklyPayload.distraction_minutes) },
          { label: "Shifts",      val: String(pane.dayflow.weeklyPayload.context_shift_count) }
        ] : []

        delegate: Rectangle {
          required property var modelData
          height: Style.space(52)
          width: (weekCol.width - Style.space(24)) / 4
          radius: Style.cornerRadius
          color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
          border.color: pane.dayflow ? pane.dayflow.fgFill(0.08) : "transparent"

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

    // heatmap — 7 days x 24 hours
    Rectangle {
      width: parent.width
      height: heatCol.implicitHeight + Style.space(16)
      radius: Style.cornerRadius
      color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
      border.color: pane.dayflow ? pane.dayflow.fgFill(0.08) : "transparent"

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
    Row {
      width: parent.width
      spacing: Style.space(10)

      Rectangle {
        width: (parent.width - Style.space(10)) / 2
        height: catCol.implicitHeight + Style.space(16)
        radius: Style.cornerRadius
        color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
        border.color: pane.dayflow ? pane.dayflow.fgFill(0.08) : "transparent"

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
                anchors.right: parent.right
                text: pane.dayflow ? pane.dayflow.fmtDur(modelData.minutes) : ""
                color: pane.dayflow ? pane.dayflow.dim : "gray"
                font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
              }
            }
          }
        }
      }

      Rectangle {
        width: (parent.width - Style.space(10)) / 2
        height: appCol.implicitHeight + Style.space(16)
        radius: Style.cornerRadius
        color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
        border.color: pane.dayflow ? pane.dayflow.fgFill(0.08) : "transparent"

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
                anchors.right: parent.right
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
    Rectangle {
      visible: pane.dayflow !== null && pane.dayflow.weekSummary !== ""
      width: parent.width
      height: sumText.implicitHeight + Style.space(20)
      radius: Style.cornerRadius
      color: pane.dayflow ? pane.dayflow.fgFill(0.04) : "transparent"
      border.color: pane.dayflow ? pane.dayflow.fgFill(0.08) : "transparent"

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
