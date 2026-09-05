package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Category is a user-definable bucket for activity classification.
// The description is shown to the vision model so it can map screenshots
// to the right bucket.
type Category struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color,omitempty"` // optional hex color for UI
}

type Config struct {
	Provider             string     `json:"provider"` // openrouter, local, custom, mcp
	OpenRouterAPIKey     string     `json:"openrouter_api_key"`
	Model                string     `json:"model"`
	APIBaseURL           string     `json:"api_base_url"` // OpenAI-compatible endpoint; empty = OpenRouter
	CaptureIntervalSec   int        `json:"capture_interval_sec"`
	BlockMinutes         int        `json:"block_minutes"`
	FramesPerBlock       int        `json:"frames_per_block"`
	JPEGQuality          int        `json:"jpeg_quality"`
	KeepFrames           bool       `json:"keep_frames"`
	RetentionDays        int        `json:"retention_days"`
	IgnoreApps           []string   `json:"ignore_apps"`     // hyprctl window classes, case-insensitive
	CaptureCommand       string     `json:"capture_command"` // override; default auto-detect grim
	Output               string     `json:"output"`          // grim -o <output>; empty = all outputs
	SiteName             string     `json:"site_name"`       // OpenRouter X-Title
	MaxStorageMB         int        `json:"max_storage_mb"`  // 0 = unlimited frame storage
	AutoPauseLocked      bool       `json:"auto_pause_locked"`
	FilterInappropriate  bool       `json:"filter_inappropriate"` // redact adult/explicit content
	Debug                bool       `json:"debug"`                // verbose engine log to debug.log
	Categories           []Category `json:"categories"`
	ClassificationPrompt string     `json:"classification_prompt"` // extra instructions for the vision model
}

// normalizeAPIBaseURL trims whitespace and trailing slashes, and appends /v1
// when the URL has no path so that Ollama/LM Studio endpoints work out of the box.
func normalizeAPIBaseURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	u = strings.TrimSuffix(u, "/")
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1"
	}
	return parsed.String()
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
		Provider:             "openrouter",
		Model:                "google/gemma-4-31b-it",
		CaptureIntervalSec:   10,
		BlockMinutes:         15,
		FramesPerBlock:       30,
		JPEGQuality:          55,
		KeepFrames:           false,
		RetentionDays:        7,
		IgnoreApps:           []string{},
		SiteName:             "dayflow-linux",
		MaxStorageMB:         10240,
		AutoPauseLocked:      true,
		FilterInappropriate:  true,
		Categories:           defaultCategories(),
		ClassificationPrompt: defaultClassificationPrompt,
	}
}

const defaultClassificationPrompt = `Browsing and coding can each be work or personal depending on what is visible.
- Work = actively shipping or maintaining projects, job-related tasks, debugging, configuring systems, reading docs to solve a problem, applying for roles, or writing project code.
- Personal = entertainment, social media scrolling, idle chat, adult content, or consumption with no clear goal.
- A personal side project still counts as productive when the user is intentionally building or learning for that project.
- Err on the side of productive when the user is actively creating, debugging, or problem solving.`

func defaultCategories() []Category {
	return []Category{
		{Name: "coding", Description: "Writing, debugging, reviewing, or shipping code, config, scripts, or infrastructure."},
		{Name: "browsing", Description: "General web browsing, reading docs, or searching without a concrete task."},
		{Name: "communication", Description: "Email, chat, calls, video meetings, or messaging."},
		{Name: "writing", Description: "Writing documents, notes, markdown, specs, or long-form text."},
		{Name: "design", Description: "Creating or editing designs, images, video, UI/UX, or 3D assets."},
		{Name: "media", Description: "Watching videos, listening to music, gaming, or other entertainment."},
		{Name: "meetings", Description: "In a meeting, standup, interview, or call."},
		{Name: "system", Description: "OS maintenance, package installs, backups, file management, or sysadmin work."},
		{Name: "idle", Description: "Screen locked, away, or no visible activity."},
		{Name: "personal", Description: "Personal, private, or sensitive activity that is not work-related."},
		{Name: "other", Description: "Anything that does not fit the other buckets."},
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
		cfg.Model = "google/gemma-4-31b-it"
	}
	if cfg.Provider == "" {
		cfg.Provider = "openrouter"
	}
	cfg.APIBaseURL = normalizeAPIBaseURL(cfg.APIBaseURL)
	if cfg.SiteName == "" {
		cfg.SiteName = "dayflow-linux"
	}
	// max_storage_mb: absent config gets the 10GB default; explicit 0 means unlimited.
	if cfg.MaxStorageMB == 0 && !strings.Contains(string(b), "max_storage_mb") {
		cfg.MaxStorageMB = 10240
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

func writeConfig(cfg Config) error {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath(), b, 0o600)
}

// patchConfig merges a JSON patch object into the current config. Values with
// the sentinel "***redacted***" are ignored so the panel can safely round-trip
// the API key field without overwriting it.
func patchConfig(patch string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	var patchMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(patch), &patchMap); err != nil {
		return fmt.Errorf("patch must be a JSON object: %w", err)
	}
	if v, ok := patchMap["openrouter_api_key"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil && s == "***redacted***" {
			delete(patchMap, "openrouter_api_key")
		}
	}
	if v, ok := patchMap["api_base_url"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			patchMap["api_base_url"] = json.RawMessage(`"` + normalizeAPIBaseURL(s) + `"`)
		}
	}
	baseJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(baseJSON, &merged); err != nil {
		return err
	}
	for k, v := range patchMap {
		merged[k] = v
	}
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(mergedJSON, &cfg); err != nil {
		return fmt.Errorf("patched config is invalid: %w", err)
	}
	if cfg.Model == "" {
		return fmt.Errorf("model is required")
	}
	if cfg.Provider == "" {
		cfg.Provider = "openrouter"
	}
	return writeConfig(cfg)
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
// provider, model, api_base_url, capture_interval_sec, block_minutes, frames_per_block,
// jpeg_quality, keep_frames, retention_days, ignore_apps (comma list),
// openrouter_api_key, output, capture_command, max_storage_mb, auto_pause_locked,
// filter_inappropriate, debug.
func setConfigValue(key, value string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	switch key {
	case "provider":
		p := strings.ToLower(strings.TrimSpace(value))
		switch p {
		case "openrouter", "local", "custom", "mcp":
			cfg.Provider = p
			if p == "openrouter" {
				cfg.APIBaseURL = ""
			} else if p == "local" && cfg.APIBaseURL == "" {
				cfg.APIBaseURL = "http://localhost:11434/v1"
			}
		default:
			return fmt.Errorf("provider must be one of: openrouter, local, custom, mcp")
		}
	case "model":
		cfg.Model = value
	case "api_base_url":
		cfg.APIBaseURL = normalizeAPIBaseURL(value)
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
			return fmt.Errorf("keep_frames must be true or false")
		}
		cfg.KeepFrames = b
	case "auto_pause_locked":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("auto_pause_locked must be true or false")
		}
		cfg.AutoPauseLocked = b
	case "filter_inappropriate":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("filter_inappropriate must be true or false")
		}
		cfg.FilterInappropriate = b
	case "debug":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("debug must be true or false")
		}
		cfg.Debug = b
	case "site_name":
		cfg.SiteName = value
	case "classification_prompt":
		cfg.ClassificationPrompt = value
	case "categories":
		if err := json.Unmarshal([]byte(value), &cfg.Categories); err != nil {
			return fmt.Errorf("categories must be a JSON array of {name, description, color?}: %w", err)
		}
	case "ignore_apps":
		if value == "" {
			cfg.IgnoreApps = []string{}
		} else {
			cfg.IgnoreApps = strings.Split(value, ",")
			for i := range cfg.IgnoreApps {
				cfg.IgnoreApps[i] = strings.TrimSpace(cfg.IgnoreApps[i])
			}
		}
	case "max_storage_mb":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("max_storage_mb must be an integer")
		}
		cfg.MaxStorageMB = n
	case "output":
		cfg.Output = value
	case "capture_command":
		cfg.CaptureCommand = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return writeConfig(cfg)
}
