import QtQuick
import Quickshell
import Quickshell.Io
import qs.Ui

Flickable {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null
  property var presets: []
  property string usageText: dayflow ? dayflow.storageText : ""

  width: parent ? parent.width : 0
  implicitHeight: Math.min(col.implicitHeight + Style.space(12), Style.space(460))
  height: implicitHeight
  contentHeight: col.implicitHeight + Style.space(12)
  clip: true

  function applyPresets(raw) {
    try {
      root.presets = JSON.parse(raw)
    } catch (e) {
      console.warn("dayflow: could not parse model presets")
    }
  }

  Process {
    id: modelsProc
    command: ["dayflow", "models", "--json"]
    running: true
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: root.applyPresets(text)
    }
  }

  Connections {
    target: dayflow
    function onStorageTextChanged() { root.usageText = dayflow.storageText }
  }

  Column {
    id: col
    width: parent.width
    spacing: Style.space(14)

    Text {
      visible: !dayflow || !dayflow.configLoaded
      width: parent.width
      text: "Loading settings..."
      color: dayflow ? dayflow.dim : Color.dim
      font.family: dayflow ? dayflow.fontFamily : Style.font.family
      font.pixelSize: Style.font.body
    }

    Column {
      visible: dayflow && dayflow.configLoaded
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        text: "AI provider"
        color: dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        font.bold: true
      }

      Text {
        width: parent.width
        text: "Choose openrouter, local (Ollama/LM Studio), custom, or mcp."
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }

      SettingsField { dayflow: root.dayflow;
        label: "Provider"
        value: dayflow.configDraft.provider || "openrouter"
        hint: "openrouter | local | custom | mcp"
        onEdited: dayflow.configDraft.provider = text
      }

      SettingsField { dayflow: root.dayflow;
        label: "Model"
        value: dayflow.configDraft.model || ""
        hint: "e.g. google/gemma-4-31b-it"
        onEdited: dayflow.configDraft.model = text
      }

      Text {
        width: parent.width
        text: "Presets"
        color: dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        font.bold: true
      }

      Flow {
        width: parent.width
        spacing: Style.space(6)

        Repeater {
          model: root.presets
          delegate: Rectangle {
            width: chipText.implicitWidth + Style.space(12)
            height: chipText.implicitHeight + Style.space(6)
            radius: Style.cornerRadius
            color: dayflow.configDraft.model === modelData.slug
              ? dayflow.accentFill(0.16)
              : (chipMouse.containsMouse ? dayflow.accentFill(0.08) : dayflow.fgFill(0.04))
            border.color: dayflow.configDraft.model === modelData.slug
              ? dayflow.accentFill(0.5)
              : dayflow.fgFill(0.12)

            Text {
              id: chipText
              anchors.centerIn: parent
              text: modelData.name
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
            }
            MouseArea {
              id: chipMouse
              anchors.fill: parent
              hoverEnabled: true
              onClicked: dayflow.configDraft.model = modelData.slug
            }
          }
        }
      }

      Text {
        width: parent.width
        text: root.presets.length > 0 && dayflow.configDraft.model
          ? (function() {
              for (var i = 0; i < root.presets.length; i++) {
                if (root.presets[i].slug === dayflow.configDraft.model) return root.presets[i].notes
              }
              return ""
            })()
          : ""
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
        visible: text !== ""
      }

      Text {
        width: parent.width
        text: "OpenRouter sends this app name in the HTTP-Referer and X-Title headers so its analytics know which app is calling."
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }

      SettingsField { dayflow: root.dayflow;
        label: "App / site name"
        value: dayflow.configDraft.site_name || "dayflow-linux"
        hint: "dayflow-linux"
        onEdited: dayflow.configDraft.site_name = text
      }

      Rectangle {
        width: parent.width
        height: keyBox.implicitHeight + Style.space(14)
        radius: Style.cornerRadius
        color: dayflow.fgFill(0.04)
        border.color: dayflow.fgFill(0.12)

        Column {
          id: keyBox
          width: parent.width - Style.space(12)
          anchors.centerIn: parent
          spacing: Style.space(4)

          Row {
            width: parent.width
            spacing: Style.space(4)
            Text {
              width: parent.width - showBtn.width - parent.spacing
              text: "API key"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              font.bold: true
            }
            Rectangle {
              id: showBtn
              width: showText.implicitWidth + Style.space(10)
              height: showText.implicitHeight + Style.space(4)
              radius: Style.cornerRadius
              color: dayflow.btnBg(showMa.containsMouse)
              border.color: dayflow.accentFill(0.35)

              Text {
                id: showText
                anchors.centerIn: parent
                text: keyInput.echoMode === TextInput.Password ? "Show" : "Hide"
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.caption
              }
              MouseArea {
                id: showMa
                anchors.fill: parent
                hoverEnabled: true
                onClicked: keyInput.echoMode = (keyInput.echoMode === TextInput.Password ? TextInput.Normal : TextInput.Password)
              }
            }
          }

          Text {
            width: parent.width
            text: "Stored in ~/.config/dayflow/config.json. Sent only to OpenRouter or a custom endpoint you configure."
            color: dayflow.dim
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.caption
            wrapMode: Text.WordWrap
          }

          Rectangle {
            width: parent.width
            height: keyInput.implicitHeight + Style.space(8)
            radius: Style.cornerRadius
            color: dayflow.fgFill(0.06)
            border.color: dayflow.fgFill(0.14)

            TextInput {
              id: keyInput
              anchors.fill: parent
              anchors.margins: Style.space(5)
              text: dayflow.configDraft.openrouter_api_key || ""
              echoMode: TextInput.Password
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.body
              onTextChanged: dayflow.configDraft.openrouter_api_key = text
            }
          }
        }
      }

      SettingsField { dayflow: root.dayflow;
        label: "API base URL"
        value: dayflow.configDraft.api_base_url || ""
        hint: "blank for OpenRouter, or http://localhost:11434/v1"
        onEdited: dayflow.configDraft.api_base_url = text
      }

      Text {
        width: parent.width
        text: "Capture"
        color: dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        font.bold: true
      }

      Text {
        width: parent.width
        text: "These numbers control how often frames are taken and summarized. Lower interval = more detail, higher cost."
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }

      Row {
        width: parent.width
        spacing: Style.space(6)

        SettingsField { dayflow: root.dayflow;
          width: (parent.width - 2 * parent.spacing) / 3
          label: "Interval (s)"
          value: String(dayflow.configDraft.capture_interval_sec || 10)
          hint: "10"
          numeric: true
          onEdited: dayflow.configDraft.capture_interval_sec = parseInt(text, 10) || 0
        }
        SettingsField { dayflow: root.dayflow;
          width: (parent.width - 2 * parent.spacing) / 3
          label: "Block (min)"
          value: String(dayflow.configDraft.block_minutes || 15)
          hint: "15"
          numeric: true
          onEdited: dayflow.configDraft.block_minutes = parseInt(text, 10) || 0
        }
        SettingsField { dayflow: root.dayflow;
          width: (parent.width - 2 * parent.spacing) / 3
          label: "Frames/block"
          value: String(dayflow.configDraft.frames_per_block || 30)
          hint: "30"
          numeric: true
          onEdited: dayflow.configDraft.frames_per_block = parseInt(text, 10) || 0
        }
      }

      Row {
        width: parent.width
        spacing: Style.space(6)

        SettingsField { dayflow: root.dayflow;
          width: (parent.width - 2 * parent.spacing) / 3
          label: "JPEG quality"
          value: String(dayflow.configDraft.jpeg_quality || 55)
          hint: "55"
          numeric: true
          onEdited: dayflow.configDraft.jpeg_quality = parseInt(text, 10) || 0
        }
        SettingsField { dayflow: root.dayflow;
          width: (parent.width - 2 * parent.spacing) / 3
          label: "Retention (days)"
          value: String(dayflow.configDraft.retention_days || 7)
          hint: "7"
          numeric: true
          onEdited: dayflow.configDraft.retention_days = parseInt(text, 10) || 0
        }
        SettingsField { dayflow: root.dayflow;
          width: (parent.width - 2 * parent.spacing) / 3
          label: root.usageText !== ""
            ? "Max storage (MB) — using " + root.usageText
            : "Max storage (MB)"
          value: String(dayflow.configDraft.max_storage_mb || 10240)
          hint: "10240"
          numeric: true
          onEdited: dayflow.configDraft.max_storage_mb = parseInt(text, 10) || 0
        }
      }

      Text {
        width: parent.width
        text: "Category buckets"
        color: dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        font.bold: true
      }

      Text {
        width: parent.width
        text: "The model uses these names and descriptions to classify every block. A category like 'browsing' can be work or personal depending on what is on screen — the classification instructions below decide that."
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }

      Repeater {
        model: dayflow.settingsCatModel
        delegate: Column {
          width: parent.width
          spacing: Style.space(4)

          Row {
            width: parent.width
            spacing: Style.space(6)

            Rectangle {
              width: parent.width * 0.28
              height: catName.implicitHeight + Style.space(8)
              radius: Style.cornerRadius
              color: dayflow.fgFill(0.04)
              border.color: dayflow.fgFill(0.12)

              TextInput {
                id: catName
                anchors.fill: parent
                anchors.margins: Style.space(5)
                text: model.name
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                onEditingFinished: dayflow.settingsCatModel.setProperty(model.index, "name", text)
              }
            }

            Rectangle {
              width: parent.width - parent.width * 0.28 - remBtn.width - 2 * parent.spacing
              height: catDesc.implicitHeight + Style.space(8)
              radius: Style.cornerRadius
              color: dayflow.fgFill(0.04)
              border.color: dayflow.fgFill(0.12)

              TextInput {
                id: catDesc
                anchors.fill: parent
                anchors.margins: Style.space(5)
                text: model.description
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                onEditingFinished: dayflow.settingsCatModel.setProperty(model.index, "description", text)
              }
            }

            Rectangle {
              id: remBtn
              width: delText.implicitWidth + Style.space(12)
              height: catName.height
              radius: Style.cornerRadius
              color: dayflow.btnBg(maDel.containsMouse)
              border.color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground

              Text {
                id: delText
                anchors.centerIn: parent
                text: "×"
                color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
              }
              MouseArea {
                id: maDel
                anchors.fill: parent
                hoverEnabled: true
                onClicked: dayflow.settingsCatModel.remove(model.index)
              }
            }
          }
        }
      }

      Rectangle {
        width: addBtnText.implicitWidth + Style.space(16)
        height: addBtnText.implicitHeight + Style.space(8)
        radius: Style.cornerRadius
        color: dayflow.btnBg(addMa.containsMouse)
        border.color: dayflow.accentFill(0.5)

        Text {
          id: addBtnText
          anchors.centerIn: parent
          text: "+ Add category"
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
        }
        MouseArea {
          id: addMa
          anchors.fill: parent
          hoverEnabled: true
          onClicked: dayflow.settingsCatModel.append({ name: "", description: "" })
        }
      }

      Text {
        width: parent.width
        text: "Classification instructions"
        color: dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.body
        font.bold: true
      }

      Text {
        width: parent.width
        text: "Extra prompt text appended to every summarization request. Use it to teach the model what counts as work vs. personal for you."
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }

      Rectangle {
        width: parent.width
        height: Math.min(promptBox.implicitHeight + Style.space(12), Style.space(140))
        radius: Style.cornerRadius
        color: dayflow.fgFill(0.04)
        border.color: dayflow.fgFill(0.12)

        TextEdit {
          id: promptBox
          anchors.fill: parent
          anchors.margins: Style.space(6)
          text: dayflow.configDraft.classification_prompt || ""
          color: dayflow.foreground
          font.family: dayflow.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: TextEdit.Wrap
          onTextChanged: dayflow.configDraft.classification_prompt = text
        }
      }

      Row {
        width: parent.width
        spacing: Style.space(6)

        Rectangle {
          width: saveText.implicitWidth + Style.space(16)
          height: saveText.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: dayflow.accentFill(0.16)
          border.color: dayflow.accentFill(0.5)

          Text {
            id: saveText
            anchors.centerIn: parent
            text: "Save settings"
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            font.bold: true
          }
          MouseArea {
            anchors.fill: parent
            onClicked: dayflow.saveConfig()
          }
        }

        Rectangle {
          width: resetText.implicitWidth + Style.space(16)
          height: resetText.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: dayflow.btnBg(resetMa.containsMouse)
          border.color: dayflow.fgFill(0.12)

          Text {
            id: resetText
            anchors.centerIn: parent
            text: "Reload"
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
          }
          MouseArea {
            id: resetMa
            anchors.fill: parent
            hoverEnabled: true
            onClicked: dayflow.loadConfig()
          }
        }
      }

      Text {
        width: parent.width
        text: dayflow.notice
        color: Color.urgent !== undefined ? Color.urgent : dayflow.foreground
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
        visible: text !== ""
      }

      Text {
        width: parent.width
        text: "Capture settings require a restart of dayflow-capture to take full effect."
        color: dayflow.dim
        font.family: dayflow.fontFamily
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
    }
  }
}
