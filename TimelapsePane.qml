import QtQuick
import qs.Commons
import qs.Ui

// FullView "Timelapse" pane — frame playback for the viewed day.
// `host` is the FullView window, which owns the frame list, playback
// state, and the playback enable/status processes.
Column {
  id: pane

  property var host: null
  readonly property var dayflow: host ? host.dayflow : null

  width: parent ? parent.width : 0
  height: parent ? parent.height : 0
  spacing: Style.space(8)
  leftPadding: Style.space(16)
  rightPadding: Style.space(16)

  // controls
  Row {
    width: parent.width
    spacing: Style.space(6)

    DayNavRow {
      dayflow: pane.dayflow
    }

    Item { width: Style.space(8); height: 1 }

    // play / pause
    Rectangle {
      height: Style.space(28)
      width: playText.implicitWidth + Style.space(16)
      radius: Style.cornerRadius
      color: playMouse.containsMouse && pane.dayflow ? pane.dayflow.accentFill(0.12) : "transparent"
      border.color: pane.dayflow ? pane.dayflow.accentFill(0.4) : "transparent"
      opacity: host && host.tlFrames.length > 1 ? 1 : 0.45

      Text {
        id: playText
        anchors.centerIn: parent
        text: host && host.tlPlaying ? "Pause" : "Play"
        color: pane.dayflow ? pane.dayflow.foreground : "white"
        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
        font.pixelSize: Style.font.caption
      }

      MouseArea {
        id: playMouse
        anchors.fill: parent
        hoverEnabled: true
        enabled: host !== null && host.tlFrames.length > 1
        cursorShape: Qt.PointingHandCursor
        onClicked: {
          if (pane.dayflow) pane.dayflow.uilog("timelapse " + (host.tlPlaying ? "pause" : "play"))
          if (host) host.tlPlaying = !host.tlPlaying
        }
      }
    }

    Text {
      anchors.verticalCenter: parent.verticalCenter
      text: host !== null && host.tlFrames.length > 0
        ? "frame " + (host.tlIndex + 1) + " / " + host.tlFrames.length +
          " · " + Qt.formatTime(new Date(host.tlFrames[host.tlIndex].ts * 1000), "hh:mm:ss")
        : ""
      color: pane.dayflow ? pane.dayflow.dim : "gray"
      font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
      font.pixelSize: Style.font.caption
    }
  }

  BusyBar {
    width: parent.width
    pal: pane.dayflow
    active: host !== null && host.tlLoading
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
      source: host !== null && host.tlFrames.length > 0
        ? "file://" + host.tlFrames[host.tlIndex].path
        : ""
    }

    Text {
      anchors.centerIn: parent
      visible: host !== null && host.tlFrames.length === 0 && !host.tlLoading
      width: parent.width - Style.space(40)
      horizontalAlignment: Text.AlignHCenter
      wrapMode: Text.WordWrap
      text: host !== null && host.tlError !== "" ? host.tlError
        : (host !== null && host.tlPlaybackOn
            ? "No frames kept for this day."
            : "Frame playback is off. Frames are deleted after summarization — enable playback to keep them (standard 10GB cap).")
      color: "white"
      font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
      font.pixelSize: Style.font.body
    }

    // enable button, only when playback is off and day has no frames
    Rectangle {
      anchors.horizontalCenter: parent.horizontalCenter
      anchors.bottom: parent.bottom
      anchors.bottomMargin: Style.space(20)
      visible: host !== null && !host.tlPlaybackOn && !host.tlLoading
      height: Style.space(30)
      width: enableText.implicitWidth + Style.space(20)
      radius: Style.cornerRadius
      color: enableMouse.containsMouse && pane.dayflow
        ? pane.dayflow.accentFill(0.2)
        : (pane.dayflow ? pane.dayflow.accentFill(0.12) : "transparent")
      border.color: pane.dayflow ? pane.dayflow.accentFill(0.5) : "transparent"

      Text {
        id: enableText
        anchors.centerIn: parent
        text: "Enable playback"
        color: "white"
        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
        font.pixelSize: Style.font.body
      }

      MouseArea {
        id: enableMouse
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: if (host) host.enablePlayback()
      }
    }
  }

  // scrub strip
  Rectangle {
    width: parent.width
    height: Style.space(18)
    radius: Style.cornerRadius
    color: pane.dayflow ? pane.dayflow.fgFill(0.06) : "transparent"
    border.color: pane.dayflow ? pane.dayflow.fgFill(0.10) : "transparent"

    Rectangle {
      width: host !== null && host.tlFrames.length > 1
        ? (host.tlIndex / (host.tlFrames.length - 1)) * parent.width
        : 0
      height: parent.height
      radius: Style.cornerRadius
      color: pane.dayflow ? pane.dayflow.accentFill(0.5) : "transparent"
    }

    MouseArea {
      anchors.fill: parent
      enabled: host !== null && host.tlFrames.length > 0
      cursorShape: Qt.PointingHandCursor
      onClicked: function(m) { if (host) host.tlSeek(Math.round(m.x / width * (host.tlFrames.length - 1))) }
      onPositionChanged: function(m) {
        if (pressed && host) host.tlSeek(Math.round(m.x / width * (host.tlFrames.length - 1)))
      }
    }
  }

  Text {
    width: parent.width
    visible: host !== null && host.tlError !== ""
    text: "! " + (host ? host.tlError : "")
    color: Color.urgent !== undefined ? Color.urgent : "red"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
    wrapMode: Text.WordWrap
  }
}
