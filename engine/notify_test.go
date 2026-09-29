package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeNotifySend installs a `notify-send` stub into dir that appends its
// argv to the returned marker file (one line per send). Tests must never
// exec the real binary.
func fakeNotifySend(t *testing.T, dir string) string {
	t.Helper()
	marker := filepath.Join(dir, "notify-sent.log")
	writeBin(t, dir, "notify-send", "echo \"$@\" >> \""+marker+"\"\n")
	return marker
}

// notifyTestCfg returns the standard test env with notifications enabled
// and exactly the named classes on.
func notifyTestCfg(t *testing.T, classes ...string) Config {
	t.Helper()
	cfg := testEnv(t)
	cfg.AutoPauseLocked = false // keep loginctl execs out of stall checks
	cfg.Notifications = NotificationConfig{
		Enabled:          true,
		Classes:          map[string]bool{},
		GoalReminderHour: defaultGoalReminderHour,
	}
	for _, c := range classes {
		cfg.Notifications.Classes[c] = true
	}
	return cfg
}

func notifyMarkerLines(t *testing.T, marker string) []string {
	t.Helper()
	b, err := os.ReadFile(marker)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// waitForMarker polls the marker file until it has n lines — notifyAsync
// emits on a goroutine, so assertions must give it a scheduling window.
func waitForMarker(t *testing.T, marker string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if lines := notifyMarkerLines(t, marker); len(lines) >= n {
			return lines
		}
		time.Sleep(20 * time.Millisecond)
	}
	return notifyMarkerLines(t, marker)
}

func countMarkerContaining(lines []string, sub string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func TestNotifyDefaults(t *testing.T) {
	n := defaultNotifications()
	if !n.Enabled {
		t.Fatal("notifications master switch should default on")
	}
	if !n.Classes[notifyClassStall] {
		t.Fatal("stall class should default on — silence means data loss")
	}
	for _, c := range []string{notifyClassPaused, notifyClassRecovered, notifyClassStandup, notifyClassGoal} {
		if n.Classes[c] {
			t.Fatalf("class %q should default off (opt-in nudge)", c)
		}
	}
	if n.GoalReminderHour != defaultGoalReminderHour {
		t.Fatalf("goal reminder hour = %d, want %d", n.GoalReminderHour, defaultGoalReminderHour)
	}
}

func TestNotifyConfigDefaultsSurviveLoad(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")

	// A config without the key keeps the defaults (stall on, enabled on).
	os.WriteFile(cp, []byte(`{}`), 0o600)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Notifications.Enabled || !cfg.Notifications.Classes[notifyClassStall] {
		t.Fatalf("absent notifications key should keep defaults: %+v", cfg.Notifications)
	}

	// A partial object (config patch / hand edit) that omits "enabled" or
	// "classes" must not silently disable the master switch or stall alerts.
	os.WriteFile(cp, []byte(`{"notifications":{"classes":{"paused":true}}}`), 0o600)
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Notifications.Enabled {
		t.Fatal("enabled omitted by patch should backfill to true")
	}
	// classes unmarshal merges into the default map, so stall stays on
	// alongside the patched class.
	if !cfg.Notifications.Classes[notifyClassPaused] || !cfg.Notifications.Classes[notifyClassStall] {
		t.Fatalf("patched classes should merge over defaults: %+v", cfg.Notifications.Classes)
	}
	os.WriteFile(cp, []byte(`{"notifications":{"enabled":false}}`), 0o600)
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.Enabled {
		t.Fatal("explicit enabled:false must be honored")
	}
	if !cfg.Notifications.Classes[notifyClassStall] {
		t.Fatal("omitted classes should backfill stall on")
	}
}

func TestNotifySendsThroughStub(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
	lines := notifyMarkerLines(t, marker)
	if len(lines) != 1 || !strings.Contains(lines[0], "capture stalled") {
		t.Fatalf("marker lines %v, want one send with the class label", lines)
	}
}

func TestNotifyDailyCap(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := notifyQuietPeriod
	notifyQuietPeriod = 0 // isolate the cap from the quiet period
	t.Cleanup(func() { notifyQuietPeriod = old })

	for i := 0; i < notifyDailyCap+3; i++ {
		notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
	}
	if lines := notifyMarkerLines(t, marker); len(lines) != notifyDailyCap {
		t.Fatalf("sends = %d, want hard cap of %d/day", len(lines), notifyDailyCap)
	}
	day := time.Now().Format("2006-01-02")
	if got := metaGet(db, "notify_count:"+notifyClassStall+":"+day); got != strconv.Itoa(notifyDailyCap) {
		t.Fatalf("notify_count meta = %q, want %d", got, notifyDailyCap)
	}
}

func TestNotifyQuietPeriod(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
	if lines := notifyMarkerLines(t, marker); len(lines) != 1 {
		t.Fatalf("quiet period should suppress the second send: %v", lines)
	}
	// Backdate the last-send marker beyond the quiet period — the next
	// send must go through.
	last := time.Now().Add(-2 * notifyQuietPeriod).Unix()
	metaSet(db, "notify_last:"+notifyClassStall, strconv.FormatInt(last, 10))
	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
	if lines := notifyMarkerLines(t, marker); len(lines) != 2 {
		t.Fatalf("send after quiet period should emit: %v", lines)
	}
}

func TestNotifyRecoveryDoesNotResetBudget(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall, notifyClassRecovered)
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := notifyQuietPeriod
	notifyQuietPeriod = 0
	t.Cleanup(func() { notifyQuietPeriod = old })

	// A stall, a recovery, and more stalls — the thrash pattern that must
	// not re-notify forever. Each class spends its own budget; recovery
	// emissions never reset the stall count.
	for i := 0; i < notifyDailyCap; i++ {
		notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
		if i == 0 {
			notify(db, cfg, notifyClassRecovered, "dayflow", "capture resumed")
		}
	}
	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled") // over cap
	lines := notifyMarkerLines(t, marker)
	if got := countMarkerContaining(lines, "capture stalled"); got != notifyDailyCap {
		t.Fatalf("stall sends = %d, want %d (recovery must not reset the budget)", got, notifyDailyCap)
	}
	if got := countMarkerContaining(lines, "capture resumed"); got != 1 {
		t.Fatalf("recovered sends = %d, want 1", got)
	}
	day := time.Now().Format("2006-01-02")
	if got := metaGet(db, "notify_count:"+notifyClassStall+":"+day); got != strconv.Itoa(notifyDailyCap) {
		t.Fatalf("stall budget was reset: notify_count = %q, want %d", got, notifyDailyCap)
	}
}

func TestNotifyDisabledClassNeverExecs(t *testing.T) {
	cfg := notifyTestCfg(t) // every class gated off
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := notifyQuietPeriod
	notifyQuietPeriod = 0
	t.Cleanup(func() { notifyQuietPeriod = old })

	for _, class := range []string{notifyClassStall, notifyClassPaused, notifyClassRecovered, notifyClassStandup, notifyClassGoal} {
		notify(db, cfg, class, "dayflow", "test")
	}
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("gated-off classes execed notify-send: %v", lines)
	}

	// The master switch gates every class even when the class is on.
	cfg.Notifications.Classes[notifyClassStall] = true
	cfg.Notifications.Enabled = false
	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("master switch off should suppress all sends: %v", lines)
	}
	// Gated sends never touch the rate-limit budget.
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM meta WHERE k LIKE 'notify_%'`).Scan(&n)
	if n != 0 {
		t.Fatalf("suppressed sends wrote %d meta keys, want 0", n)
	}
}

func TestNotifyMissingBinary(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	t.Setenv("PATH", t.TempDir()) // no notify-send anywhere
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled") // must not panic
	// A missing binary leaves the budget intact — installing notify-send
	// later shouldn't find a cap already spent on sends that never ran.
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM meta WHERE k LIKE 'notify_%'`).Scan(&n)
	if n != 0 {
		t.Fatalf("absent binary consumed %d meta keys, want 0", n)
	}
}

func TestNotifyHungBinaryBounded(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	old := notifyExecTimeout
	notifyExecTimeout = 200 * time.Millisecond
	t.Cleanup(func() { notifyExecTimeout = old })

	bin := t.TempDir()
	// `exec` so the stub process IS the sleeper — a plain sleep grandchild
	// would keep the pipe open past the context kill.
	writeBin(t, bin, "notify-send", "exec sleep 30\n")
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start := time.Now()
	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("notify blocked %s on a hung notify-send", elapsed)
	}
}

func TestNotifyOnceDailyClasses(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStandup)
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := notifyQuietPeriod
	notifyQuietPeriod = 0
	t.Cleanup(func() { notifyQuietPeriod = old })

	for i := 0; i < 3; i++ {
		notify(db, cfg, notifyClassStandup, "dayflow", "standup draft ready")
	}
	if lines := notifyMarkerLines(t, marker); len(lines) != 1 {
		t.Fatalf("nudge classes emit once per day, got %v", lines)
	}
}

// TestCaptureStallFromSummarize exercises the wedge-detector the summarize
// oneshot runs — the daemon cannot report its own stuck select loop.
func TestCaptureStallFromSummarize(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	cfg.CaptureIntervalSec = 10
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "") // can't check → session counts as present
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// No daemon history at all (never installed/ran) → not a stall.
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("no daemon history should not alert: %v", lines)
	}

	logEvent(db, "daemon_start", "grim -t jpeg -")

	// A fresh heartbeat → daemon alive, no alert.
	logEvent(db, "capture_saved", "x")
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("fresh heartbeat should not alert: %v", lines)
	}

	// A heartbeat inside the staleness floor is not a stall — the floor
	// absorbs the post-suspend race between the Persistent summarize timer
	// and the daemon's first wake tick.
	db.Exec(`UPDATE events SET ts=?`, time.Now().Add(-10*time.Minute).Unix())
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("inside-floor heartbeat should not alert: %v", lines)
	}

	// Heartbeat well past the floor (wedged or dead daemon) → stall alert.
	db.Exec(`UPDATE events SET ts=?`, time.Now().Add(-2*time.Hour).Unix())
	checkCaptureStall(db, cfg)
	lines := notifyMarkerLines(t, marker)
	if len(lines) != 1 || !strings.Contains(lines[0], "capture stalled") {
		t.Fatalf("stale heartbeat should alert once: %v", lines)
	}

	// An intentional stop is terminal — daemon_stop is never a stall.
	db.Exec(`DELETE FROM events`)
	logEvent(db, "daemon_start", "grim -t jpeg -")
	logEvent(db, "daemon_stop", "terminated")
	db.Exec(`UPDATE events SET ts=?`, time.Now().Add(-2*time.Hour).Unix())
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 1 {
		t.Fatalf("daemon_stop should not alert: %v", lines)
	}
}

// daemon_start is a boot marker, not a heartbeat: under Restart=always a
// crash loop keeps the newest start row fresh forever while zero ticks
// complete — the run's age is measured from its first unbroken start.
func TestCaptureStallDaemonStartIsNotFreshness(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	cfg.CaptureIntervalSec = 10
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// A single fresh start with no heartbeat yet — just booted, not a stall.
	logEvent(db, "daemon_start", "grim -t jpeg -")
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("fresh daemon_start should not alert: %v", lines)
	}

	// A completed tick after the start → alive.
	logEvent(db, "capture_saved", "x")
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("post-start heartbeat should not alert: %v", lines)
	}

	// Crash loop: only daemon_start rows — the newest is seconds old but
	// the unbroken run began long ago → stall.
	db.Exec(`DELETE FROM events`)
	db.Exec(`INSERT INTO events(ts, type, detail) VALUES(?, 'daemon_start', 'grim')`,
		time.Now().Add(-2*time.Hour).Unix())
	db.Exec(`INSERT INTO events(ts, type, detail) VALUES(?, 'daemon_start', 'grim')`,
		time.Now().Add(-time.Hour).Unix())
	logEvent(db, "daemon_start", "grim -t jpeg -")
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 1 {
		t.Fatalf("crash-looping daemon (starts only) must alert: %v", lines)
	}
}

// The meta heartbeat survives events-table trimming: a dead daemon whose
// heartbeat rows were evicted still goes stale via the meta key, and a
// fresh meta key rescues a live daemon whose events were trimmed.
func TestCaptureStallMetaHeartbeat(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	cfg.CaptureIntervalSec = 10
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "")
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// No events at all; meta fresh → alive.
	metaSet(db, metaCaptureHeartbeat, strconv.FormatInt(time.Now().Unix(), 10))
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("fresh meta heartbeat should not alert: %v", lines)
	}

	// Meta stale, still no events → stall.
	metaSet(db, metaCaptureHeartbeat,
		strconv.FormatInt(time.Now().Add(-2*time.Hour).Unix(), 10))
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 1 {
		t.Fatalf("stale meta heartbeat should alert: %v", lines)
	}

	// A fresh heartbeat event under a stale meta key → alive (either
	// source counts).
	logEvent(db, "daemon_start", "grim -t jpeg -")
	logEvent(db, "capture_saved", "x")
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 1 {
		t.Fatalf("fresh event under stale meta should not alert: %v", lines)
	}

	// And the reverse: fresh meta under a stale last event — the 2000-row
	// cap can't manufacture a stall by evicting heartbeats.
	db.Exec(`UPDATE events SET ts=?`, time.Now().Add(-2*time.Hour).Unix())
	metaSet(db, metaCaptureHeartbeat, strconv.FormatInt(time.Now().Unix(), 10))
	checkCaptureStall(db, cfg)
	if lines := notifyMarkerLines(t, marker); len(lines) != 1 {
		t.Fatalf("fresh meta under stale event should not alert: %v", lines)
	}
}

// A quiet-type last event suppresses the alert only while the quiet
// condition still holds — a daemon that died mid-pause/lock is a stall.
func TestCaptureStallQuietStates(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	cfg.CaptureIntervalSec = 10
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "") // unset → session counts as present
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := notifyQuietPeriod
	notifyQuietPeriod = 0 // isolate assertions from the quiet period
	t.Cleanup(func() { notifyQuietPeriod = old })
	resetBudget := func() {
		db.Exec(`DELETE FROM meta WHERE k LIKE 'notify_%'`)
	}
	stale := time.Now().Add(-2 * time.Hour).Unix()

	sent := func() int { return len(notifyMarkerLines(t, marker)) }

	// Stale quiet claims with nothing currently quiet → the stall fires.
	for _, quietType := range []string{"capture_paused", "auto_paused", "paused"} {
		db.Exec(`DELETE FROM events`)
		logEvent(db, "daemon_start", "grim -t jpeg -")
		logEvent(db, quietType, "test")
		db.Exec(`UPDATE events SET ts=?`, stale)
		resetBudget()
		before := sent()
		checkCaptureStall(db, cfg)
		if sent() == before {
			t.Fatalf("stale %s with no current quiet state should alert", quietType)
		}
	}

	// Manual pause via the PAUSED file suppresses the alert even when the
	// last event is a stale quiet claim — the state is genuinely current.
	db.Exec(`DELETE FROM events`)
	logEvent(db, "daemon_start", "grim -t jpeg -")
	logEvent(db, "paused", "")
	db.Exec(`UPDATE events SET ts=?`, stale)
	setPaused(true)
	t.Cleanup(func() { setPaused(false) })
	resetBudget()
	before := sent()
	checkCaptureStall(db, cfg)
	if sent() != before {
		t.Fatalf("currently-paused capture should not alert: %v", notifyMarkerLines(t, marker))
	}
	setPaused(false)

	// An absent wayland session is quiet-by-design for the grim backend:
	// capture_paused suppresses while the socket is still missing.
	db.Exec(`DELETE FROM events`)
	logEvent(db, "daemon_start", "grim -t jpeg -")
	logEvent(db, "capture_paused", "no wayland session")
	db.Exec(`UPDATE events SET ts=?`, stale)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "wayland-missing")
	resetBudget()
	before = sent()
	checkCaptureStall(db, cfg)
	if sent() != before {
		t.Fatalf("absent wayland session should not alert: %v", notifyMarkerLines(t, marker))
	}

	// ... but a custom capture_command is ungated — its silence counts.
	cfg.CaptureCommand = "/bin/cat /nonexistent"
	resetBudget()
	checkCaptureStall(db, cfg)
	if sent() == before {
		t.Fatal("capture_command + stale capture_paused should alert — the command is ungated")
	}
}

func TestCheckNudgesStandupAndGoal(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStandup, notifyClassGoal)
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := notifyQuietPeriod
	notifyQuietPeriod = 0
	t.Cleanup(func() { notifyQuietPeriod = old })

	now := time.Date(2026, 9, 28, 18, 0, 0, 0, time.Local) // past reminder hour
	today := now.Format("2006-01-02")

	// No summarized blocks and no goal → nothing.
	checkNudgesAt(db, cfg, now)
	time.Sleep(200 * time.Millisecond)
	if lines := notifyMarkerLines(t, marker); len(lines) != 0 {
		t.Fatalf("empty day should not nudge: %v", lines)
	}

	// Summarized block today + no draft → standup-ready, once.
	dayStart := time.Date(2026, 9, 28, 0, 0, 0, 0, time.Local)
	if err := upsertBlock(db, dayStart.Add(9*time.Hour), dayStart.Add(9*time.Hour+15*time.Minute),
		"work", "summary", "coding", 1, "done", ""); err != nil {
		t.Fatal(err)
	}
	checkNudgesAt(db, cfg, now)
	lines := waitForMarker(t, marker, 1)
	if countMarkerContaining(lines, "standup draft ready") != 1 {
		t.Fatalf("expected standup-ready notification: %v", lines)
	}
	// Once-daily class: a second check emits nothing more.
	checkNudgesAt(db, cfg, now)
	time.Sleep(200 * time.Millisecond)
	if lines := notifyMarkerLines(t, marker); countMarkerContaining(lines, "standup") != 1 {
		t.Fatalf("standup nudge should emit once per day: %v", lines)
	}

	// Goal set, not completed, past the reminder hour → goal-pending.
	if err := setGoal(db, today, "ship the thing"); err != nil {
		t.Fatal(err)
	}
	checkNudgesAt(db, cfg, now)
	lines = waitForMarker(t, marker, 2)
	if countMarkerContaining(lines, "daily goal still pending") != 1 {
		t.Fatalf("expected goal-pending notification: %v", lines)
	}

	// Completed goal suppresses the nudge; before the reminder hour too.
	if err := completeGoal(db, today, true); err != nil {
		t.Fatal(err)
	}
	early := time.Date(2026, 9, 28, 9, 0, 0, 0, time.Local)
	checkNudgesAt(db, cfg, now)
	checkNudgesAt(db, cfg, early)
	time.Sleep(200 * time.Millisecond)
	if lines := notifyMarkerLines(t, marker); countMarkerContaining(lines, "daily goal") != 1 {
		t.Fatalf("completed goal / pre-hour should not nudge: %v", lines)
	}
}

func TestConfigSetNotifications(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	os.WriteFile(cp, []byte(`{}`), 0o600)

	if err := setConfigValue("notifications.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.Enabled {
		t.Fatal("notifications.enabled=false should persist")
	}
	if !cfg.Notifications.Classes[notifyClassStall] {
		t.Fatal("toggling the master switch must not wipe the class gates")
	}
	if err := setConfigValue("notifications.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := setConfigValue("notifications.enabled", "maybe"); err == nil {
		t.Fatal("non-bool value should error")
	}

	// Whole-object set merges over loaded values — omitted fields keep
	// their settings.
	if err := setConfigValue("notifications", `{"classes":{"paused":true}}`); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Notifications.Classes[notifyClassPaused] || !cfg.Notifications.Classes[notifyClassStall] {
		t.Fatalf("classes = %+v, want paused+stall on (merge over defaults)", cfg.Notifications.Classes)
	}
	if !cfg.Notifications.Enabled {
		t.Fatal("object set without enabled should keep the master switch on")
	}
}

func TestConfigPatchNotifications(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "config.json")
	t.Setenv("DAYFLOW_CONFIG", cp)
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("OPENROUTER_API_KEY", "")
	os.WriteFile(cp, []byte(`{}`), 0o600)

	// patchConfig replaces the notifications object wholesale — a patch
	// that only sets classes must still write enabled:true and keep the
	// stall class on (omission must never disable it).
	if err := patchConfig(`{"notifications":{"classes":{"paused":true}}}`); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Notifications.Enabled {
		t.Fatal("classes-only patch should not disable the master switch")
	}
	if !cfg.Notifications.Classes[notifyClassPaused] || !cfg.Notifications.Classes[notifyClassStall] {
		t.Fatalf("patch should keep stall on alongside paused: %+v", cfg.Notifications.Classes)
	}

	// But an explicit stall:false is honored — opt-out, not omission.
	if err := patchConfig(`{"notifications":{"classes":{"stall":false}}}`); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.Classes[notifyClassStall] {
		t.Fatal("explicit stall:false must persist")
	}

	if err := patchConfig(`{"notifications":{"enabled":false}}`); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.Enabled {
		t.Fatal("explicit enabled:false patch must persist")
	}
	if !cfg.Notifications.Classes[notifyClassStall] {
		t.Fatal("omitted classes should backfill stall on")
	}
}

// The daily cap and quiet period are claimed by single-statement atomic
// upserts — the daemon goroutine and the summarize oneshot share one
// budget, so concurrent senders must never slip under the cap together.
func TestNotifyConcurrentSendersStayUnderCap(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	bin := t.TempDir()
	marker := fakeNotifySend(t, bin)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := notifyQuietPeriod
	notifyQuietPeriod = 0 // isolate the cap from the quiet period
	t.Cleanup(func() { notifyQuietPeriod = old })

	const senders = 16
	var wg sync.WaitGroup
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
		}()
	}
	wg.Wait()
	if lines := notifyMarkerLines(t, marker); len(lines) != notifyDailyCap {
		t.Fatalf("concurrent sends = %d, want hard cap of %d/day", len(lines), notifyDailyCap)
	}
	day := time.Now().Format("2006-01-02")
	if got := metaGet(db, "notify_count:"+notifyClassStall+":"+day); got != strconv.Itoa(notifyDailyCap) {
		t.Fatalf("notify_count = %q, want %d — the cap counter must never overshoot", got, notifyDailyCap)
	}
}

// A failing notify-send still spends the budget (no retry-storm) but the
// failure is traced as a notify_failed event carrying the class name only —
// the label-only rule for bodies applies to the events table too.
func TestNotifyExecFailureLogged(t *testing.T) {
	cfg := notifyTestCfg(t, notifyClassStall)
	bin := t.TempDir()
	writeBin(t, bin, "notify-send", "exit 1\n")
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	notify(db, cfg, notifyClassStall, "dayflow", "capture stalled")
	var n int
	db.QueryRow(`SELECT COUNT(1) FROM events WHERE type='notify_failed' AND detail=?`,
		notifyClassStall).Scan(&n)
	if n != 1 {
		t.Fatalf("notify_failed events=%d, want 1", n)
	}
	day := time.Now().Format("2006-01-02")
	if got := metaGet(db, "notify_count:"+notifyClassStall+":"+day); got != "1" {
		t.Fatalf("failed send should still spend budget: notify_count=%q", got)
	}
}

func TestStallAlertable(t *testing.T) {
	// Failure #1 must never alert — the old fails*interval product fired on
	// the first tick whenever the interval alone met the threshold.
	if stallAlertable(1, time.Now().Add(-stallNotifyDur-time.Minute)) {
		t.Fatal("a single failure must never alert, however long the interval")
	}
	if stallAlertable(2, time.Now().Add(-time.Minute)) {
		t.Fatal("a young streak should not alert")
	}
	if !stallAlertable(2, time.Now().Add(-stallNotifyDur-time.Second)) {
		t.Fatal("a streak spanning the threshold should alert")
	}
	if stallAlertable(10, time.Time{}) {
		t.Fatal("a streak with no recorded start must not alert")
	}
}
