package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const version = "1.0.0"

func usage() {
	fmt.Fprintf(os.Stderr, `dayflow %s — private, automatic work journal for Wayland/Omarchy

Usage: dayflow <command> [args]

Engine:
  daemon              Run the capture loop in the foreground (for systemd)
  summarize [--now]   Summarize all complete pending blocks (--now includes current)
  install             Write + enable systemd user units (capture service, summarize timer)
  uninstall           Disable and remove the systemd units

Query:
  today [--json]      Print today's timeline
  day <YYYY-MM-DD> [--json]
  status [--json]     Show recording state and counts
  blocks [--json]     List blocks that failed summarization
  standup [--json]    Generate a standup update from yesterday/today
  insights [day|week|month] [--json]  Focus, category, app, and distraction analytics

Control:
  pause | resume | toggle   Control screen capture
  config                  Print config path and current config
  config set <key> <val>  Update config (model, api_base_url, capture_interval_sec,
                          block_minutes, frames_per_block, jpeg_quality, keep_frames,
                          retention_days, max_storage_mb, auto_pause_locked, ignore_apps,
                          output, capture_command, openrouter_api_key, provider,
                          filter_inappropriate, debug)
  ignore [--active|class] Add an app to the ignore list (--active = focused window)
  unignore <class>        Remove an app from the ignore list
  events [--json] [-n N]  Recent event log (captures, skips, errors, summaries)
  usage [--json]          Token usage totals from the api_calls log
  stats [--json]          Storage, block counts, date range, and API usage
  week | month [--json]   Timeline rollups
  export [day|YYYY-MM-DD|week|month]   Markdown export to stdout
  mcp                     Run the MCP server over stdio (for agents)
  tui                     Interactive terminal timeline (day/week/month, search, standup, insights)
  search <query>          Search block titles, summaries, and apps
  retry                   Reset failed/dead blocks for re-summarization
  scrub <query>           Delete blocks whose title or summary contains <query>

Setup & health:
  setup                   Interactive AI-provider onboarding (OpenRouter or local endpoint)
  models                  List vision-capable models on your OpenRouter account
  doctor                  Check session, grim, key, model, and endpoint support

Config: %s
Data:   %s
`, version, configPath(), dataDir())
	os.Exit(2)
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	jsonOut := hasFlag(args, "--json")

	cfg, err := loadConfig()
	if err != nil {
		fatal(err)
	}

	switch cmd {
	case "daemon":
		fatal(runDaemon(cfg))

	case "summarize":
		db, err := openDB()
		fatal(err)
		defer db.Close()
		n, err := summarizePending(db, cfg, hasFlag(args, "--now"))
		fatal(err)
		if !jsonOut {
			fmt.Printf("summarized %d block(s)\n", n)
		} else {
			json.NewEncoder(os.Stdout).Encode(map[string]int{"summarized": n})
		}

	case "today":
		printTimeline(cfg, time.Now(), jsonOut)

	case "timeline":
		// timeline [--json] [YYYY-MM-DD]
		d := time.Now()
		for _, a := range args {
			if len(a) == 10 && a[4] == '-' {
				parsed, err := time.ParseInLocation("2006-01-02", a, time.Local)
				fatal(err)
				d = parsed
			}
		}
		printTimeline(cfg, d, jsonOut)

	case "day":
		var d time.Time
		for _, a := range args {
			if a[0] != '-' {
				var err error
				d, err = time.ParseInLocation("2006-01-02", a, time.Local)
				fatal(err)
			}
		}
		if d.IsZero() {
			usage()
		}
		printTimeline(cfg, d, jsonOut)

	case "status":
		printStatus(cfg, jsonOut)

	case "blocks":
		printFailed(cfg, jsonOut)

	case "pause":
		setPaused(true)
		db, _ := openDB()
		logEvent(db, "paused", "")
		fmt.Println("paused")
	case "resume":
		setPaused(false)
		db, _ := openDB()
		logEvent(db, "resumed", "")
		fmt.Println("resumed")
	case "toggle":
		setPaused(!paused())
		db, _ := openDB()
		if paused() {
			logEvent(db, "paused", "")
		} else {
			logEvent(db, "resumed", "")
		}
		if paused() {
			fmt.Println("paused")
		} else {
			fmt.Println("resumed")
		}

	case "config":
		if len(args) >= 3 && args[0] == "set" {
			if args[1] == "model" {
				if vis, ok := isVisionModel(cfg, args[2]); ok && !vis {
					fatal(fmt.Errorf("model %q cannot read images — dayflow needs a vision model (see 'dayflow models')", args[2]))
				}
			}
			fatal(setConfigValue(args[1], args[2]))
			fmt.Println("set", args[1])
			break
		}
		masked := cfg
		if masked.OpenRouterAPIKey != "" {
			masked.OpenRouterAPIKey = "***redacted***"
		}
		b, _ := json.MarshalIndent(masked, "", "  ")
		fmt.Printf("%s\n%s\n", configPath(), b)

	case "ignore":
		var cls string
		for _, a := range args {
			if a == "--active" {
				cls = activeWindowClass()
			} else if a[0] != '-' {
				cls = a
			}
		}
		if cls == "" {
			fatal(fmt.Errorf("no app class given — use --active on Hyprland or pass a class"))
		}
		for _, ig := range cfg.IgnoreApps {
			if strings.EqualFold(ig, cls) {
				fmt.Println("already ignored:", cls)
				return
			}
		}
		fatal(setConfigValue("ignore_apps", strings.Join(append(cfg.IgnoreApps, cls), ",")))
		db, _ := openDB()
		logEvent(db, "app_ignored", cls)
		fmt.Println("ignoring:", cls)

	case "unignore":
		if len(args) == 0 {
			usage()
		}
		cls := args[0]
		var kept []string
		for _, ig := range cfg.IgnoreApps {
			if !strings.EqualFold(ig, cls) {
				kept = append(kept, ig)
			}
		}
		fatal(setConfigValue("ignore_apps", strings.Join(kept, ",")))
		db, _ := openDB()
		logEvent(db, "app_unignored", cls)
		fmt.Println("unignored:", cls)

	case "events":
		printEvents(cfg, args, jsonOut)

	case "usage":
		printUsage(jsonOut)

	case "stats":
		printStats(cfg, jsonOut)

	case "week", "month":
		db, err := openDB()
		fatal(err)
		defer db.Close()
		var start, end time.Time
		if cmd == "week" {
			start, end = weekBounds(time.Now())
		} else {
			start, end = monthBounds(time.Now())
		}
		blocks, err := blocksBetween(db, start, end)
		fatal(err)
		if jsonOut {
			json.NewEncoder(os.Stdout).Encode(map[string]any{
				"start": start.Format("2006-01-02"), "end": end.Format("2006-01-02"), "blocks": blocks})
		} else {
			fmt.Print(markdownTimeline(blocks, "dayflow "+cmd))
		}

	case "standup":
		db, err := openDB()
		fatal(err)
		defer db.Close()
		md, j, err := generateStandup(db, cfg, jsonOut)
		fatal(err)
		if jsonOut {
			json.NewEncoder(os.Stdout).Encode(j)
		} else {
			fmt.Print(md)
		}

	case "insights":
		db, err := openDB()
		fatal(err)
		defer db.Close()
		var start, end time.Time
		label := "dayflow insights"
		sel := "week"
		for _, a := range args {
			if a[0] != '-' {
				sel = a
			}
		}
		switch sel {
		case "day", "today":
			start, end = dayBounds(time.Now())
			label = "dayflow insights — today"
		case "yesterday":
			start, end = dayBounds(time.Now().AddDate(0, 0, -1))
			label = "dayflow insights — yesterday"
		case "week":
			start, end = weekBounds(time.Now())
			label = "dayflow insights — this week"
		case "month":
			start, end = monthBounds(time.Now())
			label = "dayflow insights — this month"
		default:
			usage()
		}
		in, err := generateInsights(db, cfg, start, end)
		fatal(err)
		if jsonOut {
			json.NewEncoder(os.Stdout).Encode(in.JSON())
		} else {
			fmt.Print(formatInsightsMarkdown(in, start, end, label))
		}

	case "search":
		if len(args) == 0 || args[0][0] == '-' {
			usage()
		}
		printSearch(args[0], jsonOut)

	case "scrub":
		if len(args) == 0 || args[0][0] == '-' {
			usage()
		}
		db, err := openDB()
		fatal(err)
		defer db.Close()
		n, err := deleteBlocksLike(db, args[0])
		fatal(err)
		fmt.Printf("deleted %d block(s) matching %q\n", n, args[0])

	case "retry":
		db, err := openDB()
		fatal(err)
		defer db.Close()
		n, err := resetFailedBlocks(db)
		fatal(err)
		fmt.Printf("reset %d failed/dead block(s) for re-summarization\n", n)

	case "export":
		// export [day|YYYY-MM-DD|week|month] [--copy]
		db, err := openDB()
		fatal(err)
		defer db.Close()
		var start, end time.Time
		label := "dayflow"
		now := time.Now()
		sel := "today"
		for _, a := range args {
			if a[0] != '-' {
				sel = a
			}
		}
		switch sel {
		case "today", "day":
			start, end = dayBounds(now)
			label = "dayflow — " + now.Format("2006-01-02")
		case "yesterday":
			start, end = dayBounds(now.AddDate(0, 0, -1))
			label = "dayflow — " + now.AddDate(0, 0, -1).Format("2006-01-02")
		case "week":
			start, end = weekBounds(now)
			label = "dayflow — week of " + start.Format("2006-01-02")
		case "month":
			start, end = monthBounds(now)
			label = "dayflow — " + now.Format("January 2006")
		default:
			t, err := time.ParseInLocation("2006-01-02", sel, time.Local)
			fatal(err)
			start, end = dayBounds(t)
			label = "dayflow — " + sel
		}
		blocks, err := blocksBetween(db, start, end)
		fatal(err)
		md := markdownTimeline(blocks, label)
		if hasFlag(args, "--copy") {
			c := exec.Command("wl-copy")
			c.Stdin = strings.NewReader(md)
			fatal(c.Run())
			fmt.Println("copied to clipboard")
		} else {
			fmt.Print(md)
		}

	case "mcp":
		fatal(runMCP(cfg))
	case "tui":
		fatal(runTUI(cfg))

	case "setup":
		fatal(runSetup())
	case "doctor":
		runDoctor(cfg)
	case "models":
		listModels(cfg)

	case "install":
		fatal(installUnits())
	case "uninstall":
		fatal(uninstallUnits())

	case "version", "--version", "-v":
		fmt.Println(version)

	default:
		usage()
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "dayflow:", err)
		os.Exit(1)
	}
}

func printTimeline(cfg Config, day time.Time, asJSON bool) {
	db, err := openDB()
	fatal(err)
	defer db.Close()
	blocks, err := blocksForDay(db, day, true)
	fatal(err)
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"date":   day.Format("2006-01-02"),
			"blocks": blocks,
			"cards":  mergeCards(blocks),
		})
		return
	}
	fmt.Printf("== %s ==\n", day.Format("Monday, 2 January 2006"))
	if len(blocks) == 0 {
		fmt.Println("(no summarized blocks)")
		return
	}
	for _, b := range blocks {
		app := ""
		if b.App != "" {
			app = "  @" + b.AppName
		}
		fmt.Printf("\n%s-%s  %s  [%s]%s\n  %s\n", b.StartStr, b.EndStr, b.Title, catDisplay(b.Category), app, b.Summary)
		for _, a := range b.Activities {
			fmt.Printf("    • %s: %s [%s]\n", appDisplayName(a.App), a.Title, catDisplay(a.Category))
		}
	}
}

func printStatus(cfg Config, asJSON bool) {
	db, err := openDB()
	fatal(err)
	defer db.Close()
	now := time.Now()
	frames, _ := countFramesToday(db, now)
	blocksDone, _ := countBlocksToday(db, now)
	pending, _ := pendingBlocks(db, cfg, now)
	lastTS, _ := lastFrameTS(db)
	last := ""
	if lastTS > 0 {
		last = time.Unix(lastTS, 0).Local().Format("3:04 PM")
	}
	var blocksTotal int
	db.QueryRow(`SELECT COUNT(1) FROM blocks WHERE status='done'`).Scan(&blocksTotal)
	storage := dataDirSize()
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"paused":         paused(),
			"frames_today":   frames,
			"blocks_done":    blocksDone,
			"blocks_pending": len(pending),
			"blocks_total":   blocksTotal,
			"last_frame":     last,
			"model":          cfg.Model,
			"ignored_apps":   cfg.IgnoreApps,
			"active_app":     activeWindowClass(),
			"configured":     cfg.OpenRouterAPIKey != "",
			"storage_bytes":  storage,
			"storage_text":   humanBytes(storage),
			"version":        version,
		})
		return
	}
	state := "recording"
	if paused() {
		state = "PAUSED"
	}
	fmt.Printf("state: %s\nframes today: %d\nblocks summarized: %d\nblocks pending: %d\nlast frame: %s\nmodel: %s\nstorage: %s\n",
		state, frames, blocksDone, len(pending), last, cfg.Model, humanBytes(storage))
}

func printFailed(cfg Config, asJSON bool) {
	db, err := openDB()
	fatal(err)
	defer db.Close()
	rows, err := db.Query(`SELECT start_ts, error FROM blocks WHERE status='failed' ORDER BY start_ts DESC LIMIT 20`)
	fatal(err)
	defer rows.Close()
	type F struct {
		Start string `json:"start"`
		Error string `json:"error"`
	}
	var out []F
	for rows.Next() {
		var ts int64
		var e string
		if err := rows.Scan(&ts, &e); err != nil {
			continue
		}
		out = append(out, F{time.Unix(ts, 0).Local().Format("2006-01-02 15:04"), e})
	}
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}
	for _, f := range out {
		fmt.Printf("%s  %s\n", f.Start, f.Error)
	}
	if len(out) == 0 {
		fmt.Println("no failed blocks")
	}
}

func printEvents(cfg Config, args []string, asJSON bool) {
	limit := 50
	for i, a := range args {
		if a == "-n" && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				limit = n
			}
		}
	}
	db, err := openDB()
	fatal(err)
	defer db.Close()
	rows, err := db.Query(`SELECT ts, type, detail FROM events ORDER BY ts DESC LIMIT ?`, limit)
	fatal(err)
	defer rows.Close()
	type E struct {
		Time   string `json:"time"`
		Type   string `json:"type"`
		Detail string `json:"detail"`
	}
	var out []E
	for rows.Next() {
		var ts int64
		var t, d string
		if err := rows.Scan(&ts, &t, &d); err != nil {
			continue
		}
		out = append(out, E{time.Unix(ts, 0).Local().Format("2006-01-02 15:04:05"), t, d})
	}
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}
	for _, e := range out {
		fmt.Printf("%s  %-18s  %s\n", e.Time, e.Type, e.Detail)
	}
}

func printSearch(query string, asJSON bool) {
	db, err := openDB()
	fatal(err)
	defer db.Close()
	rows, err := db.Query(`SELECT start_ts,end_ts,title,summary,category,app FROM blocks
	  WHERE status='done' AND (title LIKE ? OR summary LIKE ? OR app LIKE ?) ORDER BY start_ts DESC LIMIT 50`,
		"%"+query+"%", "%"+query+"%", "%"+query+"%")
	fatal(err)
	defer rows.Close()
	type M struct {
		Start, End, Title, Summary, Category, App string
	}
	var out []M
	for rows.Next() {
		var s, e int64
		var m M
		if err := rows.Scan(&s, &e, &m.Title, &m.Summary, &m.Category, &m.App); err != nil {
			continue
		}
		m.Start = time.Unix(s, 0).Local().Format("2006-01-02 3:04 PM")
		m.End = time.Unix(e, 0).Local().Format("3:04 PM")
		out = append(out, m)
	}
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}
	for _, m := range out {
		fmt.Printf("%s–%s  %s  [%s] @%s\n  %s\n", m.Start, m.End, m.Title, m.Category, m.App, m.Summary)
	}
	if len(out) == 0 {
		fmt.Println("no matches")
	}
}

func printUsage(asJSON bool) {
	db, err := openDB()
	fatal(err)
	defer db.Close()
	var calls, prompt, completion, ok, failed int
	fatal(db.QueryRow(`SELECT COUNT(1), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
	  COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status!='ok' THEN 1 ELSE 0 END),0)
	  FROM api_calls`).Scan(&calls, &prompt, &completion, &ok, &failed))
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(map[string]int{
			"api_calls": calls, "ok": ok, "failed": failed,
			"prompt_tokens": prompt, "completion_tokens": completion,
		})
		return
	}
	fmt.Printf("api calls: %d (%d ok, %d failed)\nprompt tokens: %d\ncompletion tokens: %d\n",
		calls, ok, failed, prompt, completion)
}

// printStats reports storage usage, journal counts, date coverage, and API
// usage — the "how much is this thing using" view.
func printStats(cfg Config, asJSON bool) {
	db, err := openDB()
	fatal(err)
	defer db.Close()

	var blocksTotal, blocksDone, blocksFailed, blocksDead, framesPending, eventsTotal int
	db.QueryRow(`SELECT COUNT(1),
	  COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END),0),
	  COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
	  COALESCE(SUM(CASE WHEN status='dead' THEN 1 ELSE 0 END),0)
	  FROM blocks`).Scan(&blocksTotal, &blocksDone, &blocksFailed, &blocksDead)
	db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&framesPending)
	db.QueryRow(`SELECT COUNT(1) FROM events`).Scan(&eventsTotal)

	var firstTS, lastTS sql.NullInt64
	db.QueryRow(`SELECT MIN(start_ts), MAX(end_ts) FROM blocks WHERE status='done'`).Scan(&firstTS, &lastTS)
	firstDay, lastDay := "", ""
	if firstTS.Valid {
		firstDay = time.Unix(firstTS.Int64, 0).Local().Format("2006-01-02")
	}
	if lastTS.Valid {
		lastDay = time.Unix(lastTS.Int64, 0).Local().Format("2006-01-02")
	}

	var calls, okCalls, failedCalls, promptTok, completionTok int
	var avgLatency float64
	db.QueryRow(`SELECT COUNT(1),
	  COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END),0),
	  COALESCE(SUM(CASE WHEN status!='ok' THEN 1 ELSE 0 END),0),
	  COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
	  COALESCE(AVG(latency_ms),0)
	  FROM api_calls`).Scan(&calls, &okCalls, &failedCalls, &promptTok, &completionTok, &avgLatency)

	var dbBytes, walBytes int64
	if fi, err := os.Stat(dbPath()); err == nil {
		dbBytes = fi.Size()
	}
	if fi, err := os.Stat(dbPath() + "-wal"); err == nil {
		walBytes = fi.Size()
	}
	framesBytes, frameFiles := dirStats(framesDir())
	totalBytes := dataDirSize()

	if asJSON {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"storage": map[string]any{
				"total_bytes": totalBytes, "total": humanBytes(totalBytes),
				"db_bytes": dbBytes, "db": humanBytes(dbBytes),
				"wal_bytes": walBytes, "wal": humanBytes(walBytes),
				"frames_bytes": framesBytes, "frames": humanBytes(framesBytes),
				"frame_files": frameFiles,
				"data_dir":    dataDir(),
				"cap_mb":      cfg.MaxStorageMB,
			},
			"blocks": map[string]any{
				"total": blocksTotal, "done": blocksDone,
				"failed": blocksFailed, "dead": blocksDead,
				"pending_frames": framesPending,
				"first_day":      firstDay, "last_day": lastDay,
			},
			"events": eventsTotal,
			"api": map[string]any{
				"calls": calls, "ok": okCalls, "failed": failedCalls,
				"prompt_tokens": promptTok, "completion_tokens": completionTok,
				"avg_latency_ms": int(avgLatency),
			},
			"config": map[string]any{
				"provider": cfg.Provider, "model": cfg.Model,
				"retention_days": cfg.RetentionDays, "keep_frames": cfg.KeepFrames,
				"max_storage_mb": cfg.MaxStorageMB, "debug": cfg.Debug,
			},
		})
		return
	}

	fmt.Printf("Storage\n")
	fmt.Printf("  data dir:   %s (%s)\n", humanBytes(totalBytes), dataDir())
	fmt.Printf("  database:   %s + %s wal\n", humanBytes(dbBytes), humanBytes(walBytes))
	fmt.Printf("  frames:     %s (%d files awaiting summary)\n", humanBytes(framesBytes), frameFiles)
	fmt.Printf("  cap:        %s\n", map[bool]string{true: "unlimited", false: fmt.Sprintf("%d MB", cfg.MaxStorageMB)}[cfg.MaxStorageMB == 0])
	fmt.Printf("Journal\n")
	fmt.Printf("  blocks:     %d total (%d done, %d failed, %d dead)\n", blocksTotal, blocksDone, blocksFailed, blocksDead)
	if firstDay != "" {
		fmt.Printf("  coverage:   %s → %s\n", firstDay, lastDay)
	}
	fmt.Printf("  events:     %d log rows\n", eventsTotal)
	fmt.Printf("API\n")
	fmt.Printf("  calls:      %d (%d ok, %d failed)\n", calls, okCalls, failedCalls)
	fmt.Printf("  tokens:     %d in / %d out\n", promptTok, completionTok)
	fmt.Printf("  avg latency: %.0f ms\n", avgLatency)
	fmt.Printf("Config\n")
	fmt.Printf("  provider:   %s\n  model:      %s\n", cfg.Provider, cfg.Model)
	fmt.Printf("  retention:  %d days, keep_frames=%v\n", cfg.RetentionDays, cfg.KeepFrames)
	fmt.Printf("  debug log:  %v (%s)\n", cfg.Debug, debugLogPath())
}
