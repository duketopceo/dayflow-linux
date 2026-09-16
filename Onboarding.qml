import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// First-run wizard. Shown instead of the tab content when `status --json`
// reports the install unconfigured. Every step has a skip affordance —
// the wizard is an offer, never a gate.
Flickable {
  id: root
  property var dayflow: parent && parent.panel ? parent.panel : null
  property int step: 0
  property var detected: ({})
  // "openrouter" | "local" | "custom"
  property string mode: "openrouter"
  property string apiKey: ""
  property string baseUrl: ""
  property string modelSlug: "google/gemma-4-31b-it"
  property var catPicks: ({})
  property string testResult: ""
  property bool testing: false

  signal dismissed()

  width: parent ? parent.width : 0
  implicitHeight: Math.min(col.implicitHeight + Style.space(12), Style.space(460))
  height: implicitHeight
  contentHeight: col.implicitHeight + Style.space(12)
  clip: true

  Process {
    id: detectProc
    command: ["dayflow", "detect", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try {
          root.detected = JSON.parse(text)
          if (root.detected.ollama) root.mode = "ollama"
        } catch (e) {
          root.detected = {}
        }
      }
    }
  }

  // config patch -> doctor --json for the connection test.
  // The patch JSON carries the API key, so it goes over stdin, not argv.
  Process {
    id: applyProc
    property string pendingPatch: ""
    stdinEnabled: true
    onStarted: { write(pendingPatch + "\n"); pendingPatch = "" }
    onExited: function(exitCode) {
      if (exitCode === 0) {
        testProc.running = true
      } else {
        root.testResult = "config write failed"
        root.testing = false
      }
    }
  }

  Process {
    id: testProc
    command: ["dayflow", "doctor", "--json"]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        root.testing = false
        try {
          var d = JSON.parse(text)
          if (d.failures === 0) {
            root.testResult = "Connection OK — all checks passed."
          } else {
            var bad = []
            for (var i = 0; i < d.checks.length; i++) {
              if (d.checks[i].status === "fail") bad.push(d.checks[i].name)
            }
            root.testResult = "Failing checks: " + bad.join(", ")
          }
        } catch (e) {
          root.testResult = "doctor did not return JSON"
        }
      }
    }
    stderr: StdioCollector {}
  }

  Process {
    id: installProc
    command: ["dayflow", "install"]
    onExited: function(exitCode) {
      root.testResult += exitCode === 0 ? " Services installed." : " Install failed — run `dayflow install` in a terminal."
      if (root.dayflow) root.dayflow.loadConfig()
    }
  }

  // The API key goes to OmaSeal over stdin when the keyring is available;
  // on failure we fall back to embedding it in the config patch.
  Process {
    id: keySetProc
    property string pendingKey: ""
    stdinEnabled: true
    onStarted: { write(pendingKey + "\n"); pendingKey = "" }
    onExited: function(exitCode) {
      if (exitCode !== 0) root.keyInPatch = true
      root.applyPatch()
    }
  }

  property bool keyInPatch: false

  function apply() {
    root.testing = true
    root.testResult = "Testing..."
    if (root.mode === "openrouter" && root.apiKey !== "") {
      root.keyInPatch = false
      keySetProc.pendingKey = root.apiKey
      keySetProc.command = ["dayflow", "key", "set", "openrouter"]
      keySetProc.running = true
      return
    }
    root.applyPatch()
  }

  function applyPatch() {
    var patch = { model: root.modelSlug }
    if (root.mode === "openrouter") {
      patch.provider = "openrouter"
      if (root.keyInPatch) patch.openrouter_api_key = root.apiKey
      patch.api_base_url = ""
    } else if (root.mode === "ollama" || root.mode === "lmstudio") {
      patch.provider = "local"
      patch.api_base_url = root.mode === "lmstudio"
        ? "http://localhost:1234/v1" : "http://localhost:11434/v1"
      patch.openrouter_api_key = ""
    } else {
      patch.provider = "custom"
      patch.api_base_url = root.baseUrl
      patch.openrouter_api_key = ""
    }
    var cats = []
    var keys = Object.keys(root.catPicks)
    for (var i = 0; i < keys.length; i++) {
      if (root.catPicks[keys[i]]) cats.push(keys[i])
    }
    if (cats.length > 0) {
      patch.categories = cats.map(function(n) { return { name: n, description: n } })
    }
    applyProc.pendingPatch = JSON.stringify(patch)
    applyProc.command = ["dayflow", "config", "patch", "-"]
    applyProc.running = true
  }

  Component.onCompleted: detectProc.running = true

  Column {
    id: col
    width: parent.width
    spacing: Style.space(10)

    // ---- step 0: welcome ----
    Column {
      visible: root.step === 0
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        text: "Welcome to Dayflow"
        color: root.dayflow ? root.dayflow.foreground : Color.foreground
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.title
        font.bold: true
      }
      Text {
        width: parent.width
        text: "Dayflow journals your screen activity locally and summarizes it with an AI model. To get started, pick where the model runs."
        color: root.dayflow ? root.dayflow.dim : Color.muted
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
        wrapMode: Text.WordWrap
      }
      Row {
        spacing: Style.space(6)
        Rectangle {
          width: goText.implicitWidth + Style.space(16)
          height: goText.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.accentFill(0.16) : "transparent"
          border.color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"
          Text { id: goText; anchors.centerIn: parent; text: "Get started"
            color: root.dayflow ? root.dayflow.foreground : Color.foreground
            font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.body; font.bold: true }
          MouseArea { anchors.fill: parent; onClicked: root.step = 1 }
        }
        Rectangle {
          width: skipText.implicitWidth + Style.space(16)
          height: skipText.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.btnBg(skipMa.containsMouse) : "transparent"
          border.color: root.dayflow ? root.dayflow.fgFill(0.12) : "transparent"
          Text { id: skipText; anchors.centerIn: parent; text: "Skip for now"
            color: root.dayflow ? root.dayflow.dim : Color.muted
            font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.body }
          MouseArea { id: skipMa; anchors.fill: parent; hoverEnabled: true; onClicked: root.dismissed() }
        }
      }
    }

    // ---- step 1: provider ----
    Column {
      visible: root.step === 1
      width: parent.width
      spacing: Style.space(8)

      Text {
        width: parent.width
        text: "Where should the model run?"
        color: root.dayflow ? root.dayflow.foreground : Color.foreground
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.body; font.bold: true
      }

      Flow {
        width: parent.width
        spacing: Style.space(6)
        Repeater {
          model: {
            var opts = [{ id: "openrouter", label: "OpenRouter (cloud)" }]
            if (root.detected.ollama) opts.push({ id: "ollama", label: "Ollama (detected)" })
            if (root.detected.lmstudio) opts.push({ id: "lmstudio", label: "LM Studio (detected)" })
            if (!root.detected.ollama && !root.detected.lmstudio)
              opts.push({ id: "ollama", label: "Local endpoint (Ollama default)" })
            opts.push({ id: "custom", label: "Custom endpoint" })
            return opts
          }
          delegate: Rectangle {
            width: modeLabel.implicitWidth + Style.space(14)
            height: modeLabel.implicitHeight + Style.space(8)
            radius: Style.cornerRadius
            color: root.mode === modelData.id
              ? (root.dayflow ? root.dayflow.accentFill(0.16) : "transparent")
              : (modeMa.containsMouse && root.dayflow ? root.dayflow.accentFill(0.08) : "transparent")
            border.color: root.mode === modelData.id
              ? (root.dayflow ? root.dayflow.accentFill(0.5) : "transparent")
              : (root.dayflow ? root.dayflow.fgFill(0.12) : "transparent")
            Text { id: modeLabel; anchors.centerIn: parent; text: modelData.label
              color: root.dayflow ? root.dayflow.foreground : Color.foreground
              font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
              font.pixelSize: Style.font.caption }
            MouseArea { id: modeMa; anchors.fill: parent; hoverEnabled: true
              onClicked: root.mode = modelData.id }
          }
        }
      }

      Text {
        visible: root.mode === "openrouter"
        width: parent.width
        text: "Paste an OpenRouter key (sk-or-...). Get one at openrouter.ai/keys."
        color: root.dayflow ? root.dayflow.dim : Color.muted
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
      Rectangle {
        visible: root.mode === "openrouter"
        width: parent.width
        height: keyIn.implicitHeight + Style.space(8)
        radius: Style.cornerRadius
        color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
        border.color: root.dayflow ? root.dayflow.fgFill(0.14) : "transparent"
        TextInput { id: keyIn; anchors.fill: parent; anchors.margins: Style.space(5)
          text: root.apiKey; echoMode: TextInput.Password
          color: root.dayflow ? root.dayflow.foreground : Color.foreground
          font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
          font.pixelSize: Style.font.body
          onTextChanged: root.apiKey = text }
      }

      Rectangle {
        visible: root.mode === "custom"
        width: parent.width
        height: urlIn.implicitHeight + Style.space(8)
        radius: Style.cornerRadius
        color: root.dayflow ? root.dayflow.fgFill(0.04) : "transparent"
        border.color: root.dayflow ? root.dayflow.fgFill(0.14) : "transparent"
        TextInput { id: urlIn; anchors.fill: parent; anchors.margins: Style.space(5)
          text: root.baseUrl
          color: root.dayflow ? root.dayflow.foreground : Color.foreground
          font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
          font.pixelSize: Style.font.body
          onTextChanged: root.baseUrl = text }
        Text { anchors.fill: parent; anchors.margins: Style.space(5)
          visible: urlIn.text === "" && !urlIn.activeFocus
          text: "http://localhost:11434/v1"
          color: root.dayflow ? root.dayflow.dim : Color.muted
          font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
          font.pixelSize: Style.font.body }
      }

      Row {
        spacing: Style.space(6)
        Rectangle {
          width: nextText.implicitWidth + Style.space(16)
          height: nextText.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.accentFill(0.16) : "transparent"
          border.color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"
          opacity: (root.mode === "openrouter" && root.apiKey === "")
            || (root.mode === "custom" && root.baseUrl === "") ? 0.45 : 1
          Text { id: nextText; anchors.centerIn: parent; text: "Continue"
            color: root.dayflow ? root.dayflow.foreground : Color.foreground
            font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.body; font.bold: true }
          MouseArea { anchors.fill: parent
            enabled: parent.opacity === 1
            onClicked: {
              if (root.mode !== "openrouter" && root.modelSlug.indexOf("/") >= 0)
                root.modelSlug = "gemma3:4b"
              root.step = 2
            } }
        }
        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: "Skip setup"
          color: root.dayflow ? root.dayflow.dim : Color.muted
          font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
          font.underline: true
          MouseArea { anchors.fill: parent; onClicked: root.dismissed() }
        }
      }
    }

    // ---- step 2: model + categories ----
    Column {
      visible: root.step === 2
      width: parent.width
      spacing: Style.space(8)

      Text {
        width: parent.width
        text: "Pick a vision model"
        color: root.dayflow ? root.dayflow.foreground : Color.foreground
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.body; font.bold: true
      }
      Flow {
        width: parent.width
        spacing: Style.space(6)
        Repeater {
          model: (root.detected.presets || []).filter(function(p) {
            return root.mode === "openrouter" || p.slug.indexOf(":") >= 0
          })
          delegate: Rectangle {
            width: presetLabel.implicitWidth + Style.space(14)
            height: presetLabel.implicitHeight + Style.space(8)
            radius: Style.cornerRadius
            color: root.modelSlug === modelData.slug
              ? (root.dayflow ? root.dayflow.accentFill(0.16) : "transparent")
              : "transparent"
            border.color: root.modelSlug === modelData.slug
              ? (root.dayflow ? root.dayflow.accentFill(0.5) : "transparent")
              : (root.dayflow ? root.dayflow.fgFill(0.12) : "transparent")
            Text { id: presetLabel; anchors.centerIn: parent; text: modelData.name
              color: root.dayflow ? root.dayflow.foreground : Color.foreground
              font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
              font.pixelSize: Style.font.caption }
            MouseArea { anchors.fill: parent; onClicked: root.modelSlug = modelData.slug }
          }
        }
      }

      Text {
        width: parent.width
        text: "Categories to track (optional — defaults work fine)"
        color: root.dayflow ? root.dayflow.dim : Color.muted
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
      Flow {
        width: parent.width
        spacing: Style.space(6)
        Repeater {
          model: ["coding", "browsing", "communication", "writing", "design",
                  "media", "meetings", "system", "personal"]
          delegate: Rectangle {
            width: catLabel.implicitWidth + Style.space(14)
            height: catLabel.implicitHeight + Style.space(6)
            radius: Style.cornerRadius
            color: root.catPicks[modelData]
              ? (root.dayflow ? root.dayflow.accentFill(0.16) : "transparent")
              : "transparent"
            border.color: root.catPicks[modelData]
              ? (root.dayflow ? root.dayflow.accentFill(0.5) : "transparent")
              : (root.dayflow ? root.dayflow.fgFill(0.12) : "transparent")
            Text { id: catLabel; anchors.centerIn: parent; text: modelData
              color: root.dayflow ? root.dayflow.foreground : Color.foreground
              font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
              font.pixelSize: Style.font.caption }
            MouseArea { anchors.fill: parent
              onClicked: {
                var p = Object.assign({}, root.catPicks)
                p[modelData] = !p[modelData]
                root.catPicks = p
              } }
          }
        }
      }

      Row {
        spacing: Style.space(6)
        Rectangle {
          width: finText.implicitWidth + Style.space(16)
          height: finText.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.accentFill(0.16) : "transparent"
          border.color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"
          Text { id: finText; anchors.centerIn: parent; text: "Save & test"
            color: root.dayflow ? root.dayflow.foreground : Color.foreground
            font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.body; font.bold: true }
          MouseArea { anchors.fill: parent; onClicked: { root.apply(); root.step = 3 } }
        }
        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: "Back"
          color: root.dayflow ? root.dayflow.dim : Color.muted
          font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
          font.underline: true
          MouseArea { anchors.fill: parent; onClicked: root.step = 1 }
        }
      }
    }

    // ---- step 3: result ----
    Column {
      visible: root.step === 3
      width: parent.width
      spacing: Style.space(8)

      Text {
        width: parent.width
        text: root.testing ? "Checking..." : root.testResult
        color: root.dayflow ? root.dayflow.foreground : Color.foreground
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
        wrapMode: Text.WordWrap
      }
      Text {
        visible: !root.testing
        width: parent.width
        text: "Install services to start capturing in the background, or open the panel and finish setup in Settings."
        color: root.dayflow ? root.dayflow.dim : Color.muted
        font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
        font.pixelSize: Style.font.caption
        wrapMode: Text.WordWrap
      }
      Row {
        spacing: Style.space(6)
        Rectangle {
          visible: !root.testing
          width: instText.implicitWidth + Style.space(16)
          height: instText.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.accentFill(0.16) : "transparent"
          border.color: root.dayflow ? root.dayflow.accentFill(0.5) : "transparent"
          Text { id: instText; anchors.centerIn: parent; text: "Install services"
            color: root.dayflow ? root.dayflow.foreground : Color.foreground
            font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.body; font.bold: true }
          MouseArea { anchors.fill: parent; onClicked: installProc.running = true }
        }
        Rectangle {
          visible: !root.testing
          width: doneText.implicitWidth + Style.space(16)
          height: doneText.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: root.dayflow ? root.dayflow.btnBg(doneMa.containsMouse) : "transparent"
          border.color: root.dayflow ? root.dayflow.fgFill(0.12) : "transparent"
          Text { id: doneText; anchors.centerIn: parent; text: "Done"
            color: root.dayflow ? root.dayflow.foreground : Color.foreground
            font.family: root.dayflow ? root.dayflow.fontFamily : Style.font.family
            font.pixelSize: Style.font.body }
          MouseArea { id: doneMa; anchors.fill: parent; hoverEnabled: true
            onClicked: {
              if (root.dayflow) root.dayflow.loadConfig()
              root.dismissed()
            } }
        }
      }
    }
  }
}
