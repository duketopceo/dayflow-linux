import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Flickable {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null
  property var presets: []
  property var providers: []
  property string usageText: dayflow ? dayflow.storageText : ""

  width: parent ? parent.width : 0
  implicitHeight: Math.min(col.implicitHeight + Style.space(12), Style.space(460))
  height: implicitHeight
  contentHeight: col.implicitHeight + Style.space(12)
  clip: true

  // Draft writes reassign a shallow copy — member writes on a plain JS
  // object don't notify the bindings that read them.
  function setDraft(key, value) {
    var d = Object.assign({}, dayflow.configDraft)
    d[key] = value
    dayflow.configDraft = d
  }

  // Dedicated-path writes: fresh single-key `config patch -` payloads over
  // stdin via the shared queue (keeps secrets out of argv).
  function commitPatchKey(obj, label) {
    dayflow.queuePatch(JSON.stringify(obj), label)
  }

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

  Process {
    id: providersProc
    command: ["dayflow", "provider", "list", "--json"]
    running: true
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try {
          var d = JSON.parse(text)
          root.providers = d.providers || []
        } catch (e) {
          root.providers = []
        }
      }
    }
  }

  // Local endpoint probe — feeds the "Local models" section below. Runs once
  // when the tab opens; the button beside the section header re-probes.
  property var localDetect: null
  Process {
    id: detectProc
    command: ["dayflow", "detect", "--json"]
    running: true
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try { root.localDetect = JSON.parse(text) } catch (e) { root.localDetect = null }
      }
    }
  }

  // One-click local setup: provider=local + endpoint + chosen model id.
  function useLocal(base, model) {
    setDraft("provider", "local")
    setDraft("api_base_url", base)
    if (model) setDraft("model", model)
    dayflow.notice = "local provider set — Save to apply"
  }

  // ignore_apps never round-trips a list: the field commits by diffing the
  // typed list against live config and emitting per-app ignore/unignore —
  // `config set ignore_apps` would clobber apps ignored via --active.
  property var pendingFieldCmds: []

  function queueFieldCmd(cmd) {
    root.pendingFieldCmds = root.pendingFieldCmds.concat([cmd])
    if (!fieldCmdProc.running) {
      fieldCmdProc.command = root.pendingFieldCmds[0]
      fieldCmdProc.didStart = false
      fieldCmdProc.running = true
    }
  }

  function drainFieldCmds(exitCode) {
    if (exitCode !== 0 && root.dayflow) root.dayflow.notice = "settings write failed"
    root.pendingFieldCmds = root.pendingFieldCmds.slice(1)
    if (root.pendingFieldCmds.length > 0) {
      fieldCmdProc.command = root.pendingFieldCmds[0]
      fieldCmdProc.didStart = false
      fieldCmdProc.running = true
    }
  }

  Process {
    id: fieldCmdProc
    property bool didStart: false
    onStarted: fieldCmdProc.didStart = true
    onExited: function(exitCode) {
      root.drainFieldCmds(exitCode)
      if (exitCode === 0 && root.dayflow) root.dayflow.loadConfig()
    }
    onRunningChanged: {
      if (!fieldCmdProc.running && !fieldCmdProc.didStart) root.drainFieldCmds(-1)
      if (!fieldCmdProc.running) fieldCmdProc.didStart = false
    }
  }

  function commitIgnoreApps(text) {
    var want = []
    var parts = text.split(",")
    for (var i = 0; i < parts.length; i++) {
      var p = parts[i].trim()
      if (p !== "") want.push(p)
    }
    var have = dayflow.config.ignore_apps || []
    for (var h = 0; h < have.length; h++) {
      if (want.indexOf(have[h]) < 0) queueFieldCmd(["dayflow", "unignore", have[h]])
    }
    for (var w = 0; w < want.length; w++) {
      if (have.indexOf(want[w]) < 0) queueFieldCmd(["dayflow", "ignore", want[w]])
    }
  }

  // Writes a single provider field immediately (prompt overrides bypass the
  // configDraft save path — `provider set` writes config itself). Writes are
  // queued: reassigning command on a running Process drops the second write.
  property var pendingProviderWrites: []

  function queueProviderWrite(cmd) {
    root.pendingProviderWrites = root.pendingProviderWrites.concat([cmd])
    if (!providerSetProc.running) {
      providerSetProc.command = root.pendingProviderWrites[0]
      providerSetProc.didStart = false
      providerSetProc.running = true
    }
  }

  function drainProviderWrites(exitCode) {
    if (exitCode !== 0 && root.dayflow) root.dayflow.notice = "prompt override save failed"
    root.pendingProviderWrites = root.pendingProviderWrites.slice(1)
    if (root.pendingProviderWrites.length > 0) {
      providerSetProc.command = root.pendingProviderWrites[0]
      providerSetProc.didStart = false // reset before arming — a failed start must drain
      providerSetProc.running = true
    }
  }

  Process {
    id: providerSetProc
    property bool didStart: false
    onStarted: providerSetProc.didStart = true
    onExited: function(exitCode) { root.drainProviderWrites(exitCode) }
    // FailedToStart emits no exited — drain the queue anyway so queued
    // writes are not stranded forever.
    onRunningChanged: {
      if (!providerSetProc.running && !providerSetProc.didStart) root.drainProviderWrites(-1)
      if (!providerSetProc.running) providerSetProc.didStart = false
    }
  }

  Connections {
    target: dayflow
    function onStorageTextChanged() { root.usageText = dayflow.storageText }
  }

  // Reusable bits -------------------------------------------------------

  component DimNote: Text {
    color: root.dayflow ? root.dayflow.dim : "gray"
    font.family: root.dayflow ? root.dayflow.fontFamily : ""
    font.pixelSize: Style.font.caption
    wrapMode: Text.WordWrap
    textFormat: Text.PlainText
    width: parent ? parent.width : 0
  }

  component Toggle: SettingsToggle {
    pal: root.dayflow
  }

  component Field: SettingsField {
    dayflow: root.dayflow
  }

  Column {
    id: col
    width: parent.width
    spacing: Style.space(14)

    BusyBar {
      width: parent.width
      pal: root.dayflow
      active: modelsProc.running || providersProc.running
    }

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

      // ---- AI provider ----
      SettingsGroup {
        pal: root.dayflow
        groupId: "provider"
        title: "AI provider"
        caption: dayflow.configDraft.provider || ""
        open: dayflow.settingsOpenGroups.provider === true
        onToggled: dayflow.settingsGroupToggle("provider")

        DimNote { text: "Choose openrouter, local (Ollama/LM Studio), custom, or mcp." }

        Field { label: "Provider"
          value: dayflow.configDraft.provider || "openrouter"
          hint: "openrouter | local | custom | mcp"
          onEdited: function(t) { root.setDraft("provider", t) }
        }
        Field { label: "Model"
          value: dayflow.configDraft.model || ""
          hint: "e.g. google/gemma-4-31b-it"
          onEdited: function(t) { root.setDraft("model", t) }
        }

        DimNote { text: "Presets"; font.bold: true; color: root.dayflow.foreground }

        Flow {
          width: parent.width
          spacing: Style.space(6)
          Repeater {
            model: root.presets
            delegate: Item {
              required property var modelData
              width: presetChip.implicitWidth
              height: presetChip.implicitHeight
              PanelButton {
                id: presetChip
                pal: root.dayflow
                frame: "quiet"
                compact: true
                selected: dayflow.configDraft.model === modelData.slug
                text: modelData.name
                onClicked: root.setDraft("model", modelData.slug)
              }
            }
          }
        }

        DimNote {
          text: root.presets.length > 0 && dayflow.configDraft.model
            ? (function() {
                for (var i = 0; i < root.presets.length; i++) {
                  if (root.presets[i].slug === dayflow.configDraft.model) return root.presets[i].notes
                }
                return ""
              })()
            : ""
          visible: text !== ""
        }

        DimNote { text: "OpenRouter sends this app name in the HTTP-Referer and X-Title headers so its analytics know which app is calling." }

        Field { label: "App / site name"
          value: dayflow.configDraft.site_name || "dayflow-linux"
          hint: "dayflow-linux"
          onEdited: function(t) { root.setDraft("site_name", t) }
        }

        PanelCard {
          pal: root.dayflow
          well: true
          width: parent.width
          height: keyBox.implicitHeight + Style.space(14)

          Column {
            id: keyBox
            width: parent.width - Style.space(12)
            anchors.centerIn: parent
            spacing: Style.space(4)

            Item {
              width: parent.width
              height: keyTitle.implicitHeight
              Text {
                id: keyTitle
                anchors.left: parent.left
                text: "API key"
                color: dayflow.foreground
                font.family: dayflow.fontFamily
                font.pixelSize: Style.font.body
                font.bold: true
              }
              PanelButton {
                anchors.right: parent.right
                pal: root.dayflow
                frame: "quiet"
                compact: true
                text: keyInput.echoMode === TextInput.Password ? "Show" : "Hide"
                onClicked: keyInput.echoMode = (keyInput.echoMode === TextInput.Password ? TextInput.Normal : TextInput.Password)
              }
            }

            DimNote {
              text: dayflow.keyringOpenrouter
                ? "Stored in the system keyring — edits route to `dayflow key set`, not config.json."
                : "Stored in ~/.config/dayflow/config.json. Sent only to OpenRouter or a custom endpoint you configure."
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

        Field { label: "API base URL"
          value: dayflow.configDraft.api_base_url || ""
          hint: "blank for OpenRouter, or http://localhost:11434/v1"
          onEdited: function(t) { root.setDraft("api_base_url", t) }
        }

        // ---- Local models ----
        Column {
          width: parent.width
          spacing: Style.space(6)

          Item {
            width: parent.width
            height: Math.max(lmTitle.implicitHeight, rescanBtn.implicitHeight)
            Text {
              id: lmTitle
              anchors.left: parent.left
              anchors.verticalCenter: parent.verticalCenter
              text: "Local models"
              color: dayflow.foreground
              font.family: dayflow.fontFamily
              font.pixelSize: Style.font.caption
              font.bold: true
            }
            PanelButton {
              id: rescanBtn
              anchors.right: parent.right
              anchors.verticalCenter: parent.verticalCenter
              pal: root.dayflow
              frame: "quiet"
              compact: true
              text: detectProc.running ? "Detecting…" : "Detect"
              enabled: !detectProc.running
              onClicked: detectProc.running = true
            }
          }

          DimNote {
            visible: root.localDetect !== null && !(root.localDetect.ollama || root.localDetect.lmstudio)
            text: "No Ollama (:11434) or LM Studio (:1234) endpoint found."
          }

          Flow {
            width: parent.width
            spacing: Style.space(6)
            Repeater {
              model: root.localDetect && root.localDetect.ollama ? (root.localDetect.ollama_models || []) : []
              delegate: Item {
                required property var modelData
                width: ollamaChip.implicitWidth
                height: ollamaChip.implicitHeight
                PanelButton {
                  id: ollamaChip
                  pal: root.dayflow
                  frame: "quiet"
                  compact: true
                  selected: dayflow.configDraft.model === modelData && dayflow.configDraft.provider === "local"
                  text: modelData + " · ollama"
                  onClicked: root.useLocal("http://localhost:11434/v1", modelData)
                }
              }
            }
          }

          Flow {
            width: parent.width
            spacing: Style.space(6)
            Repeater {
              model: root.localDetect && root.localDetect.lmstudio ? (root.localDetect.lmstudio_models || []) : []
              delegate: Item {
                required property var modelData
                width: lmsChip.implicitWidth
                height: lmsChip.implicitHeight
                PanelButton {
                  id: lmsChip
                  pal: root.dayflow
                  frame: "quiet"
                  compact: true
                  selected: dayflow.configDraft.model === modelData && dayflow.configDraft.provider === "local"
                  text: modelData + " · lm studio"
                  onClicked: root.useLocal("http://localhost:1234/v1", modelData)
                }
              }
            }
          }

          // Endpoint is up but reported no model ids — still offer the switch.
          DimNote {
            visible: root.localDetect !== null && root.localDetect.ollama
              && (!root.localDetect.ollama_models || root.localDetect.ollama_models.length === 0)
            text: "Ollama detected — no models listed."
            MouseArea {
              anchors.fill: parent
              onClicked: root.useLocal("http://localhost:11434/v1", "")
            }
          }
        }

        DimNote { text: "Decisions endpoint — judges which agent sessions are worth summarizing." }

        Field { label: "Decisions URL"
          value: dayflow.configDraft.decisions_url || ""
          hint: "blank = OpenRouter"
          onEdited: function(t) { root.setDraft("decisions_url", t) }
        }
        DimNote { text: "URL shape picks transport — `/v1` base or `…/chat/completions` selects chat transport." }
        Field { label: "Decisions model"
          value: dayflow.configDraft.decisions_model || ""
          hint: "blank = classification model"
          onEdited: function(t) { root.setDraft("decisions_model", t) }
        }
        Field { label: "Decisions API key"
          value: dayflow.configDraft.decisions_api_key || ""
          hint: "blank = no Authorization header"
          secret: true
          onEdited: function(t) { root.setDraft("decisions_api_key", t) }
        }
      }

      // ---- Capture ----
      SettingsGroup {
        pal: root.dayflow
        groupId: "capture"
        title: "Capture"
        caption: dayflow.configDraft.capture_interval_sec + "s"
        open: dayflow.settingsOpenGroups.capture === true
        onToggled: dayflow.settingsGroupToggle("capture")

        DimNote { text: "These numbers control how often frames are taken and summarized. Lower interval = more detail, higher cost." }

        Row {
          width: parent.width
          spacing: Style.space(6)
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "Interval (s)"
            value: String(dayflow.configDraft.capture_interval_sec !== undefined ? dayflow.configDraft.capture_interval_sec : 10)
            hint: "10"
            numeric: true
            onEdited: function(t) { root.setDraft("capture_interval_sec", parseInt(t, 10) || 0) }
          }
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "Block (min)"
            value: String(dayflow.configDraft.block_minutes !== undefined ? dayflow.configDraft.block_minutes : 15)
            hint: "15"
            numeric: true
            onEdited: function(t) { root.setDraft("block_minutes", parseInt(t, 10) || 0) }
          }
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "Frames/block"
            value: String(dayflow.configDraft.frames_per_block !== undefined ? dayflow.configDraft.frames_per_block : 30)
            hint: "30"
            numeric: true
            onEdited: function(t) { root.setDraft("frames_per_block", parseInt(t, 10) || 0) }
          }
        }

        Flow {
          width: parent.width
          spacing: Style.space(6)
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "JPEG quality"
            value: String(dayflow.configDraft.jpeg_quality !== undefined ? dayflow.configDraft.jpeg_quality : 55)
            hint: "55"
            numeric: true
            onEdited: function(t) { root.setDraft("jpeg_quality", parseInt(t, 10) || 0) }
          }
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "Frame max dim (px)"
            value: String(dayflow.configDraft.frame_max_dim !== undefined ? dayflow.configDraft.frame_max_dim : 1920)
            hint: "1920; 0 = off"
            numeric: true
            onEdited: function(t) {
              var n = parseInt(t, 10)
              if (!isNaN(n)) root.setDraft("frame_max_dim", n)
            }
          }
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "Retention (days)"
            value: String(dayflow.configDraft.retention_days !== undefined ? dayflow.configDraft.retention_days : 0)
            hint: "0 = until caps"
            numeric: true
            onEdited: function(t) { root.setDraft("retention_days", parseInt(t, 10) || 0) }
          }
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "Frames cap (MB)"
            value: String(dayflow.configDraft.max_frames_mb !== undefined ? dayflow.configDraft.max_frames_mb : 20480)
            hint: "20480"
            numeric: true
            onEdited: function(t) { root.setDraft("max_frames_mb", parseInt(t, 10) || 0) }
          }
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "Text data cap (MB)"
            value: String(dayflow.configDraft.max_db_mb !== undefined ? dayflow.configDraft.max_db_mb : 10240)
            hint: "10240"
            numeric: true
            onEdited: function(t) { root.setDraft("max_db_mb", parseInt(t, 10) || 0) }
          }
          Field {
            width: (parent.width - 2 * parent.spacing) / 3
            label: "Total cap, legacy (MB)"
            value: String(dayflow.configDraft.max_storage_mb !== undefined ? dayflow.configDraft.max_storage_mb : 0)
            hint: "0 = off"
            numeric: true
            onEdited: function(t) { root.setDraft("max_storage_mb", parseInt(t, 10) || 0) }
          }
        }

        DimNote {
          text: "Currently using " + (root.usageText !== "" ? root.usageText : "—") + " of data."
        }
      }

      // ---- Classification ----
      SettingsGroup {
        pal: root.dayflow
        groupId: "classification"
        title: "Classification"
        open: dayflow.settingsOpenGroups.classification === true
        onToggled: dayflow.settingsGroupToggle("classification")

        Toggle {
          label: "Jev classification"
          on: dayflow.configDraft.jev_classification !== false
          onToggled: root.setDraft("jev_classification", !on)
        }
        Field { label: "Classification model"
          value: dayflow.configDraft.classification_model || ""
          hint: "typesafe/jev-1.13"
          onEdited: function(t) { root.setDraft("classification_model", t) }
        }

        DimNote { text: "Category buckets"; font.bold: true; color: root.dayflow.foreground }
        DimNote { text: "The model uses these names and descriptions to classify every block. A category like 'browsing' can be work or personal depending on what is on screen — the classification instructions below decide that." }

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

        PanelButton {
          pal: root.dayflow
          compact: true
          text: "+ Add category"
          onClicked: dayflow.settingsCatModel.append({ name: "", description: "" })
        }

        DimNote { text: "Classification instructions"; font.bold: true; color: root.dayflow.foreground }
        DimNote { text: "Extra prompt text appended to every summarization request. Use it to teach the model what counts as work vs. personal for you." }

        PanelCard {
          pal: root.dayflow
          width: parent.width
          height: Math.min(promptBox.implicitHeight + Style.space(12), Style.space(140))
          TextEdit {
            id: promptBox
            anchors.fill: parent
            anchors.margins: Style.space(6)
            text: dayflow.configDraft.classification_prompt || ""
            color: dayflow.foreground
            font.family: dayflow.fontFamily
            font.pixelSize: Style.font.body
            wrapMode: TextEdit.Wrap
            onTextChanged: root.setDraft("classification_prompt", text)
          }
        }
      }

      // ---- Agents ----
      SettingsGroup {
        pal: root.dayflow
        groupId: "agents"
        title: "Agents"
        open: dayflow.settingsOpenGroups.agents === true
        onToggled: dayflow.settingsGroupToggle("agents")

        DimNote { text: "Off by default. When on, a bounded, scrubbed transcript excerpt is sent to your chat provider and to OpenRouter's decisions endpoint, which judges which sessions are worth summarizing. Claude Code, Codex, OpenCode, Devin, and Cursor transcripts are read locally for the session list either way." }

        Toggle {
          label: "Agent-session recaps"
          on: dayflow.configDraft.agent_recaps === true
          onToggled: root.setDraft("agent_recaps", !on)
        }
        Toggle {
          label: "Batch recaps (OpenRouter batch, ~50% off, async)"
          on: dayflow.configDraft.agent_recap_batch === true
          onToggled: root.setDraft("agent_recap_batch", !on)
        }
        Toggle {
          label: "Record agent completions immediately (hooks + opt-in watcher)"
          on: dayflow.configDraft.agent_completions === true
          onToggled: root.setDraft("agent_completions", !on)
        }
      }

      // ---- Sync ----
      SettingsGroup {
        pal: root.dayflow
        groupId: "sync"
        title: "Sync"
        caption: dayflow.configDraft.knowledge_sync === true ? "on" : "off"
        open: dayflow.settingsOpenGroups.sync === true
        onToggled: dayflow.settingsGroupToggle("sync")

        DimNote { text: "Off by default. When on, journal entries sync to your knowledge brain — captured content leaves this machine." }

        Toggle {
          label: "Knowledge sync"
          on: dayflow.configDraft.knowledge_sync === true
          onToggled: root.setDraft("knowledge_sync", !on)
        }

        Field { label: "Transport"
          value: dayflow.configDraft.knowledge_transport || "http"
          hint: "http | ssh"
          onCommitted: function(text) { root.commitPatchKey({ knowledge_transport: text }, "knowledge_transport") }
        }
        Field { label: "Brain URL"
          visible: (dayflow.configDraft.knowledge_transport || "http") !== "ssh"
          value: dayflow.configDraft.knowledge_url || ""
          hint: "https://brain.example"
          onCommitted: function(text) { root.commitPatchKey({ knowledge_url: text }, "knowledge_url") }
        }
        Field { label: "SSH host"
          visible: dayflow.configDraft.knowledge_transport === "ssh"
          value: dayflow.configDraft.knowledge_ssh_host || ""
          hint: "brain.local"
          onCommitted: function(text) { root.commitPatchKey({ knowledge_ssh_host: text }, "knowledge_ssh_host") }
        }
        Field { label: "SSH container"
          visible: dayflow.configDraft.knowledge_transport === "ssh"
          value: dayflow.configDraft.knowledge_container || ""
          hint: "brain-container"
          onCommitted: function(text) { root.commitPatchKey({ knowledge_container: text }, "knowledge_container") }
        }
        Field { label: "SSH port"
          visible: dayflow.configDraft.knowledge_transport === "ssh"
          value: String(dayflow.configDraft.knowledge_port !== undefined ? dayflow.configDraft.knowledge_port : 8421)
          hint: "8421"
          numeric: true
          onCommitted: function(text) { root.commitPatchKey({ knowledge_port: parseInt(text, 10) || 0 }, "knowledge_port") }
        }
        Field { label: "Secret ref"
          value: dayflow.configDraft.knowledge_secret_ref || ""
          hint: "omaseal://service/account"
          onCommitted: function(text) { root.commitPatchKey({ knowledge_secret_ref: text }, "knowledge_secret_ref") }
        }
        DimNote { text: "Sync fields save on Enter — they write a single-key patch straight to config, not the draft." }
      }

      // ---- Behavior ----
      SettingsGroup {
        pal: root.dayflow
        groupId: "behavior"
        title: "Behavior"
        open: dayflow.settingsOpenGroups.behavior === true
        onToggled: dayflow.settingsGroupToggle("behavior")

        Field { label: "Ignored apps (comma-separated window classes)"
          value: (dayflow.config.ignore_apps || []).join(", ")
          hint: "obsidian, spotify"
          onCommitted: function(text) { root.commitIgnoreApps(text) }
        }
        DimNote { text: "Saves on Enter — adds/removes apps individually so it can't clobber apps ignored from the footer." }

        Toggle {
          label: "Auto-pause when screen is locked"
          on: dayflow.configDraft.auto_pause_locked === true
          onToggled: root.setDraft("auto_pause_locked", !on)
        }
        Toggle {
          label: "Filter inappropriate content (redact adult/explicit)"
          on: dayflow.configDraft.filter_inappropriate === true
          onToggled: root.setDraft("filter_inappropriate", !on)
        }
        Toggle {
          label: "Desktop notifications"
          on: dayflow.config.notifications === undefined || dayflow.config.notifications.enabled !== false
          onToggled: root.queueFieldCmd(["dayflow", "config", "set", "notifications.enabled", on ? "false" : "true"])
        }
      }

      // ---- Advanced ----
      SettingsGroup {
        pal: root.dayflow
        groupId: "advanced"
        title: "Advanced"
        open: dayflow.settingsOpenGroups.advanced === true
        onToggled: dayflow.settingsGroupToggle("advanced")

        Field { label: "Request timeout (s)"
          value: String(dayflow.configDraft.request_timeout_sec !== undefined ? dayflow.configDraft.request_timeout_sec : 180)
          hint: "180"
          numeric: true
          onCommitted: function(text) { root.commitPatchKey({ request_timeout_sec: parseInt(text, 10) || 0 }, "request_timeout_sec") }
        }
        Toggle {
          label: "Keep raw frames after summarization"
          on: dayflow.configDraft.keep_frames === true
          onToggled: root.setDraft("keep_frames", !on)
        }
        Field { label: "Output (monitor)"
          value: dayflow.configDraft.output || ""
          hint: "blank = all outputs; \"auto\" = focused"
          onEdited: function(t) { root.setDraft("output", t) }
        }
        Field { label: "Capture command"
          value: dayflow.configDraft.capture_command || ""
          hint: "blank = auto-detect backend"
          onEdited: function(t) { root.setDraft("capture_command", t) }
        }
        Toggle {
          label: "Debug logging to debug.log"
          on: dayflow.configDraft.debug === true
          onToggled: root.setDraft("debug", !on)
        }
        Field { label: "Routing — primary provider"
          value: (dayflow.config.routing || {}).primary || ""
          hint: "provider id; blank = default"
          // `config patch` merges top-level keys only — a bare
          // {routing:{primary:…}} would wipe secondary/task_provider,
          // so overlay the live object first.
          onCommitted: function(text) {
            var r = Object.assign({}, dayflow.config.routing || {})
            r.primary = text
            root.commitPatchKey({ routing: r }, "routing.primary")
          }
        }
        Field { label: "Routing — secondary provider"
          value: (dayflow.config.routing || {}).secondary || ""
          hint: "provider id; blank = none"
          onCommitted: function(text) {
            var r = Object.assign({}, dayflow.config.routing || {})
            r.secondary = text
            root.commitPatchKey({ routing: r }, "routing.secondary")
          }
        }
        Field { label: "Pricing overrides (JSON model→price map)"
          value: dayflow.config.pricing ? JSON.stringify(dayflow.config.pricing) : ""
          hint: "{\"model/slug\": 0.5}"
          onCommitted: function(text) {
            try {
              root.commitPatchKey({ pricing: JSON.parse(text) }, "pricing")
            } catch (e) {
              dayflow.notice = "pricing: invalid JSON"
            }
          }
        }
        DimNote { text: "Routing and pricing save on Enter as single-key patches; entries can be added or overwritten here but not removed — use `dayflow config edit` for that." }
      }

      // ---- Prompt overrides ----
      SettingsGroup {
        pal: root.dayflow
        groupId: "overrides"
        title: "Prompt overrides"
        caption: "saves on Enter"
        open: dayflow.settingsOpenGroups.overrides === true
        onToggled: dayflow.settingsGroupToggle("overrides")
        visible: root.providers.length > 0

        DimNote { text: "Per-provider replacements for the built-in prompts. Saved on Enter — leave blank to use the default." }

        Repeater {
          model: root.providers
          delegate: Column {
            id: provBlock
            property var prov: modelData
            width: parent.width
            spacing: Style.space(4)

            DimNote {
              text: (provBlock.prov.name || provBlock.prov.id) + "  (" + provBlock.prov.id + ")"
              font.bold: true
            }

            Repeater {
              model: [
                { label: "Title prompt", key: "title_prompt" },
                { label: "Summary prompt", key: "summary_prompt" },
                { label: "Detailed prompt", key: "detailed_prompt" },
                { label: "Chat prompt", key: "chat_prompt" }
              ]
              delegate: Column {
                id: ovField
                property var spec: modelData
                width: parent.width
                spacing: Style.space(2)

                DimNote { text: ovField.spec.label }

                Rectangle {
                  width: parent.width
                  height: ovInput.implicitHeight + Style.space(8)
                  radius: Style.cornerRadius
                  color: dayflow.fgFill(0.04)
                  border.color: dayflow.fgFill(0.12)

                  TextInput {
                    id: ovInput
                    anchors.fill: parent
                    anchors.margins: Style.space(5)
                    text: (provBlock.prov.prompt_overrides && provBlock.prov.prompt_overrides[ovField.spec.key]) || ""
                    color: dayflow.foreground
                    font.family: dayflow.fontFamily
                    font.pixelSize: Style.font.body
                    selectByMouse: true
                    onEditingFinished: {
                      root.queueProviderWrite(["dayflow", "provider", "set",
                        provBlock.prov.id, ovField.spec.key, text])
                    }
                  }
                }
              }
            }
          }
        }
      }

      Row {
        width: parent.width
        spacing: Style.space(6)

        PanelButton {
          pal: root.dayflow
          text: "Save settings"
          selected: true
          onClicked: { dayflow.uilog("settings save"); dayflow.saveConfig() }
        }

        PanelButton {
          pal: root.dayflow
          frame: "quiet"
          text: "Reload"
          onClicked: { dayflow.uilog("settings reload"); dayflow.loadConfig() }
        }
      }

      DimNote { text: "Capture settings require a restart of dayflow-capture to take full effect." }
    }
  }
}
