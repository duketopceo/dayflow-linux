import QtQuick
import qs.Commons
import qs.Ui

// Copy action on the shared PanelButton kit — shows "Copying…" and
// disables while its dayflow Process runs so a click mid-flight can't
// rewrite the command under a running copy.
PanelButton {
  id: btn

  required property var dayflow
  required property string label
  required property var proc

  signal activated()

  pal: dayflow
  compact: true
  text: btn.proc && btn.proc.running ? "Copying…" : btn.label
  enabled: !btn.proc || !btn.proc.running
  onClicked: btn.activated()
}
