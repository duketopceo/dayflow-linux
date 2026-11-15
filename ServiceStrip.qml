import QtQuick
import qs.Commons
import qs.Commons as Commons
import qs.Ui

// Bottom strip of the desktop app: capture state, last frame age, summarizer
// state and systemd unit health from a ServiceStatus, with the actions that
// are safe from here (pause/resume, restart capture, start a stopped unit).
Rectangle {
  id: root

  required property var pal
  required property var svc

  height: Style.space(30)
  color: pal ? pal.fgFill(0.04) : "transparent"
  border.color: pal ? pal.fgFill(0.08) : "transparent"

  // Unit rows shown when the detail row is open.
  property bool detailOpen: false

  readonly property string stateWord: {
    if (!svc.engineAvailable) return "engine not answering"
    if (svc.captureState === "recording") return "recording"
    if (svc.captureState === "paused") return "paused"
    if (svc.captureState === "locked") return "locked"
    if (svc.captureState === "down") return "capture down"
    return svc.captureState
  }

  readonly property string healthWord: {
    if (svc.health === "ok") return "services ok"
    if (svc.health === "degraded") return "services degraded"
    if (svc.health === "unmanaged") return "services not managed by systemd"
    return "services unknown"
  }

  readonly property color stateColor: {
    if (!pal) return "gray"
    if (svc.captureState === "recording" && svc.health !== "degraded") return pal.accentFill(1)
    if (svc.captureState === "down" || svc.health === "degraded" || !svc.engineAvailable)
      return Commons.Color.urgent !== undefined ? Commons.Color.urgent : pal.foreground
    return pal.dim
  }

  function unitLabel(u) { return u.id.replace("dayflow-", "").replace(".service", "").replace(".timer", " timer") }

  Row {
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
    anchors.leftMargin: Style.space(12)
    anchors.rightMargin: Style.space(8)
    spacing: Style.space(10)

    Rectangle {
      anchors.verticalCenter: parent.verticalCenter
      width: Style.space(8)
      height: width
      radius: width / 2
      color: root.stateColor
    }

    Text {
      objectName: "stripState"
      anchors.verticalCenter: parent.verticalCenter
      text: root.stateWord
      textFormat: Text.PlainText
      color: pal ? pal.foreground : "white"
      font.family: pal ? pal.fontFamily : ""
      font.pixelSize: Style.font.caption
      font.bold: true
    }

    Text {
      objectName: "stripDetail"
      anchors.verticalCenter: parent.verticalCenter
      width: parent.width - x - actions.width - parent.spacing
      elide: Text.ElideRight
      text: "last frame " + svc.ageText()
        + " · " + svc.framesToday + " frames today"
        + " · " + (svc.summarizerRunning
            ? "summarizing"
            : svc.blocksPending + " pending")
        + " · " + root.healthWord
      textFormat: Text.PlainText
      color: pal ? pal.dim : "gray"
      font.family: pal ? pal.fontFamily : ""
      font.pixelSize: Style.font.caption
    }

    Row {
      id: actions
      anchors.verticalCenter: parent.verticalCenter
      spacing: Style.space(4)

      PanelButton {
        pal: root.pal
        compact: true
        text: root.detailOpen ? "Hide units" : "Units"
        visible: svc.unitsKnown
        onClicked: root.detailOpen = !root.detailOpen
      }
      PanelButton {
        pal: root.pal
        compact: true
        text: svc.paused ? "Resume" : "Pause"
        enabled: svc.engineAvailable
        onClicked: svc.togglePause()
      }
      PanelButton {
        pal: root.pal
        compact: true
        text: svc.captureUnitActive ? "Restart capture" : "Start capture"
        visible: svc.unitsKnown
        onClicked: svc.captureUnitActive ? svc.restartCapture() : svc.startUnit("dayflow-capture.service")
      }
    }
  }

  // Unit detail popover, anchored above the strip.
  Rectangle {
    visible: root.detailOpen && svc.unitsKnown
    anchors.right: parent.right
    anchors.bottom: parent.top
    anchors.margins: Style.space(8)
    width: Style.space(300)
    height: unitCol.implicitHeight + Style.space(16)
    radius: Style.cornerRadius
    color: Commons.Color.background
    border.color: pal ? pal.fgFill(0.14) : "transparent"
    z: 5

    Column {
      id: unitCol
      anchors.fill: parent
      anchors.margins: Style.space(8)
      spacing: Style.space(4)

      Repeater {
        model: svc.units
        delegate: Row {
          required property var modelData
          width: unitCol.width
          spacing: Style.space(8)

          Text {
            width: parent.width - startBtn.width - parent.spacing
            anchors.verticalCenter: parent.verticalCenter
            text: root.unitLabel(modelData) + ": " + modelData.active
              + (modelData.sub && modelData.sub !== modelData.active ? " (" + modelData.sub + ")" : "")
            textFormat: Text.PlainText
            elide: Text.ElideRight
            color: modelData.active === "active"
              ? (pal ? pal.foreground : "white")
              : (Commons.Color.urgent !== undefined ? Commons.Color.urgent : "white")
            font.family: pal ? pal.fontFamily : ""
            font.pixelSize: Style.font.caption
          }
          PanelButton {
            id: startBtn
            pal: root.pal
            compact: true
            text: "Start"
            visible: modelData.active !== "active"
            onClicked: svc.startUnit(modelData.id)
          }
        }
      }
    }
  }
}
