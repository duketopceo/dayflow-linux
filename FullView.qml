import QtQuick
import Quickshell
import qs.Commons
import qs.Commons as Commons

// Full-view window: the expanded Dayflow surface opened from the panel's
// "Full view" button. The rail and panes live in FullViewBody (shared with
// the desktop app shell); this file is only the Quickshell window wrapper.
FloatingWindow {
  id: root

  property var dayflow: null

  title: "Dayflow"
  // Opaque theme surface: a low-alpha window assumed Hyprland blur,
  // which Omarchy 4.x ships off (same fill as other FloatingWindows).
  color: Commons.Color.background
  implicitWidth: 1100
  implicitHeight: 720
  minimumSize: Qt.size(840, 560)
  visible: root.dayflow !== null

  FullViewBody {
    anchors.fill: parent
    dayflow: root.dayflow
  }

  onVisibleChanged: {
    if (!visible && root.dayflow) root.dayflow.fullViewOpen = false
  }
}
