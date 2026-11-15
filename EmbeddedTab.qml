import QtQuick
import qs.Commons

// Hosts one of the popup's tab components (StandupTab, ChatTab, Settings) as
// a page of the full-size shell. The tabs cap their own height for the 540px
// popup; this page lifts the cap (maxHeight) to the page height, so each tab
// fills the window and scrolls inside itself, in a readable centered column.
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
      height: (prefsCard.visible ? prefsCard.height + Style.space(10) : 0)
        + (loader.item ? loader.item.height : Style.space(120))

      // App-only preferences, shown above the engine settings on the Settings page.
      Rectangle {
        id: prefsCard
        visible: page.panelTab === "settings" && page.host && page.host.appPrefs !== null
        width: parent.width
        height: prefsCol.implicitHeight + Style.space(16)
        radius: Style.cornerRadius
        color: page.dayflow ? page.dayflow.fgFill(0.04) : "transparent"
        border.color: page.dayflow ? page.dayflow.fgFill(0.08) : "transparent"

        Column {
          id: prefsCol
          width: parent.width - Style.space(16)
          anchors.centerIn: parent
          spacing: Style.space(6)

          Text {
            text: "This app"
            textFormat: Text.PlainText
            color: page.dayflow ? page.dayflow.foreground : "white"
            font.family: page.dayflow ? page.dayflow.fontFamily : ""
            font.pixelSize: Style.font.body
            font.bold: true
          }
          SettingsToggle {
            pal: page.dayflow
            label: "Start in the tray at login"
            on: page.host && page.host.appPrefs ? page.host.appPrefs.autostart : false
            enabled: page.host && page.host.appPrefs ? !page.host.appPrefs.busy : false
            onToggled: page.host.appPrefs.setAutostart(!page.host.appPrefs.autostart)
          }
          Text {
            width: parent.width
            text: "Recording runs in the background service whether or not this window is open."
            textFormat: Text.PlainText
            wrapMode: Text.WordWrap
            color: page.dayflow ? page.dayflow.dim : "gray"
            font.family: page.dayflow ? page.dayflow.fontFamily : ""
            font.pixelSize: Style.font.caption
          }
        }
      }

      Loader {
        id: loader
        y: prefsCard.visible ? prefsCard.height + Style.space(10) : 0
        width: parent.width
        // The tabs read their panel as parent.panel (PopupBody's Loader contract).
        property var panel: page.dayflow
        active: page.dayflow !== null && page.tabSource !== ""
        source: page.tabSource
        onLoaded: {
          if (item && item.maxHeight !== undefined)
            item.maxHeight = Qt.binding(function() {
              return flick.height - Style.space(32) - (prefsCard.visible ? prefsCard.height + Style.space(10) : 0)
            })
        }
      }
    }
  }
}
