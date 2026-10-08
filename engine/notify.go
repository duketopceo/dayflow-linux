package main

import (
	"context"
	"database/sql"
	"os/exec"
	"strconv"
	"time"
)

// Notification classes — per-class gates under Config.Notifications.Classes.
// stall is the only default-on class (a stalled capture loop is silent data
// loss); every other class is an opt-in nudge. Notification bodies are
// event-class labels only ("capture stalled") — never block titles, goal
// text, or file paths — because notification daemons render on lock screens.
const (
	notifyClassStall     = "stall"
	notifyClassPaused    = "paused"
	notifyClassRecovered = "recovered"
	notifyClassStandup   = "standup"
	notifyClassGoal      = "goal"
)

// NotificationConfig gates desktop notifications. Enabled is the master
// switch (default on); Classes is the per-class gate (only stall defaults
// on); GoalReminderHour is the local hour at/after which a pending daily
// goal nudges (<=0 falls back to defaultGoalReminderHour).
type NotificationConfig struct {
	Enabled          bool            `json:"enabled"`
	Classes          map[string]bool `json:"classes"`
	GoalReminderHour int             `json:"goal_reminder_hour"`
}

// defaultNotifications ships enabled with only the stall class on.
func defaultNotifications() NotificationConfig {
	return NotificationConfig{
		Enabled:          true,
		Classes:          map[string]bool{notifyClassStall: true},
		GoalReminderHour: defaultGoalReminderHour,
	}
}

const defaultGoalReminderHour = 17 // 5pm local — the end-of-day nudge window

// Rate-limit knobs, vars so tests can shrink them. The budget lives in the
// meta table (notify_count:<class>:<date>, notify_last:<class>) so it holds
// across processes and restarts — the daemon and the summarize oneshot share
// one budget, and recovery never resets it (dock thrash would otherwise
// re-notify forever).
var notifyDailyCap = 3
var notifyQuietPeriod = 20 * time.Minute
var notifyExecTimeout = 5 * time.Second

// stallNotifySecs is how long the capture error streak must run before the
// stall class fires — measured in wall-clock elapsed since the first
// failure, not in ticks, so a large capture interval can't turn failure #1
// into an alert.
const stallNotifySecs = 300

const stallNotifyDur = stallNotifySecs * time.Second

// stallAlertable reports whether a consecutive-failure streak qualifies as
// a stall: at least two failures spanning ~stallNotifySecs of wall-clock.
// The streak floor plus the monotonic elapsed check replace the old
// fails*interval product, which fired on failure #1 whenever the interval
// alone met the threshold (any interval >= 300s).
func stallAlertable(streak int, since time.Time) bool {
	return streak >= 2 && !since.IsZero() && time.Since(since) >= stallNotifyDur
}

// metaCaptureHeartbeat is the daemon's per-tick liveness key. Unlike the
// events table (capped at 2000 rows by trimLogTables), a meta row is never
// evicted, so a dead daemon's last sign of life can't be trimmed out from
// under the stall detector.
const metaCaptureHeartbeat = "capture_heartbeat_ts"

// metaBackendNeedsWayland records the resolved backend's socket gate ("1"/"0")
// at each daemon start and reload swap. The stall detector reads it instead
// of re-deriving the resolver's env predicate — the daemon's own answer can
// never drift from a future backend (portal) or a changed resolver.
const metaBackendNeedsWayland = "capture_backend_needs_wayland"

// notifyOnceDaily lowers the cap for nudge classes to one send per day —
// "standup ready" on every slow tick would be noise even under the cap.
var notifyOnceDaily = map[string]bool{
	notifyClassStandup: true,
	notifyClassGoal:    true,
}

// notifyEnabled reports whether the notifications master switch and the
// per-class gate are both on.
func notifyEnabled(cfg Config, class string) bool {
	return cfg.Notifications.Enabled && cfg.Notifications.Classes[class]
}

func metaGet(db *sql.DB, k string) string {
	var v string
	db.QueryRow(`SELECT v FROM meta WHERE k=?`, k).Scan(&v)
	return v
}

// metaSetBackendGate persists the resolved backend's wayland-socket gate
// for the stall detector — "1" socket-gated (grim, portal), "0" ungated.
func metaSetBackendGate(db *sql.DB, b captureBackend) {
	v := "0"
	if b.NeedsWaylandSocket() {
		v = "1"
	}
	metaSet(db, metaBackendNeedsWayland, v)
}

func metaSet(db *sql.DB, k, v string) {
	db.Exec(`INSERT INTO meta(k,v) VALUES(?,?)
	  ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
}

// claimNotifyQuiet asserts the per-class quiet period atomically: the
// conditional upsert only lands when the stored last-send ts is older than
// now-quietPeriod (or the key is new), so the daemon and the summarize
// oneshot can't both pass a check-then-set and double-send.
func claimNotifyQuiet(db *sql.DB, lastKey string, now time.Time) bool {
	var v string
	err := db.QueryRow(`INSERT INTO meta(k,v) VALUES(?, ?)
	  ON CONFLICT(k) DO UPDATE SET v=excluded.v
	  WHERE CAST(meta.v AS INTEGER) <= ? RETURNING v`,
		lastKey, strconv.FormatInt(now.Unix(), 10),
		now.Add(-notifyQuietPeriod).Unix()).Scan(&v)
	return err == nil
}

// claimNotifyBudget spends one unit of the per-class daily cap atomically:
// the conditional upsert increments the counter only while it is under
// limit, so concurrent senders can never both slip under the cap — and the
// stored count never exceeds it.
func claimNotifyBudget(db *sql.DB, countKey string, limit int) bool {
	var v string
	err := db.QueryRow(`INSERT INTO meta(k,v) VALUES(?, '1')
	  ON CONFLICT(k) DO UPDATE SET v=CAST(meta.v AS INTEGER)+1
	  WHERE CAST(meta.v AS INTEGER) < ? RETURNING v`,
		countKey, limit).Scan(&v)
	return err == nil
}

// notify emits one desktop notification via notify-send, gated by the
// notifications config and bounded by the meta-keyed daily cap plus a quiet
// period. Both gates are claimed by single-statement atomic upserts — the
// daemon and the summarize oneshot share one budget — and the claims land
// before exec so a failing or hung notify-send can't retry-storm from a
// streaking emission site; a missing binary stays silent and leaves the
// budget intact. An exec failure is still traced as a notify_failed event
// (class name only — the label-only rule for bodies applies to the events
// table too). Note GLib falls back to $XDG_RUNTIME_DIR/bus when
// DBUS_SESSION_BUS_ADDRESS is unset, so notify-send can still reach the
// bus from the stripped environment of the summarize oneshot. Errors never
// propagate — notifications are best-effort. Callers on the capture tick
// must use notifyAsync instead.
func notify(db *sql.DB, cfg Config, class, title, body string) {
	if db == nil || !notifyEnabled(cfg, class) {
		return
	}
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	limit := notifyDailyCap
	if notifyOnceDaily[class] {
		limit = 1
	}
	now := time.Now()
	// Quiet claimed before budget so a suppressed send doesn't spend count.
	if !claimNotifyQuiet(db, "notify_last:"+class, now) {
		return
	}
	if !claimNotifyBudget(db, "notify_count:"+class+":"+now.Format("2006-01-02"), limit) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyExecTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "notify-send", "-a", "dayflow", title, body).Run(); err != nil {
		logEvent(db, "notify_failed", class)
		debugf(cfg, "notify %s: %v", class, err)
	}
}

// notifyAsync emits off the caller's goroutine — the daemon's select loop
// must never block on dbus or a hung notify-send. Gated-off classes return
// without spawning.
func notifyAsync(db *sql.DB, cfg Config, class, title, body string) {
	if db == nil || !notifyEnabled(cfg, class) {
		return
	}
	go notify(db, cfg, class, title, body)
}

// stallEventTypes is the full capture-lifecycle set — tick heartbeats plus
// the start/stop/pause/resume transitions that explain silence.
const stallEventTypes = `'daemon_start','daemon_stop','capture_saved','capture_deduped',
  'capture_ignored','capture_error','capture_recovered','capture_output',
  'capture_output_disabled','capture_paused','capture_resumed',
  'auto_paused','auto_resumed','paused','resumed'`

// stallHeartbeatTypes is the subset that proves a capture tick actually ran
// — daemon_start is a boot marker, not a heartbeat: under Restart=always a
// crash loop writes a fresh one every few seconds while zero frames flow.
const stallHeartbeatTypes = `'daemon_stop','capture_saved','capture_deduped',
  'capture_ignored','capture_error','capture_recovered','capture_output',
  'capture_output_disabled','capture_paused','capture_resumed',
  'auto_paused','auto_resumed','paused','resumed'`

// stallStaleFloorSec is the oneshot's staleness floor. Persistent=true on
// the summarize timer fires it the instant the machine wakes, racing the
// daemon's first post-suspend heartbeat — a low floor manufactures a false
// stall after every sleep. Thirty minutes is still a real stall, and a true
// stall persists across the 15-minute runs anyway. A var so tests can
// shrink it.
var stallStaleFloorSec int64 = 30 * 60

// captureQuietNow reports whether capture is *currently* in a legitimate
// quiet state: manual pause, lock-screen auto-pause, or — when the
// resolved backend is socket-gated — no wayland session (grim exits
// instantly without a socket, so the daemon parks in capture_paused by
// design). Custom commands and X11 are ungated, so their silence always
// counts. The backend flag comes from the daemon's own meta row, not the
// checker's env — the oneshot's env only needs to reach the socket probe.
// A missing flag predates the feature: default gated (legacy daemons were
// all grim), the conservative-quiet direction. A stale quiet-type event
// only suppresses the stall alert while its condition still holds — a
// daemon that died mid-pause must alert.
func captureQuietNow(db *sql.DB, cfg Config) bool {
	if paused() {
		return true
	}
	if cfg.AutoPauseLocked && screenLocked() {
		return true
	}
	return metaGet(db, metaBackendNeedsWayland) != "0" && !waylandReachable()
}

// checkCaptureStall reports a wedged or dead capture daemon from a live
// process — the 15-minute summarize oneshot calls it because a daemon stuck
// in an unbounded call cannot notify about itself. Freshness comes from two
// sources: the meta heartbeat (one row per capture tick, immune to the
// events table's 2000-row cap) and the newest lifecycle event, which also
// disambiguates wedged/dead silence from an intentional quiet state
// (stopped daemon, current pause, lock, absent wayland session).
func checkCaptureStall(db *sql.DB, cfg Config) {
	if db == nil || !notifyEnabled(cfg, notifyClassStall) {
		return
	}
	var metaTS int64
	if ts, err := strconv.ParseInt(metaGet(db, metaCaptureHeartbeat), 10, 64); err == nil {
		metaTS = ts
	}
	var lastType string
	var lastTS int64
	err := db.QueryRow(`SELECT type, ts FROM events WHERE type IN (`+stallEventTypes+`)
	  ORDER BY ts DESC, id DESC LIMIT 1`).Scan(&lastType, &lastTS)
	if err != nil || lastTS == 0 {
		if metaTS == 0 {
			return // daemon never ran on this install — nothing to call a stall
		}
		lastType = "" // events trimmed away; the meta heartbeat is all that remains
	}
	if metaTS == 0 {
		// pause/resume events come from the CLI too — without any daemon
		// start on record they are user noise, not a stall.
		var starts int
		db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='daemon_start'`).Scan(&starts)
		if starts == 0 {
			return
		}
	}
	stale := stallStaleFloorSec
	if s := int64(3 * cfg.CaptureIntervalSec); s > stale {
		stale = s
	}
	var fresh int64
	switch lastType {
	case "daemon_stop":
		return // intentional stop — the daemon announced its own silence
	case "capture_paused", "auto_paused", "paused":
		// A quiet claim suppresses only while the quiet condition still
		// holds — a daemon that died while paused or locked is a stall.
		if captureQuietNow(db, cfg) {
			return
		}
		fresh = lastTS
	case "daemon_start":
		// A start with no later heartbeat is a crash loop (or a daemon on
		// its first tick). Restarts keep the newest start row fresh, so
		// measure the run from its FIRST unbroken start — and the meta
		// heartbeat can't vouch here because a crash mid-tick refreshes it
		// on every restart.
		db.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM events WHERE type='daemon_start'
		  AND id > COALESCE((
		    SELECT MAX(id) FROM events WHERE type IN (` + stallHeartbeatTypes + `)),0)`).Scan(&fresh)
		if fresh == 0 {
			fresh = lastTS
		}
	default:
		fresh = lastTS
	}
	if lastType != "daemon_start" && metaTS > fresh {
		fresh = metaTS
	}
	if fresh == 0 || time.Now().Unix()-fresh <= stale {
		return
	}
	if captureQuietNow(db, cfg) {
		return
	}
	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
}

// checkNudges runs the opt-in once-a-day nudge checks on the daemon's slow
// tick: a ready standup draft and a still-pending daily goal. Each check is
// a cheap indexed query; emission is async and capped inside notifyAsync.
func checkNudges(db *sql.DB, cfg Config) {
	checkNudgesAt(db, cfg, time.Now())
}

func checkNudgesAt(db *sql.DB, cfg Config, now time.Time) {
	if db == nil || !cfg.Notifications.Enabled {
		return
	}
	today := now.Format("2006-01-02")
	// standup-ready: today has summarized blocks and no draft row yet.
	if notifyEnabled(cfg, notifyClassStandup) {
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		var blocks, drafts int
		db.QueryRow(`SELECT COUNT(1) FROM blocks WHERE start_ts >= ? AND status='done'`,
			dayStart.Unix()).Scan(&blocks)
		if blocks > 0 {
			db.QueryRow(`SELECT COUNT(1) FROM standup_drafts WHERE date=?`, today).Scan(&drafts)
		}
		if blocks > 0 && drafts == 0 {
			notifyAsync(db, cfg, notifyClassStandup, "dayflow", "standup draft ready")
		}
	}
	// goal-pending: a goal is set for today, not completed, and it is at or
	// past the configured reminder hour.
	if notifyEnabled(cfg, notifyClassGoal) {
		hour := cfg.Notifications.GoalReminderHour
		if hour <= 0 {
			hour = defaultGoalReminderHour
		}
		if now.Hour() >= hour {
			var goal string
			var done int
			err := db.QueryRow(`SELECT goal, completed FROM day_goals WHERE date=?`, today).
				Scan(&goal, &done)
			if err == nil && goal != "" && done == 0 {
				notifyAsync(db, cfg, notifyClassGoal, "dayflow", "daily goal still pending")
			}
		}
	}
}
