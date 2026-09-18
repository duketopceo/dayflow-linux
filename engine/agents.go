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

// agentSessionsForDay collects Claude Code and Codex sessions active inside
// the given day — a session counts when its [Start, End] message-timestamp
// range overlaps the day, so one spanning midnight appears on both days.
func agentSessionsForDay(d time.Time) []AgentSession {
	s, e := dayBounds(d)
	out := []AgentSession{}
	out = append(out, scanClaude(claudeDir(), s, e)...)
	out = append(out, scanCodex(codexDir(), s, e)...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// jsonlFiles walks root for *.jsonl files modified at or after s. No upper
// bound: a file written after e can still hold messages timestamped inside
// [s, e) — a session spanning midnight would otherwise drop off both days.
// Overlap filtering on message timestamps happens in scanJSONL.
func jsonlFiles(root string, s time.Time) []string {
	var paths []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		if !info.ModTime().Before(s) {
			paths = append(paths, p)
		}
		return nil
	})
	return paths
}

func truncTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 120
	if len(s) > max {
		// Truncate on a rune boundary — a mid-rune cut emits invalid UTF-8.
		return string([]rune(s)[:max]) + "…"
	}
	return s
}

// scanJSONL walks JSONL transcripts under root, letting parse fold each line
// into the session accumulator. Typed decode only — no map[string]any over
// every line of a multi-MB transcript.
func scanJSONL(root, source string, s, e time.Time, parse func([]byte, *AgentSession)) []AgentSession {
	out := []AgentSession{}
	for _, p := range jsonlFiles(root, s) {
		sess := AgentSession{Source: source, File: p}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			parse(sc.Bytes(), &sess)
		}
		// A scan error (e.g. a line over the 1MB buffer) means the session
		// data is truncated — skip it rather than report partials.
		scanErr := sc.Err()
		f.Close()
		if scanErr != nil || sess.Start == 0 {
			continue
		}
		// Buckets are message timestamps, not mtime: keep the session only
		// when its [Start, End] range overlaps the scanned window.
		if sess.Start >= e.Unix() || sess.End < s.Unix() {
			continue
		}
		sess.Title = truncTitle(sess.Title)
		sess.Project = projectName(sess.Cwd, p)
		out = append(out, sess)
	}
	return out
}

func trackRange(sess *AgentSession, ts string) {
	if ts == "" {
		return
	}
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

// contentText extracts plain text from a message content value that is either
// a string or an array of {type, text} parts.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	for _, p := range parts {
		if (p.Type == "text" || p.Type == "input_text") && strings.TrimSpace(p.Text) != "" {
			return p.Text
		}
	}
	return ""
}

type claudeLine struct {
	Timestamp string          `json:"timestamp"`
	Cwd       string          `json:"cwd"`
	Type      string          `json:"type"`
	Message   json.RawMessage `json:"message"`
}

type claudeMessage struct {
	Content json.RawMessage `json:"content"`
}

func scanClaude(root string, s, e time.Time) []AgentSession {
	return scanJSONL(root, "claude", s, e, func(raw []byte, sess *AgentSession) {
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			return
		}
		trackRange(sess, line.Timestamp)
		if sess.Cwd == "" {
			sess.Cwd = line.Cwd
		}
		if line.Type == "user" || line.Type == "assistant" {
			sess.Messages++
		}
		if sess.Title == "" && line.Type == "user" && len(line.Message) > 0 {
			var msg claudeMessage
			if json.Unmarshal(line.Message, &msg) == nil {
				sess.Title = contentText(msg.Content)
			}
		}
	})
}

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexPayload struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Cwd     string          `json:"cwd"`
	Content json.RawMessage `json:"content"`
}

func scanCodex(root string, s, e time.Time) []AgentSession {
	return scanJSONL(root, "codex", s, e, func(raw []byte, sess *AgentSession) {
		var line codexLine
		if json.Unmarshal(raw, &line) != nil {
			return
		}
		trackRange(sess, line.Timestamp)
		if len(line.Payload) == 0 {
			return
		}
		var p codexPayload
		if json.Unmarshal(line.Payload, &p) != nil {
			return
		}
		if line.Type == "session_meta" {
			if sess.Cwd == "" {
				sess.Cwd = p.Cwd
			}
			return
		}
		if p.Type == "message" && (p.Role == "user" || p.Role == "assistant") {
			sess.Messages++
			if sess.Title == "" && p.Role == "user" {
				sess.Title = contentText(p.Content)
			}
		}
	})
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
