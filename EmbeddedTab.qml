import QtQuick
import qs.Commons

// Hosts one of the popup's tab components (StandupTab, ChatTab, Settings) as
// a page of the full-size shell. Temporary bridge: those tabs size themselves
// for the 540px popup (their own height caps), so this page centers them in
// a readable column and scrolls. Later app PRs replace each with a full page.
Item {
  id: page

  // Set by FullViewBody's pane Loader.
  property var host: null
  property string tabSource: ""
  property string panelTab: ""

  readonly property var dayflow: host ? host.dayflow : null

  // Switching the panel's current tab makes it fetch what the tab renders.
  onPanelTabChanged: if (dayflow && panelTab !== "") dayflow.currentTab = panelTab
  onDayflowChanged: if (dayflow && panelTab !== "") dayflow.currentTab = panelTab

  Flickable {
    id: flick
    anchors.fill: parent
    contentWidth: width
    contentHeight: holder.height + Style.space(32)
    clip: true
    boundsBehavior: Flickable.StopAtBounds

    Item {
      id: holder
      y: Style.space(16)
      width: Math.min(flick.width - Style.space(32), Style.space(760))
      x: Math.round((flick.width - width) / 2)
      height: loader.item ? loader.item.height : Style.space(120)

      Loader {
        id: loader
        width: parent.width
        // The tabs read their panel as parent.panel (PopupBody's Loader contract).
        property var panel: page.dayflow
        active: page.dayflow !== null && page.tabSource !== ""
        source: page.tabSource
      }
    }
  }
}
