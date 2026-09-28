package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// cursorSource adapts Cursor's globalStorage state.vscdb to the agentSource
// seam. The store is undocumented and version-fragile; the layouts observed:
//
//	composerHeaders table — one row per composer: composerId, workspaceId,
//	  createdAt/lastUpdatedAt (epoch ms), isArchived/isSubagent flags, and a
//	  `value` JSON blob (type "head") carrying isDraft/name-ish fields.
//	cursorDiskKV — generic key/value (value is BLOB but usually JSON text):
//	  composerData:<composerId>   — per-composer JSON: fullConversationHeadersOnly
//	                              is the ordered [{bubbleId, type}] list (type
//	                              1=user, 2=assistant); conversationMap may hold
//	                              the same bubbles inline.
//	  bubbleId:<composerId>:<bid> — one JSON message body {type, text, ...}.
//	  composer.content.<hash>     — content-addressed doc blobs (markdown/HTML),
//	                              NOT keyed by composerId — ignored unless an
//	                              exact composer.content.<composerId> key exists.
//	ItemTable — VS Code generic KV; some versions index composers under
//	  composer.composerData instead of the composerHeaders table.
//
// Every failure degrades to "no sessions", never a hard error (R3); logs and
// notes carry identifiers and sizes only, never content (R3b). storeCache
// shares one read handle per store across a pass.
type cursorSource struct{ storeCache }

func (c *cursorSource) Name() string { return "cursor" }

// cursorDBPath is the globalStorage store; DAYFLOW_CURSOR_DB overrides it for
// tests.
func cursorDBPath() string {
	if p := os.Getenv("DAYFLOW_CURSOR_DB"); p != "" {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "Cursor", "User", "globalStorage", "state.vscdb")
}

// cursorWorkspacesRoot backs workspaceId → project-dir mapping via
// <root>/<workspaceId>/workspace.json; DAYFLOW_CURSOR_WORKSPACES overrides it
// for tests.
func cursorWorkspacesRoot() string {
	if p := os.Getenv("DAYFLOW_CURSOR_WORKSPACES"); p != "" {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "Cursor", "User", "workspaceStorage")
}

// cursorHeader is one composerHeaders row's recap-relevant fields. Timestamps
// are epoch ms; 0 means absent.
type cursorHeader struct {
	id, workspace, name string
	created, updated    int64
	draft               bool
}

// cursorNote logs a degraded-store note (identifiers/sizes only, R3b) and
// returns it for the scan status.
func cursorNote(msg string) string {
	appendLog("cursor scan: " + msg)
	return msg
}

func (c *cursorSource) Scan(s, e time.Time) ([]AgentSession, scanNote) {
	path := cursorDBPath()
	if _, err := os.Stat(path); err != nil {
		return nil, scanNote{text: noteStoreMissing, absent: true}
	}
	st, err := c.open(path)
	if err != nil {
		return nil, scanNote{text: cursorNote("store unreadable: " + err.Error())}
	}

	hasKV := sqliteTableExists(st.db, "cursorDiskKV")
	var headers []cursorHeader
	var note string
	switch {
	case sqliteTableExists(st.db, "composerHeaders"):
		headers, err = cursorHeaderRows(st.db, s.Unix()*1000, e.Unix()*1000)
		if err != nil {
			return nil, scanNote{text: cursorNote("composerHeaders query failed: " + err.Error())}
		}
	case hasKV:
		// Older layout without the headers table — index rows live as
		// composerData:<id> blobs. Defensive fallback; may still yield
		// sessions if the blobs carry their own timestamps.
		headers, err = cursorHeaderFallback(st.db, s.Unix()*1000, e.Unix()*1000)
		if err != nil {
			return nil, scanNote{text: cursorNote("composerData scan failed: " + err.Error())}
		}
	default:
		return nil, scanNote{text: cursorNote("no composer tables")}
	}
	if !hasKV {
		return nil, scanNote{text: cursorNote("cursorDiskKV table missing")}
	}
	if len(headers) == 0 {
		return nil, scanNote{}
	}

	var out []AgentSession
	skipped := 0
	for _, h := range headers {
		if h.draft {
			continue // unsent composer drafts — never sessions
		}
		if sess, ok, broken := cursorSession(st.db, path, h); ok {
			out = append(out, sess)
		} else if broken {
			skipped++
		}
	}
	if skipped > 0 {
		note = cursorNote(fmt.Sprintf(
			"%d composer(s) in range failed to parse", skipped))
	}
	return out, scanNote{text: note}
}

// cursorHeaderRows lists composerHeaders overlapping [sMs,eMs): a composer
// spans [createdAt, lastUpdatedAt] (both epoch ms; a NULL update time means
// createdAt). The overlap filter runs in Go, not SQL — sqlite type ordering
// would silently drop rows if a version stores timestamps as text. Column
// types drift between versions, so values scan as any and coerce.
func cursorHeaderRows(db *sql.DB, sMs, eMs int64) ([]cursorHeader, error) {
	rows, err := db.Query(`SELECT composerId, workspaceId, createdAt,
	  lastUpdatedAt, value FROM composerHeaders ORDER BY createdAt`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []cursorHeader
	for rows.Next() {
		var h cursorHeader
		var ws, created, updated, value any
		if err := rows.Scan(&h.id, &ws, &created, &updated, &value); err != nil {
			return nil, err
		}
		h.workspace = cursorStr(ws)
		h.created = cursorMs(created)
		h.updated = cursorMs(updated)
		if h.updated == 0 {
			h.updated = h.created
		}
		if h.created == 0 || h.created >= eMs || h.updated < sMs {
			continue
		}
		// Parse the value blob only for in-range rows — out-of-window
		// composers skip the JSON decode entirely.
		cursorHeaderMeta(cursorBytes(value), &h)
		out = append(out, h)
	}
	return out, rows.Err()
}

// cursorHeaderFallback builds headers from composerData:<id> blobs when the
// composerHeaders table doesn't exist — the blobs carry composerId +
// createdAt/lastUpdatedAt themselves.
func cursorHeaderFallback(db *sql.DB, sMs, eMs int64) ([]cursorHeader, error) {
	rows, err := db.Query(`SELECT key, value FROM cursorDiskKV
	  WHERE key LIKE 'composerData:%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []cursorHeader
	for rows.Next() {
		var key string
		var value any
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		var d struct {
			ComposerID string `json:"composerId"`
			CreatedAt  any    `json:"createdAt"`
			UpdatedAt  any    `json:"lastUpdatedAt"`
			IsDraft    bool   `json:"isDraft"`
			Name       string `json:"name"`
		}
		if json.Unmarshal(cursorBytes(value), &d) != nil {
			continue
		}
		h := cursorHeader{
			id:      d.ComposerID,
			name:    d.Name,
			created: cursorMs(d.CreatedAt),
			updated: cursorMs(d.UpdatedAt),
			draft:   d.IsDraft,
		}
		if h.id == "" {
			h.id = strings.TrimPrefix(key, "composerData:")
		}
		if h.updated == 0 {
			h.updated = h.created
		}
		if h.created == 0 || h.created >= eMs || h.updated < sMs {
			continue
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// cursorHeaderMeta pulls recap-relevant bits out of the header `value` JSON:
// draft markers (drafts are never sessions) and an optional display name.
func cursorHeaderMeta(raw []byte, h *cursorHeader) {
	if len(raw) == 0 {
		return
	}
	var v struct {
		IsDraft bool   `json:"isDraft"`
		Name    string `json:"name"`
		Text    string `json:"text"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return
	}
	h.draft = v.IsDraft ||
		strings.HasPrefix(h.id, "draft-") || h.id == "empty-state-draft"
	if v.Name != "" {
		h.name = v.Name
	} else if v.Text != "" {
		h.name = v.Text
	}
}

// cursorStr/cursorBytes/cursorMs coerce loosely-typed sqlite values — the
// schema promises INTEGER/TEXT but the store is undocumented, so accept
// ints, floats, byte blobs, numeric strings, and RFC3339 strings.
func cursorStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

func cursorBytes(v any) []byte {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return t
	case string:
		return []byte(t)
	default:
		return []byte(fmt.Sprint(t))
	}
}

func cursorMs(v any) int64 {
	switch t := v.(type) {
	case nil:
		return 0
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case []byte:
		return cursorMs(string(t))
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n
		}
		// RFC3339Nano already covers the no-fraction RFC3339 shape.
		if ts, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return ts.UnixMilli()
		}
	}
	return 0
}

// cursorTurn is one message-ish unit extracted from a composer: enough for
// the excerpt, the message count, and the fingerprint.
type cursorTurn struct {
	id   string // bubbleId or synthesized — fingerprint identity
	role string // "user" | "assistant" | "" for other kinds
	text string
	ms   int64 // epoch ms when the bubble carries a timestamp; else 0
}

// cursorKV fetches one cursorDiskKV value; the column is BLOB but values are
// usually JSON text.
func cursorKV(db *sql.DB, key string) ([]byte, bool) {
	var v any
	if err := db.QueryRow(`SELECT value FROM cursorDiskKV WHERE key=?`, key).
		Scan(&v); err != nil {
		return nil, false
	}
	b := cursorBytes(v)
	if len(b) == 0 {
		return nil, false
	}
	return b, true
}

// cursorMessages loads one composer's turns. withText=false is the cheap
// fingerprint path — header ids/types + update times only, no bubble bodies;
// withText=true also pulls bubbleId rows for turn text.
// cursorMessages returns the composer's turns, its last-update ms, and
// parseErr — true only when a composerData blob existed but failed to decode
// and no fallback produced turns either (real degradation). A composer that
// simply has no user turns (drafts, archived shells, assistant-only) is a
// normal idle state, not an error.
func cursorMessages(db *sql.DB, composerID string, withText bool) ([]cursorTurn, int64, bool) {
	var updated int64
	var turns []cursorTurn
	var parseErr bool

	raw, ok := cursorKV(db, "composerData:"+composerID)
	if ok {
		var d struct {
			Headers []struct {
				BubbleID string          `json:"bubbleId"`
				Type     json.RawMessage `json:"type"`
			} `json:"fullConversationHeadersOnly"`
			Map     json.RawMessage `json:"conversationMap"`
			Updated any             `json:"lastUpdatedAt"`
			Version int             `json:"_v"`
		}
		if json.Unmarshal(raw, &d) == nil {
			updated = cursorMs(d.Updated)
			seen := map[string]bool{}
			// One batched load of every bubbleId:<id>:* row instead of a
			// query per header (the N+1 path).
			var bodies map[string][]byte
			if withText {
				bodies = cursorBubbleBodies(db, composerID)
			}
			for _, hd := range d.Headers {
				if hd.BubbleID == "" {
					continue
				}
				seen[hd.BubbleID] = true
				t := cursorTurn{id: hd.BubbleID, role: cursorRole(hd.Type, nil)}
				if bt, ok := cursorTurnFromBubble(bodies[hd.BubbleID]); ok {
					if bt.role != "" {
						t.role = bt.role
					}
					t.text = bt.text
					t.ms = bt.ms
				}
				turns = append(turns, t)
			}
			// Inline bubbles not in the header list (version drift between
			// the two index forms) append in document order.
			if kvs, err := orderedObject(d.Map); err == nil {
				for _, kv := range kvs {
					if seen[kv.k] {
						continue
					}
					t, ok := cursorTurnFromBubble(kv.v)
					if !ok {
						continue
					}
					if t.id == "" {
						t.id = kv.k
					}
					if !withText {
						t.text = ""
					}
					turns = append(turns, t)
				}
			}
		} else {
			parseErr = true
		}
	}

	if len(turns) == 0 {
		// bubbleId:<composerId>:<bid> rows with no composerData index —
		// ordered by key (ordering is approximate; excerpt-worthy either way).
		turns = cursorBubbleRows(db, composerID, withText)
	}
	if len(turns) == 0 {
		// Plan-described alternate layout: a composer.content.<composerId>
		// JSON blob holding the whole conversation.
		if raw, ok := cursorKV(db, "composer.content."+composerID); ok {
			turns = cursorCollectTurns(raw, withText)
		}
	}
	return turns, updated, parseErr && len(turns) == 0
}

// cursorBubbleBodies loads every bubbleId:<composerId>:* row in one query —
// the alternative is one cursorDiskKV lookup per turn.
func cursorBubbleBodies(db *sql.DB, composerID string) map[string][]byte {
	rows, err := db.Query(`SELECT key, value FROM cursorDiskKV
	  WHERE key LIKE ? ORDER BY key`, "bubbleId:"+composerID+":%")
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var key string
		var value any
		if err := rows.Scan(&key, &value); err != nil {
			return nil
		}
		out[strings.TrimPrefix(key, "bubbleId:"+composerID+":")] = cursorBytes(value)
	}
	return out
}

// cursorBubbleRows enumerates bubbleId:<composerId>:* rows directly,
// ordered by key (ordering is approximate; excerpt-worthy either way).
func cursorBubbleRows(db *sql.DB, composerID string, withText bool) []cursorTurn {
	bodies := cursorBubbleBodies(db, composerID)
	bids := make([]string, 0, len(bodies))
	for bid := range bodies {
		bids = append(bids, bid)
	}
	sort.Strings(bids) // same order as ORDER BY key — the prefix is constant
	var out []cursorTurn
	for _, bid := range bids {
		t, ok := cursorTurnFromBubble(bodies[bid])
		if !ok {
			continue
		}
		if t.id == "" {
			t.id = bid
		}
		if !withText {
			t.text = ""
		}
		out = append(out, t)
	}
	return out
}

// cursorRole maps a bubble type/role marker to "user"|"assistant"|"".
// Numeric type 1 is a user turn, 2 an assistant turn; string forms seen in
// the wild include "user", "ai", "assistant". Anything else (tool bubbles,
// capability blobs) is not transcript text.
func cursorRole(typ, role json.RawMessage) string {
	r := strings.ToLower(strings.TrimSpace(cursorStrRaw(role)))
	switch r {
	case "user", "human":
		return "user"
	case "assistant", "ai", "agent":
		return "assistant"
	}
	switch strings.TrimSpace(cursorStrRaw(typ)) {
	case "user", "human":
		return "user"
	case "assistant", "ai", "agent":
		return "assistant"
	case "1":
		return "user"
	case "2":
		return "assistant"
	}
	return ""
}

// cursorStrRaw renders a JSON scalar as a bare string (numbers decode
// unquoted).
func cursorStrRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// cursorTurnFromBubble decodes one bubble object ({type, text, ...}).
func cursorTurnFromBubble(raw json.RawMessage) (cursorTurn, bool) {
	var b struct {
		Type      json.RawMessage `json:"type"`
		Role      json.RawMessage `json:"role"`
		BubbleID  string          `json:"bubbleId"`
		ID        string          `json:"id"`
		Text      string          `json:"text"`
		RawText   string          `json:"rawText"`
		RichText  json.RawMessage `json:"richText"`
		CreatedAt any             `json:"createdAt"`
		Timestamp any             `json:"timestamp"`
		UpdatedAt any             `json:"lastUpdatedAt"`
		Timing    struct {
			Start any `json:"clientStartTime"`
			End   any `json:"clientEndTime"`
		} `json:"timingInfo"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return cursorTurn{}, false
	}
	t := cursorTurn{
		id:   b.BubbleID,
		role: cursorRole(b.Type, b.Role),
		text: strings.TrimSpace(b.Text),
	}
	if t.id == "" {
		t.id = b.ID
	}
	if t.text == "" {
		t.text = strings.TrimSpace(b.RawText)
	}
	if t.text == "" {
		t.text = strings.TrimSpace(cursorRichText(b.RichText))
	}
	for _, v := range []any{b.CreatedAt, b.Timestamp, b.UpdatedAt, b.Timing.Start, b.Timing.End} {
		if ms := cursorMs(v); ms > 0 {
			t.ms = ms
			break
		}
	}
	return t, true
}

// cursorRichText pulls plain text out of a richText value, which is either a
// plain string or a serialized rich-text document ({root:{children:...}}).
func cursorRichText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		trim := strings.TrimSpace(s)
		if strings.HasPrefix(trim, "{") {
			return strings.Join(jsonTextLeaves([]byte(trim), 0), " ")
		}
		return trim
	}
	return strings.Join(jsonTextLeaves(raw, 0), " ")
}

// jsonTextLeaves collects "text" leaf strings from a rich-text document in
// document order. Depth-bounded; returns nil on non-JSON input.
func jsonTextLeaves(raw []byte, depth int) []string {
	if depth > 32 {
		return nil
	}
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 {
		return nil
	}
	var out []string
	switch trim[0] {
	case '{':
		kvs, err := orderedObject(trim)
		if err != nil {
			return nil
		}
		for _, kv := range kvs {
			if kv.k == "text" {
				var s string
				if json.Unmarshal(kv.v, &s) == nil && s != "" {
					out = append(out, s)
				}
				continue
			}
			out = append(out, jsonTextLeaves(kv.v, depth+1)...)
		}
	case '[':
		var arr []json.RawMessage
		if json.Unmarshal(trim, &arr) != nil {
			return nil
		}
		for _, item := range arr {
			out = append(out, jsonTextLeaves(item, depth+1)...)
		}
	}
	return out
}

// orderedKV is one JSON object member in document order — conversationMap is
// a JSON object whose order is the turn order, so decoding into a Go map
// would lose it.
type orderedKV struct {
	k string
	v json.RawMessage
}

func orderedObject(raw []byte) ([]orderedKV, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("not a JSON object")
	}
	var out []orderedKV
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, fmt.Errorf("non-string object key")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, orderedKV{key, v})
	}
	if _, err := dec.Token(); err != nil { // '}'
		return nil, err
	}
	return out, nil
}

// cursorCollectTurns is the tolerant path for blobs whose shape we don't
// know (composer.content.<id> and friends): walk the JSON in document order
// and keep every object that carries both a role-ish marker and text — that
// pair is what makes a thing a turn. Non-JSON blobs (the hash-addressed
// markdown/HTML docs in the real store) yield nothing.
func cursorCollectTurns(raw []byte, withText bool) []cursorTurn {
	var out []cursorTurn
	var walk func(raw json.RawMessage, depth int)
	walk = func(raw json.RawMessage, depth int) {
		if depth > 64 {
			return
		}
		trim := bytes.TrimSpace(raw)
		if len(trim) == 0 {
			return
		}
		switch trim[0] {
		case '{':
			kvs, err := orderedObject(trim)
			if err != nil {
				return
			}
			if t, ok := cursorTurnFromObject(kvs); ok {
				if !withText {
					t.text = ""
				}
				out = append(out, t)
			}
			for _, kv := range kvs {
				walk(kv.v, depth+1)
			}
		case '[':
			var arr []json.RawMessage
			if json.Unmarshal(trim, &arr) != nil {
				return
			}
			for _, item := range arr {
				walk(item, depth+1)
			}
		}
	}
	walk(raw, 0)
	return out
}

// cursorTurnFromObject recognizes a turn-shaped object: a type/role marker
// plus a text-ish field. IDs come from bubbleId/id; timestamps from the
// usual ms fields.
func cursorTurnFromObject(kvs []orderedKV) (cursorTurn, bool) {
	var typ, role, textRaw json.RawMessage
	var id string
	var ms int64
	for _, kv := range kvs {
		switch kv.k {
		case "type":
			typ = kv.v
		case "role":
			role = kv.v
		case "bubbleId", "id", "messageId":
			if id == "" {
				id = cursorStrRaw(kv.v)
			}
		case "text", "content", "message":
			if textRaw == nil {
				textRaw = kv.v
			}
		case "createdAt", "timestamp", "lastUpdatedAt":
			if ms == 0 {
				var v any
				if json.Unmarshal(kv.v, &v) == nil {
					ms = cursorMs(v)
				}
			}
		}
	}
	r := cursorRole(typ, role)
	if r == "" {
		return cursorTurn{}, false
	}
	text := strings.TrimSpace(cursorStrRaw(textRaw))
	if text == "" && len(textRaw) > 0 {
		text = strings.TrimSpace(contentText(textRaw))
	}
	if text == "" {
		return cursorTurn{}, false
	}
	return cursorTurn{id: id, role: r, text: text, ms: ms}, true
}

// cursorSessionTurns normalizes bubble turns to the shared shape. A user
// turn without text is unusable — nothing to title or excerpt — so
// header-listed bubbles whose bodies are missing count as messages but
// can't anchor a session.
func cursorSessionTurns(turns []cursorTurn) []sessionTurn {
	out := make([]sessionTurn, 0, len(turns))
	for _, t := range turns {
		var ts int64
		if t.ms > 0 {
			ts = t.ms / 1000
		}
		out = append(out, sessionTurn{
			role: t.role, text: t.text, unixTs: ts,
			usableUser: t.role == "user" && strings.TrimSpace(t.text) != "",
		})
	}
	return out
}

// cursorSession folds one composer's turns into an AgentSession. Composers
// with no usable user turns are skipped — drafts, archived shells, and
// content blobs that don't carry text all land here.
func cursorSession(db *sql.DB, path string, h cursorHeader) (AgentSession, bool, bool) {
	turns, _, parseErr := cursorMessages(db, h.id, true)
	if parseErr {
		return AgentSession{}, false, true
	}
	sess := AgentSession{
		Source:    "cursor",
		Title:     truncTitle(h.name),
		File:      "cursor://" + h.id,
		store:     path,
		sessionID: h.id,
	}
	if h.created > 0 {
		sess.Start = h.created / 1000
	}
	if u := h.updated; u > 0 && u/1000 > sess.End {
		sess.End = u / 1000
	}
	firstUser, userTurns := foldSession(&sess, cursorSessionTurns(turns))
	if userTurns == 0 || sess.Start == 0 {
		return AgentSession{}, false, false
	}
	if sess.End < sess.Start {
		sess.End = sess.Start
	}
	if sess.Title == "" {
		sess.Title = truncTitle(firstUser)
	}
	if cwd := cursorWorkspaceFolder(h.workspace); cwd != "" {
		if strings.HasPrefix(cwd, "/") {
			sess.Cwd = cwd
		}
		sess.Project = filepath.Base(cwd)
	}
	return sess, true, false
}

// workspaceIDRe bounds what a composerHeaders workspaceId may look like
// before it joins a workspaceStorage path — anything else is rejected so a
// hostile or drifted id can't escape the storage root.
var workspaceIDRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// cursorWorkspaceFolder maps a composerHeaders workspaceId to a project
// directory via workspaceStorage/<id>/workspace.json (a {"folder": uri}
// file). Returns the decoded URI path — a local filesystem path for file://
// URIs, the remote path for vscode-remote:// ones, "" when unmappable (the
// "empty-window" workspace included).
func cursorWorkspaceFolder(wsid string) string {
	if !workspaceIDRe.MatchString(wsid) {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(cursorWorkspacesRoot(), wsid, "workspace.json"))
	if err != nil {
		return ""
	}
	var w struct {
		Folder string `json:"folder"`
	}
	if json.Unmarshal(raw, &w) != nil || w.Folder == "" {
		return ""
	}
	u, err := url.Parse(w.Folder)
	if err != nil {
		return ""
	}
	p, _ := url.PathUnescape(u.Path)
	if p == "" {
		// Opaque or authority-only URI — nothing usable.
		return ""
	}
	return strings.TrimSuffix(p, "/")
}

// cursorLocate resolves a session's store: sess.store when Scan built it,
// else the synthetic File key — "cursor://<composerId>" opens the default
// (or DAYFLOW_CURSOR_DB) store.
func cursorLocate(sess AgentSession) (path, composerID string) {
	if sess.store != "" && sess.sessionID != "" {
		return sess.store, sess.sessionID
	}
	id, ok := strings.CutPrefix(sess.File, "cursor://")
	if !ok || id == "" || strings.Contains(id, "/") {
		return "", ""
	}
	return cursorDBPath(), id
}

// turns re-queries a composer's turns at recap time: withText=false is the
// cheap fingerprint path, withText=true adds bubble text for the excerpt.
// The store handle comes from the per-pass cache — one open per store, not
// per call (openROStore fails on a missing path, so no pre-stat needed).
func (c *cursorSource) turns(sess AgentSession, withText bool) ([]cursorTurn, int64, bool) {
	path, id := cursorLocate(sess)
	if path == "" {
		return nil, 0, false
	}
	st, err := c.open(path)
	if err != nil {
		return nil, 0, false
	}
	if !sqliteTableExists(st.db, "cursorDiskKV") {
		return nil, 0, false
	}
	turns, updated, _ := cursorMessages(st.db, id, withText)
	return turns, updated, len(turns) > 0
}

// Fingerprint content-hashes the composer's turn ids + update timestamps —
// a shared DB file's mtime would thrash the recap cache on every keystroke
// (KTD2).
func (c *cursorSource) Fingerprint(sess AgentSession) (recapFingerprint, bool) {
	turns, updated, ok := c.turns(sess, false)
	if !ok {
		return recapFingerprint{}, false
	}
	h := sha256.New()
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(updated))
	h.Write(b[:])
	for _, t := range turns {
		h.Write([]byte(t.id))
		h.Write([]byte{0})
		binary.BigEndian.PutUint64(b[:], uint64(t.ms))
		h.Write(b[:])
		h.Write([]byte(t.role))
		h.Write([]byte{0})
	}
	return hashFingerprint(h.Sum(nil)), true
}

// Excerpt re-queries first/last user text and last assistant text, rendered
// through the shared excerpt builder so the egress shape is identical to the
// other sources.
func (c *cursorSource) Excerpt(sess AgentSession) string {
	turns, _, ok := c.turns(sess, true)
	if !ok {
		return ""
	}
	return excerptFromTurns(cursorSessionTurns(turns))
}
