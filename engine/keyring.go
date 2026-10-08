package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// keyringService namespaces dayflow secrets inside OmaSeal.
const keyringService = "dayflow"

// keyringAvailable reports whether the omaseal CLI is on PATH.
func keyringAvailable() bool {
	_, err := exec.LookPath("omaseal")
	return err == nil
}

// keyringGet resolves a secret from OmaSeal (service "dayflow"). Returns ""
// on any failure — missing keyring, locked agent mode, or not_found — so
// callers can fall through to file/env sources.
func keyringGet(account string) (string, error) {
	return keyringGetService(keyringService, account)
}

// keyringGetService resolves a secret from an arbitrary OmaSeal service —
// knowledge sync reads the Kurultai profile's omaseal:// refs, which live
// under the "kurultai" service, not "dayflow".
func keyringGetService(service, account string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "omaseal", "get", service, account).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// keyringSet stores a secret via `omaseal set` (value arrives on stdin).
func keyringSet(account, secret string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "omaseal", "set", keyringService, account)
	c.Stdin = strings.NewReader(secret + "\n")
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("omaseal set: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// keyringDel removes a secret. not_found is treated as success.
func keyringDel(account string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "omaseal", "del", keyringService, account).CombinedOutput(); err != nil {
		if strings.Contains(string(out), "not_found") || strings.Contains(string(out), "not found") {
			return nil
		}
		return fmt.Errorf("omaseal del: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// resolveProviderKey returns the effective API key for a provider given the
// whole cfg: the stored field first, then — for openrouter providers that
// actually point at OpenRouter — the legacy top-level openrouter_api_key
// (migrateLegacyProviders only folds it in when Providers is empty — a
// populated list whose entry lacks api_key still owns that key), then the
// OmaSeal account named after the provider id. The endpoint guard keeps the
// legacy key from being sent as a Bearer token to an arbitrary base URL.
func resolveProviderKey(cfg Config, p Provider) string {
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.Kind == "openrouter" && cfg.OpenRouterAPIKey != "" && providerUsesOpenRouterHeaders(p) {
		return cfg.OpenRouterAPIKey
	}
	if !providerNeedsAuth(p) || !keyringAvailable() {
		return ""
	}
	k, _ := keyringGet(p.ID)
	if k == "" && p.Kind == "openrouter" {
		k, _ = keyringGet("openrouter") // shared default account
	}
	return k
}
