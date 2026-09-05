package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func debugLogPath() string { return filepath.Join(dataDir(), "debug.log") }

var debugMu sync.Mutex

// debugf appends a timestamped line to debug.log when config debug is on.
// Best-effort: logging failures are ignored so they never break capture.
func debugf(cfg Config, format string, args ...any) {
	if !cfg.Debug {
		return
	}
	debugMu.Lock()
	defer debugMu.Unlock()
	f, err := os.OpenFile(debugLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n",
		time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
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
