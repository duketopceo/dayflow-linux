import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// Full-view window — the expanded Dayflow surface opened from the panel's
// "Full view" button. Owns a left section rail; each feature pane registers
// itself in `sections`. State stays on the panel root (`dayflow`), passed
// in by the panel's Loader.
FloatingWindow {
  id: root

  property var dayflow: null
  property string section: "today"

  // Rail model — later feature units append their pane entries here.
  readonly property var sections: [
    { key: "today",     label: "Today" },
    { key: "week",      label: "Week" },
    { key: "timelapse", label: "Timelapse" }
  ]

  // ---- timelapse state ----
  property var tlFrames: []
  property int tlIndex: 0
  property bool tlPlaying: false
  property bool tlLoading: false
  property string tlError: ""

  function tlLoad() {
    if (!root.dayflow) return
    root.dayflow.uilog("timelapse load " + root.dayflow.viewDateStr())
    framesProc.command = ["dayflow", "frames", root.dayflow.viewDateStr(), "--json"]
    root.tlLoading = true
    root.tlPlaying = false
    framesProc.running = true
  }

  function tlSeek(i) {
    if (root.tlFrames.length === 0) return
    root.tlIndex = Math.max(0, Math.min(root.tlFrames.length - 1, i))
  }

  Process {
    id: framesProc
    command: ["dayflow", "frames", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        root.tlLoading = false
        try {
          var d = JSON.parse(text)
          var list = (d.frames || []).filter(function(f) { return f.exists })
          root.tlFrames = list
          root.tlIndex = 0
          root.tlError = ""
        } catch (e) {
          root.tlFrames = []
          root.tlError = "could not load frames"
        }
      }
    }
    onExited: function(exitCode) {
      root.tlLoading = false
      if (exitCode !== 0) {
        root.tlFrames = []
        root.tlError = "frames listing failed"
      }
    }
  }

  Process {
    id: playbackProc
    property string pendingArg: "status"
    command: ["dayflow", "playback", pendingArg]
    onExited: function(exitCode) {
      if (root.dayflow) {
        root.dayflow.notice = exitCode === 0
          ? "playback " + playbackProc.pendingArg
          : "playback toggle failed"
      }
      if (exitCode === 0) {
        playbackStatusProc.running = true
        root.tlLoad()
      }
    }
  }

  Timer {
    interval: 250
    repeat: true
    running: root.tlPlaying && root.tlFrames.length > 1
    onTriggered: {
      if (root.tlIndex >= root.tlFrames.length - 1) {
        root.tlPlaying = false
      } else {
        root.tlIndex++
      }
    }
  }

  title: "Dayflow"
  color: "transparent"
  implicitWidth: 1100
  implicitHeight: 720
  minimumSize: Qt.size(840, 560)
  visible: root.dayflow !== null

  Component.onCompleted: {
    if (root.dayflow) {
      root.dayflow.loadTimeline()
      root.dayflow.refreshForTab("week")
    }
    playbackStatusProc.running = true
  }

  Rectangle {
    anchors.fill: parent
    radius: Style.cornerRadius
    color: root.dayflow ? root.dayflow.fgFill(0.03) : "transparent"
    border.color: root.dayflow ? root.dayflow.fgFill(0.10) : "transparent"
    clip: true

    Row {
      anchors.fill: parent

      // ---- section rail ----
      Rectangle {
        width: Style.space(170)
        height: parent.height
        color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
        border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

        Column {
          anchors.fill: parent
          anchors.margins: Style.space(12)
          spacing: Style.space(4)

          Text {
            text: "Dayflow"
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.subtitle
            font.bold: true
            bottomPadding: Style.space(8)
          }

          Repeater {
            model: root.sections

            delegate: Rectangle {
              required property var modelData
              width: parent.width
              height: Style.space(30)
              radius: Style.cornerRadius
              color: root.section === modelData.key
                ? (root.dayflow ? root.dayflow.accentFill(0.14) : "transparent")
                : (railMouse.containsMouse
                    ? (root.dayflow ? root.dayflow.fgFill(0.06) : "transparent")
                    : "transparent")
              border.color: root.section === modelData.key
                ? (root.dayflow ? root.dayflow.accentFill(0.4) : "transparent")
                : "transparent"

              Text {
                anchors.verticalCenter: parent.verticalCenter
                anchors.left: parent.left
                anchors.leftMargin: Style.space(10)
                text: modelData.label
                color: root.section === modelData.key
                  ? (root.dayflow ? root.dayflow.foreground : "white")
                  : (root.dayflow ? root.dayflow.dim : "gray")
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
              }

              MouseArea {
                id: railMouse
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                  if (root.dayflow) root.dayflow.uilog("fullview section " + modelData.key)
                  root.section = modelData.key
                  if (modelData.key === "timelapse" && root.tlFrames.length === 0 && !root.tlLoading)
                    root.tlLoad()
                }
              }
            }
          }

          Item { width: 1; height: Style.space(8) }

          Text {
            width: parent.width
            visible: root.dayflow !== null
            text: root.dayflow
              ? root.dayflow.framesToday + " frames today · " +
                root.dayflow.blocksPending + " pending"
              : ""
            color: root.dayflow ? root.dayflow.dim : "gray"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }
        }
      }

      // ---- main area ----
      Column {
        width: parent.width - Style.space(170)
        height: parent.height

        // header
        Item {
          width: parent.width
          height: Style.space(52)

          Text {
            anchors.verticalCenter: parent.verticalCenter
            anchors.left: parent.left
            anchors.leftMargin: Style.space(16)
            text: {
              for (var i = 0; i < root.sections.length; i++)
                if (root.sections[i].key === root.section) return root.sections[i].label
              return ""
            }
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.subtitle
            font.bold: true
          }

          Text {
            anchors.verticalCenter: parent.verticalCenter
            anchors.right: parent.right
            anchors.rightMargin: Style.space(16)
            visible: root.dayflow !== null && root.dayflow.dateLabel !== ""
            text: root.dayflow ? root.dayflow.dateLabel : ""
            color: root.dayflow ? root.dayflow.dim : "gray"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
          }
        }

        Loader {
          width: parent.width
          height: parent.height - Style.space(52)
          sourceComponent: root.section === "week" ? weekPane
            : root.section === "timelapse" ? tlPane
            : todayPane
        }
      }
    }
  }

  // ============ Today pane ============
  Component {
    id: todayPane

    Flickable {
      width: parent ? parent.width : 0
      height: parent ? parent.height : 0
      contentHeight: todayCol.implicitHeight
      clip: true

      Column {
        id: todayCol
        width: parent.width - Style.space(32)
        x: Style.space(16)
        spacing: Style.space(10)

        // day nav + actions
        Row {
          width: parent.width
          spacing: Style.space(6)

          Repeater {
            model: [
              { label: "‹", act: -1 },
              { label: "Today", act: 0 },
              { label: "›", act: 1 }
            ]

            delegate: Rectangle {
              required property var modelData
              height: Style.space(28)
              width: navText.implicitWidth + Style.space(16)
              radius: Style.cornerRadius
              color: navMouse.containsMouse ? root.dayflow.fgFill(0.08) : "transparent"
              border.color: root.dayflow ? root.dayflow.fgFill(0.15) : "transparent"

              Text {
                id: navText
                anchors.centerIn: parent
                text: modelData.label
                color: root.dayflow ? root.dayflow.foreground : "white"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
              }

              MouseArea {
                id: navMouse
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                  if (!root.dayflow) return
                  if (modelData.act === 0) {
                    if (root.dayflow.dayOffset !== 0) {
                      root.dayflow.dayOffset = 0
                      root.dayflow.loadTimeline()
                    }
                  } else {
                    root.dayflow.goDay(modelData.act)
                  }
                }
              }
            }
          }

          Text {
            anchors.verticalCenter: parent.verticalCenter
            text: root.dayflow ? root.dayflow.viewDateLabel() : ""
            color: root.dayflow ? root.dayflow.dim : "gray"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
          }
        }

        BusyBar {
          width: parent.width
          pal: root.dayflow
          active: root.dayflow ? root.dayflow.timelineLoading : false
        }

        Text {
          visible: root.dayflow !== null && root.dayflow.spans.length === 0 && !root.dayflow.timelineLoading
          text: "No activity recorded for this day."
          color: root.dayflow ? root.dayflow.dim : "gray"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }

        // merged-span timeline — wide rows
        Repeater {
          model: root.dayflow ? root.dayflow.spans : []

          delegate: Rectangle {
            required property var modelData
            width: todayCol.width
            height: Style.space(46)
            radius: Style.cornerRadius
            color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

            Rectangle {
              width: Style.space(4)
              height: parent.height - Style.space(8)
              anchors.left: parent.left
              anchors.leftMargin: Style.space(6)
              anchors.verticalCenter: parent.verticalCenter
              radius: Style.space(2)
              color: root.dayflow ? root.dayflow.categoryColor(modelData.category) : "gray"
            }

            Text {
              anchors.left: parent.left
              anchors.leftMargin: Style.space(18)
              anchors.verticalCenter: parent.verticalCenter
              width: Style.space(120)
              text: modelData.start + "–" + modelData.end
              color: root.dayflow ? root.dayflow.dim : "gray"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.caption
            }

            Column {
              anchors.left: parent.left
              anchors.leftMargin: Style.space(150)
              anchors.right: durText.left
              anchors.rightMargin: Style.space(8)
              anchors.verticalCenter: parent.verticalCenter
              spacing: Style.space(1)

              Text {
                width: parent.width
                text: modelData.title + (modelData.count > 1 ? "  ·" + modelData.count : "")
                color: root.dayflow ? root.dayflow.foreground : "white"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
                font.bold: true
                elide: Text.ElideRight
              }

              Text {
                width: parent.width
                text: (modelData.summary !== "" ? modelData.summary + " · " : "") +
                      modelData.appName + " · " +
                      (root.dayflow ? root.dayflow.appDisplayName(modelData.category) : modelData.category)
                color: root.dayflow ? root.dayflow.dim : "gray"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.caption
                elide: Text.ElideRight
              }
            }

            Text {
              id: durText
              anchors.right: parent.right
              anchors.rightMargin: Style.space(12)
              anchors.verticalCenter: parent.verticalCenter
              text: root.dayflow ? root.dayflow.fmtDur(modelData.minutes) : ""
              color: root.dayflow ? root.dayflow.foreground : "white"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.body
            }
          }
        }
      }
    }
  }

  // ============ Week pane ============
  Component {
    id: weekPane

    Flickable {
      width: parent ? parent.width : 0
      height: parent ? parent.height : 0
      contentHeight: weekCol.implicitHeight
      clip: true

      Column {
        id: weekCol
        width: parent.width - Style.space(32)
        x: Style.space(16)
        spacing: Style.space(12)

        // stat strip
        Row {
          width: parent.width
          spacing: Style.space(8)

          Repeater {
            model: root.dayflow ? [
              { label: "Tracked",     val: root.dayflow.fmtDur(root.dayflow.weeklyPayload.total_minutes) },
              { label: "Focus",       val: root.dayflow.fmtDur(root.dayflow.weeklyPayload.focus_minutes) },
              { label: "Distraction", val: root.dayflow.fmtDur(root.dayflow.weeklyPayload.distraction_minutes) },
              { label: "Shifts",      val: String(root.dayflow.weeklyPayload.context_shift_count) }
            ] : []

            delegate: Rectangle {
              required property var modelData
              height: Style.space(52)
              width: (weekCol.width - Style.space(24)) / 4
              radius: Style.cornerRadius
              color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
              border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

              Column {
                anchors.centerIn: parent
                spacing: Style.space(2)

                Text {
                  anchors.horizontalCenter: parent.horizontalCenter
                  text: modelData.val
                  color: root.dayflow ? root.dayflow.foreground : "white"
                  font.family: root.dayflow ? root.dayflow.fontFamily : ""
                  font.pixelSize: Style.font.subtitle
                  font.bold: true
                }
                Text {
                  anchors.horizontalCenter: parent.horizontalCenter
                  text: modelData.label
                  color: root.dayflow ? root.dayflow.dim : "gray"
                  font.family: root.dayflow ? root.dayflow.fontFamily : ""
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
          color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
          border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

          Column {
            id: heatCol
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            spacing: Style.space(3)

            Text {
              text: "Focus heatmap"
              color: root.dayflow ? root.dayflow.foreground : "white"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
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
                  text: root.dayflow ? root.dayflow.dayName(index) : ""
                  color: index === (root.dayflow ? root.dayflow.todayIndex() : -1)
                    ? (root.dayflow ? root.dayflow.foreground : "white")
                    : (root.dayflow ? root.dayflow.dim : "gray")
                  font.bold: index === (root.dayflow ? root.dayflow.todayIndex() : -1)
                  font.family: root.dayflow ? root.dayflow.fontFamily : ""
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
                    color: root.dayflow
                      ? root.dayflow.cellColor(root.dayflow.categoryForHour(dayIndex, index))
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
            color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

            Column {
              id: catCol
              width: parent.width - Style.space(16)
              anchors.centerIn: parent
              spacing: Style.space(4)

              Text {
                text: "Categories"
                color: root.dayflow ? root.dayflow.foreground : "white"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
                font.bold: true
                bottomPadding: Style.space(4)
              }

              Repeater {
                model: root.dayflow ? root.dayflow.weeklyPayload.category_donut : []

                delegate: Row {
                  required property var modelData
                  width: catCol.width
                  spacing: Style.space(8)

                  Rectangle {
                    width: Style.space(10)
                    height: Style.space(10)
                    radius: Style.space(5)
                    anchors.verticalCenter: parent.verticalCenter
                    color: root.dayflow
                      ? root.dayflow.payloadColor(modelData.color, modelData.category)
                      : "gray"
                  }

                  Text {
                    width: parent.width - Style.space(80)
                    text: root.dayflow ? root.dayflow.appDisplayName(modelData.category) : modelData.category
                    color: root.dayflow ? root.dayflow.foreground : "white"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                    elide: Text.ElideRight
                  }

                  Text {
                    anchors.right: parent.right
                    text: root.dayflow ? root.dayflow.fmtDur(modelData.minutes) : ""
                    color: root.dayflow ? root.dayflow.dim : "gray"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
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
            color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

            Column {
              id: appCol
              width: parent.width - Style.space(16)
              anchors.centerIn: parent
              spacing: Style.space(4)

              Text {
                text: "Top apps"
                color: root.dayflow ? root.dayflow.foreground : "white"
                font.family: root.dayflow ? root.dayflow.fontFamily : ""
                font.pixelSize: Style.font.body
                font.bold: true
                bottomPadding: Style.space(4)
              }

              Repeater {
                model: root.dayflow ? root.dayflow.weeklyPayload.app_treemap : []

                delegate: Row {
                  required property var modelData
                  width: appCol.width
                  spacing: Style.space(8)

                  Text {
                    width: parent.width - Style.space(70)
                    text: root.dayflow ? root.dayflow.appDisplayName(modelData.app) : modelData.app
                    color: root.dayflow ? root.dayflow.foreground : "white"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                    elide: Text.ElideRight
                  }

                  Text {
                    anchors.right: parent.right
                    text: root.dayflow ? root.dayflow.fmtDur(modelData.minutes) : ""
                    color: root.dayflow ? root.dayflow.dim : "gray"
                    font.family: root.dayflow ? root.dayflow.fontFamily : ""
                    font.pixelSize: Style.font.caption
                  }
                }
              }
            }
          }
        }

        // week summary
        Rectangle {
          visible: root.dayflow !== null && root.dayflow.weekSummary !== ""
          width: parent.width
          height: sumText.implicitHeight + Style.space(20)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
          border.color: root.dayflow ? root.dayflow.fgFill(0.08) : "transparent"

          Text {
            id: sumText
            width: parent.width - Style.space(16)
            anchors.centerIn: parent
            text: root.dayflow ? root.dayflow.weekSummary : ""
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }
        }

        Item { width: 1; height: Style.space(8) }
      }
    }
  }

  // ============ Timelapse pane ============
  Component {
    id: tlPane

    Column {
      width: parent ? parent.width : 0
      height: parent ? parent.height : 0
      spacing: Style.space(8)
      leftPadding: Style.space(16)
      rightPadding: Style.space(16)

      // controls
      Row {
        width: parent.width
        spacing: Style.space(6)

        Repeater {
          model: [
            { label: "‹", act: -1 },
            { label: "Today", act: 0 },
            { label: "›", act: 1 }
          ]

          delegate: Rectangle {
            required property var modelData
            height: Style.space(28)
            width: tlNavText.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: tlNavMouse.containsMouse ? root.dayflow.fgFill(0.08) : "transparent"
            border.color: root.dayflow ? root.dayflow.fgFill(0.15) : "transparent"

            Text {
              id: tlNavText
              anchors.centerIn: parent
              text: modelData.label
              color: root.dayflow ? root.dayflow.foreground : "white"
              font.family: root.dayflow ? root.dayflow.fontFamily : ""
              font.pixelSize: Style.font.body
            }

            MouseArea {
              id: tlNavMouse
              anchors.fill: parent
              hoverEnabled: true
              cursorShape: Qt.PointingHandCursor
              onClicked: {
                if (!root.dayflow) return
                if (modelData.act === 0) {
                  if (root.dayflow.dayOffset !== 0) {
                    root.dayflow.dayOffset = 0
                    root.dayflow.loadTimeline()
                  }
                } else {
                  root.dayflow.goDay(modelData.act)
                }
              }
            }
          }
        }

        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: root.dayflow ? root.dayflow.viewDateLabel() : ""
          color: root.dayflow ? root.dayflow.dim : "gray"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }

        Item { width: Style.space(8); height: 1 }

        // play / pause
        Rectangle {
          height: Style.space(28)
          width: playText.implicitWidth + Style.space(16)
          radius: Style.cornerRadius
          color: playMouse.containsMouse ? root.dayflow.accentFill(0.12) : "transparent"
          border.color: root.dayflow ? root.dayflow.accentFill(0.4) : "transparent"
          opacity: root.tlFrames.length > 1 ? 1 : 0.45

          Text {
            id: playText
            anchors.centerIn: parent
            text: root.tlPlaying ? "Pause" : "Play"
            color: root.dayflow ? root.dayflow.foreground : "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
          }

          MouseArea {
            id: playMouse
            anchors.fill: parent
            hoverEnabled: true
            enabled: root.tlFrames.length > 1
            cursorShape: Qt.PointingHandCursor
            onClicked: {
              if (root.dayflow) root.dayflow.uilog("timelapse " + (root.tlPlaying ? "pause" : "play"))
              root.tlPlaying = !root.tlPlaying
            }
          }
        }

        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: root.tlFrames.length > 0
            ? "frame " + (root.tlIndex + 1) + " / " + root.tlFrames.length +
              " · " + Qt.formatTime(new Date(root.tlFrames[root.tlIndex].ts * 1000), "hh:mm:ss")
            : ""
          color: root.dayflow ? root.dayflow.dim : "gray"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.caption
        }
      }

      BusyBar {
        width: parent.width
        pal: root.dayflow
        active: root.tlLoading
      }

      // image area
      Rectangle {
        width: parent.width
        height: parent.height - Style.space(150)
        radius: Style.cornerRadius
        color: "black"
        clip: true

        Image {
          anchors.fill: parent
          fillMode: Image.PreserveAspectFit
          // downscale at decode — full-res JPEGs would stall the scrubber
          sourceSize.width: Math.round(width * 2)
          sourceSize.height: Math.round(height * 2)
          source: root.tlFrames.length > 0
            ? "file://" + root.tlFrames[root.tlIndex].path
            : ""
        }

        Text {
          anchors.centerIn: parent
          visible: root.tlFrames.length === 0 && !root.tlLoading
          width: parent.width - Style.space(40)
          horizontalAlignment: Text.AlignHCenter
          wrapMode: Text.WordWrap
          text: root.tlError !== "" ? root.tlError
            : (root.tlPlaybackOn
                ? "No frames kept for this day."
                : "Frame playback is off. Frames are deleted after summarization — enable playback to keep them (standard 10GB cap).")
          color: "white"
          font.family: root.dayflow ? root.dayflow.fontFamily : ""
          font.pixelSize: Style.font.body
        }

        // enable button, only when playback is off and day has no frames
        Rectangle {
          anchors.horizontalCenter: parent.horizontalCenter
          anchors.bottom: parent.bottom
          anchors.bottomMargin: Style.space(20)
          visible: !root.tlPlaybackOn && !root.tlLoading
          height: Style.space(30)
          width: enableText.implicitWidth + Style.space(20)
          radius: Style.cornerRadius
          color: enableMouse.containsMouse ? root.dayflow.accentFill(0.2) : root.dayflow.accentFill(0.12)
          border.color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"

          Text {
            id: enableText
            anchors.centerIn: parent
            text: "Enable playback"
            color: "white"
            font.family: root.dayflow ? root.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
          }

          MouseArea {
            id: enableMouse
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: {
              if (root.dayflow) root.dayflow.uilog("playback enable")
              playbackProc.pendingArg = "on"
              playbackProc.running = true
            }
          }
        }
      }

      // scrub strip
      Rectangle {
        width: parent.width
        height: Style.space(18)
        radius: Style.cornerRadius
        color: root.dayflow ? root.dayflow.fgFill(0.06) : "transparent"
        border.color: root.dayflow ? root.dayflow.fgFill(0.10) : "transparent"

        Rectangle {
          width: root.tlFrames.length > 1
            ? (root.tlIndex / (root.tlFrames.length - 1)) * parent.width
            : 0
          height: parent.height
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"
        }

        MouseArea {
          anchors.fill: parent
          enabled: root.tlFrames.length > 0
          cursorShape: Qt.PointingHandCursor
          onClicked: function(m) { root.tlSeek(Math.round(m.x / width * (root.tlFrames.length - 1))) }
          onPositionChanged: function(m) {
            if (pressed) root.tlSeek(Math.round(m.x / width * (root.tlFrames.length - 1)))
          }
        }
      }

      Text {
        width: parent.width
        visible: root.tlError !== ""
        text: "! " + root.tlError
        color: Color.urgent !== undefined ? Color.urgent : "red"
        font.family: root.dayflow ? root.dayflow.fontFamily : ""
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
    }
  }

  // Day changes reload the scrubber when it's visible.
  Connections {
    target: root.dayflow
    function onDayOffsetChanged() {
      if (root.section === "timelapse") root.tlLoad()
    }
  }

  // playback on/off state for the pane gate
  property bool tlPlaybackOn: false

  Process {
    id: playbackStatusProc
    command: ["dayflow", "playback", "status", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try {
          var d = JSON.parse(text)
          root.tlPlaybackOn = d.enabled === true
        } catch (e) {}
      }
    }
  }

  onVisibleChanged: {
    if (!visible && root.dayflow) root.dayflow.fullViewOpen = false
  }
}
