package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func debugLogPath() string { return filepath.Join(dataDir(), "debug.log") }

var debugMu sync.Mutex

// debugLogKeepDays is how long rotated debug-YYYYMMDD.log archives survive.
const debugLogKeepDays = 10

// appendLog writes one timestamped line to debug.log, rotating the file first
// when it rolled past midnight or grew past 8MB. Archives are pruned after
// debugLogKeepDays. Best-effort: failures never break the caller.
func appendLog(line string) {
	debugMu.Lock()
	defer debugMu.Unlock()
	p := debugLogPath()
	if fi, err := os.Stat(p); err == nil {
		newDay := fi.ModTime().Local().Format("2006-01-02") != time.Now().Local().Format("2006-01-02")
		if newDay || fi.Size() > 8<<20 {
			arch := filepath.Join(dataDir(),
				"debug-"+fi.ModTime().Format("20060102")+".log")
			// A second same-day rotation must not clobber the first archive.
			for n := 2; ; n++ {
				if _, err := os.Stat(arch); os.IsNotExist(err) {
					break
				}
				arch = filepath.Join(dataDir(), fmt.Sprintf(
					"debug-%s-%d.log", fi.ModTime().Format("20060102"), n))
			}
			os.Rename(p, arch)
			pruneLogArchives()
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), sanitizeLogLine(line))
}

// sanitizeLogLine keeps one log call one line: argv text (or a rendered
// error) carrying \n/\r or other control chars must not forge extra lines.
func sanitizeLogLine(s string) string {
	const max = 500
	r := []rune(s)
	out := r[:0]
	for _, c := range r {
		if c < 0x20 || c == 0x7f {
			out = append(out, ' ')
			continue
		}
		out = append(out, c)
	}
	s = strings.Join(strings.Fields(string(out)), " ")
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return s
}

// pruneLogArchives deletes rotated debug-*.log files past the keep window.
func pruneLogArchives() {
	cutoff := time.Now().Add(-debugLogKeepDays * 24 * time.Hour)
	matches, _ := filepath.Glob(filepath.Join(dataDir(), "debug-*.log"))
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().Before(cutoff) {
			os.Remove(m)
		}
	}
}

// tailLogLines returns the last n lines of debug.log — the read side of the
// `dayflow log` write channel, also exposed as the MCP get_log tool.
func tailLogLines(n int) []string {
	f, err := os.Open(debugLogPath())
	if err != nil {
		return []string{}
	}
	defer f.Close()
	// The file rotates at 8MB, so a full scan is bounded.
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if lines == nil {
		return []string{}
	}
	return lines
}

// debugf appends a timestamped line to debug.log when config debug is on.
func debugf(cfg Config, format string, args ...any) {
	if !cfg.Debug {
		return
	}
	appendLog(fmt.Sprintf(format, args...))
}

// humanBytes renders a byte count as "1.4 MB".
func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	f := float64(n)
	i := -1
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// fmtDur renders minutes as "2h 15m" / "45m".
func fmtDur(mins int) string {
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	h, m := mins/60, mins%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// dirStats returns total bytes and regular file count under dir.
func dirStats(dir string) (int64, int) {
	var total int64
	var count int
	filepath.Walk(dir, func(_ string, fi os.FileInfo, _ error) error {
		if fi != nil && fi.Mode().IsRegular() {
			total += fi.Size()
			count++
		}
		return nil
	})
	return total, count
}
