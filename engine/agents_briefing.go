package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
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

// maxBriefingTurns caps the condensed narrative per thread (upstream uses 12).
const maxBriefingTurns = 12

// briefingActiveWindow marks a session "in progress" when its last activity
// is this recent — same 15-minute rule the upstream recap prompt uses.
const briefingActiveWindow = 15 * time.Minute

// deriveStatus classifies a thread from its timestamps and turn order:
// recent activity → inProgress; ending on an unanswered user ask → blocked;
// otherwise completed (the model pass may upgrade to reviewReady).
func deriveStatus(sess AgentSession, turns []sessionTurn, now time.Time) string {
	if sess.End >= now.Unix()-int64(briefingActiveWindow/time.Second) {
		return statusInProgress
	}
	last := ""
	for _, t := range turns {
		if t.role == "assistant" && strings.TrimSpace(t.text) != "" {
			last = "assistant"
		}
		if t.role == "user" && t.usableUser {
			last = "user"
		}
	}
	if last == "user" {
		return statusBlocked
	}
	return statusCompleted
}

// turnText bounds one condensed turn's text for display and egress.
func turnText(s string) string {
	return truncate(strings.Join(strings.Fields(stripCtl(s)), " "), 160)
}

// condenseTurns folds a session's raw turns into the capped narrative:
// usable user turns and non-empty assistant turns only, consecutive
// same-role turns merged, edge turns always preserved (KTD4).
func condenseTurns(turns []sessionTurn) []briefingTurn {
	var out []briefingTurn
	for _, t := range turns {
		var role string
		switch {
		case t.role == "user" && t.usableUser:
			role = "user"
		case t.role == "assistant" && strings.TrimSpace(t.text) != "":
			role = "agent"
		default:
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
	out = append(append(append([]briefingTurn{}, out[:head]...),
		marker), out[len(out)-tail:]...)
	return out
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
	for _, name := range order {
		ths := groups[name]
		summary := "1 session"
		if len(ths) > 1 {
			summary = fmt.Sprintf("%d sessions", len(ths))
		}
		ws = append(ws, briefingWorkstream{
			ID:      kebab(name),
			Name:    name,
			Summary: summary,
			Threads: ths,
		})
	}
	sort.SliceStable(ws, func(i, j int) bool {
		var ei, ej int64
		for _, th := range ws[i].Threads {
			if th.EndedAt > ei {
				ei = th.EndedAt
			}
		}
		for _, th := range ws[j].Threads {
			if th.EndedAt > ej {
				ej = th.EndedAt
			}
		}
		return ei > ej
	})
	return ws
}

// briefingFingerprint folds every session's adapter fingerprint into one
// day key — any transcript growth invalidates the cached briefing.
func briefingFingerprint(sessions []AgentSession, srcs []agentSource) string {
	srcFor := func(name string) agentSource {
		for _, s := range srcs {
			if s != nil && s.Name() == name {
				return s
			}
		}
		return nil
	}
	var parts []string
	for _, sess := range sessions {
		src := srcFor(sess.Source)
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
	srcFor := func(name string) agentSource {
		for _, s := range srcs {
			if s != nil && s.Name() == name {
				return s
			}
		}
		return nil
	}
	now := time.Now()
	threads := make([]briefingThread, 0, len(sessions))
	for _, sess := range sessions {
		var turns []sessionTurn
		if src := srcFor(sess.Source); src != nil {
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
		Mode:        "fallback",
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
	Workstreams []struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Summary string   `json:"summary"`
		Bullets []string `json:"bullets"`
		Threads []struct {
			ID            string `json:"id"`
			Title         string `json:"title"`
			LatestOutcome string `json:"latest_outcome"`
			ReviewReady   bool   `json:"review_ready"`
			Highlights    []struct {
				Turn int    `json:"turn"`
				Kind string `json:"kind"`
			} `json:"turn_highlights"`
			ArtifactName string `json:"artifact_name"`
			ArtifactPath string `json:"artifact_path"`
		} `json:"threads"`
	} `json:"workstreams"`
}

// briefingModelPayload renders the bounded, scrubbed skeleton the model
// sees: no raw transcript text beyond the already-condensed turns, each
// field capped, whole payload under maxBriefingPayload.
const maxBriefingPayload = 8000

func briefingModelPayload(b *agentBriefing) string {
	var sb strings.Builder
	sb.WriteString(`{"day":` + strconv.Quote(b.Day) + `,"workstreams":[`)
	for i, ws := range b.Workstreams {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"id":` + strconv.Quote(ws.ID) + `,"name":` + strconv.Quote(scrubText(ws.Name)) + `,"threads":[`)
		for j, th := range ws.Threads {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`{"id":` + strconv.Quote(th.ID) +
				`,"source":` + strconv.Quote(th.Source) +
				`,"status":` + strconv.Quote(th.Status) +
				`,"title":` + strconv.Quote(scrubText(th.Title)) + `,"turns":[`)
			for k, tn := range th.Turns {
				if k > 0 {
					sb.WriteByte(',')
				}
				sb.WriteString(`{"role":` + strconv.Quote(tn.Role) +
					`,"text":` + strconv.Quote(scrubText(tn.Text)) + `}`)
			}
			sb.WriteString(`]}`)
		}
		sb.WriteString(`]}`)
		if sb.Len() > maxBriefingPayload {
			break
		}
	}
	sb.WriteString(`]}`)
	return truncate(sb.String(), maxBriefingPayload)
}

// applyBriefingPolish folds the model's prose onto the skeleton by id.
// Every field is independently optional; a malformed entry degrades without
// sinking the briefing.
func applyBriefingPolish(b *agentBriefing, raw string) {
	var res briefingPolishResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return
	}
	validHighlight := map[string]bool{
		"keyDecision": true, "keyInfo": true, "readyForReview": true,
	}
	for wi := range b.Workstreams {
		ws := &b.Workstreams[wi]
		for _, pws := range res.Workstreams {
			if pws.ID != ws.ID {
				continue
			}
			if s := sanitizeRecap(pws.Name); s != "" {
				ws.Name = s
			}
			if s := sanitizeRecap(pws.Summary); s != "" {
				ws.Summary = s
			}
			if len(pws.Bullets) > 0 {
				var bs []string
				for _, bl := range pws.Bullets {
					if s := sanitizeRecap(bl); s != "" {
						bs = append(bs, s)
					}
				}
				ws.Bullets = bs
			}
			for ti := range ws.Threads {
				th := &ws.Threads[ti]
				for _, pth := range pws.Threads {
					if pth.ID != th.ID {
						continue
					}
					if s := sanitizeRecap(pth.Title); s != "" {
						th.Title = s
					}
					if s := sanitizeRecap(pth.LatestOutcome); s != "" {
						th.LatestOutcome = s
					}
					// KTD3: model upgrades completed → reviewReady only.
					if pth.ReviewReady && th.Status == statusCompleted {
						th.Status = statusReviewReady
					}
					for _, h := range pth.Highlights {
						if !validHighlight[h.Kind] || h.Turn < 0 || h.Turn >= len(th.Turns) {
							continue
						}
						th.Turns[h.Turn].Highlight = h.Kind
					}
					// Artifacts must be grounded: the path must appear
					// verbatim in a source turn, else the model invented it.
					if pth.ArtifactPath != "" && pth.ArtifactName != "" {
						for _, tn := range th.Turns {
							if strings.Contains(tn.Text, pth.ArtifactPath) {
								th.Turns[len(th.Turns)-1].ArtifactName = sanitizeRecap(pth.ArtifactName)
								th.Turns[len(th.Turns)-1].ArtifactPath = pth.ArtifactPath
								break
							}
						}
					}
				}
			}
		}
	}
}

// polishBriefing runs the one batched chat call that rewrites briefing
// prose. Gated exactly like attachRecaps: off when agent_recaps is unset,
// DisableJudges is set, or the chat provider is a minutes-scale CLI.
func polishBriefing(db *sql.DB, cfg Config, b *agentBriefing) {
	if db == nil || cfg.DisableJudges || !cfg.AgentRecaps {
		return
	}
	chatP, err := providerForTask(cfg, "chat")
	if err != nil || chatP.Kind == "cli" {
		return
	}
	messages := []orMessage{
		{Role: "system", Content: []orContent{{Type: "text", Text: briefingPrompt}}},
		{Role: "user", Content: []orContent{{Type: "text", Text: briefingModelPayload(b)}}},
	}
	text, _, _, err := callChatModel(db, cfg, "agent_briefing", messages)
	if err != nil || strings.TrimSpace(text) == "" {
		return
	}
	// The model may wrap JSON in fences — strip to the outermost braces.
	if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		text = text[i : j+1]
	}
	// Snapshot before applying: Workstreams mutates in place, so a shallow
	// struct copy can't detect the change.
	before, _ := json.Marshal(b.Workstreams)
	applyBriefingPolish(b, text)
	// A response that changed nothing still counts as "model ran" only if it
	// parsed — otherwise the fallback mode honestly reports no polish.
	after, _ := json.Marshal(b.Workstreams)
	if string(before) != string(after) {
		b.Mode = "model"
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
	srcs := agentSources()
	defer closeAgentSources(srcs)
	sessions, statuses := scanAgentSources(d, srcs...)
	recordAgentSourceScans(db, statuses)

	day := d.Local().Format("2006-01-02")
	var fp string
	if len(sessions) > 0 {
		fp = briefingFingerprint(sessions, srcs)
	}
	if !refresh && db != nil && fp != "" {
		if b, ok := loadBriefing(db, fp, day); ok {
			// A cached fallback briefing regenerates once recaps come on —
			// the deterministic payload shouldn't pin the day forever.
			if b.Mode == "model" || cfg.DisableJudges || !cfg.AgentRecaps {
				b.Sources = statuses
				return *b
			}
		}
	}
	b := buildBriefing(d, sessions, statuses, srcs)
	if len(sessions) > 0 {
		polishBriefing(db, cfg, &b)
	}
	if db != nil && fp != "" {
		storeBriefing(db, fp, b)
	}
	return b
}

// printBriefing renders the briefing as JSON or a compact text digest.
func printBriefing(db *sql.DB, cfg Config, d time.Time, jsonOut, refresh bool) {
	b := agentBriefingFor(db, cfg, d, refresh)
	b.RecapsEnabled = cfg.AgentRecaps
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
	if b.Mode == "fallback" && !cfg.AgentRecaps {
		fmt.Println("note: agent_recaps is off — briefing shows deterministic summaries only" +
			" (`dayflow config set agent_recaps true` for model-written prose)")
	}
	for _, st := range b.Sources {
		if st.Drift {
			fmt.Printf("note: %s produced sessions before but none today — its store may have drifted\n", st.Source)
		}
	}
}
