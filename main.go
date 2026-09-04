package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const version = "0.1.0"

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
		fmt.Println("paused")
	case "resume":
		setPaused(false)
		fmt.Println("resumed")
	case "toggle":
		setPaused(!paused())
		if paused() {
			fmt.Println("paused")
		} else {
			fmt.Println("resumed")
		}

	case "config":
		b, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Printf("%s\n%s\n", configPath(), b)

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
