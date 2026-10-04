package main

// Knowledge sync — opt-in push of distilled atoms to a Kurultai brain.
//
// Egress surface: only summarized text leaves the machine — the day's
// brief markdown and per-workstream briefing prose. Raw frames,
// transcripts, and turn text are never sent.
//
// Transports (knowledge_transport):
//   - "http" (default): direct POST <knowledge_url>/ingest with the
//     secret in the Authorization header. Requires the brain to accept
//     authenticated remote ingest.
//   - "ssh": fallback for brains whose /ingest is loopback-only —
//     `ssh <host> docker exec -i <container> curl … /ingest`, inside
//     the container the request is genuinely loopback. The ingest
//     secret travels on stdin, never in argv.
//
// Every endpoint detail is user config — no hosts, containers, or
// secret references are baked into the binary.
//
// Dedup: the whole day document is content-hashed into meta; unchanged
// days are skipped. Source_id stability comes from the ingest `name`
// param (dayflow/<date>.md), so re-pushes upsert the same atoms.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// knowledgePush pushes one named markdown document into the brain.
type knowledgePush func(name string, doc []byte) error

// resolveOmaseal turns "omaseal://service/account" into the secret value;
// literal values pass through unchanged. Empty/unresolvable → "".
func resolveOmaseal(v string) string {
	if !strings.HasPrefix(v, "omaseal://") {
		return v
	}
	parts := strings.SplitN(v[len("omaseal://"):], "/", 2)
	if len(parts) != 2 {
		return ""
	}
	s, _ := keyringGetService(parts[0], parts[1])
	return s
}

// knowledgePushFor selects the push transport. Unknown values error
// loudly — silent fallback across transports would hide which one ran.
func knowledgePushFor(cfg Config) (knowledgePush, error) {
	switch cfg.KnowledgeTransport {
	case "", "http":
		return httpIngestPush(cfg)
	case "ssh":
		return sshIngestPush(cfg)
	default:
		return nil, fmt.Errorf("unknown knowledge_transport %q (want \"http\" or \"ssh\")", cfg.KnowledgeTransport)
	}
}

// knowledgeSecret resolves the configured ingest secret reference.
func knowledgeSecret(cfg Config) (string, error) {
	if cfg.KnowledgeSecretRef == "" {
		return "", fmt.Errorf("knowledge_secret_ref is required (\"omaseal://service/account\" or a literal value)")
	}
	secret := resolveOmaseal(cfg.KnowledgeSecretRef)
	if secret == "" {
		return "", fmt.Errorf("knowledge ingest secret unavailable (omaseal locked? %s)", cfg.KnowledgeSecretRef)
	}
	return secret, nil
}

// httpIngestPush posts the doc directly to <knowledge_url>/ingest — for
// brains configured to accept secret-authenticated remote ingest.
func httpIngestPush(cfg Config) (knowledgePush, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.KnowledgeURL), "/")
	if base == "" {
		return nil, fmt.Errorf("knowledge_url is required for http transport")
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, fmt.Errorf("knowledge_url %q is not a valid URL", cfg.KnowledgeURL)
	}
	secret, err := knowledgeSecret(cfg)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}

	return func(name string, doc []byte) error {
		u := fmt.Sprintf("%s/ingest?name=%s&format=md", base, url.QueryEscape(name))
		req, err := http.NewRequest("POST", u, bytes.NewReader(doc))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "text/markdown")
		req.Header.Set("x-kurultai-agent-id", "dayflow")
		req.Header.Set("x-kurultai-namespace", "dayflow")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("ingest POST: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return fmt.Errorf("ingest POST: HTTP %d: %.200s", resp.StatusCode, body)
		}
		return nil
	}, nil
}

// sshIngestPush returns a knowledgePush that relays through SSH into the
// brain container's loopback /ingest. Secret and body both travel on
// stdin: the remote `read` consumes the secret line, curl consumes the
// rest — the secret never lands in a remote process's argv.
func sshIngestPush(cfg Config) (knowledgePush, error) {
	host := cfg.KnowledgeSSHHost
	if host == "" {
		return nil, fmt.Errorf("knowledge_ssh_host is required for ssh transport")
	}
	container := cfg.KnowledgeContainer
	if container == "" {
		return nil, fmt.Errorf("knowledge_container is required for ssh transport")
	}
	port := cfg.KnowledgePort
	if port == 0 {
		port = 8421
	}
	secret, err := knowledgeSecret(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, fmt.Errorf("ssh not found: %w", err)
	}

	return func(name string, doc []byte) error {
		// `read` eats the secret line; the remainder of stdin is the body.
		// ssh joins remote argv with spaces — the whole command must arrive
		// as one quoted string or `sh -c` sees only its first word.
		remote := fmt.Sprintf(
			`docker exec -i %s sh -c 'IFS= read -r S && exec curl -sS -f -X POST "http://127.0.0.1:%d/ingest?name=%s&format=md" -H "x-kurultai-ingest-secret: $S" -H "x-kurultai-agent-id: dayflow" -H "x-kurultai-namespace: dayflow" --data-binary @-'`,
			container, port, name)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "ssh", "-o", "ConnectTimeout=10", "-o", "BatchMode=yes",
			host, remote)
		cmd.Stdin = io.MultiReader(strings.NewReader(secret+"\n"), bytes.NewReader(doc))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ssh ingest relay: %v: %.200s", err, out)
		}
		return nil
	}, nil
}

// knowledgeDocFor builds the day's markdown document: journal brief plus
// agent workstreams as headed sections. Chunked server-side, so each
// section becomes an atom sharing the day's source_id.
func knowledgeDocFor(db *sql.DB, cfg Config, day time.Time) []byte {
	ds := day.Format("2006-01-02")
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: Dayflow %s\ntags: dayflow, journal\n---\n\n# Dayflow %s\n\n", ds, ds)

	// The brain chunks markdown by section and quality-gates each atom
	// (tags + ~80 chars minimum), so every section carries a lead-in
	// line — a bare heading followed by a tiny body lands in quarantine.
	start, end := dayBounds(day)
	blocks, berr := blocksBetween(db, start, end)
	if berr == nil && len(blocks) > 0 {
		b.WriteString("## Journal\n\n")
		b.WriteString(markdownBrief(blocks, ds))
		fmt.Fprintf(&b, "\n_%d summarized blocks, %s–%s local; top categories: %s._\n",
			len(blocks), blocks[0].StartStr, blocks[len(blocks)-1].EndStr, topCategories(blocks, 3))
	}

	if db != nil {
		briefing := agentBriefingFor(db, cfg, day, false)
		if len(blocks) == 0 && len(briefing.Workstreams) == 0 {
			fmt.Fprintf(&b, "## Journal\n\nNo captured blocks or agent sessions for %s — an offline, idle, or untracked day.\n", ds)
		}
		if len(briefing.Workstreams) > 0 {
			nthreads := 0
			sources := map[string]bool{}
			for _, ws := range briefing.Workstreams {
				nthreads += len(ws.Threads)
				for _, th := range ws.Threads {
					sources[th.Source] = true
				}
			}
			var srcs []string
			for s := range sources {
				srcs = append(srcs, s)
			}
			sort.Strings(srcs)
			fmt.Fprintf(&b, "## Agent workstreams\n\n%d workstreams spanning %d agent conversation threads across %s — condensed from locally indexed sessions; raw turns never leave this machine.\n\n",
				len(briefing.Workstreams), nthreads, strings.Join(srcs, ", "))
		}
		for _, ws := range briefing.Workstreams {
			var sources []string
			seen := map[string]bool{}
			for _, th := range ws.Threads {
				if !seen[th.Source] {
					seen[th.Source] = true
					sources = append(sources, th.Source)
				}
			}
			fmt.Fprintf(&b, "### %s (%s)\n\n%s\n", ws.Name, strings.Join(sources, ", "), ws.Summary)
			for _, bl := range ws.Bullets {
				fmt.Fprintf(&b, "- %s\n", bl)
			}
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

// topCategories returns the n most common block categories, ties broken
// alphabetically for deterministic output.
func topCategories(blocks []Block, n int) string {
	counts := map[string]int{}
	for _, bl := range blocks {
		c := bl.Category
		if c == "" {
			c = "uncategorized"
		}
		counts[c]++
	}
	var names []string
	for c := range counts {
		names = append(names, c)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > n {
		names = names[:n]
	}
	return strings.Join(names, ", ")
}

// syncKnowledge pushes the day's distilled document. Returns
// (pushed, skipped-unchanged, error).
func syncKnowledge(db *sql.DB, cfg Config, day time.Time, push knowledgePush) (int, int, error) {
	doc := knowledgeDocFor(db, cfg, day)
	name := "dayflow/" + day.Format("2006-01-02") + ".md"
	h := sha256.Sum256(doc)
	sum := hex.EncodeToString(h[:])
	mk := "ksync:" + name
	if db != nil && metaGet(db, mk) == sum {
		return 0, 1, nil
	}
	if err := push(name, doc); err != nil {
		debugf(cfg, "knowledge sync %s: %v", name, err)
		return 0, 0, err
	}
	if db != nil {
		metaSet(db, mk, sum)
	}
	return 1, 0, nil
}

// knowledgeSyncAfterExport pushes yesterday's finalized journal — called
// from the export tail so the daily timer keeps the brain current. Today
// is deliberately skipped: the doc is still growing and each push
// replaces the day's atoms.
func knowledgeSyncAfterExport(db *sql.DB, cfg Config) {
	if !cfg.KnowledgeSync {
		return
	}
	push, err := knowledgePushFor(cfg)
	if err != nil {
		debugf(cfg, "knowledge sync skipped: %v", err)
		return
	}
	y := time.Now().AddDate(0, 0, -1)
	pushed, skipped, err := syncKnowledge(db, cfg, y, push)
	if err != nil {
		debugf(cfg, "knowledge sync failed: %v", err)
		return
	}
	debugf(cfg, "knowledge sync: pushed=%d unchanged=%d", pushed, skipped)
}

// printSync runs `dayflow sync [day]`.
func printSync(db *sql.DB, cfg Config, day time.Time, jsonOut bool) {
	if !cfg.KnowledgeSync {
		fatal(fmt.Errorf("knowledge sync is off — set knowledge_sync: true in config.json or the Settings toggle"))
	}
	push, err := knowledgePushFor(cfg)
	if err != nil {
		fatal(err)
	}
	pushed, skipped, err := syncKnowledge(db, cfg, day, push)
	if jsonOut {
		fmt.Printf(`{"day":%q,"pushed":%d,"unchanged":%d,"error":%q}`+"\n",
			day.Format("2006-01-02"), pushed, skipped, errString(err))
		return
	}
	if err != nil {
		fmt.Printf("sync %s: pushed %d, unchanged %d, error: %v\n",
			day.Format("2006-01-02"), pushed, skipped, err)
		return
	}
	fmt.Printf("sync %s: pushed %d, unchanged %d\n", day.Format("2006-01-02"), pushed, skipped)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
