// Popup body — the panel's content column, extracted unchanged so the
// preview sandbox can host it in a FloatingWindow (Ribbon U1).
import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
      Column {
        id: content
        width: parent.width
        leftPadding: Style.space(10)
        rightPadding: Style.space(10)
        topPadding: Style.space(10)
        bottomPadding: Style.space(10)
        spacing: Style.space(8)

        // ---- header ----
        Row {
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(6)

          Column {
            width: parent.width - toggleBtn.width - expandBtn.width - Style.space(6) - parent.spacing
            spacing: Style.space(1)

            Row {
              spacing: Style.space(5)

              Rectangle {
                width: Style.space(7)
                height: Style.space(7)
                radius: width / 2
                anchors.verticalCenter: parent.verticalCenter
                color: dayflow.paused ? Color.urgent : Color.accent
              }

              Text {
                text: "Dayflow"
                textFormat: Text.PlainText
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.subtitle
                font.bold: true
              }
            }

            Text {
              // Quiet state caption — the model name is gone (nobody acts
              // on it); only non-default states are worth header space.
              visible: dayflow.paused || !dayflow.configured
              text: !dayflow.configured ? "not configured" : "paused"
              textFormat: Text.PlainText
              color: dayflow.dim
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              elide: Text.ElideRight
              width: parent.width
            }
          }

          Rectangle {
            id: expandBtn
            height: Style.space(26)
            width: exg.implicitWidth + Style.space(14)
            radius: Style.cornerRadius
            color: mexg.containsMouse ? dayflow.accentFill(0.12) : "transparent"
            border.color: dayflow.accentFill(0.5)

            Text {
              id: exg
              anchors.centerIn: parent
              text: dayflow.expanded ? "Shrink" : "Expand"
              textFormat: Text.PlainText
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: mexg
              anchors.fill: parent
              hoverEnabled: true
              onClicked: {
                dayflow.expanded = !dayflow.expanded
                persistExpandedProc.command = ["dayflow", "config", "set", "panel_expanded",
                  dayflow.expanded ? "true" : "false"]
                persistExpandedProc.running = true
              }
            }
          }

          Rectangle {
            id: toggleBtn
            height: Style.space(26)
            width: tgl.implicitWidth + Style.space(16)
            radius: Style.cornerRadius
            color: dayflow.paused
              ? dayflow.accentFill(0.15)
              : (mtgl.containsMouse ? dayflow.accentFill(0.12) : "transparent")
            border.color: dayflow.accentFill(0.5)

            Text {
              id: tgl
              anchors.centerIn: parent
              text: dayflow.paused ? "Resume capture" : "Pause"
              textFormat: Text.PlainText
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }

            MouseArea {
              id: mtgl
              anchors.fill: parent
              hoverEnabled: true
              onClicked: { dayflow.uilog("expand toggle"); toggleProc.running = true }
            }
          }
        }

        // ---- tab bar ----
        Row {
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(4)

          Repeater {
            model: ["today", "standup", "chat", "agents", "settings"]

            // modelData binds on the wrapper — a delegate root with
            // `required` props (PanelButton.pal) doesn't receive it.
            delegate: Item {
              id: tabDelegate
              property string tabName: modelData
              width: tabBtn.implicitWidth
              height: tabBtn.implicitHeight
              PanelButton {
                id: tabBtn
                pal: dayflow
                frame: "selected"
                selected: dayflow.currentTab === tabDelegate.tabName
                // Display name — internal tab ids stay stable; renaming a
                // tab is a one-word change in this map.
                text: ({"standup": "Recap"})[tabDelegate.tabName]
                  || tabDelegate.tabName.charAt(0).toUpperCase() + tabDelegate.tabName.slice(1)
                onClicked: { dayflow.uilog("tab " + tabDelegate.tabName); dayflow.currentTab = tabDelegate.tabName }
              }
            }
          }
        }

        PanelSeparator {
          foreground: dayflow.foreground
          width: parent.width - content.leftPadding - content.rightPadding
        }

        // ---- error / not-configured states ----
        Column {
          visible: dayflow.errorText !== "" || !dayflow.configured
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(4)

          Text {
            visible: dayflow.errorText !== ""
            width: parent.width
            text: "! " + dayflow.errorText
            textFormat: Text.PlainText
            color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }

          Text {
            visible: !dayflow.configured
            width: parent.width
            text: "Not configured yet. Run `dayflow setup` in a terminal."
            textFormat: Text.PlainText
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: Text.WordWrap
          }
        }

        // ---- content ----
        Loader {
          id: tabLoader
          width: parent.width - content.leftPadding - content.rightPadding
          height: item ? item.implicitHeight : Style.space(120)
          property var panel: dayflow
          // Gate order: install surface > first-run onboarding > tabs.
          function pickSource() {
            if (dayflow.engineMissing) return "InstallPrompt.qml"
            if (!dayflow.configured && !dayflow.onboardingSkipped) return "Onboarding.qml"
            if (dayflow.currentTab === "today") return "TodayTab.qml"
            if (dayflow.currentTab === "standup") return "StandupTab.qml"
            if (dayflow.currentTab === "chat") return "ChatTab.qml"
            if (dayflow.currentTab === "agents") return "AgentsTab.qml"
            return "Settings.qml"
          }
          source: pickSource()
          onLoaded: {
            if (item && item.dismissed) {
              item.dismissed.connect(function() { dayflow.onboardingSkipped = true })
            }
          }
        }

        PanelSeparator {
          foreground: dayflow.foreground
          width: parent.width - content.leftPadding - content.rightPadding
        }

        // ---- footer: status + ⋯ actions (pause/resume lives in the header) ----
        Row {
          objectName: "footerRow"
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(6)

          Text {
            visible: !dayflow.actionsOpen
            width: parent.width - actionsBtn.width - parent.spacing
            anchors.verticalCenter: parent.verticalCenter
            text: dayflow.framesToday + " frames · " + dayflow.blocksPending + " pending" +
                  (summarizeProc.running
                    ? " · " + (dayflow.summarizePhase !== "" ? dayflow.summarizePhase : "summarizing…")
                    : "") +
                  (dayflow.storageText !== "" ? " · " + dayflow.storageText : "") +
                  (dayflow.ignoredApps.length ? " · ignoring " + dayflow.ignoredApps.map(function(a) { return dayflow.appDisplayName(a) }).join(", ") : "")
            textFormat: Text.PlainText
            color: dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            elide: Text.ElideRight
          }

          // Open state: the actions swap in for the status text — still one row.
          Row {
            visible: dayflow.actionsOpen
            spacing: Style.space(4)
            anchors.verticalCenter: parent.verticalCenter

            PanelButton {
              pal: dayflow
              compact: true
              text: "Ignore app"
              enabled: dayflow.activeApp !== ""
              onClicked: {
                dayflow.actionsOpen = false
                dayflow.uilog("ignore app " + dayflow.activeApp)
                if (!ignoreProc.running) ignoreProc.running = true
              }
            }

            PanelButton {
              pal: dayflow
              compact: true
              text: summarizeProc.running
                ? "Summarizing…"
                : "Summarize" + (dayflow.blocksPending > 0 ? " (" + dayflow.blocksPending + ")" : "")
              enabled: dayflow.blocksPending > 0 && !summarizeProc.running
              onClicked: {
                dayflow.actionsOpen = false
                dayflow.uilog("summarize now")
                if (!summarizeProc.running) summarizeProc.running = true
              }
            }

            PanelButton {
              pal: dayflow
              compact: true
              text: "Full view"
              onClicked: {
                dayflow.actionsOpen = false
                dayflow.uilog("full view open")
                dayflow.fullViewOpen = true
                dayflow.close()
              }
            }
          }

          PanelButton {
            id: actionsBtn
            pal: dayflow
            frame: "quiet"
            compact: true
            selected: dayflow.actionsOpen
            text: summarizeProc.running ? "…" : (dayflow.actionsOpen ? "×" : "⋯")
            width: Style.space(28)
            anchors.verticalCenter: parent.verticalCenter
            onClicked: { dayflow.uilog("actions " + (dayflow.actionsOpen ? "close" : "open")); dayflow.actionsOpen = !dayflow.actionsOpen }
          }
        }

        // ---- status ----
        Column {
          visible: dayflow.engineVersion !== "" &&
                   dayflow.engineVersion !== dayflow.pluginVersion &&
                   dayflow.skewDismissedFor !== (dayflow.engineVersion + ":" + dayflow.pluginVersion)
          width: parent.width - content.leftPadding - content.rightPadding
          spacing: Style.space(4)

          Text {
            width: parent.width
            // engine-newer is a downgrade for install.sh (it installs the
            // manifest-pinned version) — and an older engine can refuse a
            // newer DB schema, so there is no safe action to offer here.
            text: dayflow.versionNewer(dayflow.engineVersion, dayflow.pluginVersion)
              ? "engine v" + dayflow.engineVersion + " is newer than panel v" + dayflow.pluginVersion +
                " — update the plugin (the engine won't be downgraded: it could reject the newer DB schema)"
              : "engine v" + dayflow.engineVersion + " ≠ panel v" + dayflow.pluginVersion +
                " — update the engine to match the plugin"
            textFormat: Text.PlainText
            color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }

          Row {
            spacing: Style.space(6)

            Rectangle {
              // Downgrade offer hidden when the engine is newer — see above.
              visible: !dayflow.versionNewer(dayflow.engineVersion, dayflow.pluginVersion)
              width: updText.implicitWidth + Style.space(12)
              height: updText.implicitHeight + Style.space(4)
              radius: Style.cornerRadius
              color: dayflow.accentFill(updMa.containsMouse ? 0.28 : 0.16)
              border.color: dayflow.accentFill(0.5)
              opacity: dayflow.engineInstalling ? 0.5 : 1
              Text {
                id: updText
                anchors.centerIn: parent
                text: dayflow.engineInstalling ? "Updating…" : "Update engine"
                textFormat: Text.PlainText
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
                font.bold: true
              }
              MouseArea {
                id: updMa
                anchors.fill: parent
                enabled: !dayflow.engineInstalling
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: dayflow.requestEngineInstall()
              }
            }

            Rectangle {
              width: disText.implicitWidth + Style.space(12)
              height: disText.implicitHeight + Style.space(4)
              radius: Style.cornerRadius
              color: dayflow.btnBg(disMa.containsMouse)
              border.color: dayflow.fgFill(0.12)
              Text {
                id: disText
                anchors.centerIn: parent
                text: "Not now"
                textFormat: Text.PlainText
                color: dayflow.dim
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
              }
              MouseArea {
                id: disMa
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onClicked: dayflow.skewDismissedFor = dayflow.engineVersion + ":" + dayflow.pluginVersion
              }
            }
          }

          // Installer's stderr tail after a failed Update — InstallPrompt
          // isn't loaded in the skew path, so surface it here.
          Text {
            visible: dayflow.installErr !== ""
            width: parent.width
            text: dayflow.installErr
            textFormat: Text.PlainText
            color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }
        }

        Text {
          visible: dayflow.notice !== ""
          width: parent.width - content.leftPadding - content.rightPadding
          text: dayflow.notice
          textFormat: Text.PlainText
          color: dayflow.noticeIsError && Color.urgent !== undefined
            ? Color.urgent
            : (Color.accent !== undefined ? Color.accent : dayflow.foreground)
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.caption
          wrapMode: Text.WordWrap
        }
      }
