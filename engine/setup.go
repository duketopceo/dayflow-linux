package main

import (
	"bufio"
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

func runDoctor(cfg Config) {
	fail := 0
	check := func(name string, ok bool, hint string) {
		if ok {
			fmt.Printf("  ok   %s\n", name)
		} else {
			fail++
			fmt.Printf("  FAIL %s — %s\n", name, hint)
		}
	}
	check("wayland session", os.Getenv("WAYLAND_DISPLAY") != "", "not running under Wayland")
	_, grimErr := exec.LookPath("grim")
	check("grim installed", grimErr == nil || cfg.CaptureCommand != "", "install grim or set capture_command")
	check("config file", fileExists(configPath()), "run: dayflow setup")
	check("api reachable", cfg.OpenRouterAPIKey != "" || cfg.APIBaseURL != "", "run: dayflow setup")
	vis, reachable := isVisionModel(cfg, cfg.Model)
	if !reachable && cfg.APIBaseURL == "" {
		fmt.Printf("  warn model %s — couldn't verify vision support (offline?)\n", cfg.Model)
	} else if cfg.APIBaseURL != "" {
		fmt.Printf("  ok   using custom endpoint %s — vision support not verified\n", cfg.APIBaseURL)
	} else {
		check("model is vision-capable", vis, cfg.Model+" cannot read images: dayflow config set model google/gemma-4-31b-it")
	}
	_, hyErr := exec.LookPath("hyprctl")
	if hyErr != nil && len(cfg.IgnoreApps) > 0 {
		fmt.Println("  warn hyprctl not found — ignore_apps won't work on this compositor")
	}
	fmt.Printf("  data: %s\n  config: %s\n", dataDir(), configPath())
	if fail > 0 {
		os.Exit(1)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
