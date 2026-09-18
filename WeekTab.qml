import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Flickable {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null
  width: parent.width
  implicitHeight: Math.min(wcol.implicitHeight, Style.space(360))
  height: implicitHeight
  contentHeight: wcol.implicitHeight
  clip: true

  Column {
    id: wcol
    width: parent.width
    spacing: Style.space(8)

    BusyBar {
      width: parent.width
      pal: dayflow
      active: dayflow.procByName("weeklyProc").running || dayflow.procByName("weekTimelineProc").running || dayflow.procByName("insightsFetchProc").running
    }

    Flow {
      width: parent.width
      spacing: Style.space(4)

      Repeater {
        model: [
          { label: "Copy week · md", proc: "copyWeekProc", logName: "copy week md" },
          { label: "Copy week · mini", proc: "copyWeekMiniProc", logName: "copy week mini" }
        ]
        delegate: CopyButton {
          required property var modelData
          dayflow: root.dayflow
          label: modelData.label
          proc: root.dayflow.procByName(modelData.proc)
          onActivated: {
            root.dayflow.uilog(modelData.logName)
            proc.running = true
          }
        }
      }
    }

    Text {
      visible: dayflow.insights.total_minutes === 0
        && !dayflow.procByName("weeklyProc").running && !dayflow.procByName("weekTimelineProc").running && !dayflow.procByName("insightsFetchProc").running
      width: parent.width
      text: "No weekly data yet."
      textFormat: Text.PlainText
      color: dayflow.dim
      font.family: dayflow.fontFamily
      font.pixelSize: Style.font.body
      wrapMode: Text.WordWrap
    }

    Rectangle {
      visible: dayflow.insights.total_minutes > 0
      width: parent.width
      height: statCol.implicitHeight + Style.space(16)
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)

      Column {
        id: statCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(6)

        Text {
          text: "This week" + (dayflow.insights.days ? " · " + dayflow.insights.days + " days" : "")
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        Row {
          width: parent.width
          spacing: Style.space(6)

          Repeater {
            model: [
              { label: "Tracked", value: dayflow.fmtHours(dayflow.insights.total_minutes) + " hr", dimmed: false },
              { label: "Focus", value: dayflow.fmtHours(dayflow.insights.focus_minutes) + " hr", dimmed: false },
              { label: "Distracted / idle", value: dayflow.fmtHours(dayflow.insights.distraction_minutes + dayflow.insights.idle_minutes) + " hr", dimmed: true }
            ]

            delegate: Rectangle {
              width: (parent.width - 2 * parent.spacing) / 3
              height: tileCol.implicitHeight + Style.space(10)
              radius: Style.cornerRadius
              color: dayflow.fgFill(0.04)
              border.color: dayflow.fgFill(0.08)

              Column {
                id: tileCol
                anchors.centerIn: parent
                width: parent.width - Style.space(12)
                spacing: Style.space(1)

                Text {
                  width: parent.width
                  text: modelData.label
                  textFormat: Text.PlainText
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  elide: Text.ElideRight
                }

                Text {
                  width: parent.width
                  text: modelData.value
                  textFormat: Text.PlainText
                  color: modelData.dimmed ? dayflow.dim : dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.body
                  font.bold: true
                  elide: Text.ElideRight
                }
              }
            }
          }
        }
      }
    }

    Rectangle {
      // Detailed visuals are reserved for the expanded panel and Full View.
      visible: dayflow.expanded && dayflow.weeklyPayload.category_donut.length > 0
      width: parent.width
      height: chartsCol.implicitHeight + Style.space(16)
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)

      Flow {
        id: chartsCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(10)

        // Expanded mode splits the charts into two columns.
        property real colW: dayflow.expanded
          ? (chartsCol.width - chartsCol.spacing) / 2
          : chartsCol.width

        Column {
          width: chartsCol.colW
          spacing: Style.space(10)

          Text {
            text: "Category breakdown"
            textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Row {
            id: donutRow
            width: parent.width
            height: Style.space(24)
            spacing: 0

            Repeater {
              model: dayflow.weeklyPayload.category_donut
              delegate: Rectangle {
                width: donutRow.width * (modelData.percentage / 100)
                height: parent.height
                color: dayflow.payloadColor(modelData.color, modelData.name)
              }
            }
          }

          Repeater {
            model: dayflow.weeklyPayload.category_donut
            delegate: Row {
              width: parent.width
              spacing: Style.space(8)

              Rectangle {
                width: Style.space(10)
                height: Style.space(10)
                radius: Style.space(2)
                color: dayflow.payloadColor(modelData.color, modelData.name)
                anchors.verticalCenter: parent.verticalCenter
              }

              Text {
                text: (modelData.display || modelData.name) + "  " + modelData.percentage + "%"
                textFormat: Text.PlainText
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
                anchors.verticalCenter: parent.verticalCenter
              }
            }
          }

          Text {
            visible: dayflow.weeklyPayload.app_treemap.length > 0
            text: "Top apps"
            textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
        }

        Column {
          visible: dayflow.weeklyPayload.app_treemap.length > 0
          width: parent.width
            spacing: Style.space(4)

            Repeater {
              model: dayflow.weeklyPayload.app_treemap
              delegate: Row {
                width: parent.width
                spacing: Style.space(8)

                Rectangle {
                  width: Math.max(Style.space(4), parent.width * (modelData.percentage / 100))
                  height: Style.space(14)
                  radius: Style.space(2)
                  color: dayflow.accentFill(0.5)
                }

                Text {
                  text: (modelData.display || modelData.name) + "  " + modelData.percentage + "%"
                  textFormat: Text.PlainText
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  anchors.verticalCenter: parent.verticalCenter
                }
              }
            }
          }

        }

        Column {
          width: chartsCol.colW
          spacing: Style.space(10)

            Text {
              visible: dayflow.weeklyPayload.heatmap.length > 0
              text: "Focus heatmap"
              textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
        }

        Column {
          visible: dayflow.weeklyPayload.heatmap.length > 0
          width: parent.width
            spacing: 2

            Repeater {
              model: dayflow.weeklyPayload.heatmap
              delegate: Row {
                id: heatRow
                property var dayData: modelData
                width: parent.width
                spacing: 2

                Text {
                  width: Style.space(28)
                  text: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"][heatRow.dayData.day] || ""
                  textFormat: Text.PlainText
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  anchors.verticalCenter: parent.verticalCenter
                }

                Row {
                  width: heatRow.width - Style.space(28) - parent.spacing
                  spacing: 1

                  Repeater {
                    model: heatRow.dayData.hours || []
                    delegate: Rectangle {
                      width: (heatRow.width - Style.space(28) - 2 - 23) / 24
                      height: Style.space(12)
                      radius: 2
                      color: modelData.category !== "" && modelData.minutes > 0
                        ? dayflow.payloadFill(modelData.color, modelData.category,
                            0.15 + 0.85 * Math.min(1, modelData.minutes / 60))
                        : dayflow.fgFill(0.03)
                    }
                  }
                }
              }
            }
          }

          Text {
            visible: dayflow.weeklyPayload.context_shifts.length > 0
            text: "Context shifts"
            textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
        }

        Column {
          visible: dayflow.weeklyPayload.context_shifts.length > 0
          width: parent.width
            spacing: Style.space(4)

            Repeater {
              model: dayflow.weeklyPayload.context_shifts.slice(0, 6)
              delegate: Row {
                width: parent.width
                spacing: Style.space(6)

                Text {
                  text: (modelData.source || "?") + " → " + (modelData.target || "?")
                  textFormat: Text.PlainText
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  elide: Text.ElideRight
                }

                Text {
                  text: modelData.count + "×"
                  textFormat: Text.PlainText
                  color: dayflow.dim
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                }
              }
            }
          }

          Text {
            visible: dayflow.weeklyPayload.highlights.length > 0
            text: "Highlights"
            textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Repeater {
            model: dayflow.weeklyPayload.highlights
            delegate: Text {
              width: parent.width
              text: "• " + modelData
              textFormat: Text.PlainText
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              wrapMode: Text.WordWrap
            }
          }

          Text {
            visible: dayflow.weeklyPayload.suggestions.length > 0
            text: "Suggestions"
            textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Repeater {
            model: dayflow.weeklyPayload.suggestions
            delegate: Text {
              width: parent.width
              text: "• " + modelData
              textFormat: Text.PlainText
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              wrapMode: Text.WordWrap
            }
          }
        }
      }
    }

    Rectangle {
      visible: dayflow.insights.total_minutes > 0
      width: parent.width
      height: reviewCol.implicitHeight + Style.space(16)
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)

      Column {
        id: reviewCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(8)

        Row {
          width: parent.width
          spacing: Style.space(8)

          Text {
            text: "Weekly review"
            textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Rectangle {
            width: genText.implicitWidth + Style.space(12)
            height: genText.implicitHeight + Style.space(6)
            radius: Style.cornerRadius
            color: dayflow.btnBg(genMa.containsMouse)
            border.color: dayflow.accentFill(0.5)
            visible: !dayflow.weekSummaryLoading && dayflow.weekSummary === ""

            Text {
              id: genText
              anchors.centerIn: parent
              text: "Generate"
              textFormat: Text.PlainText
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: genMa
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { dayflow.uilog("week review"); dayflow.weekSummaryLoading = true; if (!dayflow.procByName("reviewProc").running) dayflow.procByName("reviewProc").running = true }
            }
          }

          Rectangle {
            width: regText.implicitWidth + Style.space(12)
            height: regText.implicitHeight + Style.space(6)
            radius: Style.cornerRadius
            color: dayflow.btnBg(regMa.containsMouse)
            border.color: dayflow.accentFill(0.5)
            visible: !dayflow.weekSummaryLoading && dayflow.weekSummary !== ""

            Text {
              id: regText
              anchors.centerIn: parent
              text: "Regenerate"
              textFormat: Text.PlainText
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: regMa
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { dayflow.uilog("week review"); dayflow.weekSummaryLoading = true; if (!dayflow.procByName("reviewProc").running) dayflow.procByName("reviewProc").running = true }
            }
          }
        }

        Text {
          width: parent.width
          visible: dayflow.weekSummary === "" && !dayflow.weekSummaryLoading
          text: "Generate a weekly review to get AI advice, corrections, and suggestions."
          textFormat: Text.PlainText
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          wrapMode: Text.WordWrap
        }

        Text {
          width: parent.width
          visible: dayflow.weekSummaryLoading
          text: "Generating weekly review..."
          textFormat: Text.PlainText
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: Text.WordWrap
        }

        Text {
          width: parent.width
          visible: !dayflow.weekSummaryLoading && dayflow.weekSummary !== ""
          text: dayflow.weekSummary
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: Text.WordWrap
          textFormat: Text.PlainText
        }
      }
    }

    Rectangle {
      visible: dayflow.expanded && dayflow.weekBlocks.length > 0
      width: parent.width
      height: heatCol.implicitHeight + Style.space(16)
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)

      Column {
        id: heatCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(8)

        Text {
          text: "Week heat map"
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        Text {
          text: "1 cell = 1 hour"
          textFormat: Text.PlainText
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
        }

        Repeater {
          model: 7
          delegate: Row {
            width: parent.width
            spacing: Style.space(2)

            Text {
              width: Style.space(28)
              text: dayflow.dayName(index)
              textFormat: Text.PlainText
              color: index === dayflow.todayIndex() ? dayflow.foreground : dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              font.bold: index === dayflow.todayIndex()
              anchors.verticalCenter: parent.verticalCenter
            }

            Row {
              id: hourRow
              width: parent.width - Style.space(28) - Style.space(2)
              spacing: 1

              Repeater {
                model: 24
                delegate: Rectangle {
                  width: (parent.width - 23 * hourRow.spacing) / 24
                  height: Style.space(10)
                  radius: 2
                  color: dayflow.cellColor(dayflow.categoryForHour(index, modelData))
                }
              }
            }
          }
        }

        // Hour tick labels under the grid (aligned to cell boundaries).
        Row {
          width: parent.width
          spacing: Style.space(2)

          Item { width: Style.space(28); height: 1 }

          Item {
            width: parent.width - Style.space(28) - Style.space(2)
            height: Style.space(11)

            Repeater {
              model: [0, 6, 12, 18]
              delegate: Text {
                x: (parent.width / 24) * modelData
                text: modelData + "h"
                color: dayflow.dim
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
              }
            }
          }
        }
      }
    }

    // ---- full week timeline (merged spans per day) ----
    Rectangle {
      visible: dayflow.expanded && dayflow.weekBlocks.length > 0
      width: parent.width
      height: weekCol.implicitHeight + Style.space(16)
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)

      Column {
        id: weekCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(8)

        Text {
          text: "Week timeline" +
                (dayflow.weekStart ? "  " + dayflow.weekStart.substring(5) + " – " + dayflow.weekEnd.substring(5) : "")
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        Repeater {
          model: dayflow.weekDayOrder()
          delegate: Column {
            property int dayIndex: modelData
            property var daySpans: dayflow.weekDaySpans(dayIndex)
            visible: daySpans.length > 0
            width: parent.width
            spacing: Style.space(2)

            Row {
              width: parent.width
              spacing: Style.space(6)
              Text {
                text: dayflow.dayName(dayIndex) + (dayIndex === dayflow.todayIndex() ? " — today" : "")
                textFormat: Text.PlainText
                color: dayIndex === dayflow.todayIndex() ? dayflow.foreground : dayflow.dim
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
                anchors.verticalCenter: parent.verticalCenter
              }
              Text {
                text: dayflow.fmtDur(dayflow.weekDayMinutes(dayIndex))
                textFormat: Text.PlainText
                color: dayflow.dim
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
                anchors.verticalCenter: parent.verticalCenter
              }
            }

            Repeater {
              model: parent.daySpans
              delegate: Row {
                width: parent.width
                spacing: Style.space(6)

                Rectangle {
                  width: Style.space(6)
                  height: wTxt.implicitHeight
                  radius: width / 2
                  color: dayflow.cellColor(modelData.category)
                  anchors.verticalCenter: parent.verticalCenter
                }

                Text {
                  id: wTxt
                  width: parent.width - Style.space(6) - parent.spacing
                  text: modelData.start + "–" + modelData.end +
                        "  " + modelData.title +
                        (modelData.count > 1 ? "  (" + dayflow.fmtDur(modelData.minutes) + ")" : "")
                  textFormat: Text.PlainText
                  color: dayflow.foreground
                  font.family: dayflow.fontFamily
                  font.pixelSize: Style.font.caption
                  elide: Text.ElideRight
                }
              }
            }
          }
        }
      }
    }

    Loader {
      id: catLoader
      width: parent.width
      height: item ? item.implicitHeight : 0
      sourceComponent: sectionList
      onLoaded: { item.title = "Categories" }
    }
    Binding { target: catLoader.item; property: "items"; value: dayflow.insights.categories; when: catLoader.status === Loader.Ready }

    Loader {
      id: appLoader
      width: parent.width
      height: item ? item.implicitHeight : 0
      sourceComponent: sectionList
      onLoaded: { item.title = "Top apps" }
    }
    Binding { target: appLoader.item; property: "items"; value: dayflow.insights.apps; when: appLoader.status === Loader.Ready }

    Loader {
      id: distLoader
      width: parent.width
      height: item ? item.implicitHeight : 0
      sourceComponent: sectionList
      onLoaded: { item.title = "Distractions to watch" }
    }
    Binding { target: distLoader.item; property: "items"; value: dayflow.insights.top_distractions; when: distLoader.status === Loader.Ready }

    Rectangle {
      visible: dayflow.expanded && dayflow.insights.focus_blocks.length > 0
      width: parent.width
      height: fCol.implicitHeight + Style.space(16)
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)

      Column {
        id: fCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(6)

        Text {
          text: "Longest focus blocks"
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        Repeater {
          model: dayflow.insights.focus_blocks
          delegate: Text {
            width: parent.width
            text: "• " + (modelData.start_str || modelData.start) + "–" + (modelData.end_str || modelData.end) + " " + modelData.title
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }
        }
      }
    }
  }

  // ---- helpers ----
  Component {
    id: sectionList
    Rectangle {
      visible: items.length > 0
      width: parent ? parent.width : 0
      implicitHeight: sCol.implicitHeight + Style.space(16)
      height: implicitHeight
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)
      property var items: []
      property string title: ""

      Column {
        id: sCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(6)

        Text {
          text: title
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        Repeater {
          model: items
          delegate: Row {
            width: parent.width
            spacing: Style.space(8)

            Text {
              width: parent.width - mins.implicitWidth - parent.spacing
              text: modelData.display || modelData.name
              textFormat: Text.PlainText
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
              anchors.verticalCenter: parent.verticalCenter
            }

            Text {
              id: mins
              text: dayflow.fmtHours(modelData.minutes) + " hr"
              textFormat: Text.PlainText
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              anchors.verticalCenter: parent.verticalCenter
            }
          }
        }
      }
    }
  }

}
