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
// stall class fires — scaled against the tick interval at the call site so
// slow-interval configs don't alert sooner in wall-clock terms.
const stallNotifySecs = 300

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

func metaSet(db *sql.DB, k, v string) {
	db.Exec(`INSERT INTO meta(k,v) VALUES(?,?)
	  ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
}

// notify emits one desktop notification via notify-send, gated by the
// notifications config and bounded by the meta-keyed daily cap plus a quiet
// period. The budget is consumed before exec so a failing or hung
// notify-send can't retry-storm from a streaking emission site; a missing
// binary stays silent and leaves the budget intact. Errors never propagate
// — notifications are best-effort. Callers on the capture tick must use
// notifyAsync instead.
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
	countKey := "notify_count:" + class + ":" + time.Now().Format("2006-01-02")
	count, _ := strconv.Atoi(metaGet(db, countKey))
	if count >= limit {
		return
	}
	lastKey := "notify_last:" + class
	if ts, err := strconv.ParseInt(metaGet(db, lastKey), 10, 64); err == nil &&
		time.Since(time.Unix(ts, 0)) < notifyQuietPeriod {
		return
	}
	metaSet(db, countKey, strconv.Itoa(count+1))
	metaSet(db, lastKey, strconv.FormatInt(time.Now().Unix(), 10))
	ctx, cancel := context.WithTimeout(context.Background(), notifyExecTimeout)
	defer cancel()
	_ = exec.CommandContext(ctx, "notify-send", "-a", "dayflow", title, body).Run()
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

// checkCaptureStall reports a wedged or dead capture daemon from a live
// process — the 15-minute summarize oneshot calls it because a daemon stuck
// in an unbounded call cannot notify about itself. It watches the capture
// loop's heartbeat in events: the most recent lifecycle event distinguishes
// wedged/dead silence from an intentional quiet state (manual pause, lock,
// absent wayland session), which is never reported as a stall.
func checkCaptureStall(db *sql.DB, cfg Config) {
	if db == nil || !notifyEnabled(cfg, notifyClassStall) {
		return
	}
	var lastType string
	var last int64
	err := db.QueryRow(`SELECT type, ts FROM events WHERE type IN (
	  'daemon_start','capture_saved','capture_deduped','capture_ignored',
	  'capture_error','capture_recovered','capture_output','capture_output_disabled',
	  'capture_paused','capture_resumed','auto_paused','auto_resumed','paused','resumed')
	  ORDER BY ts DESC, id DESC LIMIT 1`).Scan(&lastType, &last)
	if err != nil || last == 0 {
		return // daemon never ran on this install — nothing to call a stall
	}
	switch lastType {
	case "capture_paused", "auto_paused", "paused":
		return // known-quiet state: the silence is intentional
	}
	stale := int64(60)
	if s := int64(3 * cfg.CaptureIntervalSec); s > stale {
		stale = s
	}
	if time.Now().Unix()-last <= stale {
		return
	}
	if paused() || (cfg.AutoPauseLocked && screenLocked()) {
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
