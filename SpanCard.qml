import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// Full detail card for one timeline span — title, summary, per-block
// children / per-app segments, and the inline edit form (title, category,
// productive). Rendered under the DayTimeline for the clicked block.
Rectangle {
  id: cardRoot
  required property var pal
  required property var span
  // The viewed day's captured frames ({ts, path, exists}) — the card
  // renders the frame nearest the span start as a thumbnail. Callers
  // feed pal.dayFrames; empty hides the image entirely.
  property var dayFrames: []

  width: parent ? parent.width : 0
  height: cardCol.implicitHeight + Style.space(14)
  radius: Style.cornerRadius
  color: pal.fgFill(0.04)
  border.color: pal.fgFill(0.08)
  clip: true

  property bool editing: false
  property string editTitle: ""
  property string editCategory: ""
  property bool editProd: false

  // Selection feedback — a quick accent wash so a week-grid or timeline
  // click has an unmistakable visual result even before the eye parses
  // the card contents.
  function flash() { flashAnim.restart() }
  Rectangle {
    id: flashRect
    anchors.fill: parent
    radius: cardRoot.radius
    color: pal.accentFill(0.35)
    opacity: 0
  }
  SequentialAnimation {
    id: flashAnim
    NumberAnimation { target: flashRect; property: "opacity"; to: 1; duration: 90 }
    NumberAnimation { target: flashRect; property: "opacity"; to: 0; duration: 480 }
  }

  function beginEdit() {
    editTitle = span.title || ""
    editCategory = span.category || ""
    editProd = span.productive === true
    editing = true
  }

  // Category-colored edge so spans scan by activity type.
  Rectangle {
    anchors.left: parent.left
    anchors.top: parent.top
    anchors.bottom: parent.bottom
    width: Style.space(3)
    color: pal.categoryColor(span.category)
  }

  Column {
    id: cardCol
    width: parent.width - Style.space(20)
    anchors.centerIn: parent
    anchors.horizontalCenterOffset: Style.space(3)
    spacing: Style.space(3)

    Row {
      width: parent.width
      spacing: Style.space(8)

      Image {
        id: appIcon
        width: Style.space(14)
        height: Style.space(14)
        source: pal.appIcon(span.app)
        visible: status === Image.Ready
        anchors.verticalCenter: parent.verticalCenter
      }

      Text {
        width: parent.width - appIcon.width - categoryPill.width - (productiveMark.visible ? productiveMark.width : 0) - (lowConfMark.visible ? lowConfMark.width : 0) - editText.implicitWidth - parent.spacing * ((productiveMark.visible ? 1 : 0) + (lowConfMark.visible ? 1 : 0) + 3)
        text: (span.start || "") + "–" + (span.end || "") +
              " · " + pal.fmtDur(span.minutes || 0) +
              (span.count > 1 ? " · " + span.count + " blocks" : "") +
              (span.appName ? " · " + span.appName : "")
        textFormat: Text.PlainText
        color: pal.dim
        font.family: pal.fontFamily
        font.pixelSize: Style.font.caption
        elide: Text.ElideRight
        anchors.verticalCenter: parent.verticalCenter
      }

      Rectangle {
        id: categoryPill
        height: catText.implicitHeight + Style.space(4)
        width: catText.implicitWidth + Style.space(10)
        radius: height / 2
        color: pal.pillBgColor(span.category)

        Text {
          id: catText
          anchors.centerIn: parent
          text: pal.catDisplay(span.category || "")
          textFormat: Text.PlainText
          color: pal.foreground
          font.family: pal.fontFamily
          font.pixelSize: Style.font.caption
        }
      }

      Text {
        id: productiveMark
        visible: span.productive === true
        text: "⚡"
        textFormat: Text.PlainText
        color: pal.dim
        font.family: pal.fontFamily
        font.pixelSize: Style.font.caption
        anchors.verticalCenter: parent.verticalCenter
      }

      Text {
        // Jev scored a judgment on this card below the confidence
        // threshold — treat the label/summary as suspect.
        id: lowConfMark
        visible: span.low_confidence === true
        text: "?"
        textFormat: Text.PlainText
        color: Qt.rgba(0.95, 0.70, 0.15, 1.0)
        font.family: pal.fontFamily
        font.pixelSize: Style.font.caption
        font.bold: true
        anchors.verticalCenter: parent.verticalCenter
      }

      Text {
        id: editText
        text: cardRoot.editing ? "close" : "edit"
        textFormat: Text.PlainText
        color: editLink.containsMouse ? pal.foreground : pal.dim
        font.family: pal.fontFamily
        font.pixelSize: Style.font.caption
        font.underline: editLink.containsMouse
        anchors.verticalCenter: parent.verticalCenter
        MouseArea {
          id: editLink
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: {
            if (cardRoot.editing) cardRoot.editing = false
            else cardRoot.beginEdit()
          }
        }
      }
    }

    Text {
      width: parent.width
      text: span.title || ""
      textFormat: Text.PlainText
      color: pal.foreground
      font.family: pal.fontFamily
      font.pixelSize: Style.font.body
      font.bold: true
      wrapMode: Text.WordWrap
      maximumLineCount: 2
      elide: Text.ElideRight
    }

    Text {
      width: parent.width
      text: span.summary || ""
      textFormat: Text.PlainText
      color: pal.foreground
      opacity: 0.75
      font.family: pal.fontFamily
      font.pixelSize: Style.font.body
      wrapMode: Text.WordWrap
      maximumLineCount: 3
      elide: Text.ElideRight
    }

    // Timelapse thumbnail — the captured frame nearest the span start.
    Image {
      readonly property var nearest: {
        var best = null, dist = -1
        var t0 = Number(span.start_ts || 0)
        for (var i = 0; i < cardRoot.dayFrames.length; i++) {
          var f = cardRoot.dayFrames[i]
          if (!f.exists) continue
          var d = Math.abs(Number(f.ts) - t0)
          if (dist < 0 || d < dist) { dist = d; best = f }
        }
        return best
      }
      visible: nearest !== null && status !== Image.Error
      width: parent.width
      height: Style.space(64)
      fillMode: Image.PreserveAspectCrop
      sourceSize.width: Math.round(width * 2)
      sourceSize.height: Math.round(height * 2)
      source: nearest ? "file://" + nearest.path : ""
    }

    // ---- inline edit form (title, category, productive) ----
    Column {
      visible: cardRoot.editing
      width: parent.width
      spacing: Style.space(6)

      Rectangle {
        width: parent.width
        height: titleEdit.implicitHeight + Style.space(8)
        radius: Style.cornerRadius
        color: pal.fgFill(0.06)
        border.color: pal.fgFill(0.12)
        clip: true
        TextEdit {
          id: titleEdit
          anchors.fill: parent
          anchors.margins: Style.space(4)
          text: cardRoot.editTitle || ""
          color: pal.foreground
          font.family: pal.fontFamily
          font.pixelSize: Style.font.body
          wrapMode: TextEdit.Wrap
          onTextChanged: { if (activeFocus) cardRoot.editTitle = text }
        }
      }

      Row {
        width: parent.width
        spacing: Style.space(6)

        Rectangle {
          width: parent.width - prodPill.width - Style.space(6)
          height: catEdit.implicitHeight + Style.space(8)
          radius: Style.cornerRadius
          color: pal.fgFill(0.06)
          border.color: pal.fgFill(0.12)
          clip: true
          TextEdit {
            id: catEdit
            anchors.fill: parent
            anchors.margins: Style.space(4)
            text: cardRoot.editCategory || ""
            color: pal.foreground
            font.family: pal.fontFamily
            font.pixelSize: Style.font.body
            onTextChanged: { if (activeFocus) cardRoot.editCategory = text }
          }
        }

        Rectangle {
          id: prodPill
          height: catEdit.implicitHeight + Style.space(8)
          width: prodPillText.implicitWidth + Style.space(12)
          radius: height / 2
          color: cardRoot.editProd ? pal.accentFill(0.15) : pal.fgFill(0.06)
          border.color: cardRoot.editProd ? pal.accentFill(0.5) : pal.fgFill(0.12)
          Text {
            id: prodPillText
            anchors.centerIn: parent
            text: cardRoot.editProd ? "⚡ productive" : "not productive"
            textFormat: Text.PlainText
            color: pal.foreground
            font.family: pal.fontFamily
            font.pixelSize: Style.font.caption
          }
          MouseArea {
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: cardRoot.editProd = !cardRoot.editProd
          }
        }
      }

      Row {
        spacing: Style.space(8)

        PanelButton {
          pal: cardRoot.pal
          compact: true
          selected: true
          text: "Save"
          onClicked: {
            cardRoot.editing = false
            cardRoot.pal.saveBlockEdits(cardRoot.span.start_ts,
              cardRoot.editTitle, cardRoot.editCategory,
              cardRoot.editProd, cardRoot.span)
          }
        }

        PanelButton {
          pal: cardRoot.pal
          compact: true
          frame: "quiet"
          text: "Cancel"
          onClicked: cardRoot.editing = false
        }
      }
    }

    // Merged span: one row per underlying block — app icon + time + title,
    // the literal "what apps at what times" breakdown.
    Repeater {
      model: span.count > 1 ? span.children : []

      delegate: Row {
        required property var modelData
        width: parent.width
        spacing: Style.space(3)

        Image {
          visible: status === Image.Ready
          width: visible ? Style.space(10) : 0
          height: Style.space(10)
          y: Style.space(1)
          source: pal.appIcon(modelData.app)
        }
        Text {
          width: parent.width - parent.spacing - Style.space(10)
          text: modelData.start + "–" + modelData.end +
                (modelData.app ? "  " + pal.appDisplayName(modelData.app) : "") +
                "  " + modelData.title
          textFormat: Text.PlainText
          color: pal.dim
          font.family: pal.fontFamily
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }
      }
    }

    // Single block: per-app segments with icons.
    Repeater {
      model: span.count === 1 && span.children && span.children[0]
        ? (span.children[0].activities || []) : []

      delegate: Row {
        required property var modelData
        width: parent.width
        spacing: Style.space(3)

        Image {
          visible: status === Image.Ready
          width: visible ? Style.space(10) : 0
          height: Style.space(10)
          y: Style.space(1)
          source: pal.appIcon(modelData.app)
        }
        Text {
          width: parent.width - parent.spacing - Style.space(10)
          text: pal.appDisplayName(modelData.app) + " · " + modelData.title
          textFormat: Text.PlainText
          color: pal.dim
          font.family: pal.fontFamily
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }
      }
    }
  }
}
