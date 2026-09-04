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
func isVisionModel(apiKey, model string) (bool, bool) {
	models, err := fetchModels(apiKey)
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
	fmt.Println("Get an API key at https://openrouter.ai/keys")
	fmt.Print("OpenRouter API key (sk-or-...): ")
	key, _ := r.ReadString('\n')
	key = strings.TrimSpace(key)
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

	// pick a vision model
	type mv struct{ id, price string }
	var vision []mv
	for _, m := range models {
		for _, mod := range m.Architecture.InputModalities {
			if mod == "image" {
				vision = append(vision, mv{m.ID, m.Pricing.Prompt})
				break
			}
		}
	}
	sort.Slice(vision, func(i, j int) bool { return vision[i].id < vision[j].id })
	suggested := "google/gemma-4-31b-it"
	fmt.Println("Recommended vision models:")
	for _, id := range []string{"google/gemma-4-31b-it", "google/gemma-4-26b-a4b-it", "google/gemini-3.5-flash-lite", "google/gemini-3.1-flash-lite"} {
		mark := " "
		for _, v := range vision {
			if v.id == id {
				mark = "✓"
			}
		}
		fmt.Printf("  %s %s\n", mark, id)
	}
	fmt.Printf("Model [%s]: ", suggested)
	choice, _ := r.ReadString('\n')
	choice = strings.TrimSpace(choice)
	if choice == "" {
		choice = suggested
	}
	vis, ok := isVisionModel(key, choice)
	if ok && !vis {
		return fmt.Errorf("model %q cannot read images — pick a vision model (see 'dayflow models')", choice)
	}
	if !ok {
		fmt.Println("warning: could not verify model capabilities; keeping it anyway")
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

func listModels(cfg Config) {
	models, err := fetchModels(cfg.OpenRouterAPIKey)
	if err != nil {
		fmt.Println("could not fetch models:", err)
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
	check("api key set", cfg.OpenRouterAPIKey != "", "run: dayflow setup")
	if cfg.OpenRouterAPIKey != "" {
		vis, reachable := isVisionModel(cfg.OpenRouterAPIKey, cfg.Model)
		if !reachable {
			fmt.Printf("  warn model %s — couldn't verify vision support (offline?)\n", cfg.Model)
		} else {
			check("model is vision-capable", vis, cfg.Model+" cannot read images: dayflow config set model google/gemma-4-31b-it")
		}
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
