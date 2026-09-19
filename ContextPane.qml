import QtQuick
import qs.Commons
import qs.Ui

// FullView "Context" pane — bipartite flow canvas of category shifts for the
// week. `host` is the FullView window; `host.ctxNodes()` aggregates the links.
Column {
  id: pane

  property var host: null
  readonly property var dayflow: host ? host.dayflow : null

  width: parent ? parent.width : 0
  height: parent ? parent.height : 0
  spacing: Style.space(10)
  leftPadding: Style.space(16)
  rightPadding: Style.space(16)
  topPadding: Style.space(4)

  Text {
    text: "Context shifts — where your attention moves between categories"
    color: pane.dayflow ? pane.dayflow.dim : "gray"
    font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
    font.pixelSize: Style.font.body
  }

  // bipartite flow canvas: sources left, targets right, ribbon width ∝ minutes
  Canvas {
    id: ctxCanvas
    width: parent.width
    height: parent.height - topList.height - Style.space(90)

    function layoutNodes() {
      var data = host ? host.ctxNodes() : { names: [], mins: {}, total: 0 }
      var links = pane.dayflow ? (pane.dayflow.weeklyPayload.context_shifts || []) : []
      var nodeW = Style.space(10)
      var pad = Style.space(8)
      var gap = Style.space(8)
      var H = height - pad * 2
      var names = data.names

      var outM = {}, inM = {}
      for (var i = 0; i < links.length; i++) {
        var l = links[i]
        outM[l.source] = (outM[l.source] || 0) + l.minutes
        inM[l.target] = (inM[l.target] || 0) + l.minutes
      }
      var outTot = 0, inTot = 0
      for (i = 0; i < names.length; i++) {
        outTot += outM[names[i]] || 0
        inTot += inM[names[i]] || 0
      }
      var scaleL = outTot > 0 ? (H - gap * (names.length - 1)) / outTot : 0
      var scaleR = inTot > 0 ? (H - gap * (names.length - 1)) / inTot : 0

      var ly = pad, ry = pad
      var left = {}, right = {}
      for (i = 0; i < names.length; i++) {
        var n = names[i]
        left[n] = { y: ly, h: Math.max(2, (outM[n] || 0) * scaleL), used: 0 }
        ly += left[n].h + gap
        right[n] = { y: ry, h: Math.max(2, (inM[n] || 0) * scaleR), used: 0 }
        ry += right[n].h + gap
      }
      return { left: left, right: right, links: links, nodeW: nodeW, names: names }
    }

    function labelFor(name) {
      var label = pane.dayflow ? pane.dayflow.appDisplayName(name) : name
      return label.length > 9 ? label.substring(0, 8) + "..." : label
    }

    onPaint: {
      var ctx = getContext("2d")
      ctx.reset()
      var L = layoutNodes()
      var x1 = Style.space(70)
      var x2 = width - Style.space(70) - L.nodeW
      var midX = (x1 + L.nodeW + x2) / 2

      // ribbons
      for (var i = 0; i < L.links.length; i++) {
        var l = L.links[i]
        var sl = L.left[l.source], sr = L.right[l.target]
        if (!sl || !sr) continue
        var sc = pane.dayflow.categoryColor(l.source)
        // ribbon thickness ∝ this link's share of the node's flow
        var t1 = sl.h > 0 ? (l.minutes / sumOut(L.links, l.source)) * sl.h : 0
        var t2 = sr.h > 0 ? (l.minutes / sumIn(L.links, l.target)) * sr.h : 0
        var y1 = sl.y + sl.used, y2 = sr.y + sr.used
        sl.used += t1; sr.used += t2
        ctx.beginPath()
        ctx.moveTo(x1 + L.nodeW, y1)
        ctx.bezierCurveTo(midX, y1, midX, y2, x2, y2)
        ctx.lineTo(x2, y2 + t2)
        ctx.bezierCurveTo(midX, y2 + t2, midX, y1 + t1, x1 + L.nodeW, y1 + t1)
        ctx.closePath()
        ctx.fillStyle = Qt.rgba(sc.r, sc.g, sc.b, 0.30)
        ctx.fill()
      }

      // nodes + labels
      for (i = 0; i < L.names.length; i++) {
        var n = L.names[i]
        var c = pane.dayflow.categoryColor(n)
        ctx.fillStyle = c
        ctx.fillRect(x1, L.left[n].y, L.nodeW, L.left[n].h)
        ctx.fillRect(x2, L.right[n].y, L.nodeW, L.right[n].h)
        ctx.fillStyle = Qt.rgba(0.8, 0.8, 0.85, 0.9)
        ctx.font = Math.round(Style.font.caption) + "px " + (pane.dayflow ? pane.dayflow.fontFamily : "sans")
        ctx.textAlign = "right"
        ctx.fillText(labelFor(n),
                     x1 - Style.space(6), L.left[n].y + L.left[n].h / 2 + 4)
        ctx.textAlign = "left"
        ctx.fillText(labelFor(n),
                     x2 + L.nodeW + Style.space(6), L.right[n].y + L.right[n].h / 2 + 4)
      }
    }

    function sumOut(links, name) {
      var s = 0
      for (var i = 0; i < links.length; i++) if (links[i].source === name) s += links[i].minutes
      return s
    }
    function sumIn(links, name) {
      var s = 0
      for (var i = 0; i < links.length; i++) if (links[i].target === name) s += links[i].minutes
      return s
    }

    onWidthChanged: requestPaint()
    onHeightChanged: requestPaint()
    Component.onCompleted: requestPaint()

    Connections {
      target: pane
      function onDayflowChanged() { ctxCanvas.requestPaint() }
    }
    Connections {
      target: pane.dayflow
      function onWeeklyPayloadChanged() { ctxCanvas.requestPaint() }
    }
  }

  // top shifts list
  Column {
    id: topList
    width: parent.width
    spacing: Style.space(3)

    Repeater {
      model: {
        var links = pane.dayflow ? (pane.dayflow.weeklyPayload.context_shifts || []) : []
        return links.slice(0, 6)
      }

      delegate: Text {
        required property var modelData
        textFormat: Text.PlainText
        text: (pane.dayflow ? pane.dayflow.appDisplayName(modelData.source) : modelData.source) +
              " → " +
              (pane.dayflow ? pane.dayflow.appDisplayName(modelData.target) : modelData.target) +
              "  ·  " + modelData.count + "×  ·  " +
              (pane.dayflow ? pane.dayflow.fmtDur(modelData.minutes) : modelData.minutes + "m")
        color: pane.dayflow ? pane.dayflow.dim : "gray"
        font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
        font.pixelSize: Style.font.caption
      }
    }

    Text {
      visible: pane.dayflow !== null &&
        (pane.dayflow.weeklyPayload.context_shifts || []).length === 0
      text: "No category shifts recorded this week."
      color: pane.dayflow ? pane.dayflow.dim : "gray"
      font.family: pane.dayflow ? pane.dayflow.fontFamily : ""
      font.pixelSize: Style.font.body
    }
  }
}
