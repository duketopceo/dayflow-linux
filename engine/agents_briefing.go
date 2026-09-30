package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Agent briefing: the upstream-Dayflow "workstream briefing" shape ported to
// the local pipeline. The engine derives structure deterministically —
// workstream grouping, per-thread status, condensed turns — so a briefing
// renders with zero model calls. When agent_recaps is enabled, one batched
// chat call rewrites the prose fields over that skeleton; everything egress
// is scrubbed and bounded like the per-session recap path.

// Briefing JSON shape (dayflow briefing --json). Mirrors the upstream
// Dayflow recap schema with our source names; decoding is lenient both ways
// so a partially-written model pass degrades per-field.
type agentBriefing struct {
	Day         string `json:"day"`
	GeneratedAt string `json:"generated_at"`
	Mode        string `json:"mode"` // "model" | "fallback"
	// RecapsEnabled mirrors cfg.AgentRecaps so renderers can show the
	// opt-in hint when prose generation is off.
	RecapsEnabled bool                 `json:"recaps_enabled"`
	Workstreams   []briefingWorkstream `json:"workstreams"`
	Sources       []sourceScanStatus   `json:"sources"`
}

type briefingWorkstream struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Summary string           `json:"summary,omitempty"`
	Bullets []string         `json:"bullets,omitempty"`
	Threads []briefingThread `json:"threads"`
	// lastEnd is the sort key (max thread EndedAt) — kept on the struct so
	// it travels with the element during SliceStable's swaps.
	lastEnd int64
}

type briefingThread struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Source        string         `json:"source"`
	Project       string         `json:"project,omitempty"`
	SessionPath   string         `json:"session_path"`
	Status        string         `json:"status"` // blocked|reviewReady|inProgress|completed
	StartedAt     int64          `json:"started_at"`
	EndedAt       int64          `json:"ended_at"`
	LatestOutcome string         `json:"latest_outcome,omitempty"`
	Turns         []briefingTurn `json:"turns"`
}

type briefingTurn struct {
	Role         string `json:"role"` // "user" | "agent"
	Text         string `json:"text"`
	Highlight    string `json:"highlight,omitempty"` // keyDecision|keyInfo|readyForReview
	Ts           int64  `json:"ts,omitempty"`
	ArtifactName string `json:"artifact_name,omitempty"`
	ArtifactPath string `json:"artifact_path,omitempty"`
}

// briefingStatuses — the deterministic status vocabulary. reviewReady is
// reachable only through the model pass (KTD3): deterministic derivation
// never claims a deliverable exists.
const (
	statusInProgress  = "inProgress"
	statusBlocked     = "blocked"
	statusReviewReady = "reviewReady"
	statusCompleted   = "completed"
)

// briefing modes: "model" when the prose polish pass ran, "fallback" when
// the payload is the deterministic skeleton alone.
const (
	modeModel    = "model"
	modeFallback = "fallback"
)

// maxBriefingTurns caps the condensed narrative per thread (upstream uses 12).
const maxBriefingTurns = 12

// maxBriefingBullets / maxBriefingHighlights bound what the model pass can
// attach — prompt says 2-5 bullets and ≤3 highlights; enforce both.
const (
	maxBriefingBullets    = 5
	maxBriefingHighlights = 3
)

// isGroundedPath requires an artifact path to be path-shaped: absolute or
// home-relative (turn text is scrubbed, so $HOME paths surface as "~/").
// Bare substrings like "the" or "/" can't ground a model-invented artifact.
func isGroundedPath(p string) bool {
	return len(p) >= 4 &&
		(strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~/"))
}

// expandArtifactPath turns a grounded "~/x" back into a real path for
// storage — the pane opens it as file:// without needing $HOME itself.
func expandArtifactPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home + p[1:]
		}
	}
	return p
}

// briefingActiveWindow marks a session "in progress" when its last activity
// is this recent — same 15-minute rule the upstream recap prompt uses.
const briefingActiveWindow = 15 * time.Minute

// deriveStatus classifies a thread from its timestamps and turn order:
// recent activity → inProgress; ending on an unanswered user ask → blocked;
// otherwise completed (the model pass may upgrade to reviewReady).
func deriveStatus(sess AgentSession, turns []sessionTurn, now time.Time) string {
	if sess.End >= now.Add(-briefingActiveWindow).Unix() {
		return statusInProgress
	}
	// Only the last usable turn decides — scan backwards and stop.
	for i := len(turns) - 1; i >= 0; i-- {
		switch turnRole(turns[i]) {
		case "user":
			return statusBlocked
		case "agent":
			return statusCompleted
		}
	}
	return statusCompleted
}

// turnText bounds one condensed turn's text for display and egress.
// Scrubbed here — at condense time, not only on the model path — so the
// text persisted to agent_briefings, printed by --json, and shipped in
// backups carries no pasted secrets either.
func turnText(s string) string {
	// Bound before scrubbing: a multi-MB transcript line (minified blob,
	// data URI) would otherwise send the secret regexes' backtracker
	// spinning for minutes on a single turn.
	return truncate(strings.Join(strings.Fields(
		stripCtl(scrubText(truncate(s, 2000)))), " "), 160)
}

// condenseTurns folds a session's raw turns into the capped narrative:
// usable user turns and non-empty assistant turns only, consecutive
// same-role turns merged, edge turns always preserved (KTD4).
func condenseTurns(turns []sessionTurn) []briefingTurn {
	out := []briefingTurn{}
	for _, t := range turns {
		role := turnRole(t)
		if role == "" {
			continue
		}
		text := turnText(t.text)
		if text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			// Adapters can emit the same message text on consecutive
			// turns (e.g. a session title re-echoed per event) — don't
			// stack identical text on itself.
			if !strings.Contains(out[n-1].Text, text) {
				out[n-1].Text = truncate(out[n-1].Text+" — "+text, 200)
			}
			if t.unixTs > out[n-1].Ts {
				out[n-1].Ts = t.unixTs
			}
			continue
		}
		out = append(out, briefingTurn{Role: role, Text: text, Ts: t.unixTs})
	}
	if len(out) <= maxBriefingTurns {
		return out
	}
	// Keep the arc: first 5 and last 6 turns, middle collapsed into one
	// honest marker so the cap never fabricates content.
	const head, tail = 5, 6
	mid := len(out) - head - tail
	marker := briefingTurn{
		Role: "agent",
		Text: fmt.Sprintf("(%d earlier exchanges condensed)", mid),
	}
	capped := append([]briefingTurn{}, out[:head]...)
	capped = append(capped, marker)
	return append(capped, out[len(out)-tail:]...)
}

// briefingThreadID is a stable, content-derived thread key the model pass
// echoes back to target its per-thread edits.
func briefingThreadID(sess AgentSession) string {
	h := sha256.Sum256([]byte(sess.File))
	return sess.Source + "-" + hex.EncodeToString(h[:4])
}

var kebabRe = regexp.MustCompile(`[^a-z0-9]+`)

func kebab(s string) string {
	s = kebabRe.ReplaceAllString(strings.ToLower(s), "-")
	return strings.Trim(s, "-")
}

// uniqueWSID keeps workstream ids unique when distinct project names kebab
// to the same slug ("Foo Bar" vs "foo-bar") — a colliding or empty slug
// gets a numeric suffix so one polish entry can't rewrite two workstreams.
func uniqueWSID(base string, used map[string]int) string {
	if base == "" {
		base = "ws"
	}
	used[base]++
	if used[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, used[base])
}

// groupWorkstreams buckets threads by project (upstream groups by project +
// purpose; the model pass may rename but deterministic grouping is by
// project). Workstreams order by most recent thread end; within one,
// threads run oldest-first.
func groupWorkstreams(threads []briefingThread) []briefingWorkstream {
	groups := map[string][]briefingThread{}
	var order []string
	for _, th := range threads {
		name := th.Project
		if name == "" {
			name = "Miscellaneous"
		}
		if _, ok := groups[name]; !ok {
			order = append(order, name)
		}
		groups[name] = append(groups[name], th)
	}
	ws := make([]briefingWorkstream, 0, len(groups))
	usedIDs := map[string]int{}
	for _, name := range order {
		ths := groups[name]
		summary := "1 session"
		if len(ths) > 1 {
			summary = fmt.Sprintf("%d sessions", len(ths))
		}
		var lastEnd int64
		for _, th := range ths {
			if th.EndedAt > lastEnd {
				lastEnd = th.EndedAt
			}
		}
		ws = append(ws, briefingWorkstream{
			ID:      uniqueWSID(kebab(name), usedIDs),
			Name:    name,
			Summary: summary,
			Threads: ths,
			lastEnd: lastEnd,
		})
	}
	sort.SliceStable(ws, func(i, j int) bool { return ws[i].lastEnd > ws[j].lastEnd })
	return ws
}

// briefingFingerprint folds every session's adapter fingerprint into one
// day key — any transcript growth invalidates the cached briefing.
func briefingFingerprint(sessions []AgentSession, srcs []agentSource) string {
	var parts []string
	for _, sess := range sessions {
		src := agentSourceNamed(srcs, sess.Source)
		if src == nil {
			continue
		}
		fp, ok := src.Fingerprint(sess)
		if !ok {
			continue
		}
		parts = append(parts, sess.File+":"+strconv.FormatInt(fp.Mtime, 16)+
			":"+strconv.FormatInt(fp.Size, 16))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:16])
}

// buildBriefing produces the deterministic skeleton: threads with condensed
// turns and derived status, grouped into project workstreams.
func buildBriefing(d time.Time, sessions []AgentSession, statuses []sourceScanStatus, srcs []agentSource) agentBriefing {
	now := time.Now()
	threads := make([]briefingThread, 0, len(sessions))
	for _, sess := range sessions {
		var turns []sessionTurn
		if src := agentSourceNamed(srcs, sess.Source); src != nil {
			turns = src.Turns(sess)
		}
		th := briefingThread{
			ID:          briefingThreadID(sess),
			Title:       sess.Title,
			Source:      sess.Source,
			Project:     sess.Project,
			SessionPath: sess.File,
			Status:      deriveStatus(sess, turns, now),
			StartedAt:   sess.Start,
			EndedAt:     sess.End,
			Turns:       condenseTurns(turns),
		}
		// Deterministic latest outcome: the last agent turn's text.
		for i := len(th.Turns) - 1; i >= 0; i-- {
			if th.Turns[i].Role == "agent" {
				th.LatestOutcome = truncate(th.Turns[i].Text, 90)
				break
			}
		}
		threads = append(threads, th)
	}
	return agentBriefing{
		Day:         d.Local().Format("2006-01-02"),
		GeneratedAt: now.Format(time.RFC3339),
		Mode:        modeFallback,
		Workstreams: groupWorkstreams(threads),
		Sources:     statuses,
	}
}

// --- Model polish pass ---

// briefingPrompt is the system prompt for the one batched polish call. The
// model edits prose fields only; structure (ids, ordering, status enum) is
// engine-owned and echoed back by id.
const briefingPrompt = `You are writing a personal day briefing of the user's coding-agent sessions.
You receive a deterministic skeleton: workstreams of threads, each with a condensed turn list.
Rewrite the prose — keep it concrete and grounded ONLY in the turn text given. Do not invent events, files, or outcomes.

Return RAW JSON only (no markdown, no commentary) matching exactly:
{
  "workstreams": [
    {"id": "<ws id from input>", "name": "<short human name>", "summary": "<one line, max 90 chars>", "bullets": ["<outcome-phrased accomplishment, 2-5 per workstream>"],
     "threads": [{"id": "<thread id from input>", "title": "<max 60 chars>", "latest_outcome": "<one line, max 90 chars>", "review_ready": true|false,
                  "turn_highlights": [{"turn": <index>, "kind": "keyDecision|keyInfo|readyForReview"}],
                  "artifact_name": "<short label>", "artifact_path": "<path only if it literally appears in the turn text>"}]}
  ]
}

Rules: mark review_ready only when the thread produced a finished deliverable awaiting human action. At most 3 turn_highlights per thread, only where truly warranted. Skip artifact fields when no deliverable path is named. Omit thread or workstream entries you have no better text for.`

// briefingPolishResult is the leniently-decoded model response — every field
// optional, applied by id over the skeleton.
type briefingPolishResult struct {
	Workstreams []polishWorkstream `json:"workstreams"`
}
type polishWorkstream struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Summary string         `json:"summary"`
	Bullets []string       `json:"bullets"`
	Threads []polishThread `json:"threads"`
}
type polishThread struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	LatestOutcome string            `json:"latest_outcome"`
	ReviewReady   bool              `json:"review_ready"`
	Highlights    []polishHighlight `json:"turn_highlights"`
	ArtifactName  string            `json:"artifact_name"`
	ArtifactPath  string            `json:"artifact_path"`
}
type polishHighlight struct {
	Turn int    `json:"turn"`
	Kind string `json:"kind"`
}

// briefingModelPayload renders the bounded, scrubbed skeleton the model
// sees: no raw transcript text beyond the already-condensed turns, each
// field capped. The payload stays under maxBriefingPayload by dropping
// trailing threads/workstreams before marshaling — never mid-JSON.
const maxBriefingPayload = 8000

// Polished-field skeleton — the model sees only these fields, each already
// scrubbed by the callers upstream (scrubText on name/title/text).
type briefingModelDoc struct {
	Day         string                `json:"day"`
	Workstreams []briefingModelStream `json:"workstreams"`
}
type briefingModelStream struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Threads []briefingModelThread `json:"threads"`
}
type briefingModelThread struct {
	ID     string              `json:"id"`
	Source string              `json:"source"`
	Status string              `json:"status"`
	Title  string              `json:"title"`
	Turns  []briefingModelTurn `json:"turns"`
}
type briefingModelTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func briefingModelPayload(b *agentBriefing) string {
	doc := briefingModelDoc{Day: b.Day}
	for _, ws := range b.Workstreams {
		mws := briefingModelStream{ID: ws.ID, Name: scrubText(ws.Name)}
		for _, th := range ws.Threads {
			mth := briefingModelThread{
				ID: th.ID, Source: th.Source, Status: th.Status,
				Title: scrubText(th.Title),
			}
			for _, tn := range th.Turns {
				mth.Turns = append(mth.Turns, briefingModelTurn{Role: tn.Role, Text: scrubText(tn.Text)})
			}
			mws.Threads = append(mws.Threads, mth)
		}
		doc.Workstreams = append(doc.Workstreams, mws)
	}
	// Shrink to fit: drop trailing threads then whole workstreams until the
	// marshaled payload is under the cap. Dropping before marshal keeps the
	// JSON valid — truncating the string after would not.
	for len(doc.Workstreams) > 0 {
		raw, err := json.Marshal(doc)
		if err != nil {
			return ""
		}
		if len(raw) <= maxBriefingPayload {
			return string(raw)
		}
		last := &doc.Workstreams[len(doc.Workstreams)-1]
		if len(last.Threads) > 1 {
			last.Threads = last.Threads[:len(last.Threads)-1]
		} else {
			doc.Workstreams = doc.Workstreams[:len(doc.Workstreams)-1]
		}
	}
	return `{"day":` + strconv.Quote(b.Day) + `,"workstreams":[]}`
}

// applyBriefingPolish overlays the model's prose edits onto the skeleton by
// id. Every field is independently optional; a malformed entry degrades
// without sinking the briefing. Returns whether anything changed — a
// response that parses but mutates nothing doesn't count as model-authored.
func applyBriefingPolish(b *agentBriefing, raw string) bool {
	var res briefingPolishResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return false
	}
	pwsByID := map[string]*polishWorkstream{}
	for i := range res.Workstreams {
		pwsByID[res.Workstreams[i].ID] = &res.Workstreams[i]
	}
	validHighlight := map[string]bool{
		"keyDecision": true, "keyInfo": true, "readyForReview": true,
	}
	changed := false
	for wi := range b.Workstreams {
		ws := &b.Workstreams[wi]
		pws, ok := pwsByID[ws.ID]
		if !ok {
			continue
		}
		// Compare before assigning: an echo of the existing value doesn't
		// count as model-authored change.
		if s := sanitizeRecap(pws.Name); s != "" && s != ws.Name {
			ws.Name, changed = s, true
		}
		if s := sanitizeRecap(pws.Summary); s != "" && s != ws.Summary {
			ws.Summary, changed = s, true
		}
		if len(pws.Bullets) > 0 {
			var bs []string
			for _, bl := range pws.Bullets {
				if s := sanitizeRecap(bl); s != "" {
					bs = append(bs, s)
				}
			}
			if len(bs) > maxBriefingBullets {
				bs = bs[:maxBriefingBullets]
			}
			if len(bs) > 0 && !slices.Equal(bs, ws.Bullets) {
				ws.Bullets, changed = bs, true
			}
		}
		pthByID := map[string]*polishThread{}
		for i := range pws.Threads {
			pthByID[pws.Threads[i].ID] = &pws.Threads[i]
		}
		for ti := range ws.Threads {
			th := &ws.Threads[ti]
			pth, ok := pthByID[th.ID]
			if !ok {
				continue
			}
			if s := sanitizeRecap(pth.Title); s != "" && s != th.Title {
				th.Title, changed = s, true
			}
			if s := sanitizeRecap(pth.LatestOutcome); s != "" && s != th.LatestOutcome {
				th.LatestOutcome, changed = s, true
			}
			// KTD3: model upgrades completed → reviewReady only.
			if pth.ReviewReady && th.Status == statusCompleted {
				th.Status, changed = statusReviewReady, true
			}
			highlights := 0
			for _, h := range pth.Highlights {
				if highlights >= maxBriefingHighlights {
					break
				}
				if !validHighlight[h.Kind] || h.Turn < 0 || h.Turn >= len(th.Turns) {
					continue
				}
				if th.Turns[h.Turn].Highlight == h.Kind {
					continue
				}
				th.Turns[h.Turn].Highlight, changed = h.Kind, true
				highlights++
			}
			// Artifacts must be grounded: the path must be path-shaped and
			// appear verbatim in a real turn's text, else the model invented
			// it. The artifact attaches to the turn that mentions it.
			path := sanitizeRecap(pth.ArtifactPath)
			if isGroundedPath(path) && pth.ArtifactName != "" {
				for i := range th.Turns {
					tn := &th.Turns[i]
					if tn.ArtifactPath != "" || !strings.Contains(tn.Text, path) {
						continue
					}
					tn.ArtifactName = sanitizeRecap(pth.ArtifactName)
					tn.ArtifactPath = expandArtifactPath(path)
					changed = true
					break
				}
			}
		}
	}
	return changed
}

// polishDeadline bounds the whole polish call — retries and backoff
// included — well under the pane's 75s watchdog, so a slow provider serves
// the deterministic briefing instead of a killed process.
const polishDeadline = 55 * time.Second

// polishBriefing runs the one batched chat call that rewrites briefing
// prose. Gated exactly like attachRecaps: off when agent_recaps is unset,
// DisableJudges is set, or the chat provider is a minutes-scale CLI.
// polishKey records the attempt in meta so a failed polish for a given
// day+fingerprint isn't retried on every view.
func polishBriefing(db *sql.DB, cfg Config, b *agentBriefing, polishKey string) {
	if db == nil {
		return
	}
	if _, ok := chatEgressOK(cfg); !ok {
		return
	}
	if polishKey != "" {
		db.Exec(`INSERT INTO meta(k, v) VALUES(?, '1') ON CONFLICT(k) DO NOTHING`, polishKey)
	}
	messages := []orMessage{
		{Role: "system", Content: []orContent{{Type: "text", Text: briefingPrompt}}},
		{Role: "user", Content: []orContent{{Type: "text", Text: briefingModelPayload(b)}}},
	}
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		text, _, _, err := callChatModel(db, cfg, "agent_briefing", messages)
		done <- result{text, err}
	}()
	var text string
	select {
	case r := <-done:
		if r.err != nil || strings.TrimSpace(r.text) == "" {
			return
		}
		text = r.text
	case <-time.After(polishDeadline):
		debugf(cfg, "agent briefing polish exceeded %s — serving fallback", polishDeadline)
		return
	}
	// The model may wrap JSON in fences — strip to the outermost braces.
	if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		text = text[i : j+1]
	}
	// A response that changed nothing still counts as "model ran" only if it
	// mutated the briefing — otherwise fallback mode honestly reports no polish.
	if applyBriefingPolish(b, text) {
		b.Mode = modeModel
	}
}

// --- Persistence ---

// agent_briefings lives in the base schema (CREATE IF NOT EXISTS every open)
// like agent_recaps — no migration needed, and read-only opens degrade to
// a cache miss rather than failing.
func loadBriefing(db *sql.DB, fp string, day string) (*agentBriefing, bool) {
	var payload string
	if err := db.QueryRow(`SELECT payload FROM agent_briefings
	  WHERE day=? AND fingerprint=?`, day, fp).Scan(&payload); err != nil {
		return nil, false
	}
	var b agentBriefing
	if json.Unmarshal([]byte(payload), &b) != nil {
		return nil, false
	}
	return &b, true
}

func storeBriefing(db *sql.DB, fp string, b agentBriefing) {
	payload, err := json.Marshal(b)
	if err != nil {
		return
	}
	db.Exec(`INSERT INTO agent_briefings (day, fingerprint, payload, mode, created_at)
	  VALUES(?,?,?,?,?)
	  ON CONFLICT(day) DO UPDATE SET fingerprint=excluded.fingerprint,
	    payload=excluded.payload, mode=excluded.mode, created_at=excluded.created_at`,
		b.Day, fp, string(payload), b.Mode, time.Now().Unix())
}

// agentBriefingFor is the briefing entry point: scan → fingerprint → cache
// or rebuild → polish → persist. Sources refresh every call so drift
// reporting stays live even when the payload is cached.
func agentBriefingFor(db *sql.DB, cfg Config, d time.Time, refresh bool) agentBriefing {
	srcs, sessions, statuses := scanAgentDay(db, d)
	defer closeAgentSources(srcs)

	day := d.Local().Format("2006-01-02")
	var fp string
	if len(sessions) > 0 {
		fp = briefingFingerprint(sessions, srcs)
	}
	if !refresh && db != nil && fp != "" {
		if b, ok := loadBriefing(db, fp, day); ok {
			b.Sources = statuses
			b.RecapsEnabled = cfg.AgentRecaps
			// inProgress is wall-clock, not content — the fingerprint
			// can't invalidate it, so a cached "live" session whose End
			// has aged out re-derives here instead of replaying forever.
			refreshStaleStatuses(b, time.Now())
			if b.Mode == modeModel {
				return *b
			}
			// Recaps came on after a fallback was cached — polish the
			// cached skeleton directly; same fingerprint means the
			// deterministic input is unchanged, so a rebuild would
			// produce identical turns. The meta stamp records the attempt
			// so a failed polish isn't retried on every view.
			polishKey := "briefing_polish:" + day + ":" + fp
			var tried string
			db.QueryRow(`SELECT v FROM meta WHERE k=?`, polishKey).Scan(&tried)
			if cfg.AgentRecaps && !cfg.DisableJudges && tried != "1" {
				polishBriefing(db, cfg, b, polishKey)
				storeBriefing(db, fp, *b)
			}
			return *b
		}
	}
	b := buildBriefing(d, sessions, statuses, srcs)
	b.RecapsEnabled = cfg.AgentRecaps
	if len(sessions) > 0 {
		polishBriefing(db, cfg, &b, "")
	}
	if db != nil && fp != "" {
		storeBriefing(db, fp, b)
	}
	return b
}

// refreshStaleStatuses re-derives any cached inProgress thread whose last
// activity has aged past the active window — blocked when the last usable
// turn is an unanswered user ask, completed otherwise.
func refreshStaleStatuses(b *agentBriefing, now time.Time) {
	cutoff := now.Add(-briefingActiveWindow).Unix()
	for wi := range b.Workstreams {
		for ti := range b.Workstreams[wi].Threads {
			th := &b.Workstreams[wi].Threads[ti]
			if th.Status != statusInProgress || th.EndedAt >= cutoff {
				continue
			}
			th.Status = statusCompleted
			for i := len(th.Turns) - 1; i >= 0; i-- {
				if th.Turns[i].Role == "user" {
					th.Status = statusBlocked
					break
				}
				if th.Turns[i].Role == "agent" {
					break
				}
			}
		}
	}
}

// printBriefing renders the briefing as JSON or a compact text digest.
func printBriefing(db *sql.DB, cfg Config, d time.Time, jsonOut, refresh bool) {
	b := agentBriefingFor(db, cfg, d, refresh)
	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(b)
		return
	}
	if len(b.Workstreams) == 0 {
		fmt.Println("no agent sessions for", b.Day)
	} else {
		for _, ws := range b.Workstreams {
			fmt.Printf("%s — %s\n", ws.Name, ws.Summary)
			for _, th := range ws.Threads {
				fmt.Printf("  %-11s %-7s %s\n", "["+th.Status+"]", th.Source, truncTitle(th.Title))
				if th.LatestOutcome != "" {
					fmt.Printf("              └─ %s\n", th.LatestOutcome)
				}
			}
		}
	}
	if b.Mode == modeFallback && !cfg.AgentRecaps {
		fmt.Println("note: agent_recaps is off — briefing shows deterministic summaries only" +
			" (`dayflow config set agent_recaps true` for model-written prose)")
	}
	printDriftNotes(b.Sources)
}
