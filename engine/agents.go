package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AgentSession is one coding-agent session recap (Claude Code, Codex,
// OpenCode, Devin, or Cursor), derived from the tool's on-disk transcript
// store.
type AgentSession struct {
	Source   string `json:"source"` // "claude" | "codex" | "opencode" | "devin" | "cursor"
	Project  string `json:"project"`
	Cwd      string `json:"cwd"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Messages int    `json:"messages"`
	Title    string `json:"title"`
	// File is the session's stable source-scoped key: a transcript path for
	// JSONL sources, a synthetic "source://store/id" key for DB sources.
	File string `json:"file"`
	// Recap is a generated summary of what the session accomplished; empty
	// when ungenerated, unworthy, or unavailable. RecapConfidence is the Jev
	// quality score — nil means "no opinion", never zero.
	Recap           string   `json:"recap,omitempty"`
	RecapConfidence *float64 `json:"recap_confidence,omitempty"`
	// store and sessionID locate a DB-backed session inside its store so the
	// source adapter can re-query fingerprint and excerpt at recap time.
	// Empty for file-per-session JSONL sources. Unexported: File remains the
	// public stable key.
	store     string
	sessionID string
}

// agentSource is the per-tool seam for session discovery. Adapters produce
// sessions overlapping a [s,e) day window and supply the recap inputs —
// cache fingerprint and bounded excerpt — so attachRecaps never needs to
// know the storage shape. JSONL sources fingerprint by File stat; DB sources
// content-hash the session's message ids + update times.
type agentSource interface {
	// Name matches AgentSession.Source.
	Name() string
	// Scan returns sessions overlapping [s,e); note describes a degraded
	// store for drift surfacing (identifiers/sizes only — never content).
	// A missing or schema-incompatible store returns an empty slice and a
	// note, never an error that could sink the other sources.
	Scan(s, e time.Time) (sessions []AgentSession, note string)
	// Fingerprint is the recap-cache invalidator; false skips the session.
	Fingerprint(sess AgentSession) (recapFingerprint, bool)
	// Excerpt is the bounded transcript sample for recap generation.
	Excerpt(sess AgentSession) string
}

// sourceScanStatus records one adapter's outcome for drift surfacing (R3b):
// a source that was productive and now scans empty must show up in agents
// output, not silently vanish.
type sourceScanStatus struct {
	Source   string `json:"source"`
	Sessions int    `json:"sessions"`
	Status   string `json:"status"`          // "ok" | "empty" | "unavailable"
	Note     string `json:"note,omitempty"`  // store-level detail, never content
	Drift    bool   `json:"drift,omitempty"` // previously productive, now silent
}

func agentSources() []agentSource {
	return []agentSource{
		jsonlSource{name: "claude", root: claudeDir(), parse: parseClaudeLine},
		jsonlSource{name: "codex", root: codexDir(), parse: parseCodexLine},
		opencodeSource{},
		devinSource{},
		cursorSource{},
	}
}

func agentSourceFor(name string) agentSource {
	for _, s := range agentSources() {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// jsonlSource adapts a file-per-session JSONL transcript root to the
// agentSource seam. Fingerprint stays File mtime+size — cheap and exactly
// what the existing agent_recaps rows are keyed on.
type jsonlSource struct {
	name  string
	root  string
	parse func([]byte, *AgentSession)
}

func (j jsonlSource) Name() string { return j.name }

func (j jsonlSource) Scan(s, e time.Time) ([]AgentSession, string) {
	if _, err := os.Stat(j.root); err != nil {
		return nil, "transcript root not found"
	}
	return scanJSONL(j.root, j.name, s, e, j.parse), ""
}

func (j jsonlSource) Fingerprint(sess AgentSession) (recapFingerprint, bool) {
	return fingerprint(sess.File)
}

func (j jsonlSource) Excerpt(sess AgentSession) string {
	return sessionExcerpt(sess.File, j.name)
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

// scanAgentSources runs every registered source adapter over the day window
// and returns the merged, start-sorted session list plus per-source scan
// status. A failed source degrades to a status note — never a hard error.
func scanAgentSources(d time.Time) ([]AgentSession, []sourceScanStatus) {
	s, e := dayBounds(d)
	out := []AgentSession{}
	statuses := make([]sourceScanStatus, 0, len(agentSources()))
	for _, src := range agentSources() {
		sessions, note := src.Scan(s, e)
		st := sourceScanStatus{Source: src.Name(), Sessions: len(sessions), Note: note}
		switch {
		case note != "" && len(sessions) == 0:
			st.Status = "unavailable"
		case len(sessions) == 0:
			st.Status = "empty"
		default:
			st.Status = "ok"
		}
		statuses = append(statuses, st)
		out = append(out, sessions...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out, statuses
}

// agentSessionsForDay collects agent sessions active inside the given day —
// a session counts when its [Start, End] message-timestamp range overlaps
// the day, so one spanning midnight appears on both days.
func agentSessionsForDay(d time.Time) []AgentSession {
	sessions, _ := scanAgentSources(d)
	return sessions
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
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
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
		if (p.Type == "text" || p.Type == "input_text" || p.Type == "output_text") &&
			strings.TrimSpace(p.Text) != "" {
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

func parseClaudeLine(raw []byte, sess *AgentSession) {
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
}

func scanClaude(root string, s, e time.Time) []AgentSession {
	return scanJSONL(root, "claude", s, e, parseClaudeLine)
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

func parseCodexLine(raw []byte, sess *AgentSession) {
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
}

func scanCodex(root string, s, e time.Time) []AgentSession {
	return scanJSONL(root, "codex", s, e, parseCodexLine)
}

func projectName(cwd, file string) string {
	if cwd != "" {
		return filepath.Base(cwd)
	}
	return strings.TrimSuffix(filepath.Base(file), ".jsonl")
}

// recordAgentSourceScans persists each source's productivity in meta so a
// source that was productive and now scans empty surfaces as drift (R3b) —
// an events row plus the Drift flag in agents output. Source names and
// counts only; session content never touches this path.
func recordAgentSourceScans(db *sql.DB, statuses []sourceScanStatus) {
	if db == nil {
		return
	}
	for i := range statuses {
		st := &statuses[i]
		key := "agent_source_seen:" + st.Source
		if st.Sessions > 0 {
			db.Exec(`INSERT INTO meta(k, v) VALUES(?, '1')
			  ON CONFLICT(k) DO UPDATE SET v='1'`, key)
			continue
		}
		var v string
		if err := db.QueryRow(`SELECT v FROM meta WHERE k=?`, key).Scan(&v); err == nil && v == "1" {
			st.Drift = true
			logEvent(db, "agent_source_drift",
				fmt.Sprintf("source=%s sessions=0 status=%s", st.Source, st.Status))
		}
	}
}

func printAgentSessions(db *sql.DB, cfg Config, d time.Time, jsonOut, recaps bool) {
	sessions, statuses := scanAgentSources(d)
	recordAgentSourceScans(db, statuses)
	if recaps {
		attachRecaps(db, cfg, sessions)
	}
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"date":     d.Local().Format("2006-01-02"),
			"sessions": sessions,
			"count":    len(sessions),
			"sources":  statuses,
		})
		return
	}
	if len(sessions) == 0 {
		fmt.Println("no agent sessions for", d.Local().Format("2006-01-02"))
	} else {
		for _, s := range sessions {
			start := time.Unix(s.Start, 0).Local().Format("15:04")
			end := time.Unix(s.End, 0).Local().Format("15:04")
			fmt.Printf("%s–%s  %-6s %-20s %d msgs  %s\n",
				start, end, s.Source, s.Project, s.Messages, s.Title)
			if s.Recap != "" {
				fmt.Printf("         └─ %s\n", s.Recap)
			}
		}
	}
	for _, st := range statuses {
		if st.Drift {
			fmt.Printf("note: %s produced sessions before but none today — its store may have drifted\n", st.Source)
		}
	}
}
