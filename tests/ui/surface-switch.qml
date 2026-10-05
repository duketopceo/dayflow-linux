import QtQuick
import Quickshell

// Real state/actions and window; only the host popup controller and CLI are inert.
ShellRoot {
  id: test
  property int step: 0
  DashboardState { id: state }
  // Even creating/assigning the window must not make it appear without a request.
  DashboardWindow { id: app; dayflow: state }
  function require(condition, message) {
    if (!condition) { console.error("FIT_FAIL " + message); Qt.quit() }
  }
  function action(item) {
    if (item.objectName === "surfaceSwitch") return item
    for (var child of item.children || []) {
      var found = action(child)
      if (found) return found
    }
    return null
  }
  function dashboard(item) {
    if (item.floating === true && item.closeRequested) return item
    for (var child of item.children || []) {
      var found = dashboard(child)
      if (found) return found
    }
    return null
  }
  Timer {
    interval: 250; running: true; repeat: true
    onTriggered: {
      switch (test.step++) {
      case 0:
        test.require(!state.fullViewOpen && !app.visible, "startup opened app")
        state.open(); state.currentTab="settings"; state.goDay(-1)
        break
      case 1:
        test.require(state.opened && !app.visible, "plugin/tab/refresh opened app")
        test.require(test.action(state).text === "Pop out to app", "missing explicit pop-out button")
        test.action(state).clicked()
        break
      case 2:
        test.require(!state.opened && state.fullViewOpen && app.visible, "pop-out did not switch surfaces")
        test.require(state.currentTab === "settings" && state.dayOffset === -1, "pop-out lost selection")
        test.require(test.action(app.contentItem).text === "Back to plugin", "missing return button")
        test.action(app.contentItem).clicked()
        break
      case 3:
        test.require(state.opened && !state.fullViewOpen && !app.visible, "return did not reopen plugin")
        test.require(state.currentTab === "settings" && state.dayOffset === -1, "return lost selection")
        test.action(state).clicked()
        break
      case 4:
        test.require(app.visible, "second explicit open failed")
        test.dashboard(app.contentItem).closeRequested()
        break
      case 5:
        test.require(!state.fullViewOpen && !app.visible, "close left app requested")
        state.open(); state.refreshAll()
        break
      case 6:
        test.require(state.opened && !app.visible, "plugin reopen resurrected app")
        console.log("FIT_COMPLETE surface switch lifecycle")
        Qt.quit()
      }
    }
  }
}
