package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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

func installUnits() error {
	dir := unitDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	exe := selfExe()
	units := map[string]string{
		"dayflow-capture.service":   fmt.Sprintf(captureService, exe),
		"dayflow-summarize.service": fmt.Sprintf(summarizeService, exe),
		"dayflow-summarize.timer":   summarizeTimer,
		"dayflow-backup.service":    fmt.Sprintf(backupService, exe),
		"dayflow-backup.timer":      backupTimer,
	}
	for name, body := range units {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
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
	fmt.Println("\nEnabled dayflow-summarize.timer and dayflow-backup.timer (daily snapshot, keeps last 7).")
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
	run("--user", "disable", "--now", "dayflow-summarize.timer", "dayflow-capture.service", "dayflow-backup.timer")
	for _, name := range []string{"dayflow-capture.service", "dayflow-summarize.service", "dayflow-summarize.timer", "dayflow-backup.service", "dayflow-backup.timer"} {
		os.Remove(filepath.Join(unitDir(), name))
	}
	run("--user", "daemon-reload")
	fmt.Println("units removed")
	return nil
}
