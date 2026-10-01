import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// Engine-missing surface. Shown by Panel.qml instead of the tabs (and ahead
// of Onboarding) when `dayflow status --json` failed to spawn — the binary
// isn't on PATH. One click runs scripts/install.sh (resolved relative to
// this file, so it works from an installed plugin dir or a dev checkout);
// output tails surface here on failure along with the terminal fallback.
Item {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null

  width: parent ? parent.width : 0
  implicitHeight: col.implicitHeight + Style.space(12)
  height: implicitHeight

  Column {
    id: col
    width: parent.width
    spacing: Style.space(10)

    Text {
      width: parent.width
      text: "Dayflow engine not installed"
      color: root.dayflow ? root.dayflow.foreground : Color.foreground
      font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.title
      font.bold: true
    }

    Text {
      width: parent.width
      text: "The widget is installed but the dayflow engine binary isn't on PATH yet. " +
            "Install downloads the verified release for this device, places it in ~/.local/bin, " +
            "and enables the capture units. Provider setup stays yours to choose afterwards."
      color: root.dayflow ? root.dayflow.dim : Color.muted
      font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
      wrapMode: Text.WordWrap
    }

    Rectangle {
      width: installText.implicitWidth + Style.space(16)
      height: installText.implicitHeight + Style.space(8)
      radius: Style.cornerRadius
      color: installArea.containsMouse
        ? (root.dayflow ? root.dayflow.accentFill(0.28) : "transparent")
        : (root.dayflow ? root.dayflow.accentFill(0.16) : "transparent")
      border.color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"
      opacity: (root.dayflow && root.dayflow.engineInstalling) ? 0.5 : 1
      Text {
        id: installText
        anchors.centerIn: parent
        text: (root.dayflow && root.dayflow.engineInstalling) ? "Installing…" : "Install engine"
        color: root.dayflow ? root.dayflow.foreground : Color.foreground
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
        font.bold: true
      }
      MouseArea {
        id: installArea
        anchors.fill: parent
        enabled: !(root.dayflow && root.dayflow.engineInstalling)
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: { if (root.dayflow) root.dayflow.requestEngineInstall() }
      }
    }

    // Last line of installer stdout — progress during a run; on failure
    // the stderr tail (installErr) is the more useful line.
    Text {
      visible: root.dayflow && root.dayflow.installLog !== ""
      width: parent.width
      text: root.dayflow ? root.dayflow.installLog : ""
      color: root.dayflow ? root.dayflow.dim : Color.muted
      font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.caption
      wrapMode: Text.WordWrap
    }

    Text {
      visible: root.dayflow && root.dayflow.installErr !== ""
      width: parent.width
      text: root.dayflow ? root.dayflow.installErr : ""
      color: "#c06c60"
      font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.caption
      wrapMode: Text.WordWrap
    }

    Text {
      width: parent.width
      text: "Or run it yourself:  bash " + (root.dayflow ? root.dayflow.installScriptPath : "scripts/install.sh")
      color: root.dayflow ? root.dayflow.dim : Color.muted
      font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.caption
      wrapMode: Text.WrapAnywhere
    }
  }
}
