package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ModelPreset is a recommended vision model for Dayflow.
type ModelPreset struct {
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Vision bool   `json:"vision"`
	Notes  string `json:"notes"`
}

func modelPresets() []ModelPreset {
	return []ModelPreset{
		{
			Name:   "Gemma 4 31B — default",
			Slug:   "google/gemma-4-31b-it",
			Vision: true,
			Notes:  "Best overall balance on OpenRouter; fast, cheap, and accurate.",
		},
		{
			Name:   "Gemma 3 27B — bigger still small",
			Slug:   "google/gemma-3-27b-it",
			Vision: true,
			Notes:  "More capable than the 12B without being huge. Good for detailed summaries.",
		},
		{
			Name:   "Gemma 3 12B — cheap minimal",
			Slug:   "google/gemma-3-12b-it",
			Vision: true,
			Notes:  "Small, fast, cheapest. Good for everyday work tracking.",
		},
		{
			Name:   "Qwen2.5-VL 7B — local minimal",
			Slug:   "qwen2.5vl:7b",
			Vision: true,
			Notes:  "Local option for Ollama/LM Studio. Pull with: ollama pull qwen2.5vl:7b",
		},
		{
			Name:   "Gemma 3 4B — local tiny",
			Slug:   "gemma3:4b",
			Vision: true,
			Notes:  "Minimal local vision option. Lower accuracy but runs on modest hardware.",
		},
	}
}

func knownVisionModel(slug string) bool {
	for _, p := range modelPresets() {
		if p.Slug == slug {
			return p.Vision
		}
	}
	return false
}

// fetchModels lists models from OpenRouter. Needs a key; returns nil on error.
func fetchModels(apiKey string) ([]struct {
	ID           string `json:"id"`
	Architecture struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt string `json:"prompt"`
	} `json:"pricing"`
}, error) {
	req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID           string `json:"id"`
			Architecture struct {
				InputModalities []string `json:"input_modalities"`
			} `json:"architecture"`
			Pricing struct {
				Prompt string `json:"prompt"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// isVisionModel reports whether the model accepts image input on OpenRouter.
// Returns (vision, reachable): reachable=false when the API couldn't be asked.
// When a custom api_base_url is set, we cannot query OpenRouter's catalog, so
// we trust the user's choice and return (true, false).
func isVisionModel(cfg Config, model string) (bool, bool) {
	if cfg.APIBaseURL != "" {
		return true, false
	}
	if knownVisionModel(model) {
		return true, true
	}
	if cfg.OpenRouterAPIKey == "" {
		return false, false
	}
	models, err := fetchModels(cfg.OpenRouterAPIKey)
	if err != nil {
		return false, false
	}
	for _, m := range models {
		if m.ID == model || strings.TrimPrefix(m.ID, "~") == model {
			for _, mod := range m.Architecture.InputModalities {
				if mod == "image" {
					return true, true
				}
			}
			return false, true // model found but no image modality
		}
	}
	return false, true // not found — treat as non-vision
}

func runSetup() error {
	r := bufio.NewReader(os.Stdin)

	fmt.Println("dayflow setup")
	fmt.Println("=============")
	fmt.Println("Get an API key at https://openrouter.ai/keys (or use a local endpoint like http://localhost:11434/v1 for Ollama)")
	fmt.Print("API endpoint [https://openrouter.ai/api/v1]: ")
	baseURL, _ := r.ReadString('\n')
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" || baseURL == "https://openrouter.ai/api/v1" {
		baseURL = ""
	}

	fmt.Print("API key (sk-or-... or blank for local endpoint): ")
	key, _ := r.ReadString('\n')
	key = strings.TrimSpace(key)

	if baseURL == "" {
		if key == "" {
			return fmt.Errorf("no key entered")
		}
		if !strings.HasPrefix(key, "sk-or-") {
			fmt.Println("warning: key doesn't look like an OpenRouter key (expected sk-or-...)")
		}

		fmt.Println("validating key...")
		models, err := fetchModels(key)
		if err != nil {
			return fmt.Errorf("could not reach OpenRouter: %w", err)
		}
		if len(models) == 0 {
			return fmt.Errorf("key rejected or no models returned")
		}
		fmt.Printf("key OK (%d models available)\n\n", len(models))
	} else {
		fmt.Println("using custom endpoint; skipping OpenRouter validation")
	}

	// pick a vision model
	var vision []string
	if baseURL == "" {
		models, _ := fetchModels(key)
		for _, m := range models {
			for _, mod := range m.Architecture.InputModalities {
				if mod == "image" {
					vision = append(vision, m.ID)
					break
				}
			}
		}
		sort.Strings(vision)
	}
	suggested := "google/gemma-4-31b-it"
	if baseURL != "" {
		suggested = "gemma3:4b"
	}
	fmt.Println("Recommended vision models:")
	for _, p := range modelPresets() {
		if baseURL != "" && !strings.Contains(p.Slug, ":") {
			continue // skip OpenRouter-only slugs for local endpoints
		}
		mark := " "
		for _, v := range vision {
			if v == p.Slug {
				mark = "✓"
			}
		}
		fmt.Printf("  %s %-30s  %s\n", mark, p.Slug, p.Notes)
	}
	fmt.Printf("Model [%s]: ", suggested)
	choice, _ := r.ReadString('\n')
	choice = strings.TrimSpace(choice)
	if choice == "" {
		choice = suggested
	}

	if err := setConfigValue("api_base_url", baseURL); err != nil {
		return err
	}
	if err := setConfigValue("openrouter_api_key", key); err != nil {
		return err
	}
	if err := setConfigValue("model", choice); err != nil {
		return err
	}
	fmt.Printf("\nWrote %s (model=%s)\n", configPath(), choice)
	fmt.Println("Next: dayflow install && systemctl --user enable --now dayflow-capture.service")
	return nil
}

func printModelPresets() {
	b, _ := json.MarshalIndent(modelPresets(), "", "  ")
	fmt.Println(string(b))
}

func listModels(cfg Config) {
	printModelPresets()
	if cfg.APIBaseURL != "" {
		fmt.Println("\ncustom api_base_url set; list local models with your endpoint's /models route")
		return
	}
	models, err := fetchModels(cfg.OpenRouterAPIKey)
	if err != nil {
		fmt.Println("\ncould not fetch models:", err)
		return
	}
	fmt.Println("vision-capable models (image input):")
	for _, m := range models {
		for _, mod := range m.Architecture.InputModalities {
			if mod == "image" {
				fmt.Printf("  %s  in:%s\n", m.ID, m.Pricing.Prompt)
				break
			}
		}
	}
}

// doctorCheck is one doctor result; status is "ok", "warn", or "fail".
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// collectDoctorChecks runs every check and returns results plus the failure
// count. Rendering (text or JSON) happens in runDoctor.
func collectDoctorChecks(cfg Config, deep bool) ([]doctorCheck, int) {
	var checks []doctorCheck
	fail := 0
	check := func(name string, ok bool, hint string) {
		if ok {
			checks = append(checks, doctorCheck{Name: name, Status: "ok"})
		} else {
			fail++
			checks = append(checks, doctorCheck{Name: name, Status: "fail", Detail: hint})
		}
	}
	warn := func(name, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: "warn", Detail: detail})
	}

	check("wayland session", os.Getenv("WAYLAND_DISPLAY") != "", "not running under Wayland")
	_, grimErr := exec.LookPath("grim")
	check("grim installed", grimErr == nil || cfg.CaptureCommand != "", "install grim or set capture_command")
	check("config file", fileExists(configPath()), "run: dayflow setup")
	check("api reachable", cfg.OpenRouterAPIKey != "" || cfg.APIBaseURL != "", "run: dayflow setup")
	vis, reachable := isVisionModel(cfg, cfg.Model)
	if !reachable && cfg.APIBaseURL == "" {
		warn("model vision support", cfg.Model+" — couldn't verify vision support (offline?)")
	} else if cfg.APIBaseURL != "" {
		checks = append(checks, doctorCheck{Name: "custom endpoint", Status: "ok",
			Detail: cfg.APIBaseURL + " — vision support not verified"})
	} else {
		check("model is vision-capable", vis, cfg.Model+" cannot read images: dayflow config set model google/gemma-4-31b-it")
	}
	_, hyErr := exec.LookPath("hyprctl")
	if hyErr != nil && len(cfg.IgnoreApps) > 0 {
		warn("hyprctl", "hyprctl not found — ignore_apps won't work on this compositor")
	}

	if _, err := os.Stat(dbPath()); os.IsNotExist(err) {
		warn("database", "no database yet — capture has not run")
	} else {
		sv, svErr := peekSchemaVersion()
		check("schema version", svErr == nil && sv <= schemaVersion,
			fmt.Sprintf("database at schema v%d, binary expects v%d — upgrade the engine", sv, schemaVersion))
		if svErr == nil && sv < schemaVersion {
			warn("schema migration", fmt.Sprintf("database was at schema v%d; migrated to v%d — restart dayflow-capture and any dayflow mcp clients", sv, schemaVersion))
		}
		db, err := openDB()
		if err != nil {
			fail++
			checks = append(checks, doctorCheck{Name: "database open/migrate", Status: "fail", Detail: err.Error()})
		} else {
			defer db.Close()
			pragma := "quick_check"
			if deep {
				pragma = "integrity_check"
			}
			var qc string
			if err := db.QueryRow(`PRAGMA ` + pragma).Scan(&qc); err != nil {
				fail++
				checks = append(checks, doctorCheck{Name: "sqlite integrity", Status: "fail", Detail: err.Error()})
			} else {
				check("sqlite integrity", qc == "ok", pragma+": "+qc)
			}
			var fk int
			db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
			check("foreign keys", fk == 1, "foreign_keys pragma is off")
			if orphans, err := orphanFrameFiles(db); err == nil && len(orphans) > 0 {
				warn("orphan frames", fmt.Sprintf("%d file(s) under frames/ have no frames row — run: dayflow reconcile --dry-run", len(orphans)))
			}
		}
	}
	if fi, err := os.Stat(dataDir()); err == nil {
		check("data dir permissions", fi.Mode().Perm()&0o077 == 0,
			fmt.Sprintf("%s is %04o, want 0700", dataDir(), fi.Mode().Perm()))
	}
	if fi, err := os.Stat(configPath()); err == nil {
		check("config permissions", fi.Mode().Perm()&0o077 == 0,
			fmt.Sprintf("%s is %04o, want 0600", configPath(), fi.Mode().Perm()))
	}
	return checks, fail
}

func runDoctor(cfg Config, jsonOut bool, deep bool) {
	checks, fail := collectDoctorChecks(cfg, deep)
	if jsonOut {
		out := map[string]interface{}{
			"checks":         checks,
			"failures":       fail,
			"version":        version,
			"schema_version": schemaVersion,
			"configured":     fileExists(configPath()) && (cfg.OpenRouterAPIKey != "" || cfg.APIBaseURL != ""),
			"data_dir":       dataDir(),
			"config_path":    configPath(),
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		for _, c := range checks {
			switch c.Status {
			case "ok":
				if c.Detail != "" {
					fmt.Printf("  ok   %s — %s\n", c.Name, c.Detail)
				} else {
					fmt.Printf("  ok   %s\n", c.Name)
				}
			case "warn":
				fmt.Printf("  warn %s — %s\n", c.Name, c.Detail)
			default:
				fmt.Printf("  FAIL %s — %s\n", c.Name, c.Detail)
			}
		}
		fmt.Printf("  engine version: %s (schema v%d)\n", version, schemaVersion)
		fmt.Printf("  data: %s\n  config: %s\n", dataDir(), configPath())
	}
	if fail > 0 {
		os.Exit(1)
	}
}

// probeEndpoint reports whether a local inference endpoint answers /models.
func probeEndpoint(base string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(strings.TrimSuffix(base, "/") + "/models")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func runDetect(jsonOut bool) {
	type detected struct {
		Ollama   bool          `json:"ollama"`
		LMStudio bool          `json:"lmstudio"`
		Presets  []ModelPreset `json:"presets"`
	}
	d := detected{
		Ollama:   probeEndpoint("http://localhost:11434/v1"),
		LMStudio: probeEndpoint("http://localhost:1234/v1"),
		Presets:  modelPresets(),
	}
	if jsonOut {
		b, _ := json.MarshalIndent(d, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Printf("ollama:   %v\nlmstudio: %v\n", d.Ollama, d.LMStudio)
	if d.Ollama || d.LMStudio {
		fmt.Println("local endpoint found — a local model works without an API key")
	}
	printModelPresets()
}

// peekSchemaVersion reads the recorded schema version without running
// migrations. Returns 0 for a database that predates schema_migrations.
func peekSchemaVersion() (int, error) {
	db, err := sql.Open("sqlite", dbPath()+"?_pragma=query_only(1)")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, nil // unversioned database
	}
	return v, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
