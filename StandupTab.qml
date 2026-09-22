import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Flickable {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null
  width: parent.width
  implicitHeight: Math.min(suCol.implicitHeight, Style.space(420))
  height: implicitHeight
  contentHeight: suCol.implicitHeight
  clip: true

  Column {
    id: suCol
    width: parent.width
    spacing: Style.space(10)

    BusyBar {
      width: parent.width
      pal: dayflow
      active: dayflow.procByName("standupFetchProc").running || dayflow.procByName("goalProc").running
    }

  Component {
    id: draftField
    Column {
      property string label: ""
      property string field: ""
      width: parent ? parent.width : 0
      spacing: Style.space(2)

      Text {
        text: label
        textFormat: Text.PlainText
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
      }

      Rectangle {
        width: parent.width
        height: Math.min(fEdit.implicitHeight + Style.space(8), Style.space(72))
        radius: Style.cornerRadius
        color: dayflow.fgFill(0.04)
        border.color: dayflow.fgFill(0.12)
        clip: true

        TextEdit {
          id: fEdit
          anchors.fill: parent
          anchors.margins: Style.space(4)
          text: dayflow.draft[field] || ""
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: TextEdit.Wrap
          // Only user edits (focused field) mark the draft dirty — this
          // keeps applyStandup() refreshes from clobbering in-progress text.
          onTextChanged: {
            if (fEdit.activeFocus) {
              dayflow.draft[field] = text
              dayflow.draftDirty = true
            }
          }
        }
      }
    }
  }

  // ---- day goal ----
  Rectangle {
    width: parent.width
    height: goalRow.implicitHeight + Style.space(12)
    radius: Style.cornerRadius
    color: dayflow.fgFill(0.04)
    border.color: dayflow.fgFill(0.08)

    Row {
      id: goalRow
      width: parent.width - Style.space(12)
      anchors.centerIn: parent
      spacing: Style.space(8)

      Rectangle {
        width: Style.space(18)
        height: Style.space(18)
        radius: Style.space(4)
        color: dayflow.dayGoal.completed ? dayflow.accentFill(0.4) : "transparent"
        border.color: dayflow.accentFill(0.6)
        anchors.verticalCenter: parent.verticalCenter

        Text {
          anchors.centerIn: parent
          visible: dayflow.dayGoal.completed
          text: "✓"
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.pixelSize: Style.font.caption
        }

        MouseArea {
          anchors.fill: parent
          enabled: dayflow.dayGoal.goal !== ""
          onClicked: {
            dayflow.procByName("goalSetProc").command = ["dayflow", "goal",
              dayflow.dayGoal.completed ? "clear" : "done", "--json"]
            dayflow.procByName("goalSetProc").running = true
          }
        }
      }

      Item {
        width: parent.width - Style.space(30) -
          (streakChip.visible ? streakChip.implicitWidth + Style.space(8) : 0)
        height: goalInput.implicitHeight
        anchors.verticalCenter: parent.verticalCenter

        TextInput {
          id: goalInput
          width: parent.width
          text: dayflow.dayGoal.goal
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          clip: true
          selectByMouse: true
          anchors.verticalCenter: parent.verticalCenter
          Keys.onReturnPressed: function(event) {
            dayflow.procByName("goalSetProc").command = ["dayflow", "goal", "set", text, "--json"]
            dayflow.procByName("goalSetProc").running = true
            focus = false
          }
        }

        Text {
          anchors.fill: parent
          visible: dayflow.dayGoal.goal === ""
          text: "Today's goal…"
          textFormat: Text.PlainText
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          verticalAlignment: Text.AlignVCenter
          // overlays the empty input; clicks pass to the input under it
          z: -1
        }
      }

      Text {
        id: streakChip
        visible: !!dayflow.dayGoal.streak && dayflow.dayGoal.streak.current > 0
        text: !!dayflow.dayGoal.streak
          ? dayflow.dayGoal.streak.current + "d streak" +
            (dayflow.dayGoal.streak.best > dayflow.dayGoal.streak.current
              ? " · best " + dayflow.dayGoal.streak.best + "d" : "")
          : ""
        textFormat: Text.PlainText
        color: dayflow.accentFill(0.9)
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        font.bold: true
        anchors.verticalCenter: parent.verticalCenter
      }
    }
  }

  // ---- daily workflow grid ----
  Loader {
    width: parent.width
    height: item ? item.implicitHeight : 0
    source: "DailyWorkflowGrid.qml"
    property var panel: dayflow
  }

  Text {
    visible: dayflow.standup.yesterday.entries.length === 0 && dayflow.standup.today.entries.length === 0
    width: parent.width
    text: "No standup data yet — need a few summarized blocks."
    textFormat: Text.PlainText
    color: dayflow.dim
    font.family: dayflow.fontFamily
    font.pixelSize: Style.font.body
    wrapMode: Text.WordWrap
  }

  Component {
    id: standupDay

    Rectangle {
      property string label: ""
      property var day: ({ date: "", total_minutes: 0, entries: [] })

      width: parent.width
      height: sdCol.implicitHeight + Style.space(16)
      radius: Style.cornerRadius
      color: dayflow.fgFill(0.04)
      border.color: dayflow.fgFill(0.08)

      Column {
        id: sdCol
        width: parent.width - Style.space(16)
        anchors.centerIn: parent
        spacing: Style.space(8)

        Row {
          width: parent.width

          Text {
            text: label
            textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }

          Text {
            anchors.right: parent.right
            text: dayflow.fmtDur(day.total_minutes) + " tracked"
            textFormat: Text.PlainText
            color: dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
          }
        }

        Repeater {
          model: day.entries || []
          delegate: Row {
            width: parent.width
            spacing: Style.space(8)

            Rectangle {
              width: durText.implicitWidth + Style.space(10)
              height: durText.implicitHeight + Style.space(4)
              radius: Style.cornerRadius
              color: (modelData.productive === true)
                ? dayflow.accentFill(0.12)
                : dayflow.fgFill(0.05)

              Text {
                id: durText
                anchors.centerIn: parent
                text: dayflow.fmtDur(modelData.minutes)
                textFormat: Text.PlainText
                color: (modelData.productive === true) ? dayflow.foreground : dayflow.dim
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
              }
            }

            Column {
              width: parent.width - parent.spacing - (durText.implicitWidth + Style.space(10))
              spacing: Style.space(1)

              Text {
                width: parent.width
                text: modelData.title
                textFormat: Text.PlainText
                color: (modelData.productive === true) ? dayflow.foreground : dayflow.dim
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                wrapMode: Text.WordWrap
              }

              Text {
                width: parent.width
                text: (modelData.app ? modelData.app + " · " : "")
                      + dayflow.catDisplay(modelData.category)
                      + (modelData.span ? " · " + modelData.span : "")
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
    }
  }

  Loader {
    width: parent.width
    visible: dayflow.standup.yesterday.entries.length > 0
    sourceComponent: standupDay
    onLoaded: {
      item.label = "Yesterday"
      item.day = dayflow.standup.yesterday
    }
  }

  Loader {
    width: parent.width
    visible: dayflow.standup.today.entries.length > 0
    sourceComponent: standupDay
    onLoaded: {
      item.label = "Today"
      item.day = dayflow.standup.today
    }
  }

  Rectangle {
    visible: dayflow.standup.today.entries.length > 0 || dayflow.standup.yesterday.entries.length > 0
    width: parent.width
    height: bCol.implicitHeight + Style.space(16)
    radius: Style.cornerRadius
    color: dayflow.fgFill(0.04)
    border.color: dayflow.fgFill(0.08)

    Column {
      id: bCol
      width: parent.width - Style.space(16)
      anchors.centerIn: parent
      spacing: Style.space(6)

      Text {
        text: "Blockers"
        textFormat: Text.PlainText
        color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        font.bold: true
      }

      Text {
        width: parent.width
        text: dayflow.draft.blockers !== ""
          ? dayflow.draft.blockers
          : "Nothing flagged — fill this in when you write your update."
        textFormat: Text.PlainText
        color: dayflow.draft.blockers !== "" ? dayflow.foreground : dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        wrapMode: Text.WordWrap
      }

      Text {
        visible: dayflow.draft.priorities !== ""
        width: parent.width
        text: "Priorities: " + dayflow.draft.priorities
        textFormat: Text.PlainText
        color: dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        wrapMode: Text.WordWrap
      }

      Rectangle {
        id: copyBtn
        height: Style.space(28)
        width: cpy.implicitWidth + Style.space(16)
        radius: Style.cornerRadius
        color: mcp.containsMouse
          ? dayflow.accentFill(0.12)
          : "transparent"
        border.color: dayflow.accentFill(0.25)

        Text {
          id: cpy
          anchors.centerIn: parent
          text: "Copy standup"
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
        }

        MouseArea {
          id: mcp
          anchors.fill: parent
          hoverEnabled: true
          onClicked: { dayflow.uilog("standup refresh"); if (!dayflow.procByName("standupProc").running) dayflow.procByName("standupProc").running = true }
        }
      }
    }
  }
  Rectangle {
    width: parent.width
    height: draftCol.implicitHeight + Style.space(16)
    radius: Style.cornerRadius
    color: dayflow.fgFill(0.04)
    border.color: dayflow.fgFill(0.08)

    Column {
      id: draftCol
      width: parent.width - Style.space(16)
      anchors.centerIn: parent
      spacing: Style.space(6)

      Row {
        width: parent.width

        Text {
          text: "Standup draft" + (dayflow.draft.date ? " · " + dayflow.draft.date : "")
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          font.bold: true
        }

        Text {
          anchors.right: parent.right
          visible: dayflow.draftDirty
          text: "unsaved changes"
          textFormat: Text.PlainText
          color: dayflow.dim
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
        }
      }

      Loader {
        width: parent.width
        height: item ? item.implicitHeight : 0
        sourceComponent: draftField
        onLoaded: { item.label = "Highlights"; item.field = "highlights" }
      }

      Loader {
        width: parent.width
        height: item ? item.implicitHeight : 0
        sourceComponent: draftField
        onLoaded: { item.label = "Tasks"; item.field = "tasks" }
      }

      Loader {
        width: parent.width
        height: item ? item.implicitHeight : 0
        sourceComponent: draftField
        onLoaded: { item.label = "Blockers"; item.field = "blockers" }
      }

      Loader {
        width: parent.width
        height: item ? item.implicitHeight : 0
        sourceComponent: draftField
        onLoaded: { item.label = "Priorities"; item.field = "priorities" }
      }

      Rectangle {
        height: Style.space(28)
        width: saveDraftText.implicitWidth + Style.space(16)
        radius: Style.cornerRadius
        color: mSaveDraft.containsMouse
          ? dayflow.accentFill(0.12)
          : "transparent"
        border.color: dayflow.accentFill(0.5)

        Text {
          id: saveDraftText
          anchors.centerIn: parent
          text: dayflow.procByName("draftSaveProc").running ? "Saving..." : "Save draft"
          textFormat: Text.PlainText
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
        }

        MouseArea {
          id: mSaveDraft
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: { dayflow.uilog("draft save"); dayflow.saveDraft() }
        }
      }
    }
  }

}
}
