import QtQml

// Shared minute-of-day helpers for the timeline surfaces (DayTimeline,
// WeekCalendar). One invariant lives here: epoch `_ts` fields are
// authoritative, "12:00 AM" strings are the fixture fallback, and an end
// at/before the start minute means the card crossed midnight and reads
// as ending at 24:00.
QtObject {
  function toMin(span, key) {
    var ts = Number(span[key + "_ts"] || 0)
    if (ts > 0) {
      var dt = new Date(ts * 1000)
      return dt.getHours() * 60 + dt.getMinutes()
    }
    var s = String(span[key] || "")
    var m = s.match(/^(\d+):(\d+)\s*(AM|PM)?$/i)
    if (!m) return 0
    var h = parseInt(m[1], 10), mn = parseInt(m[2], 10)
    if (m[3]) {
      var pm = m[3].toUpperCase() === "PM"
      if (h === 12) h = pm ? 12 : 0
      else if (pm) h += 12
    }
    return h * 60 + mn
  }

  // An end minute-of-day earlier than the start reads as 24:00 — but only
  // when the epoch end genuinely runs past the epoch start (midnight
  // crossing). Missing/unparseable ends and same-minute zero-length spans
  // stay a sliver, as they always did.
  function endMin(span, startMin) {
    var e = toMin(span, "end")
    if (e > startMin) return e
    var ets = Number(span.end_ts || 0), sts = Number(span.start_ts || 0)
    if (e < startMin)
      return (ets > 0 && sts > 0 && ets > sts) ? 1440 : startMin + 1
    // e === startMin: a same-day sub-minute span stays a sliver, but an
    // end on a LATER local calendar date is a real day-crossing span.
    if (ets > sts && sts > 0) {
      var sd = new Date(sts * 1000), ed = new Date(ets * 1000)
      if (ed.getFullYear() !== sd.getFullYear() || ed.getMonth() !== sd.getMonth()
          || ed.getDate() !== sd.getDate())
        return 1440
    }
    return startMin + 1
  }
}
