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
	// Scan returns sessions overlapping [s,e); note describes a degraded or
	// absent store for drift surfacing (identifiers/sizes only — never
	// content). A missing or schema-incompatible store returns an empty
	// slice and a note, never an error that could sink the other sources.
	Scan(s, e time.Time) (sessions []AgentSession, note scanNote)
	// Fingerprint is the recap-cache invalidator; false skips the session.
	Fingerprint(sess AgentSession) (recapFingerprint, bool)
	// Excerpt is the bounded transcript sample for recap generation.
	Excerpt(sess AgentSession) string
	// Turns returns the session's normalized turn list (role, text,
	// timestamp) for the briefing's condensed narrative and status
	// derivation; nil/empty degrades the thread, never the briefing.
	Turns(sess AgentSession) []sessionTurn
	// Close releases lazily-opened store handles held by the adapter
	// (DB-backed sources share one handle per store per pass). No-op for
	// file-backed sources.
	Close()
}

// noteStoreMissing is the shared absence sentinel leaf helpers return when
// a store file/dir isn't there at all. Absence is not corruption — an
// all-absent aggregate reports "empty", never "unavailable", so an
// uninstalled tool or a store removed after productive use can't pin drift.
const noteStoreMissing = "store not found"

// scanNote is one source's aggregate store detail for drift surfacing.
// absent marks pure absence (every sub-store missing); the note text is
// kept either way so the detail stays visible in JSON output.
type scanNote struct {
	text   string
	absent bool
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
		jsonlSource{name: "claude", root: claudeDir(), parse: parseClaudeLine, lineRoleText: claudeLineRoleText, lineTurn: claudeLineTurn},
		jsonlSource{name: "codex", root: codexDir(), parse: parseCodexLine, lineRoleText: codexLineRoleText, lineTurn: codexLineTurn},
		&opencodeSource{},
		&devinSource{},
		&cursorSource{},
	}
}

// jsonlSource adapts a file-per-session JSONL transcript root to the
// agentSource seam. Fingerprint stays File mtime+size — cheap and exactly
// what the existing agent_recaps rows are keyed on.
type jsonlSource struct {
	name  string
	root  string
	parse func([]byte, *AgentSession)
	// lineRoleText decodes one transcript line into (role, text) for the
	// excerpt — the seam counterpart to parse, populated at registration so
	// a new JSONL source can't silently produce empty excerpts that settle
	// as cached recaps.
	lineRoleText func([]byte) (role, text string)
	// lineTurn decodes one transcript line into (role, text, unixTs) for
	// the briefing's condensed turns — lineRoleText plus the timestamp the
	// excerpt path doesn't need.
	lineTurn func([]byte) (role, text string, ts int64)
}

func (j jsonlSource) Name() string { return j.name }

func (j jsonlSource) Scan(s, e time.Time) ([]AgentSession, scanNote) {
	if _, err := os.Stat(j.root); err != nil {
		return nil, scanNote{text: "transcript root not found", absent: true}
	}
	return scanJSONL(j.root, j.name, s, e, j.parse), scanNote{}
}

func (j jsonlSource) Fingerprint(sess AgentSession) (recapFingerprint, bool) {
	return fingerprint(sess.File)
}

func (j jsonlSource) Excerpt(sess AgentSession) string {
	return sessionExcerpt(sess.File, j.lineRoleText)
}

// Turns decodes every line of the transcript into the shared turn shape —
// the briefing needs the whole conversation, not just the excerpt's
// first/last fields.
func (j jsonlSource) Turns(sess AgentSession) []sessionTurn {
	if j.lineTurn == nil {
		return nil
	}
	var turns []sessionTurn
	ok := eachJSONLLine(sess.File, func(line []byte) {
		role, text, ts := j.lineTurn(line)
		if role == "" {
			return
		}
		turns = append(turns, sessionTurn{
			role:       role,
			text:       text,
			unixTs:     ts,
			usableUser: role == "user" && text != "" && !isEnvelopeText(text),
		})
	})
	if !ok {
		return nil
	}
	return turns
}

// Close is a no-op — file-backed sources hold no store handles.
func (j jsonlSource) Close() {}

// roStore wraps a read connection plus the temp dir to remove when the
// live DB couldn't be opened in place and a copy was made.
type roStore struct {
	db     *sql.DB
	tmpDir string
}

func (s *roStore) close() {
	s.db.Close()
	if s.tmpDir != "" {
		os.RemoveAll(s.tmpDir)
	}
}

// openROStore opens a sqlite store read-only. A live WAL-mode store can
// refuse a plain ro open (shm recovery needs write access), so on failure
// retry once against a temp-dir copy of db+wal+shm before degrading.
func openROStore(path string) (*roStore, error) {
	const ro = "?mode=ro&_pragma=busy_timeout(3000)&_pragma=query_only(1)"
	if db, err := sql.Open("sqlite", "file:"+path+ro); err == nil {
		// Ping only opens the connection — WAL recovery happens on the first
		// real statement, so probe readability directly.
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err == nil {
			return &roStore{db: db}, nil
		}
		db.Close()
	}
	// Retry on a temp copy — opened read-write, which is safe because the
	// copy is disposable and WAL recovery may need to write shm.
	dir, err := os.MkdirTemp("", "dayflow-store-*")
	if err != nil {
		return nil, err
	}
	tmp := filepath.Join(dir, filepath.Base(path))
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(dir)
		}
	}()
	for i, suffix := range []string{"", "-wal", "-shm"} {
		if err := copyFile(path+suffix, tmp+suffix); err != nil && i == 0 {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", "file:"+tmp+"?_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, err
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil {
		db.Close()
		return nil, err
	}
	ok = true
	return &roStore{db: db, tmpDir: dir}, nil
}

func sqliteTableExists(db *sql.DB, name string) bool {
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// storeEntry is one lazily-opened store handle or its open error — failed
// opens are cached too so a dead store doesn't retry the copy each call.
type storeEntry struct {
	st  *roStore
	err error
}

// storeCache lazily opens one read-only handle per store path for an
// adapter pass — Scan/Fingerprint/Excerpt calls share it instead of
// re-opening (and possibly temp-copying a live WAL store) per session.
type storeCache struct {
	stores map[string]storeEntry
}

func (c *storeCache) open(path string) (*roStore, error) {
	if c.stores == nil {
		c.stores = map[string]storeEntry{}
	}
	if e, ok := c.stores[path]; ok {
		return e.st, e.err
	}
	st, err := openROStore(path)
	c.stores[path] = storeEntry{st: st, err: err}
	return st, err
}

// Close releases every lazily-opened store; safe on an unused cache.
func (c *storeCache) Close() {
	for _, e := range c.stores {
		if e.st != nil {
			e.st.close()
		}
	}
	c.stores = nil
}

// sessionCandidate is a store row that may hold an in-window session —
// every DB adapter keys candidates on id/title/dir.
type sessionCandidate struct {
	id, title, dir string
}

// sessionTurn is the normalized turn shape the DB-backed adapters feed the
// shared session fold and excerpt: role, text, and a unix-second timestamp
// (0 when the row carries none). usableUser marks user turns that can
// anchor or excerpt a session — each source folds its own predicate in
// (Devin requires real typed input, Cursor requires non-empty text,
// OpenCode counts any user message).
type sessionTurn struct {
	role       string // "user" | "assistant" | other
	text       string
	unixTs     int64 // epoch seconds; 0 = absent
	usableUser bool
}

// foldSession accumulates the shared scan-time fields (Start/End range,
// message count, usable-user count, first usable user text) into sess.
// Returns the first usable user text and the usable-user count — both
// callers need them for the "no user turns, no session" gate and title
// fallback.
func foldSession(sess *AgentSession, turns []sessionTurn) (firstUser string, userTurns int) {
	for _, t := range turns {
		if t.unixTs > 0 {
			if sess.Start == 0 || t.unixTs < sess.Start {
				sess.Start = t.unixTs
			}
			if t.unixTs > sess.End {
				sess.End = t.unixTs
			}
		}
		if t.role == "user" || t.role == "assistant" {
			sess.Messages++
		}
		if t.usableUser {
			userTurns++
			if firstUser == "" {
				firstUser = strings.TrimSpace(t.text)
			}
		}
	}
	return firstUser, userTurns
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

// agentProgress writes one JSON progress line to stderr — the QML
// briefing loader parses these into a determinate load bar. stderr is the
// free channel on --json calls (stdout carries the payload), and CLI
// callers get a useful heartbeat on long scans.
func agentProgress(ev map[string]any) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintln(os.Stderr, string(b))
}

// scanAgentSources runs every registered source adapter over the day window
// and returns the merged, start-sorted session list plus per-source scan
// status. A failed source degrades to a status note — never a hard error.
//
// Callers that go on to attachRecaps may pass the adapter set to share
// (built once via agentSources): a store handle opened during the scan —
// possibly through the WAL temp-copy fallback — is then reused for
// fingerprint/excerpt instead of opening again, and the caller closes it
// once via closeAgentSources. With no adapters passed, scanAgentSources
// builds its own set and releases it before returning.
func scanAgentSources(d time.Time, srcs ...agentSource) ([]AgentSession, []sourceScanStatus) {
	own := len(srcs) == 0
	if own {
		srcs = agentSources()
		defer closeAgentSources(srcs)
	}
	s, e := dayBounds(d)
	out := []AgentSession{}
	statuses := make([]sourceScanStatus, 0, len(srcs))
	for i, src := range srcs {
		agentProgress(map[string]any{"phase": "scan", "source": src.Name(), "i": i + 1, "n": len(srcs)})
		sessions, note := src.Scan(s, e)
		st := sourceScanStatus{Source: src.Name(), Sessions: len(sessions), Note: note.text}
		switch {
		case note.text != "" && len(sessions) == 0 && !note.absent:
			st.Status = "unavailable"
		case len(sessions) == 0:
			// A purely absent store lands here too — "empty" (tool not
			// installed / store never created), not "unavailable", so it
			// can't flag drift.
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

// agentSourceNamed resolves a session's source name to its adapter —
// shared by every "find the adapter that owns this session" site.
func agentSourceNamed(srcs []agentSource, name string) agentSource {
	for _, s := range srcs {
		if s != nil && s.Name() == name {
			return s
		}
	}
	return nil
}

// scanAgentDay runs one day's store scan: returns source handles (caller
// must closeAgentSources), the session list, and per-source statuses, and
// records drift bookkeeping.
func scanAgentDay(db *sql.DB, d time.Time) ([]agentSource, []AgentSession, []sourceScanStatus) {
	srcs := agentSources()
	sessions, statuses := scanAgentSources(d, srcs...)
	recordAgentSourceScans(db, statuses)
	return srcs, sessions, statuses
}

// eachJSONLLine streams a transcript's raw lines to fn; false on open or
// scan error (e.g. a >1MB line) so callers can drop partial data — parity
// across scanJSONL, sessionExcerpt, and jsonlSource.Turns. Files beyond
// agentTranscriptCap are decode bombs (a >64MB JSONL can monopolize a
// scan for minutes) — skipped up front rather than mid-parse.
func eachJSONLLine(path string, fn func([]byte)) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > agentTranscriptCap {
		return false
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		fn(sc.Bytes())
	}
	return sc.Err() == nil
}

// unixTs parses an RFC3339Nano timestamp to Unix seconds (0 on failure).
func unixTs(s string) int64 {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.Unix()
	}
	return 0
}

// printDriftNotes warns when a previously-productive store scanned empty —
// an empty day caused by a moved/broken store must not read as a quiet day.
func printDriftNotes(statuses []sourceScanStatus) {
	for _, st := range statuses {
		if st.Drift {
			fmt.Printf("note: %s produced sessions before but none today — its store may have drifted\n", st.Source)
		}
	}
}

// closeAgentSources releases every adapter's lazily-opened store handles.
// The owner of a passed-in adapter set calls it once when the pass ends.
func closeAgentSources(srcs []agentSource) {
	for _, s := range srcs {
		if s != nil {
			s.Close()
		}
	}
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
	// stripCtl first: titles come from transcript/store text that can carry
	// ESC or other control bytes — they must not reach the terminal raw.
	s = strings.Join(strings.Fields(stripCtl(s)), " ")
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
		// A scan error (e.g. a line over the 1MB buffer) means the session
		// data is truncated — skip it rather than report partials.
		if !eachJSONLLine(p, func(line []byte) { parse(line, &sess) }) || sess.Start == 0 {
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
	u := unixTs(ts)
	if u == 0 {
		return
	}
	if sess.Start == 0 || u < sess.Start {
		sess.Start = u
	}
	if u > sess.End {
		sess.End = u
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

func projectName(cwd, file string) string {
	if cwd != "" {
		return filepath.Base(cwd)
	}
	// Scheme-keyed files (opencode://db/<session-id>) have no usable
	// basename — leave the project empty so it folds into Miscellaneous
	// instead of a per-session opaque workstream name.
	if strings.Contains(file, "://") {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(file), ".jsonl")
}

// recordAgentSourceScans persists each source's productivity in meta so a
// source that was productive and whose store is now unavailable surfaces as
// drift (R3b) — one events row per outage plus the Drift flag in agents
// output. An "empty" scan (store healthy, tool just unused today) is not
// drift. Source names and counts only; session content never touches this
// path.
func recordAgentSourceScans(db *sql.DB, statuses []sourceScanStatus) {
	if db == nil {
		return
	}
	for i := range statuses {
		st := &statuses[i]
		seenKey := "agent_source_seen:" + st.Source
		driftKey := "agent_source_drifted:" + st.Source
		if st.Sessions > 0 || st.Status != "unavailable" {
			// Productive or healthy-but-idle — not drifted. Clear any
			// outstanding drift marker so a later outage logs fresh.
			if st.Sessions > 0 {
				db.Exec(`INSERT INTO meta(k, v) VALUES(?, '1')
				  ON CONFLICT(k) DO NOTHING`, seenKey)
			}
			db.Exec(`DELETE FROM meta WHERE k=?`, driftKey)
			continue
		}
		var v string
		if err := db.QueryRow(`SELECT v FROM meta WHERE k=?`, seenKey).Scan(&v); err != nil || v != "1" {
			continue // never productive — a broken store isn't drift
		}
		st.Drift = true
		if err := db.QueryRow(`SELECT v FROM meta WHERE k=?`, driftKey).Scan(&v); err == nil && v == "1" {
			continue // outage already reported — don't spam events per scan
		}
		db.Exec(`INSERT INTO meta(k, v) VALUES(?, '1')
		  ON CONFLICT(k) DO NOTHING`, driftKey)
		logEvent(db, "agent_source_drift",
			fmt.Sprintf("source=%s sessions=0 status=%s", st.Source, st.Status))
	}
}

func printAgentSessions(db *sql.DB, cfg Config, d time.Time, jsonOut, recaps bool) {
	srcs, sessions, statuses := scanAgentDay(db, d)
	defer closeAgentSources(srcs)
	if recaps {
		// Reuse the scan's adapters so DB-backed stores (and any WAL temp
		// copies) open once per pass, not once per phase.
		attachRecaps(db, cfg, sessions, srcs...)
	}
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]any{
			"date":           d.Local().Format("2006-01-02"),
			"sessions":       sessions,
			"count":          len(sessions),
			"sources":        statuses,
			"recaps_enabled": cfg.AgentRecaps,
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
				start, end, s.Source, truncTitle(s.Project), s.Messages, truncTitle(s.Title))
			if s.Recap != "" {
				fmt.Printf("         └─ %s\n", s.Recap)
			}
		}
	}
	if !cfg.AgentRecaps && len(sessions) > 0 {
		fmt.Println("note: agent_recaps is off — `dayflow config set agent_recaps true` enables recap generation" +
			" (sends a bounded, scrubbed excerpt to your chat provider)")
	}
	printDriftNotes(statuses)
}
