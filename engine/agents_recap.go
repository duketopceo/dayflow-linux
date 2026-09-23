package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Agent-session recaps: a generated one-sentence summary of what a Claude
// Code / Codex session accomplished, cached per transcript file and gated by
// Jev worthiness + quality judgments. All failures degrade to the plain
// session listing — recaps are an enhancement, never a dependency.

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

// cachedRecap returns the stored recap when its fingerprint still matches the
// file on disk. A stored row with recap=” means "judged unworthy" — fresh so
// the session is never re-judged. Transient generation failures are NOT
// persisted, so they retry on the next view.
func cachedRecap(db *sql.DB, path string, fp recapFingerprint) (recap string, quality *float64, fresh bool) {
	var r struct {
		recap   string
		quality *float64
		mtime   int64
		size    int64
	}
	err := db.QueryRow(`SELECT recap, quality_confidence, file_mtime, file_size
	  FROM agent_recaps WHERE path = ?`, path).Scan(&r.recap, &r.quality, &r.mtime, &r.size)
	if err != nil {
		return "", nil, false
	}
	if r.mtime != fp.Mtime || r.size != fp.Size {
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

// sessionExcerpt pulls a bounded text sample from a transcript: first user
// message, last user message, and last assistant text. Everything is
// rune-truncated and the user's home path is scrubbed to ~ before it can
// leave the machine.
func sessionExcerpt(path, source string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var firstUser, lastUser, lastAssistant string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		role, text := lineRoleText(sc.Bytes(), source)
		if text == "" || strings.HasPrefix(text, "<environment_context>") ||
			strings.HasPrefix(text, "<user_instructions>") {
			continue // codex-injected envelopes, not user intent
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
	}
	if sc.Err() != nil {
		return "" // scan error (e.g. >1MB line) — parity with scanJSONL
	}

	var b strings.Builder
	write := func(label, s string) {
		if s == "" {
			return
		}
		s = boundState(scrubText(s), 600)
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

// lineRoleText decodes one JSONL line into (role, text) for a known source.
func lineRoleText(raw []byte, source string) (string, string) {
	switch source {
	case "claude":
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			return "", ""
		}
		if line.Type != "user" && line.Type != "assistant" {
			return "", ""
		}
		var msg claudeMessage
		if json.Unmarshal(line.Message, &msg) != nil {
			return "", ""
		}
		return line.Type, strings.TrimSpace(contentText(msg.Content))
	case "codex":
		var line codexLine
		if json.Unmarshal(raw, &line) != nil {
			return "", ""
		}
		var p codexPayload
		if json.Unmarshal(line.Payload, &p) != nil || p.Type != "message" {
			return "", ""
		}
		if p.Role != "user" && p.Role != "assistant" {
			return "", ""
		}
		return p.Role, strings.TrimSpace(contentText(p.Content))
	}
	return "", ""
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
// empty, non-cacheable result.
func generateRecap(db *sql.DB, cfg Config, sess AgentSession, excerpt string) recapResult {
	var res recapResult
	if cfg.DisableJudges {
		return res // defense-in-depth — attachRecaps gates before calling
	}
	if p, err := providerForTask(cfg, "chat"); err == nil {
		res.Model = p.Model
	}
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
func attachRecaps(db *sql.DB, cfg Config, sessions []AgentSession) {
	if db == nil {
		return
	}
	// Cache hits first (any order), then generate for the largest uncached
	// sessions within the cap.
	order := make([]int, 0, len(sessions))
	for i := range sessions {
		fp, ok := fingerprint(sessions[i].File)
		if !ok {
			continue
		}
		if recap, quality, fresh := cachedRecap(db, sessions[i].File, fp); fresh {
			sessions[i].Recap = recap
			sessions[i].RecapConfidence = quality
			continue
		}
		order = append(order, i)
	}
	if cfg.DisableJudges || !cfg.AgentRecaps {
		return // no-egress contexts and the durable opt-out serve cache only
	}
	sort.Slice(order, func(a, b int) bool {
		return sessions[order[a]].Messages > sessions[order[b]].Messages
	})
	// Wall-clock budget: each session can cost up to ~5 blocking model calls;
	// bound the whole pass so a hanging provider can't stall the UI load that
	// invoked this. Skipped sessions trickle-fill on later views.
	deadline := time.Now().Add(45 * time.Second)
	generated := 0
	for _, i := range order {
		if generated >= maxNewRecaps || time.Now().After(deadline) {
			break
		}
		// Fingerprint before extraction AND after generation — an active
		// transcript can grow mid-pass, and a cached row must describe the
		// excerpt it was generated from.
		beforeFP, beforeOK := fingerprint(sessions[i].File)
		excerpt := sessionExcerpt(sessions[i].File, sessions[i].Source)
		if excerpt == "" {
			// Settle it: nothing to summarize now. Fingerprint still
			// invalidates the row if the transcript grows.
			afterFP, afterOK := fingerprint(sessions[i].File)
			if beforeOK && afterOK && beforeFP == afterFP {
				if err := putRecap(db, sessions[i], afterFP, "", "", nil, nil); err != nil {
					debugf(cfg, "recap cache write %s: %v", sessions[i].File, err)
				}
			}
			continue
		}
		generated++
		res := generateRecap(db, cfg, sessions[i], excerpt)
		afterFP, afterOK := fingerprint(sessions[i].File)
		if res.Cacheable && beforeOK && afterOK && beforeFP == afterFP {
			if err := putRecap(db, sessions[i], afterFP, res.Text, res.Model, res.Worthy, res.Quality); err != nil {
				debugf(cfg, "recap cache write %s: %v", sessions[i].File, err)
			}
		}
		sessions[i].Recap = res.Text
		sessions[i].RecapConfidence = res.Quality
	}
}
