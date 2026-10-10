import QtQuick
import qs.Commons

// Vertical day calendar — spans as proportional time blocks, not a text
// list. Time runs top→bottom over the day's active window; block height is
// duration-proportional (16px floor so short spans stay clickable) and the
// fill carries the category color. Click selects; the caller renders the
// detail card. `showNow` draws the current-time line when viewing today.
Item {
  id: tl

  required property var pal
  property var spans: []
  property int selectedIndex: -1
  property bool showNow: false
  signal spanClicked(int index)

  function toMin(s) {
    var p = String(s || "").split(":")
    return (parseInt(p[0], 10) || 0) * 60 + (parseInt(p[1], 10) || 0)
  }

  // Active window: first span start −30min → last span end +30min,
  // clamped to the day and floored at a 3h window so short days still
  // read with sane proportions.
  readonly property int viewStart: {
    if (!spans.length) return 8 * 60
    var m = 1440
    for (var i = 0; i < spans.length; i++) m = Math.min(m, toMin(spans[i].start))
    return Math.max(0, m - 30)
  }
  readonly property int viewEnd: {
    if (!spans.length) return 20 * 60
    var m = 0
    for (var i = 0; i < spans.length; i++) m = Math.max(m, toMin(spans[i].end))
    return Math.min(1440, Math.max(m + 30, viewStart + 180))
  }
  readonly property real pxPerMin: height / Math.max(1, viewEnd - viewStart)

  readonly property int gutterW: Style.space(34)
  implicitWidth: 100
  implicitHeight: Math.max(Style.space(160), Math.min(Style.space(340), (viewEnd - viewStart) * 1.4))
  clip: true

  function hhmm(m) {
    var h = Math.floor(m / 60), mm = m % 60
    return (h < 10 ? "0" : "") + h + ":" + (mm < 10 ? "0" : "") + mm
  }

  // Hour gridlines + labels.
  Repeater {
    model: Math.floor(tl.viewEnd / 60) - Math.ceil(tl.viewStart / 60) + 1
    delegate: Item {
      property int hourMin: (Math.ceil(tl.viewStart / 60) + index) * 60
      width: tl.width
      height: 1
      y: (hourMin - tl.viewStart) * tl.pxPerMin
      visible: y >= 0 && y < tl.height

      Rectangle {
        anchors.left: parent.left
        anchors.leftMargin: tl.gutterW
        anchors.right: parent.right
        height: 1
        color: tl.pal ? tl.pal.fgFill(0.07) : Qt.rgba(1, 1, 1, 0.07)
      }
      Text {
        anchors.left: parent.left
        anchors.verticalCenter: parent.verticalCenter
        anchors.verticalCenterOffset: -0.5
        text: Math.floor(hourMin / 60) + ":00"
        textFormat: Text.PlainText
        color: tl.pal ? tl.pal.dim : "gray"
        font.family: tl.pal ? tl.pal.fontFamily : ""
        font.pixelSize: Style.font.caption
      }
    }
  }

  // Time blocks.
  Repeater {
    model: tl.spans
    delegate: Rectangle {
      id: block
      property int startM: tl.toMin(modelData.start)
      property int endM: tl.toMin(modelData.end)
      property real rawH: Math.max(2, (endM - startM) * tl.pxPerMin)
      property bool tall: rawH >= 30
      property bool sel: index === tl.selectedIndex

      x: tl.gutterW + 4
      y: Math.min(tl.height - 4, (startM - tl.viewStart) * tl.pxPerMin)
      width: tl.width - tl.gutterW - 8
      height: Math.max(14, Math.min(rawH, tl.height - y))
      radius: Style.space(4)
      color: tl.pal ? tl.pal.payloadFill(modelData.color, modelData.category, sel ? 0.38 : 0.22)
                    : Qt.rgba(1, 1, 1, 0.2)
      border.color: tl.pal
        ? (sel ? tl.pal.accentFill(0.95) : tl.pal.payloadColor(modelData.color, modelData.category))
        : "transparent"
      border.width: sel ? 2 : 1
      clip: true

      Column {
        x: Style.space(5)
        y: Style.space(2)
        width: parent.width - Style.space(10)
        spacing: 0
        visible: block.tall

        Text {
          width: parent.width
          text: modelData.title || ""
          textFormat: Text.PlainText
          color: tl.pal ? tl.pal.foreground : "white"
          font.family: tl.pal ? tl.pal.fontFamily : ""
          font.pixelSize: Style.font.caption
          font.bold: true
          elide: Text.ElideRight
        }
        Text {
          width: parent.width
          visible: block.rawH >= 46
          text: modelData.start + "–" + modelData.end +
                (modelData.appName ? " · " + modelData.appName : "")
          textFormat: Text.PlainText
          color: tl.pal ? tl.pal.dim : "gray"
          font.family: tl.pal ? tl.pal.fontFamily : ""
          font.pixelSize: Style.font.caption
          elide: Text.ElideRight
        }
      }

      // Short blocks can't fit text — a category tick still reads.
      Rectangle {
        visible: !block.tall
        anchors.left: parent.left
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        anchors.margins: 3
        width: 3
        radius: 1.5
        color: tl.pal ? tl.pal.payloadColor(modelData.color, modelData.category) : "white"
      }

      MouseArea {
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: tl.spanClicked(index)
      }
    }
  }

  // Current-time line when viewing today.
  Rectangle {
    visible: tl.showNow && tl.pxPerMin > 0
    readonly property int nowMin: {
      var d = new Date()
      return d.getHours() * 60 + d.getMinutes()
    }
    readonly property real ny: (nowMin - tl.viewStart) * tl.pxPerMin
    anchors.left: parent.left
    anchors.leftMargin: tl.gutterW
    anchors.right: parent.right
    y: Math.min(tl.height - 1, Math.max(0, ny))
    height: 2
    color: tl.pal ? tl.pal.accentFill(0.95) : "white"
    // Within-window only — don't draw a line clamped to an edge.
    property bool inRange: nowMin >= tl.viewStart && nowMin <= tl.viewEnd
    opacity: inRange ? 1 : 0
    Timer { interval: 30000; running: tl.showNow; repeat: true; onTriggered: parent.nowMinChanged() }
  }
}
