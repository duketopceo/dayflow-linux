package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	OpenRouterAPIKey   string   `json:"openrouter_api_key"`
	Model              string   `json:"model"`
	CaptureIntervalSec int      `json:"capture_interval_sec"`
	BlockMinutes       int      `json:"block_minutes"`
	FramesPerBlock     int      `json:"frames_per_block"`
	JPEGQuality        int      `json:"jpeg_quality"`
	KeepFrames         bool     `json:"keep_frames"`
	RetentionDays      int      `json:"retention_days"`
	IgnoreApps         []string `json:"ignore_apps"`     // hyprctl window classes, case-insensitive
	CaptureCommand     string   `json:"capture_command"` // override; default auto-detect grim
	Output             string   `json:"output"`          // grim -o <output>; empty = all outputs
	SiteName           string   `json:"site_name"`       // OpenRouter X-Title
}

func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(d, "dayflow")
}

func dataDir() string {
	if d := os.Getenv("DAYFLOW_DATA_DIR"); d != "" {
		return d
	}
	d, err := os.UserHomeDir()
	if err != nil {
		d = os.Getenv("HOME")
	}
	return filepath.Join(d, ".local", "share", "dayflow")
}

func framesDir() string { return filepath.Join(dataDir(), "frames") }
func dbPath() string    { return filepath.Join(dataDir(), "dayflow.db") }
func pausePath() string { return filepath.Join(dataDir(), "PAUSED") }
func configPath() string {
	if p := os.Getenv("DAYFLOW_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(configDir(), "config.json")
}

func defaultConfig() Config {
	return Config{
		Model:              "google/gemini-2.5-flash",
		CaptureIntervalSec: 10,
		BlockMinutes:       15,
		FramesPerBlock:     30,
		JPEGQuality:        55,
		KeepFrames:         false,
		RetentionDays:      7,
		IgnoreApps:         []string{},
		SiteName:           "dayflow-linux",
	}
}

func loadConfig() (Config, error) {
	cfg := defaultConfig()
	b, err := os.ReadFile(configPath())
	if err != nil {
		if !os.IsNotExist(err) {
			return cfg, err
		}
	} else if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if k := os.Getenv("OPENROUTER_API_KEY"); k != "" {
		cfg.OpenRouterAPIKey = k
	}
	if cfg.OpenRouterAPIKey == "" {
		cfg.OpenRouterAPIKey = openRouterKeysFallback()
	}
	if cfg.CaptureIntervalSec <= 0 {
		cfg.CaptureIntervalSec = 10
	}
	if cfg.BlockMinutes <= 0 {
		cfg.BlockMinutes = 15
	}
	if cfg.FramesPerBlock <= 0 {
		cfg.FramesPerBlock = 30
	}
	if cfg.JPEGQuality <= 0 || cfg.JPEGQuality > 100 {
		cfg.JPEGQuality = 55
	}
	if cfg.Model == "" {
		cfg.Model = "google/gemini-2.5-flash"
	}
	return cfg, nil
}

// openRouterKeysFallback reads ~/.config/openrouter/keys.json if present.
func openRouterKeysFallback() string {
	b, err := os.ReadFile(filepath.Join(configDir(), "..", "openrouter", "keys.json"))
	if err != nil {
		return ""
	}
	var k struct {
		APIKey string `json:"api_key"`
	}
	if json.Unmarshal(b, &k) != nil {
		return ""
	}
	return k.APIKey
}

func writeDefaultConfig() error {
	cfg := defaultConfig()
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configPath(), b, 0o600)
}

// setConfigValue updates one key in config.json. Supported keys:
// model, capture_interval_sec, block_minutes, frames_per_block,
// jpeg_quality, keep_frames, retention_days, ignore_apps (comma list),
// openrouter_api_key, output, capture_command.
func setConfigValue(key, value string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	switch key {
	case "model":
		cfg.Model = value
	case "openrouter_api_key":
		cfg.OpenRouterAPIKey = value
	case "capture_interval_sec", "block_minutes", "frames_per_block", "jpeg_quality", "retention_days":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		switch key {
		case "capture_interval_sec":
			cfg.CaptureIntervalSec = n
		case "block_minutes":
			cfg.BlockMinutes = n
		case "frames_per_block":
			cfg.FramesPerBlock = n
		case "jpeg_quality":
			cfg.JPEGQuality = n
		case "retention_days":
			cfg.RetentionDays = n
		}
	case "keep_frames":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("keep_frames must be true/false")
		}
		cfg.KeepFrames = b
	case "ignore_apps":
		if value == "" {
			cfg.IgnoreApps = []string{}
		} else {
			cfg.IgnoreApps = strings.Split(value, ",")
			for i := range cfg.IgnoreApps {
				cfg.IgnoreApps[i] = strings.TrimSpace(cfg.IgnoreApps[i])
			}
		}
	case "output":
		cfg.Output = value
	case "capture_command":
		cfg.CaptureCommand = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath(), b, 0o600)
}
