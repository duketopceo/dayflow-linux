package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// PromptOverrides lets a provider replace the default prompt templates.
// Empty fields fall back to the built-in templates.
type PromptOverrides struct {
	TitlePrompt    string `json:"title_prompt,omitempty"`
	SummaryPrompt  string `json:"summary_prompt,omitempty"`
	DetailedPrompt string `json:"detailed_prompt,omitempty"`
	ChatPrompt     string `json:"chat_prompt,omitempty"`
}

// Provider is one LLM endpoint dayflow can route tasks to.
// Kind is one of: openrouter, local, custom, gemini, chatgpt, claude, mcp.
type Provider struct {
	ID              string          `json:"id"`
	Name            string          `json:"name,omitempty"`
	Kind            string          `json:"kind"`
	APIBaseURL      string          `json:"api_base_url,omitempty"` // empty = OpenRouter
	APIKey          string          `json:"api_key,omitempty"`
	Model           string          `json:"model,omitempty"`
	Vision          bool            `json:"vision,omitempty"` // can read images
	Chat            bool            `json:"chat,omitempty"`   // can do text/chat tasks
	Enabled         bool            `json:"enabled"`
	PromptOverrides PromptOverrides `json:"prompt_overrides,omitempty"`
}

// Routing decides which provider serves which task.
// TaskProvider keys: vision, summary, detailed, chat, review, standup.
type Routing struct {
	Primary      string            `json:"primary,omitempty"`
	Secondary    string            `json:"secondary,omitempty"`
	TaskProvider map[string]string `json:"task_provider,omitempty"`
}

var providerKinds = []string{"openrouter", "local", "custom", "gemini", "chatgpt", "claude", "mcp"}

func validProviderKind(k string) bool {
	for _, v := range providerKinds {
		if k == v {
			return true
		}
	}
	return false
}

// migrateLegacyProviders ensures cfg has a usable Providers list and Routing by
// folding the old single-provider keys into a "default" provider when needed.
func migrateLegacyProviders(cfg *Config) {
	for i := range cfg.Providers {
		cfg.Providers[i].APIBaseURL = normalizeAPIBaseURL(cfg.Providers[i].APIBaseURL)
	}
	if len(cfg.Providers) == 0 {
		cfg.Providers = []Provider{{
			ID:         "default",
			Name:       "Default",
			Kind:       cfg.Provider,
			APIBaseURL: cfg.APIBaseURL,
			APIKey:     cfg.OpenRouterAPIKey,
			Model:      cfg.Model,
			Vision:     true,
			Chat:       true,
			Enabled:    true,
		}}
	}
	if cfg.Routing.Primary == "" {
		cfg.Routing.Primary = cfg.Providers[0].ID
	}
}

// effectiveProviders returns the configured providers, or a migrated default
// provider built from the legacy single-provider fields. Used so configs built
// in code (tests, daemon) route the same way loadConfig results do.
func effectiveProviders(cfg Config) []Provider {
	if len(cfg.Providers) > 0 {
		return cfg.Providers
	}
	c := cfg
	migrateLegacyProviders(&c)
	return c.Providers
}

// providerForTask resolves the provider for a task: TaskProvider override,
// then Primary, then Secondary, then the first enabled provider.
func providerForTask(cfg Config, task string) (Provider, error) {
	provs := effectiveProviders(cfg)
	find := func(id string) *Provider {
		for i := range provs {
			if provs[i].ID == id && provs[i].Enabled {
				return &provs[i]
			}
		}
		return nil
	}
	if id := cfg.Routing.TaskProvider[task]; id != "" {
		if p := find(id); p != nil {
			return *p, nil
		}
	}
	if p := find(cfg.Routing.Primary); p != nil {
		return *p, nil
	}
	if p := find(cfg.Routing.Secondary); p != nil {
		return *p, nil
	}
	for _, p := range provs {
		if p.Enabled {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("no enabled provider configured for task %q", task)
}

// providerNeedsAuth reports whether the provider should receive an
// Authorization header. Local and MCP endpoints never get keys.
func providerNeedsAuth(p Provider) bool {
	return p.Kind != "local" && p.Kind != "mcp"
}

func providerChatURL(p Provider) string {
	if p.APIBaseURL != "" {
		return strings.TrimSuffix(p.APIBaseURL, "/") + "/chat/completions"
	}
	return openRouterURL
}

func providerUsesOpenRouterHeaders(p Provider) bool {
	return p.APIBaseURL == "" || strings.Contains(p.APIBaseURL, "openrouter.ai")
}

// callProviderChat posts an OpenAI-compatible chat request to provider p.
func callProviderChat(cfg Config, p Provider, messages []orMessage) (string, int, int, error) {
	if providerNeedsAuth(p) && p.APIKey == "" && p.APIBaseURL == "" {
		return "", 0, 0, fmt.Errorf("no API key for provider %q: set its api_key or openrouter_api_key in %s or OPENROUTER_API_KEY", p.ID, configPath())
	}
	reqBody, _ := json.Marshal(orRequest{Model: p.Model, Messages: messages})
	req, err := http.NewRequest("POST", providerChatURL(p), bytes.NewReader(reqBody))
	if err != nil {
		return "", 0, 0, err
	}
	if providerNeedsAuth(p) && p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	if providerUsesOpenRouterHeaders(p) {
		req.Header.Set("HTTP-Referer", "https://github.com/duketopceo/dayflow-linux")
		req.Header.Set("X-Title", cfg.SiteName)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return "", 0, 0, fmt.Errorf("api %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var or orResponse
	if err := json.Unmarshal(body, &or); err != nil {
		return "", 0, 0, fmt.Errorf("api response was not valid JSON: %w", err)
	}
	if or.Error != nil {
		return "", 0, 0, fmt.Errorf("api error: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return "", 0, 0, fmt.Errorf("api returned no choices")
	}
	pt, ct := 0, 0
	if or.Usage != nil {
		pt, ct = or.Usage.PromptTokens, or.Usage.CompletionTokens
	}
	return stripFences(strings.TrimSpace(or.Choices[0].Message.Content)), pt, ct, nil
}

// callProvider resolves the routed provider for task and sends messages.
func callProvider(cfg Config, task string, messages []orMessage) (string, int, int, error) {
	p, err := providerForTask(cfg, task)
	if err != nil {
		return "", 0, 0, err
	}
	return callProviderChat(cfg, p, messages)
}

// callProviderText is a system+user convenience wrapper around callProvider.
func callProviderText(cfg Config, task, system, user string) (string, int, int, error) {
	return callProvider(cfg, task, []orMessage{
		{Role: "system", Content: []orContent{{Type: "text", Text: system}}},
		{Role: "user", Content: []orContent{{Type: "text", Text: user}}},
	})
}

// findProvider locates a provider by ID regardless of enabled state.
func findProvider(cfg Config, id string) *Provider {
	provs := effectiveProviders(cfg)
	for i := range provs {
		if provs[i].ID == id {
			return &provs[i]
		}
	}
	return nil
}

var providerTasks = []string{"vision", "summary", "detailed", "chat", "review", "standup"}

func isProviderTask(s string) bool {
	for _, t := range providerTasks {
		if s == t {
			return true
		}
	}
	return false
}

// runProvider implements `dayflow provider list|add|set|remove|test`.
func runProvider(cfg Config, args []string, jsonOut bool) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		if jsonOut {
			masked := effectiveProviders(cfg)
			for i := range masked {
				if masked[i].APIKey != "" {
					masked[i].APIKey = "***redacted***"
				}
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{
				"providers": masked,
				"routing":   cfg.Routing,
			})
		}
		fmt.Println("providers:")
		for _, p := range effectiveProviders(cfg) {
			state := "disabled"
			if p.Enabled {
				state = "enabled"
			}
			key := ""
			if p.APIKey != "" {
				key = "  key=***"
			}
			base := p.APIBaseURL
			if base == "" {
				base = "openrouter"
			}
			fmt.Printf("  %-12s %-10s %-8s model=%s base=%s%s\n", p.ID, p.Kind, state, p.Model, base, key)
		}
		fmt.Printf("routing: primary=%s secondary=%s\n", cfg.Routing.Primary, cfg.Routing.Secondary)
		if len(cfg.Routing.TaskProvider) > 0 {
			tasks := make([]string, 0, len(cfg.Routing.TaskProvider))
			for t := range cfg.Routing.TaskProvider {
				tasks = append(tasks, t)
			}
			sort.Strings(tasks)
			for _, t := range tasks {
				fmt.Printf("  task %-8s -> %s\n", t, cfg.Routing.TaskProvider[t])
			}
		}
		return nil

	case "add":
		if len(args) < 3 {
			return fmt.Errorf("usage: dayflow provider add <id> <kind>")
		}
		id, kind := args[1], strings.ToLower(args[2])
		if !validProviderKind(kind) {
			return fmt.Errorf("kind must be one of: %s", strings.Join(providerKinds, ", "))
		}
		if findProvider(cfg, id) != nil {
			return fmt.Errorf("provider %q already exists", id)
		}
		p := Provider{ID: id, Name: id, Kind: kind, Model: cfg.Model, Enabled: true}
		if kind == "local" {
			p.APIBaseURL = "http://localhost:11434/v1"
		}
		cfg.Providers = append(effectiveProviders(cfg), p)
		if cfg.Routing.Primary == "" {
			cfg.Routing.Primary = id
		}
		if err := writeConfig(cfg); err != nil {
			return err
		}
		fmt.Println("added provider", id)
		return nil

	case "set":
		if len(args) < 4 {
			return fmt.Errorf("usage: dayflow provider set <id> <key> <value>")
		}
		cfg.Providers = effectiveProviders(cfg)
		idx := -1
		for i := range cfg.Providers {
			if cfg.Providers[i].ID == args[1] {
				idx = i
			}
		}
		if idx < 0 {
			return fmt.Errorf("no provider %q", args[1])
		}
		p := &cfg.Providers[idx]
		switch args[2] {
		case "name":
			p.Name = args[3]
		case "kind":
			k := strings.ToLower(args[3])
			if !validProviderKind(k) {
				return fmt.Errorf("kind must be one of: %s", strings.Join(providerKinds, ", "))
			}
			p.Kind = k
		case "api_base_url":
			p.APIBaseURL = normalizeAPIBaseURL(args[3])
		case "api_key":
			p.APIKey = args[3]
		case "model":
			p.Model = args[3]
		case "enabled":
			p.Enabled = args[3] == "true" || args[3] == "1" || args[3] == "yes"
		case "vision":
			p.Vision = args[3] == "true" || args[3] == "1" || args[3] == "yes"
		case "chat":
			p.Chat = args[3] == "true" || args[3] == "1" || args[3] == "yes"
		case "title_prompt":
			p.PromptOverrides.TitlePrompt = args[3]
		case "summary_prompt":
			p.PromptOverrides.SummaryPrompt = args[3]
		case "detailed_prompt":
			p.PromptOverrides.DetailedPrompt = args[3]
		case "chat_prompt":
			p.PromptOverrides.ChatPrompt = args[3]
		default:
			return fmt.Errorf("unknown provider key %q (name, kind, api_base_url, api_key, model, enabled, vision, chat, *_prompt)", args[2])
		}
		if err := writeConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("set %s.%s\n", p.ID, args[2])
		return nil

	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: dayflow provider remove <id>")
		}
		cfg.Providers = effectiveProviders(cfg)
		kept := cfg.Providers[:0]
		found := false
		for _, p := range cfg.Providers {
			if p.ID == args[1] {
				found = true
				continue
			}
			kept = append(kept, p)
		}
		if !found {
			return fmt.Errorf("no provider %q", args[1])
		}
		cfg.Providers = kept
		if cfg.Routing.Primary == args[1] {
			cfg.Routing.Primary = ""
		}
		if cfg.Routing.Secondary == args[1] {
			cfg.Routing.Secondary = ""
		}
		for t, id := range cfg.Routing.TaskProvider {
			if id == args[1] {
				delete(cfg.Routing.TaskProvider, t)
			}
		}
		if err := writeConfig(cfg); err != nil {
			return err
		}
		fmt.Println("removed provider", args[1])
		return nil

	case "test":
		if len(args) < 2 {
			return fmt.Errorf("usage: dayflow provider test <id|task>")
		}
		var p Provider
		if isProviderTask(args[1]) {
			var err error
			p, err = providerForTask(cfg, args[1])
			if err != nil {
				return err
			}
			fmt.Printf("task %s routes to provider %s (%s)\n", args[1], p.ID, p.Kind)
		} else {
			fp := findProvider(cfg, args[1])
			if fp == nil {
				return fmt.Errorf("no provider %q", args[1])
			}
			p = *fp
		}
		text, pt, ct, err := callProviderChat(cfg, p, []orMessage{
			{Role: "user", Content: []orContent{{Type: "text", Text: "Reply with exactly: ok"}}},
		})
		if err != nil {
			return fmt.Errorf("provider %s test failed: %w", p.ID, err)
		}
		fmt.Printf("provider %s OK: %q (tokens %d+%d)\n", p.ID, truncate(text, 80), pt, ct)
		return nil
	}
	return fmt.Errorf("unknown provider subcommand %q (list, add, set, remove, test)", sub)
}
