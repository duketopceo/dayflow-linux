package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AgentSession is one coding-agent session recap (Claude Code or Codex),
// derived from the tool's on-disk JSONL transcript.
type AgentSession struct {
	Source   string `json:"source"` // "claude" | "codex"
	Project  string `json:"project"`
	Cwd      string `json:"cwd"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Messages int    `json:"messages"`
	Title    string `json:"title"`
	File     string `json:"file"`
}

func claudeDir() string {
	if d := os.Getenv("DAYFLOW_CLAUDE_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude", "projects")
}

func codexDir() string {
	if d := os.Getenv("DAYFLOW_CODEX_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".codex", "sessions")
}

// agentSessionsForDay collects Claude Code and Codex sessions whose
// transcript file was modified inside the given day, oldest first.
func agentSessionsForDay(d time.Time) []AgentSession {
	s, e := dayBounds(d)
	var out []AgentSession
	out = append(out, scanClaude(claudeDir(), s, e)...)
	out = append(out, scanCodex(codexDir(), s, e)...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// jsonlFiles walks root for *.jsonl files modified within [s, e).
func jsonlFiles(root string, s, e time.Time) []string {
	var paths []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		mt := info.ModTime()
		if !mt.Before(s) && mt.Before(e) {
			paths = append(paths, p)
		}
		return nil
	})
	return paths
}

func firstUserText(parts []any) string {
	for _, p := range parts {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		if t == "text" || t == "input_text" {
			if s, ok := m["text"].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func truncTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 120
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func scanClaude(root string, s, e time.Time) []AgentSession {
	var out []AgentSession
	for _, p := range jsonlFiles(root, s, e) {
		sess := AgentSession{Source: "claude", File: p}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var line map[string]any
			if json.Unmarshal(sc.Bytes(), &line) != nil {
				continue
			}
			if ts, ok := line["timestamp"].(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
					u := t.Unix()
					if sess.Start == 0 || u < sess.Start {
						sess.Start = u
					}
					if u > sess.End {
						sess.End = u
					}
				}
			}
			if sess.Cwd == "" {
				if c, ok := line["cwd"].(string); ok {
					sess.Cwd = c
				}
			}
			typ, _ := line["type"].(string)
			if typ == "user" || typ == "assistant" {
				sess.Messages++
			}
			if sess.Title == "" && typ == "user" {
				if msg, ok := line["message"].(map[string]any); ok {
					switch c := msg["content"].(type) {
					case string:
						sess.Title = c
					case []any:
						sess.Title = firstUserText(c)
					}
				}
			}
		}
		f.Close()
		if sess.Start == 0 {
			continue
		}
		sess.Title = truncTitle(sess.Title)
		sess.Project = projectName(sess.Cwd, p)
		out = append(out, sess)
	}
	return out
}

func scanCodex(root string, s, e time.Time) []AgentSession {
	var out []AgentSession
	for _, p := range jsonlFiles(root, s, e) {
		sess := AgentSession{Source: "codex", File: p}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var line map[string]any
			if json.Unmarshal(sc.Bytes(), &line) != nil {
				continue
			}
			if ts, ok := line["timestamp"].(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
					u := t.Unix()
					if sess.Start == 0 || u < sess.Start {
						sess.Start = u
					}
					if u > sess.End {
						sess.End = u
					}
				}
			}
			typ, _ := line["type"].(string)
			payload, _ := line["payload"].(map[string]any)
			if typ == "session_meta" && payload != nil {
				if c, ok := payload["cwd"].(string); ok {
					sess.Cwd = c
				}
				continue
			}
			if payload == nil {
				continue
			}
			ptype, _ := payload["type"].(string)
			role, _ := payload["role"].(string)
			if ptype == "message" && (role == "user" || role == "assistant") {
				sess.Messages++
				if sess.Title == "" && role == "user" {
					if parts, ok := payload["content"].([]any); ok {
						sess.Title = firstUserText(parts)
					}
				}
			}
		}
		f.Close()
		if sess.Start == 0 {
			continue
		}
		sess.Title = truncTitle(sess.Title)
		sess.Project = projectName(sess.Cwd, p)
		out = append(out, sess)
	}
	return out
}

func projectName(cwd, file string) string {
	if cwd != "" {
		return filepath.Base(cwd)
	}
	return strings.TrimSuffix(filepath.Base(file), ".jsonl")
}

func printAgentSessions(d time.Time, jsonOut bool) {
	sessions := agentSessionsForDay(d)
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"date":     d.Local().Format("2006-01-02"),
			"sessions": sessions,
			"count":    len(sessions),
		})
		return
	}
	if len(sessions) == 0 {
		fmt.Println("no agent sessions for", d.Local().Format("2006-01-02"))
		return
	}
	for _, s := range sessions {
		start := time.Unix(s.Start, 0).Local().Format("15:04")
		end := time.Unix(s.End, 0).Local().Format("15:04")
		fmt.Printf("%s–%s  %-6s %-20s %d msgs  %s\n",
			start, end, s.Source, s.Project, s.Messages, s.Title)
	}
}
