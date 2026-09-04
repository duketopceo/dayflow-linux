package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	OpenRouterAPIKey  string `json:"openrouter_api_key"`
	Model             string `json:"model"`
	CaptureIntervalSec int   `json:"capture_interval_sec"`
	BlockMinutes      int    `json:"block_minutes"`
	FramesPerBlock    int    `json:"frames_per_block"`
	JPEGQuality       int    `json:"jpeg_quality"`
	KeepFrames        bool   `json:"keep_frames"`
	RetentionDays     int    `json:"retention_days"`
	SiteName          string `json:"site_name"` // OpenRouter HTTP-Referer
}

func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(d, "dayflow")
}

func dataDir() string {
	d, err := os.UserHomeDir()
	if err != nil {
		d = os.Getenv("HOME")
	}
	return filepath.Join(d, ".local", "share", "dayflow")
}

func framesDir() string  { return filepath.Join(dataDir(), "frames") }
func dbPath() string     { return filepath.Join(dataDir(), "dayflow.db") }
func pausePath() string  { return filepath.Join(dataDir(), "PAUSED") }
func configPath() string { return filepath.Join(configDir(), "config.json") }

func defaultConfig() Config {
	return Config{
		Model:              "google/gemini-2.5-flash",
		CaptureIntervalSec: 10,
		BlockMinutes:       15,
		FramesPerBlock:     30,
		JPEGQuality:        55,
		KeepFrames:         false,
		RetentionDays:      7,
		SiteName:           "dayflow-linux",
	}
}

func loadConfig() (Config, error) {
	cfg := defaultConfig()
	b, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
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
