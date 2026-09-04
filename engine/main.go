package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const version = "0.2.0"

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

Control:
  pause | resume | toggle   Control screen capture
  config                  Print config path and current config
  config set <key> <val>  Update config (model, capture_interval_sec, block_minutes,
                          frames_per_block, jpeg_quality, keep_frames, retention_days,
                          ignore_apps, output, capture_command, openrouter_api_key)
  ignore [--active|class] Add an app to the ignore list (--active = focused window)
  unignore <class>        Remove an app from the ignore list
  events [--json] [-n N]  Recent event log (captures, skips, errors, summaries)
  usage [--json]          Token usage totals from the api_calls log
  week | month [--json]   Timeline rollups
  export [day|YYYY-MM-DD|week|month]   Markdown export to stdout
  mcp                     Run the MCP server over stdio (for agents)

Setup & health:
  setup                   Interactive OpenRouter onboarding (key + vision model)
  models                  List vision-capable models on your OpenRouter account
  doctor                  Check session, grim, key, and model support

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
				if vis, ok := isVisionModel(cfg.OpenRouterAPIKey, args[2]); ok && !vis {
					fatal(fmt.Errorf("model %q cannot read images — dayflow needs a vision model (see 'dayflow models')", args[2]))
				}
			}
			fatal(setConfigValue(args[1], args[2]))
			fmt.Println("set", args[1])
			break
		}
		b, _ := json.MarshalIndent(cfg, "", "  ")
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

	case "export":
		// export [day|YYYY-MM-DD|week|month] — always markdown on stdout
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
		fmt.Print(markdownTimeline(blocks, label))

	case "mcp":
		fatal(runMCP(cfg))

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
	blocks, err := blocksForDay(db, day)
	fatal(err)
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"date":   day.Format("2006-01-02"),
			"blocks": blocks,
		})
		return
	}
	fmt.Printf("== %s ==\n", day.Format("Monday, 2 January 2006"))
	if len(blocks) == 0 {
		fmt.Println("(no summarized blocks)")
		return
	}
	for _, b := range blocks {
		fmt.Printf("\n%s-%s  %s  [%s]\n  %s\n", b.StartStr, b.EndStr, b.Title, b.Category, b.Summary)
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
		last = time.Unix(lastTS, 0).Local().Format("15:04")
	}
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"paused":         paused(),
			"frames_today":   frames,
			"blocks_done":    blocksDone,
			"blocks_pending": len(pending),
			"last_frame":     last,
			"model":          cfg.Model,
			"ignored_apps":   cfg.IgnoreApps,
			"active_app":     activeWindowClass(),
			"configured":     cfg.OpenRouterAPIKey != "",
			"version":        version,
		})
		return
	}
	state := "recording"
	if paused() {
		state = "PAUSED"
	}
	fmt.Printf("state: %s\nframes today: %d\nblocks summarized: %d\nblocks pending: %d\nlast frame: %s\nmodel: %s\n",
		state, frames, blocksDone, len(pending), last, cfg.Model)
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
		rows.Scan(&ts, &e)
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
		rows.Scan(&ts, &t, &d)
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
