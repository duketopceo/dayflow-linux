package main

// Knowledge sync — opt-in push of distilled atoms to a Kurultai brain
// (Ulaanbaatar, knowledge.shippedit.dev).
//
// Egress surface: only summarized text leaves the machine — the day's
// brief markdown and per-workstream briefing prose. Raw frames,
// transcripts, and turn text are never sent.
//
// Transport: the remote brain has no public atom-write surface — /mcp is
// read-only and /ingest requires a loopback peer. So the payload rides an
// SSH relay: `ssh <host> docker exec -i <container> curl … /ingest` —
// inside the container the request is genuinely loopback. The ingest
// secret travels on stdin, never in argv. When Kurultai gains an
// authenticated remote ingest route, this transport becomes a config
// swap, not a rewrite.
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
	"os/exec"
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

// sshIngestPush returns a knowledgePush that relays through SSH into the
// brain container's loopback /ingest. Secret and body both travel on
// stdin: the remote `read` consumes the secret line, curl consumes the
// rest — the secret never lands in a remote process's argv.
func sshIngestPush(cfg Config) (knowledgePush, error) {
	host := cfg.KnowledgeSSHHost
	if host == "" {
		host = "server-001"
	}
	container := cfg.KnowledgeContainer
	if container == "" {
		container = "kurultai-personal"
	}
	port := cfg.KnowledgePort
	if port == 0 {
		port = 8421
	}
	secretRef := cfg.KnowledgeSecretRef
	if secretRef == "" {
		secretRef = "omaseal://kurultai/personal-ingest-secret"
	}
	secret := resolveOmaseal(secretRef)
	if secret == "" {
		return nil, fmt.Errorf("knowledge ingest secret unavailable (omaseal locked? %s)", secretRef)
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

	start, end := dayBounds(day)
	if blocks, err := blocksBetween(db, start, end); err == nil && len(blocks) > 0 {
		b.WriteString("## Journal\n\n")
		b.WriteString(markdownBrief(blocks, ds))
		b.WriteString("\n")
	}

	if db != nil {
		briefing := agentBriefingFor(db, cfg, day, false)
		if len(briefing.Workstreams) > 0 {
			b.WriteString("## Agent workstreams\n\n")
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
	push, err := sshIngestPush(cfg)
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
	push, err := sshIngestPush(cfg)
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
