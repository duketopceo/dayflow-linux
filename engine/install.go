package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// unitMarker proves a dayflow-*.service/.timer file was created by this
// installer: only marked units (or exact-content matches) may be replaced or
// removed, so install/uninstall never clobber user-owned units that happen to
// share our names.
const unitMarker = "# Managed by dayflow — dayflow install/uninstall may replace or remove this file\n"

const captureService = `[Unit]
Description=dayflow screen capture daemon
After=graphical-session.target
Wants=graphical-session.target

[Service]
ExecStart=%s daemon
Restart=on-failure
RestartSec=5
PassEnvironment=WAYLAND_DISPLAY XDG_CURRENT_DESKTOP XDG_RUNTIME_DIR
Environment="WAYLAND_DISPLAY=wayland-1"

[Install]
WantedBy=default.target
`

const summarizeService = `[Unit]
Description=dayflow block summarizer (OpenRouter)

[Service]
Type=oneshot
ExecStart=%s summarize
`

const summarizeTimer = `[Unit]
Description=dayflow summarizer timer

[Timer]
OnCalendar=*:0/15
Persistent=true

[Install]
WantedBy=timers.target
`

const backupService = `[Unit]
Description=dayflow backup snapshot

[Service]
Type=oneshot
ExecStart=%s backup
`

const backupTimer = `[Unit]
Description=dayflow daily backup timer

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
`

const exportService = `[Unit]
Description=dayflow markdown export refresh

[Service]
Type=oneshot
ExecStart=%s export week --out %s/week.md
ExecStart=%s export today --out %s/today.md
`

const exportTimer = `[Unit]
Description=dayflow daily export refresh timer

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
`

func unitDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(d, "systemd", "user")
}

func selfExe() string {
	p, err := os.Executable()
	if err != nil {
		return "dayflow"
	}
	return p
}

// unitArg quotes a path for systemd ExecStart: quotes keep whitespace from
// splitting arguments, and % is doubled so specifier expansion can't eat it.
func unitArg(p string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(p, "%", "%%"), `"`, `\"`) + `"`
}

// unitSet renders every managed unit for this binary path, marker included.
func unitSet() map[string]string {
	exe := unitArg(selfExe())
	expDir := unitArg(exportsDir())
	units := map[string]string{
		"dayflow-capture.service":   fmt.Sprintf(captureService, exe),
		"dayflow-summarize.service": fmt.Sprintf(summarizeService, exe),
		"dayflow-summarize.timer":   summarizeTimer,
		"dayflow-backup.service":    fmt.Sprintf(backupService, exe),
		"dayflow-backup.timer":      backupTimer,
		"dayflow-export.service":    fmt.Sprintf(exportService, exe, expDir, exe, expDir),
		"dayflow-export.timer":      exportTimer,
	}
	for name, body := range units {
		units[name] = unitMarker + body
	}
	return units
}

// unitOwned reports whether path is a regular file we may replace/remove:
// a symlink or unreadable path is never ours; content must carry the marker
// or match the expected body exactly.
func unitOwned(path, expected string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 64<<10 {
		return false
	}
	return strings.HasPrefix(string(b), unitMarker) || string(b) == expected
}

// writeUnitAtomic publishes body via a same-directory temp + rename. The temp
// name is O_EXCL-created by us, so a planted symlink at the target is never
// followed — it is only replaced wholesale on rename.
func writeUnitAtomic(dir, name, body string) error {
	tmp, err := os.CreateTemp(dir, ".dayflow-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, name))
}

func installUnits() error {
	dir := unitDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	units := unitSet()
	var unowned []string
	for name, body := range units {
		path := filepath.Join(dir, name)
		if _, err := os.Lstat(path); err == nil && !unitOwned(path, body) {
			unowned = append(unowned, path)
		}
	}
	if len(unowned) > 0 {
		return fmt.Errorf("refusing to overwrite units not managed by dayflow (remove them manually to install): %s", strings.Join(unowned, ", "))
	}
	for name, body := range units {
		if err := writeUnitAtomic(dir, name, body); err != nil {
			return err
		}
		fmt.Println("wrote", filepath.Join(dir, name))
	}
	if _, err := os.Stat(configPath()); os.IsNotExist(err) {
		if err := writeDefaultConfig(); err != nil {
			return err
		}
		fmt.Println("wrote default config:", configPath())
	}
	run := func(args ...string) {
		c := exec.Command("systemctl", args...)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		c.Run()
	}
	run("--user", "daemon-reload")
	run("--user", "enable", "--now", "dayflow-summarize.timer")
	run("--user", "enable", "--now", "dayflow-backup.timer")
	run("--user", "enable", "--now", "dayflow-export.timer")
	fmt.Println("\nEnabled dayflow-summarize.timer, dayflow-backup.timer (daily snapshot, keeps last 7), dayflow-export.timer (daily markdown export).")
	fmt.Println("Start capture with:  systemctl --user enable --now dayflow-capture.service")
	fmt.Println("Already running an older build?  systemctl --user restart dayflow-capture.service")
	fmt.Println("Set your API key in:", configPath())
	return nil
}

func uninstallUnits() error {
	run := func(args ...string) {
		c := exec.Command("systemctl", args...)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		c.Run()
	}
	dir := unitDir()
	var owned []string
	for name, body := range unitSet() {
		path := filepath.Join(dir, name)
		if unitOwned(path, body) {
			owned = append(owned, name)
		} else if _, err := os.Lstat(path); err == nil {
			fmt.Println("skipping unmanaged unit:", path)
		}
	}
	if len(owned) > 0 {
		run(append([]string{"--user", "disable", "--now"}, owned...)...)
		for _, name := range owned {
			os.Remove(filepath.Join(dir, name))
		}
	}
	run("--user", "daemon-reload")
	fmt.Println("units removed")
	return nil
}
