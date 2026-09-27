package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// opencodeSource adapts OpenCode's sqlite session store to the agentSource
// seam. Two store generations exist side by side on disk: opencode.db keeps
// messages in `message` + `part`, while opencode-next.db keeps them in
// `session_message`. Both share the `session` table with epoch-ms
// timestamps. The stores are undocumented and version-fragile — every
// failure degrades to "no sessions", never a hard error (R3).
type opencodeSource struct{}

func (o opencodeSource) Name() string { return "opencode" }

// opencodeDBPath is the primary store; DAYFLOW_OPENCODE_DB overrides it for
// tests.
func opencodeDBPath() string {
	if p := os.Getenv("DAYFLOW_OPENCODE_DB"); p != "" {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "opencode", "opencode.db")
}

// opencodeDBPaths is the primary store plus opencode-next.db alongside it
// when present.
func opencodeDBPaths() []string {
	primary := opencodeDBPath()
	paths := []string{primary}
	next := filepath.Join(filepath.Dir(primary), "opencode-next.db")
	if next != primary {
		if _, err := os.Stat(next); err == nil {
			paths = append(paths, next)
		}
	}
	return paths
}

func (o opencodeSource) Scan(s, e time.Time) ([]AgentSession, string) {
	var out []AgentSession
	var notes []string
	for _, path := range opencodeDBPaths() {
		sessions, note := scanOpencodeDB(path, s, e)
		out = append(out, sessions...)
		if note != "" {
			// Identifiers and sizes only — never message content (R3b).
			notes = append(notes, filepath.Base(path)+": "+note)
			appendLog("opencode scan " + filepath.Base(path) + ": " + note)
		}
	}
	return out, strings.Join(notes, "; ")
}

// opencodeStore wraps a read connection plus the temp dir to remove when the
// live DB couldn't be opened in place and a copy was made.
type opencodeStore struct {
	db     *sql.DB
	tmpDir string
}

func (s *opencodeStore) close() {
	s.db.Close()
	if s.tmpDir != "" {
		os.RemoveAll(s.tmpDir)
	}
}

// openOpencodeStore opens path read-only. A live WAL-mode store can refuse a
// plain ro open (shm recovery needs write access), so on failure retry once
// against a temp-dir copy of db+wal+shm before degrading.
func openOpencodeStore(path string) (*opencodeStore, error) {
	const ro = "?mode=ro&_pragma=busy_timeout(3000)&_pragma=query_only(1)"
	if db, err := sql.Open("sqlite", "file:"+path+ro); err == nil {
		if err := db.Ping(); err == nil {
			return &opencodeStore{db: db}, nil
		}
		db.Close()
	}
	// Retry on a temp copy — opened read-write, which is safe because the
	// copy is disposable and WAL recovery may need to write shm.
	dir, err := os.MkdirTemp("", "dayflow-opencode-*")
	if err != nil {
		return nil, err
	}
	tmp := filepath.Join(dir, "opencode.db")
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
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	ok = true
	return &opencodeStore{db: db, tmpDir: dir}, nil
}

func sqliteTableExists(db *sql.DB, name string) bool {
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// opencodeLayout picks the message-table generation actually carrying data:
// "next" when session_message is populated (or is the only message table),
// "old" when only message+part carry rows, "" on schema drift.
func opencodeLayout(db *sql.DB) string {
	hasSM := sqliteTableExists(db, "session_message")
	hasMsg := sqliteTableExists(db, "message")
	if hasSM {
		var n, nm int
		db.QueryRow(`SELECT COUNT(1) FROM session_message`).Scan(&n)
		if hasMsg {
			db.QueryRow(`SELECT COUNT(1) FROM message`).Scan(&nm)
		}
		if n > 0 || nm == 0 {
			return "next"
		}
	}
	if hasMsg {
		return "old"
	}
	return ""
}

// scanOpencodeDB lists sessions in one store with messages inside [s,e).
// Schema drift or an unreadable store degrades to an empty result + note.
func scanOpencodeDB(path string, s, e time.Time) ([]AgentSession, string) {
	if _, err := os.Stat(path); err != nil {
		return nil, "store not found"
	}
	st, err := openOpencodeStore(path)
	if err != nil {
		return nil, "store unreadable: " + err.Error()
	}
	defer st.close()
	layout := opencodeLayout(st.db)
	if layout == "" {
		return nil, "no session message tables"
	}
	candidates, err := opencodeCandidates(st.db, layout, s.Unix()*1000, e.Unix()*1000)
	if err != nil {
		return nil, "session query failed: " + err.Error()
	}
	var out []AgentSession
	for _, c := range candidates {
		if sess, ok := opencodeSession(st.db, path, layout, c); ok {
			out = append(out, sess)
		}
	}
	return out, ""
}

type ocCandidate struct {
	id, title, dir string
}

// opencodeCandidates returns sessions having at least one message inside
// [sMs,eMs) — the same overlap rule the JSONL scanners apply to message
// timestamps, so a session spanning midnight appears on both days.
func opencodeCandidates(db *sql.DB, layout string, sMs, eMs int64) ([]ocCandidate, error) {
	msgTable := "session_message"
	if layout == "old" {
		msgTable = "message"
	}
	rows, err := db.Query(`SELECT s.id, s.title, s.directory FROM session s
	  WHERE EXISTS (SELECT 1 FROM `+msgTable+` m
	    WHERE m.session_id = s.id AND m.time_created >= ? AND m.time_created < ?)
	  ORDER BY s.time_created`, sMs, eMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ocCandidate
	for rows.Next() {
		var c ocCandidate
		if err := rows.Scan(&c.id, &c.title, &c.dir); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ocMessage is one message row's recap-relevant fields.
type ocMessage struct {
	id      string
	role    string // "user" | "assistant" | other
	created int64  // epoch ms
	updated int64  // epoch ms
	text    string // first text-part payload; empty when withText is false
}

func ocMessages(db *sql.DB, layout, sessionID string, withText bool) ([]ocMessage, error) {
	if layout == "old" {
		return ocMessagesOld(db, sessionID, withText)
	}
	return ocMessagesNext(db, sessionID, withText)
}

// ocMsgData is the session_message.data payload: user rows carry text
// directly, assistant rows carry a content-part array.
type ocMsgData struct {
	Text    string `json:"text"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func ocMsgText(data string) string {
	var d ocMsgData
	if json.Unmarshal([]byte(data), &d) != nil {
		return ""
	}
	if d.Text != "" {
		return d.Text
	}
	for _, p := range d.Content {
		if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
			return p.Text
		}
	}
	return ""
}

func ocMessagesNext(db *sql.DB, sessionID string, withText bool) ([]ocMessage, error) {
	dataCol := "''"
	if withText {
		dataCol = "data"
	}
	rows, err := db.Query(`SELECT id, time_created, time_updated, type, `+dataCol+
		` FROM session_message WHERE session_id = ? ORDER BY seq, time_created, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ocMessage
	for rows.Next() {
		var m ocMessage
		var data string
		if err := rows.Scan(&m.id, &m.created, &m.updated, &m.role, &data); err != nil {
			return nil, err
		}
		if withText {
			m.text = ocMsgText(data)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func ocMessagesOld(db *sql.DB, sessionID string, withText bool) ([]ocMessage, error) {
	rows, err := db.Query(`SELECT id, time_created, time_updated, data
	  FROM message WHERE session_id = ? ORDER BY time_created, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ocMessage
	byID := map[string]int{}
	for rows.Next() {
		var m ocMessage
		var data string
		if err := rows.Scan(&m.id, &m.created, &m.updated, &data); err != nil {
			return nil, err
		}
		var d struct {
			Role string `json:"role"`
		}
		json.Unmarshal([]byte(data), &d)
		m.role = d.Role
		byID[m.id] = len(out)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !withText || len(out) == 0 {
		return out, nil
	}
	// Old-generation text lives in part rows — first "text" part per message
	// (reasoning/tool parts carry text too but are not transcript content).
	parts, err := db.Query(`SELECT message_id, data FROM part
	  WHERE session_id = ? ORDER BY time_created, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer parts.Close()
	for parts.Next() {
		var msgID, data string
		if err := parts.Scan(&msgID, &data); err != nil {
			return nil, err
		}
		i, ok := byID[msgID]
		if !ok || out[i].text != "" {
			continue
		}
		var p struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(data), &p) != nil || p.Type != "text" {
			continue
		}
		out[i].text = p.Text
	}
	return out, parts.Err()
}

// opencodeSession folds one session's message rows into an AgentSession.
// Sessions with no user messages are skipped — there is nothing to title or
// excerpt — and Start/End span the whole session, not just the window.
func opencodeSession(db *sql.DB, path, layout string, c ocCandidate) (AgentSession, bool) {
	msgs, err := ocMessages(db, layout, c.id, true)
	if err != nil || len(msgs) == 0 {
		return AgentSession{}, false
	}
	sess := AgentSession{
		Source: "opencode",
		Cwd:    c.dir,
		Title:  truncTitle(c.title),
		// Stable source-scoped key — not a stat-able path. attachRecaps gets
		// fingerprint + excerpt from the adapter instead (KTD2).
		File:      "opencode://" + filepath.Base(path) + "/" + c.id,
		store:     path,
		sessionID: c.id,
	}
	userMsgs := 0
	var firstUser string
	for _, m := range msgs {
		if m.created > 0 {
			u := m.created / 1000
			if sess.Start == 0 || u < sess.Start {
				sess.Start = u
			}
			if u > sess.End {
				sess.End = u
			}
		}
		if m.role == "user" || m.role == "assistant" {
			sess.Messages++
		}
		if m.role == "user" {
			userMsgs++
			if firstUser == "" {
				firstUser = strings.TrimSpace(m.text)
			}
		}
	}
	if userMsgs == 0 || sess.Start == 0 {
		return AgentSession{}, false
	}
	// "New session - <timestamp>" placeholders carry no signal — fall back to
	// the first user prompt, matching the JSONL title convention.
	if sess.Title == "" || strings.HasPrefix(sess.Title, "New session") {
		sess.Title = truncTitle(firstUser)
	}
	sess.Project = projectName(sess.Cwd, sess.File)
	return sess, true
}

// messages re-queries a session's rows at recap time: withText=false is the
// cheap fingerprint path (ids + update times only), withText=true adds the
// text payload for the excerpt.
func (o opencodeSource) messages(sess AgentSession, withText bool) ([]ocMessage, bool) {
	path, id := sess.store, sess.sessionID
	if path == "" || id == "" {
		// Reconstruct the location from the File key when the session was
		// built outside Scan — opencode://<db basename>/<session id>.
		rest, ok := strings.CutPrefix(sess.File, "opencode://")
		if !ok {
			return nil, false
		}
		base, sid, ok := strings.Cut(rest, "/")
		if !ok {
			return nil, false
		}
		path = filepath.Join(filepath.Dir(opencodeDBPath()), base)
		id = sid
	}
	st, err := openOpencodeStore(path)
	if err != nil {
		return nil, false
	}
	defer st.close()
	layout := opencodeLayout(st.db)
	if layout == "" {
		return nil, false
	}
	msgs, err := ocMessages(st.db, layout, id, withText)
	if err != nil || len(msgs) == 0 {
		return nil, false
	}
	return msgs, true
}

// Fingerprint content-hashes the session's message ids + update times. A
// shared DB file's mtime would thrash the recap cache on every session's
// writes, so DB sources key invalidation on their own message set (KTD2).
func (o opencodeSource) Fingerprint(sess AgentSession) (recapFingerprint, bool) {
	msgs, ok := o.messages(sess, false)
	if !ok {
		return recapFingerprint{}, false
	}
	h := sha256.New()
	var b [8]byte
	for _, m := range msgs {
		h.Write([]byte(m.id))
		h.Write([]byte{0})
		binary.BigEndian.PutUint64(b[:], uint64(m.updated))
		h.Write(b[:])
	}
	sum := h.Sum(nil)
	return recapFingerprint{
		Mtime: int64(binary.BigEndian.Uint64(sum[:8])),
		Size:  int64(binary.BigEndian.Uint64(sum[8:16])),
	}, true
}

// Excerpt re-queries first/last user text and last assistant text, rendered
// through the shared excerpt builder so the egress shape is identical to the
// JSONL sources.
func (o opencodeSource) Excerpt(sess AgentSession) string {
	msgs, ok := o.messages(sess, true)
	if !ok {
		return ""
	}
	var firstUser, lastUser, lastAssistant string
	for _, m := range msgs {
		text := strings.TrimSpace(m.text)
		switch m.role {
		case "user":
			if text == "" || isEnvelopeText(text) {
				continue
			}
			if firstUser == "" {
				firstUser = text
			}
			lastUser = text
		case "assistant":
			if text != "" {
				lastAssistant = text
			}
		}
	}
	return buildExcerpt(firstUser, lastUser, lastAssistant)
}
