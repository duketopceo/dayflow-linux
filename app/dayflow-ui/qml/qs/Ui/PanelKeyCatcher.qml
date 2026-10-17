import QtQuick

// Same signal surface as Omarchy's PanelKeyCatcher; the standalone window
// only needs Escape/Tab, the rest of the keys fall through to the content.
Item {
  id: root
  property bool blocked: false

  signal moveRequested(int dx, int dy)
  signal activateRequested()
  signal returnRequested()
  signal closeRequested()
  signal deleteRequested()
  signal tabRequested(int direction)
  signal textKey(string text)

  focus: true
  Keys.priority: Keys.BeforeItem
  Keys.onPressed: function(event) {
    if (blocked) return
    if (event.key === Qt.Key_Escape) { closeRequested(); event.accepted = true }
  }
}
