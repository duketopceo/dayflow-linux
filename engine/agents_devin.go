package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// devinSource adapts the Devin CLI session store to the agentSource seam.
// ~/.local/share/devin/cli/sessions.db indexes sessions (id, title,
// working_directory, epoch-second timestamps) with chat_message JSON per
// node in message_nodes; transcripts/<session_id>.json holds the same
// conversation in ATIF-v1.x form for sessions the DB lacks. Both stores are
// undocumented — every failure degrades to "no sessions", never a hard
// error (R3). storeCache shares one read handle per store across a pass.
type devinSource struct{ storeCache }

func (d *devinSource) Name() string { return "devin" }

// devinDir is the Devin CLI store root; DAYFLOW_DEVIN_DIR overrides it for
// tests.
func devinDir() string {
	if p := os.Getenv("DAYFLOW_DEVIN_DIR"); p != "" {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "devin", "cli")
}

// devinMessage is one message's recap-relevant fields, from either a
// message_nodes row or an ATIF transcript step.
type devinMessage struct {
	id        string // node_id or step index — fingerprint identity
	role      string // "user" | "assistant" | other
	created   int64  // epoch s
	text      string
	bodyLen   int64 // chat_message byte length — fingerprint content signal
	userInput bool  // real typed input, not an injected continuation/context
}

func (d *devinSource) Scan(s, e time.Time) ([]AgentSession, scanNote) {
	dir := devinDir()
	if _, err := os.Stat(dir); err != nil {
		return nil, scanNote{text: "store dir not found", absent: true}
	}
	var out []AgentSession
	var notes, missing []string
	healthy := false
	sessions, known, note := d.scanDB(filepath.Join(dir, "sessions.db"), s, e)
	out = append(out, sessions...)
	switch {
	case note == noteStoreMissing:
		missing = append(missing, "sessions.db")
	case note != "":
		// Identifiers and sizes only — never message content (R3b).
		notes = append(notes, "sessions.db: "+note)
		appendLog("devin scan sessions.db: " + note)
	}
	if note == "" || len(sessions) > 0 {
		healthy = true
	}
	ts, tnote := scanDevinTranscripts(filepath.Join(dir, "transcripts"), known, s, e)
	out = append(out, ts...)
	switch {
	case tnote == noteStoreMissing:
		missing = append(missing, "transcripts")
	case tnote != "":
		notes = append(notes, "transcripts: "+tnote)
		appendLog("devin scan transcripts: " + tnote)
	}
	if tnote == "" || len(ts) > 0 {
		healthy = true
	}
	// A missing sub-store is the idle case: its note is suppressed when a
	// sibling scanned clean or produced sessions. When every store is
	// absent the detail is still surfaced — as absence ("empty"), not
	// corruption, so a store that never existed can't flag drift.
	if !healthy {
		for _, b := range missing {
			notes = append(notes, b+": "+noteStoreMissing)
		}
	}
	return out, scanNote{
		text:   strings.Join(notes, "; "),
		absent: len(missing) == 2,
	}
}

// scanDB lists sessions with at least one message inside [s,e) — the same
// message-timestamp overlap rule the JSONL scanners apply. known holds
// every session id present in the sessions table so scanDevinTranscripts
// only picks up sessions the DB lacks, never duplicates. The store handle
// comes from the adapter's per-pass cache (KTD2).
func (d *devinSource) scanDB(path string, s, e time.Time) (sessions []AgentSession, known map[string]bool, note string) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil, noteStoreMissing
	}
	st, err := d.open(path)
	if err != nil {
		return nil, nil, "store unreadable: " + err.Error()
	}
	if !sqliteTableExists(st.db, "sessions") {
		return nil, nil, "sessions table missing"
	}
	known, err = devinKnownIDs(st.db)
	if err != nil {
		return nil, nil, "sessions query failed: " + err.Error()
	}
	candidates, err := devinCandidates(st.db, s.Unix(), e.Unix())
	if err != nil {
		return nil, known, "session query failed: " + err.Error()
	}
	failures := 0
	for _, c := range candidates {
		msgs, err := devinDBMessages(st.db, c.id, true)
		if err != nil {
			// Per-session extraction errors (e.g. chat_message column
			// dropped) must not silently read as "empty" — count them
			// into a note so the status reports unavailable (R3b).
			failures++
			continue
		}
		if len(msgs) == 0 {
			continue
		}
		if sess, ok := devinSession(c.dir, c.title,
			"devin://sessions.db/"+c.id, path, c.id, msgs); ok {
			sessions = append(sessions, sess)
		}
	}
	if failures > 0 {
		return sessions, known, fmt.Sprintf("%d session(s) failed to read", failures)
	}
	return sessions, known, ""
}

func devinKnownIDs(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT id FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		known[id] = true
	}
	return known, rows.Err()
}

func devinCandidates(db *sql.DB, sSec, eSec int64) ([]sessionCandidate, error) {
	rows, err := db.Query(`SELECT s.id, s.title, s.working_directory FROM sessions s
	  WHERE s.id IN (SELECT session_id FROM message_nodes
	    WHERE created_at >= ? AND created_at < ?)
	  ORDER BY s.created_at`, sSec, eSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionCandidate
	for rows.Next() {
		var c sessionCandidate
		var title, dir sql.NullString
		if err := rows.Scan(&c.id, &title, &dir); err != nil {
			return nil, err
		}
		c.title, c.dir = title.String, dir.String
		out = append(out, c)
	}
	return out, rows.Err()
}

// devinChatMessage is the message_nodes.chat_message JSON payload: role and
// plain-text content, with metadata.is_user_input distinguishing real user
// turns from injected continuations.
type devinChatMessage struct {
	Role     string          `json:"role"`
	Content  json.RawMessage `json:"content"`
	Metadata struct {
		IsUserInput json.RawMessage `json:"is_user_input"`
	} `json:"metadata"`
}

// devinDBMessages loads one session's message rows. withText=false is the
// cheap fingerprint path (node ids + timestamps only); withText=true also
// decodes chat_message for role/text/userInput.
func devinDBMessages(db *sql.DB, sessionID string, withText bool) ([]devinMessage, error) {
	col := "''"
	if withText {
		col = "chat_message"
	}
	rows, err := db.Query(`SELECT node_id, created_at, `+col+
		`, COALESCE(LENGTH(chat_message), 0) FROM message_nodes
	  WHERE session_id = ? ORDER BY node_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []devinMessage
	for rows.Next() {
		var nodeID int64
		var m devinMessage
		var raw string
		if err := rows.Scan(&nodeID, &m.created, &raw, &m.bodyLen); err != nil {
			return nil, err
		}
		m.id = strconv.FormatInt(nodeID, 10)
		if withText {
			var cm devinChatMessage
			if json.Unmarshal([]byte(raw), &cm) == nil {
				m.role = cm.Role
				m.text = contentText(cm.Content)
				ui := strings.TrimSpace(string(cm.Metadata.IsUserInput))
				m.userInput = ui == "true" || ui == "1"
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// scanDevinTranscripts parses transcripts/*.json for sessions the DB lacks.
// ATIF schema drift skips just that file — the rest still scan.
func scanDevinTranscripts(dir string, known map[string]bool, s, e time.Time) ([]AgentSession, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// A missing transcripts dir is absence, not corruption — the
		// sentinel lets Scan apply the all-sub-stores-absent taxonomy.
		if os.IsNotExist(err) {
			return nil, noteStoreMissing
		}
		return nil, "transcripts unreadable: " + err.Error()
	}
	var out []AgentSession
	skipped := 0
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		info, err := ent.Info()
		// Same mtime bound as jsonlFiles: a file modified before s can hold
		// no step timestamped inside [s,e). No upper bound — a session
		// spanning midnight is written after e.
		if err != nil || info.ModTime().Before(s) {
			continue
		}
		p := filepath.Join(dir, ent.Name())
		msgs, sid, ok := parseDevinTranscript(p)
		if !ok {
			skipped++
			continue
		}
		if known[sid] {
			continue
		}
		sess, ok := devinSession("", "",
			"devin://transcript/"+ent.Name(), p, sid, msgs)
		if !ok {
			continue
		}
		if sess.Start >= e.Unix() || sess.End < s.Unix() {
			continue
		}
		out = append(out, sess)
	}
	if skipped > 0 {
		return out, fmt.Sprintf("%d transcript(s) skipped (unparseable or unrecognized schema_version)", skipped)
	}
	return out, ""
}

// parseDevinTranscript decodes one ATIF transcript. ATIF-v1.x is the only
// known generation; anything else is schema drift — skip the file.
func parseDevinTranscript(path string) ([]devinMessage, string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	var tr struct {
		SchemaVersion string `json:"schema_version"`
		SessionID     string `json:"session_id"`
		Steps         []struct {
			StepID    int    `json:"step_id"`
			Timestamp string `json:"timestamp"`
			Source    string `json:"source"` // "user" | "agent" | "system"
			Message   string `json:"message"`
		} `json:"steps"`
	}
	if json.Unmarshal(raw, &tr) != nil || !strings.HasPrefix(tr.SchemaVersion, "ATIF-v1.") {
		return nil, "", false
	}
	sid := tr.SessionID
	if sid == "" {
		sid = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	msgs := make([]devinMessage, 0, len(tr.Steps))
	for _, st := range tr.Steps {
		m := devinMessage{id: "step-" + strconv.Itoa(st.StepID)}
		m.created = unixTs(st.Timestamp)
		switch st.Source {
		case "user":
			// Transcript user steps are real typed input — the ATIF
			// equivalent of is_user_input on DB rows.
			m.role, m.userInput, m.text = "user", true, st.Message
		case "agent":
			m.role, m.text = "assistant", st.Message
		default:
			m.role = st.Source
		}
		msgs = append(msgs, m)
	}
	return msgs, sid, true
}

// devinTurns normalizes messages to the shared turn shape. Only real typed
// input (is_user_input / ATIF user steps) is usable — injected continuation
// prompts carry nothing to title or excerpt.
func devinTurns(msgs []devinMessage) []sessionTurn {
	out := make([]sessionTurn, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, sessionTurn{
			role: m.role, text: m.text, unixTs: m.created,
			usableUser: m.role == "user" && m.userInput,
		})
	}
	return out
}

// devinSession folds one session's messages into an AgentSession. Sessions
// with no real user input are skipped — injected continuation prompts carry
// nothing to title or excerpt. Project comes only from the DB's
// working_directory; transcript-only sessions have no cwd and stay empty
// rather than guessing from the session slug.
func devinSession(cwd, title, fileKey, store, id string, msgs []devinMessage) (AgentSession, bool) {
	sess := AgentSession{
		Source:    "devin",
		Cwd:       cwd,
		Title:     truncTitle(title),
		File:      fileKey,
		store:     store,
		sessionID: id,
	}
	firstUser, userMsgs := foldSession(&sess, devinTurns(msgs))
	if userMsgs == 0 || sess.Start == 0 {
		return AgentSession{}, false
	}
	if sess.Title == "" {
		sess.Title = truncTitle(firstUser)
	}
	if sess.Cwd != "" {
		sess.Project = filepath.Base(sess.Cwd)
	}
	return sess, true
}

// devinLocate resolves a session's message store: sess.store when Scan
// built it, else the synthetic File key —
// "devin://sessions.db/<id>" or "devin://transcript/<name>.json".
func devinLocate(sess AgentSession) (path, sessionID string, transcript bool) {
	if sess.store != "" {
		return sess.store, sess.sessionID, strings.HasSuffix(sess.store, ".json")
	}
	rest, ok := strings.CutPrefix(sess.File, "devin://")
	if !ok {
		return "", "", false
	}
	dir := devinDir()
	if name, ok := strings.CutPrefix(rest, "transcript/"); ok {
		return filepath.Join(dir, "transcripts", name), "", true
	}
	if id, ok := strings.CutPrefix(rest, "sessions.db/"); ok {
		return filepath.Join(dir, "sessions.db"), id, false
	}
	return "", "", false
}

// dbMessages re-queries a DB-backed session's rows at recap time:
// withText=false is the cheap fingerprint path, withText=true adds
// role/text for the excerpt. The store handle comes from the per-pass
// cache — one open per store, not per call.
func (d *devinSource) dbMessages(path, sessionID string, withText bool) ([]devinMessage, bool) {
	st, err := d.open(path)
	if err != nil {
		return nil, false
	}
	msgs, err := devinDBMessages(st.db, sessionID, withText)
	if err != nil || len(msgs) == 0 {
		return nil, false
	}
	return msgs, true
}

// messages re-queries a session's messages at recap time: withText=false is
// the cheap fingerprint path, withText=true adds role/text for the excerpt.
func (d *devinSource) messages(sess AgentSession, withText bool) ([]devinMessage, bool) {
	path, id, transcript := devinLocate(sess)
	if path == "" {
		return nil, false
	}
	if transcript {
		msgs, _, ok := parseDevinTranscript(path)
		return msgs, ok
	}
	return d.dbMessages(path, id, withText)
}

// Fingerprint invalidates the cached recap when the underlying conversation
// changes. Transcript-backed sessions are file-per-session — stat
// mtime+size like the JSONL sources instead of re-parsing the JSON body.
// A shared DB file's mtime would thrash the recap cache on every session's
// writes, so DB-backed sessions content-hash their own message ids +
// timestamps + body lengths (KTD2) — the length folds in a cheap content
// signal so an in-place rewrite that keeps ids and timestamps still
// invalidates the recap, without decoding the JSON bodies.
func (d *devinSource) Fingerprint(sess AgentSession) (recapFingerprint, bool) {
	path, id, transcript := devinLocate(sess)
	if path == "" {
		return recapFingerprint{}, false
	}
	if transcript {
		return fingerprint(path)
	}
	msgs, ok := d.dbMessages(path, id, false)
	if !ok {
		return recapFingerprint{}, false
	}
	h := sha256.New()
	var b [8]byte
	for _, m := range msgs {
		h.Write([]byte(m.id))
		h.Write([]byte{0})
		binary.BigEndian.PutUint64(b[:], uint64(m.created))
		h.Write(b[:])
		binary.BigEndian.PutUint64(b[:], uint64(m.bodyLen))
		h.Write(b[:])
	}
	return hashFingerprint(h.Sum(nil)), true
}

// Excerpt re-queries first/last real user input and last assistant text,
// rendered through the shared excerpt builder so the egress shape is
// identical to the other sources.
func (d *devinSource) Excerpt(sess AgentSession) string {
	return excerptFromTurns(d.Turns(sess))
}

// Turns returns the session's full normalized turn list for the briefing —
// same message path as the excerpt (DB rows or ATIF transcript).
func (d *devinSource) Turns(sess AgentSession) []sessionTurn {
	msgs, ok := d.messages(sess, true)
	if !ok {
		return nil
	}
	return devinTurns(msgs)
}
