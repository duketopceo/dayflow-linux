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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "omaseal", "get", keyringService, account).Output()
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

// resolveProviderKey returns the effective API key for a provider: the stored
// field first, then the OmaSeal account named after the provider id.
func resolveProviderKey(p Provider) string {
	if p.APIKey != "" {
		return p.APIKey
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
