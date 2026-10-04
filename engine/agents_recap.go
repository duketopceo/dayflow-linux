package main

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Agent-session recaps: a generated one-sentence summary of what a coding-
// agent session accomplished, cached per session and gated by Jev
// worthiness + quality judgments. Per-source adapters (agentSource) supply
// the cache fingerprint and bounded excerpt, so this file never knows a
// store's shape — File is a stat-able transcript path only for JSONL
// sources. All failures degrade to the plain session listing — recaps are
// an enhancement, never a dependency.

// recapFingerprint invalidates a cached recap when the transcript changes.
type recapFingerprint struct {
	Mtime int64
	Size  int64
}

func fingerprint(path string) (recapFingerprint, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return recapFingerprint{}, false
	}
	return recapFingerprint{Mtime: st.ModTime().Unix(), Size: st.Size()}, true
}

// hashFingerprint folds a sha256 message-content hash into the
// recapFingerprint pair every DB-backed source uses (first 16 bytes split
// into Mtime/Size).
func hashFingerprint(sum []byte) recapFingerprint {
	return recapFingerprint{
		Mtime: int64(binary.BigEndian.Uint64(sum[:8])),
		Size:  int64(binary.BigEndian.Uint64(sum[8:16])),
	}
}

// recapRow is one cached agent_recaps row's lookup-relevant fields.
type recapRow struct {
	recap   string
	quality *float64
	mtime   int64
	size    int64
}

// cachedRecaps batch-loads all stored recaps for the given session keys —
// one query per pass instead of one per session.
func cachedRecaps(db *sql.DB, paths []string) map[string]recapRow {
	out := map[string]recapRow{}
	if len(paths) == 0 {
		return out
	}
	var sb strings.Builder
	args := make([]any, 0, len(paths))
	for _, p := range paths {
		if len(args) > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('?')
		args = append(args, p)
	}
	rows, err := db.Query(`SELECT path, recap, quality_confidence, file_mtime, file_size
	  FROM agent_recaps WHERE path IN (`+sb.String()+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var r recapRow
		if rows.Scan(&p, &r.recap, &r.quality, &r.mtime, &r.size) == nil {
			out[p] = r
		}
	}
	return out
}

// fresh reports whether a cached recap row still describes the session —
// the stored fingerprint must equal the adapter's current one.
func fresh(r recapRow, fp recapFingerprint) bool {
	return r.mtime == fp.Mtime && r.size == fp.Size
}

// cachedRecap returns the stored recap when its fingerprint still matches the
// file on disk. A stored row with recap="" means "judged unworthy" — fresh so
// the session is never re-judged. Transient generation failures are NOT
// persisted, so they retry on the next view.
func cachedRecap(db *sql.DB, path string, fp recapFingerprint) (string, *float64, bool) {
	r, ok := cachedRecaps(db, []string{path})[path]
	if !ok || !fresh(r, fp) {
		return "", nil, false
	}
	return r.recap, r.quality, true
}

// putRecap stores (or replaces) the recap row for a transcript.
func putRecap(db *sql.DB, sess AgentSession, fp recapFingerprint, recap, model string, worthy, quality *float64) error {
	_, err := db.Exec(`INSERT INTO agent_recaps
	  (path, source, session_start, recap, worthy_confidence, quality_confidence, file_mtime, file_size, model, created_at)
	  VALUES(?,?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(path) DO UPDATE SET
	    recap=excluded.recap, worthy_confidence=excluded.worthy_confidence,
	    quality_confidence=excluded.quality_confidence, file_mtime=excluded.file_mtime,
	    file_size=excluded.file_size, model=excluded.model, created_at=excluded.created_at`,
		sess.File, sess.Source, sess.Start, recap, worthy, quality,
		fp.Mtime, fp.Size, model, time.Now().Unix())
	return err
}

// sessionExcerpt pulls a bounded text sample from a JSONL transcript: first
// user message, last user message, and last assistant text. The decoder is
// the source's registered lineRoleText — the seam counterpart to its parse
// func — so a source's excerpt can't silently be empty because its name was
// missing from a switch. Everything is rune-truncated and the user's home
// path is scrubbed to ~ before it can leave the machine.
func sessionExcerpt(path string, lineRoleText func([]byte) (role, text string)) string {
	if lineRoleText == nil {
		return ""
	}
	var firstUser, lastUser, lastAssistant string
	ok := eachJSONLLine(path, func(line []byte) {
		role, text := lineRoleText(line)
		if text == "" || isEnvelopeText(text) {
			return // injected envelopes, not user intent
		}
		switch role {
		case "user":
			if firstUser == "" {
				firstUser = text
			}
			lastUser = text
		case "assistant":
			lastAssistant = text
		}
	})
	if !ok {
		return ""
	}
	return buildExcerpt(firstUser, lastUser, lastAssistant)
}

// turnRole is the shared "is this turn usable" classifier: "user" for real
// user intent, "agent" for non-empty assistant output, "" to skip. usableUser
// folds in each source's own predicate (real typed input for Devin, non-empty
// text for Cursor); envelopes never count regardless.
func turnRole(t sessionTurn) string {
	text := strings.TrimSpace(t.text)
	if text == "" {
		return ""
	}
	switch t.role {
	case "user":
		if t.usableUser && !isEnvelopeText(text) {
			return "user"
		}
	case "assistant":
		return "agent"
	}
	return ""
}

// excerptFromTurns accumulates the excerpt inputs shared by every DB-backed
// adapter — first/last usable user text and last assistant text — and
// renders them through buildExcerpt.
func excerptFromTurns(turns []sessionTurn) string {
	var firstUser, lastUser, lastAssistant string
	for _, t := range turns {
		text := strings.TrimSpace(t.text)
		switch turnRole(t) {
		case "user":
			if firstUser == "" {
				firstUser = text
			}
			lastUser = text
		case "agent":
			lastAssistant = text
		}
	}
	return buildExcerpt(firstUser, lastUser, lastAssistant)
}

// isEnvelopeText filters tool-injected envelopes out of excerpt text.
// Covers Claude's injected user rows (slash-command echoes, system
// reminders, caveat preambles, local-command stdout) so a session that
// ends on one doesn't report "blocked — unanswered user ask".
func isEnvelopeText(text string) bool {
	for _, p := range []string{
		"<environment_context>", "<user_instructions>",
		"<command-name>", "<local-command-", "<system-reminder>",
		"Caveat:",
	} {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// buildExcerpt renders the bounded excerpt layout shared by file-backed and
// DB-backed sources: each field is rune-truncated and scrubbed before it can
// reach a model endpoint.
func buildExcerpt(firstUser, lastUser, lastAssistant string) string {
	var b strings.Builder
	write := func(label, s string) {
		if s == "" {
			return
		}
		s = scrubText(boundForScrub(s, 600))
		b.WriteString(label + ": " + s + "\n")
	}
	write("first user message", firstUser)
	write("last user message", lastUser)
	write("last assistant reply", lastAssistant)
	return truncate(b.String(), 2000)
}

// scrubText applies the egress redactions shared by every field that can
// reach a model endpoint: home-path → ~ and common credential shapes →
// [redacted]. The latter mirrors the sensitivePatterns precedent in
// summarize.go — proportional token shapes, not a PII filter.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(sk|pk)-(or-)?[A-Za-z0-9]{16,}\b`), // OpenAI/OpenRouter-style keys
	regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr|github_pat|xox[baprs]|AKIA)[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`\bBearer\s+[A-Za-z0-9._~+/=-]{10,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), // JWT
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// urlUserinfo redacts credentials embedded in URLs. Greedy [^\s]* consumes
// through the last @, so passwords containing @ or : cannot leak; the host
// after the @ survives for context.
var urlUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*)://[^\s]*@`)

func scrubText(s string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s = strings.ReplaceAll(s, home, "~")
	}
	s = urlUserinfo.ReplaceAllString(s, `$1://[redacted]@`)
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[redacted]")
	}
	return s
}

// claudeLineTurn / codexLineTurn decode one transcript line into
// (role, text, unixTs) for the briefing's turn list; the RoleText wrappers
// drop the timestamp for the excerpt path, which doesn't need it.
func claudeLineTurn(raw []byte) (string, string, int64) {
	var line claudeLine
	if json.Unmarshal(raw, &line) != nil {
		return "", "", 0
	}
	if line.Type != "user" && line.Type != "assistant" {
		return "", "", 0
	}
	var msg claudeMessage
	if json.Unmarshal(line.Message, &msg) != nil {
		return "", "", 0
	}
	return line.Type, strings.TrimSpace(contentText(msg.Content)), unixTs(line.Timestamp)
}

func claudeLineRoleText(raw []byte) (string, string) {
	role, text, _ := claudeLineTurn(raw)
	return role, text
}

func codexLineTurn(raw []byte) (string, string, int64) {
	var line codexLine
	if json.Unmarshal(raw, &line) != nil {
		return "", "", 0
	}
	var p codexPayload
	if json.Unmarshal(line.Payload, &p) != nil || p.Type != "message" {
		return "", "", 0
	}
	if p.Role != "user" && p.Role != "assistant" {
		return "", "", 0
	}
	return p.Role, strings.TrimSpace(contentText(p.Content)), unixTs(line.Timestamp)
}

func codexLineRoleText(raw []byte) (string, string) {
	role, text, _ := codexLineTurn(raw)
	return role, text
}

const recapPrompt = `You are summarizing one coding-agent session for a personal work journal.
Given an excerpt of the transcript (first/last user messages and the last
assistant reply), write ONE sentence (max 20 words) stating what the session
accomplished or worked on. Be concrete: name the project task or artifact,
not "the user asked about code". No preamble, no quotes.`

// recapResult is what one recap generation produced. Cacheable reports
// whether the outcome is a settled verdict (generated recap, or an explicit
// unworthy judgment) — transient failures leave it false so the session
// retries on the next view instead of being permanently suppressed.
type recapResult struct {
	Text      string
	Worthy    *float64
	Quality   *float64
	Model     string
	Cacheable bool
}

// generateRecap produces a recap for one session: Jev worthiness gate, chat
// generation, Jev quality gate with one regeneration. Errors degrade to an
// empty, non-cacheable result. chatModel is the chat provider's model id,
// resolved once per pass by attachRecaps.
func generateRecap(db *sql.DB, cfg Config, sess AgentSession, excerpt, chatModel string) recapResult {
	var res recapResult
	if cfg.DisableJudges {
		return res // defense-in-depth — attachRecaps gates before calling
	}
	res.Model = chatModel
	// Title/Project carry the same transcript text as the excerpt — scrub
	// them before they reach the judge endpoint too.
	state := boundState("project: "+scrubText(sess.Project)+"\ntitle: "+scrubText(sess.Title)+
		"\nmessages: "+strconv.Itoa(sess.Messages)+"\n"+excerpt, 2000)

	// Worthiness gate — trivial sessions (probe runs, one-shot questions)
	// don't deserve a generated recap or its cost. An explicit low verdict
	// is a settled outcome worth caching.
	if scores, _, err := decide(db, cfg, "agent_recap", state, map[string]string{
		"worthy": "Is this a substantial coding session worth summarizing — real work with multiple exchanges, not a trivial probe or accidental run?",
	}); err == nil && scores != nil {
		if w, ok := scores["worthy"]; ok {
			res.Worthy = &w
			if w < 0.4 {
				res.Cacheable = true
				return res
			}
		}
	}

	messages := []orMessage{
		{Role: "system", Content: []orContent{{Type: "text", Text: recapPrompt}}},
		{Role: "user", Content: []orContent{{Type: "text", Text: excerpt}}},
	}
	text, _, _, err := callChatModel(db, cfg, "agent_recap", messages)
	if err != nil || strings.TrimSpace(text) == "" {
		return res // transient — not cacheable, retries next view
	}
	res.Text = sanitizeRecap(text)

	// Quality gate — a vague or wrong recap gets one regeneration with the
	// low score fed back; the better-scored draft wins.
	qscore := func(candidate string) *float64 {
		scores, _, err := decide(db, cfg, "agent_recap",
			boundState(state+"\nrecap: "+candidate, 2000), map[string]string{
				"quality": "Does this recap accurately and specifically describe what the session accomplished, without vagueness or invented detail?",
			})
		if err != nil || scores == nil {
			return nil
		}
		if q, ok := scores["quality"]; ok {
			return &q
		}
		return nil
	}
	res.Quality = qscore(res.Text)
	if res.Quality != nil && *res.Quality < lowConfidenceThreshold {
		// Include the draft as an assistant turn so the critique is grounded.
		retry := append(messages,
			orMessage{Role: "assistant", Content: []orContent{{Type: "text", Text: res.Text}}},
			orMessage{Role: "user", Content: []orContent{{Type: "text",
				Text: "That draft was too vague. Be more specific about the concrete task or artifact."}}})
		if t2, _, _, err2 := callChatModel(db, cfg, "agent_recap", retry); err2 == nil && strings.TrimSpace(t2) != "" {
			// An unscored retry still beats a known-bad draft.
			if q2 := qscore(sanitizeRecap(t2)); q2 == nil || *q2 > *res.Quality {
				res.Text = sanitizeRecap(t2)
				res.Quality = q2
			}
		}
	}
	res.Cacheable = true
	return res
}

// sanitizeRecap prepares model output for storage and display: one line,
// no control characters (terminal/UI safety), bounded length.
func sanitizeRecap(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteByte(' ')
		} else if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	return truncate(strings.Join(strings.Fields(b.String()), " "), 300)
}

// maxNewRecaps bounds generation work per invocation — a heavy day shouldn't
// fan out dozens of judge+chat calls behind one UI load. Uncached sessions
// beyond the cap are skipped (not persisted) and fill in on later views.
const maxNewRecaps = 8

// attachRecaps fills Recap/RecapConfidence on each session, generating and
// caching where needed. Cached rows are always served; new generation is
// suppressed when cfg.DisableJudges is set (read-only/no-egress contexts) or
// the excerpt is empty. Individual failures never fail the listing.
//
// srcs should be the adapter set the scan pass used (scanAgentSources'
// optional share): DB-backed sources keep one lazily-opened store handle —
// and at most one WAL temp copy — across the whole pass. With no adapters
// passed, attachRecaps builds its own set and releases it before returning.

// chatEgressOK gates every agent-transcript egress path (session recaps and
// the briefing polish): off under DisableJudges (read-only MCP), under the
// durable agent_recaps opt-out, and when the chat provider is a minutes-scale
// cli — one exec could stall the whole view, so the pass degrades to
// cache/deterministic-only rather than starve on a hanging subprocess.
func chatEgressOK(cfg Config) (Provider, bool) {
	if cfg.DisableJudges || !cfg.AgentRecaps {
		return Provider{}, false
	}
	chatP, err := providerForTask(cfg, "chat")
	if err != nil || chatP.Kind == "cli" {
		return chatP, false
	}
	return chatP, true
}

func attachRecaps(db *sql.DB, cfg Config, sessions []AgentSession, srcs ...agentSource) {
	if db == nil {
		return
	}
	if len(srcs) == 0 {
		srcs = agentSources()
		defer closeAgentSources(srcs)
	}
	// Cache hits first (any order), then generate for the largest uncached
	// sessions within the cap. Fingerprint/excerpt come from the session's
	// source adapter — File is a stat-able path only for JSONL sources, so
	// attachRecaps never stats it directly (KTD2).
	paths := make([]string, 0, len(sessions))
	for i := range sessions {
		paths = append(paths, sessions[i].File)
	}
	cached := cachedRecaps(db, paths)
	order := make([]int, 0, len(sessions))
	for i := range sessions {
		src := agentSourceNamed(srcs, sessions[i].Source)
		if src == nil {
			continue
		}
		fp, ok := src.Fingerprint(sessions[i])
		if !ok {
			continue
		}
		if r, ok := cached[sessions[i].File]; ok && fresh(r, fp) {
			sessions[i].Recap = r.recap
			sessions[i].RecapConfidence = r.quality
			continue
		}
		order = append(order, i)
	}
	// no-egress contexts, the durable opt-out, and minutes-scale cli
	// providers serve cache only.
	chatP, ok := chatEgressOK(cfg)
	if !ok {
		return
	}
	sort.Slice(order, func(a, b int) bool {
		return sessions[order[a]].Messages > sessions[order[b]].Messages
	})
	// Batch mode: uncached sessions go out as one OpenRouter batch job and
	// are collected on a later pass. Falls through to the inline loop when
	// the routed recap provider isn't batch-capable (non-OpenRouter or cli).
	if cfg.AgentRecapBatch {
		if bp, ok := batchRecapProvider(cfg); ok {
			attachRecapsBatch(db, cfg, sessions, order, srcs, bp)
			return
		}
	}
	// Wall-clock budget: each session can cost up to ~5 blocking model calls;
	// bound the whole pass so a hanging provider can't stall the UI load that
	// invoked this. Skipped sessions trickle-fill on later views.
	deadline := time.Now().Add(45 * time.Second)
	generated := 0
	for _, i := range order {
		if generated >= maxNewRecaps || time.Now().After(deadline) {
			break
		}
		src := agentSourceNamed(srcs, sessions[i].Source)
		if src == nil {
			continue
		}
		// Fingerprint before extraction AND after generation — an active
		// transcript can grow mid-pass, and a cached row must describe the
		// excerpt it was generated from.
		beforeFP, beforeOK := src.Fingerprint(sessions[i])
		excerpt := src.Excerpt(sessions[i])
		if excerpt == "" {
			// Settle it: nothing to summarize now. Fingerprint still
			// invalidates the row if the transcript grows.
			afterFP, afterOK := src.Fingerprint(sessions[i])
			if beforeOK && afterOK && beforeFP == afterFP {
				if err := putRecap(db, sessions[i], afterFP, "", "", nil, nil); err != nil {
					debugf(cfg, "recap cache write %s: %v", sessions[i].File, err)
				}
			}
			continue
		}
		generated++
		res := generateRecap(db, cfg, sessions[i], excerpt, chatP.Model)
		afterFP, afterOK := src.Fingerprint(sessions[i])
		if res.Cacheable && beforeOK && afterOK && beforeFP == afterFP {
			if err := putRecap(db, sessions[i], afterFP, res.Text, res.Model, res.Worthy, res.Quality); err != nil {
				debugf(cfg, "recap cache write %s: %v", sessions[i].File, err)
			}
		}
		sessions[i].Recap = res.Text
		sessions[i].RecapConfidence = res.Quality
	}
}
