// Batch-mode agent recaps: instead of one inline chat call per session,
// uncached sessions are bundled into a single OpenRouter Batch API job
// (POST /api/v1/batches — roughly half price, async 24h completion window).
// attachRecaps submits a batch and collects it on a later pass; until the
// results land, sessions render deterministic summaries like any uncached
// session. Same bounded + scrubbed excerpt, same Jev worthiness gate, same
// cache table — only the chat transport differs.

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// metaRecapBatch is the meta key holding the pending batch's durable state.
// metaRecapBatchClaim is the sole-submitter lease: "token|expiry_unix". Two
// recap-enabled commands sharing one database could otherwise both pass the
// pending checks, both POST a batch, and the loser's savePendingBatch would
// overwrite the winner's ID — that batch then completes provider-side with
// no local record, billed but never collected.
const metaRecapBatch = "agent_recap_batch"
const metaRecapBatchClaim = "agent_recap_batch_claim"

// maxBatchRecaps bounds one submission — excerpts are ~2KB each, so 150
// requests is still a ~300KB POST, while leaving head-room for stragglers
// to roll into the next pass.
const maxBatchRecaps = 150

// batchSubmitBackoff suppresses immediate resubmission after a failed or
// terminal batch — one retry per hour, not one per UI load.
const batchSubmitBackoff = time.Hour

// pendingRecapBatch is the durable state for one in-flight batch, stored as
// JSON under metaRecapBatch. A cleared ID with a recent LastAttempt means
// "last submission failed or finished terminal — back off".
type pendingRecapBatch struct {
	ID          string         `json:"id"`
	SubmittedAt int64          `json:"submitted_at"`
	LastAttempt int64          `json:"last_attempt"`
	Sessions    []batchSession `json:"sessions"`
}

// batchSession is the submit-time record needed to write the recap row on
// collect: the cache fingerprint the excerpt was taken at, the worthiness
// score already paid for by the submit-time Jev gate, and the judge state
// so collection scores quality against identical context to the inline path.
type batchSession struct {
	CustomID string   `json:"custom_id"`
	Path     string   `json:"path"`
	Source   string   `json:"source"`
	Start    int64    `json:"start"`
	Mtime    int64    `json:"mtime"`
	Size     int64    `json:"size"`
	Worthy   *float64 `json:"worthy,omitempty"`
	State    string   `json:"state"`
}

func loadPendingBatch(db *sql.DB) (*pendingRecapBatch, bool) {
	v := metaGet(db, metaRecapBatch)
	if v == "" {
		return nil, false
	}
	var p pendingRecapBatch
	if err := json.Unmarshal([]byte(v), &p); err != nil {
		return nil, false
	}
	return &p, true
}

func savePendingBatch(db *sql.DB, p *pendingRecapBatch) {
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	metaSet(db, metaRecapBatch, string(b))
}

func clearPendingBatch(db *sql.DB) {
	db.Exec(`DELETE FROM meta WHERE k=?`, metaRecapBatch)
}

// batchClaimTTL bounds a submitter lease. A claim covers only the
// submit-and-save window (~one HTTP call), so two minutes is generous —
// and a crashed claimer's lease expires rather than pinning the slot.
const batchClaimTTL = 120 * time.Second

// claimBatchSubmit takes the sole-submitter lease atomically: the
// conditional upsert only lands when the stored lease has expired (or the
// key is new), so concurrent claimers can't both pass. The token ties the
// release to the claimer.
func claimBatchSubmit(db *sql.DB, token string) bool {
	var v string
	err := db.QueryRow(`INSERT INTO meta(k,v) VALUES(?, ?)
	  ON CONFLICT(k) DO UPDATE SET v=excluded.v
	  WHERE CAST(substr(meta.v, instr(meta.v,'|')+1) AS INTEGER) <= ?
	  RETURNING v`,
		metaRecapBatchClaim,
		token+"|"+strconv.FormatInt(time.Now().Add(batchClaimTTL).Unix(), 10),
		time.Now().Unix()).Scan(&v)
	return err == nil
}

// releaseBatchClaim drops the lease, but only ours — a malformed or foreign
// value never matches the token prefix, so it can't be stolen on release.
func releaseBatchClaim(db *sql.DB, token string) {
	db.Exec(`DELETE FROM meta WHERE k=? AND substr(v, 1, instr(v,'|')-1) = ?`,
		metaRecapBatchClaim, token)
}

// batchRecapProvider resolves the provider the batch should use — the same
// per-call-site routing callChatModel applies to "agent_recap" — and checks
// it can actually serve the Batch API: an OpenRouter endpoint (default or a
// custom base pointed at openrouter.ai) with an available key. Anything else
// falls back to the inline path.
func batchRecapProvider(cfg Config) (Provider, bool) {
	route := "chat"
	if cfg.Routing.TaskProvider["agent_recap"] != "" {
		route = "agent_recap"
	}
	p, err := providerForTask(cfg, route)
	if err != nil || p.Kind == "cli" || !providerUsesOpenRouterHeaders(p) {
		return p, false
	}
	if providerNeedsAuth(p) && resolveProviderKey(p) == "" {
		return p, false
	}
	return p, true
}

// batchesURL derives the Batch API endpoint from the provider's chat URL so
// tests that stub openRouterURL cover batch calls too.
func batchesURL(p Provider) string {
	return strings.TrimSuffix(providerChatURL(p), "/chat/completions") + "/batches"
}

func batchHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func newBatchRequest(cfg Config, p Provider, method, url string, body []byte) (*http.Request, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if providerNeedsAuth(p) {
		if k := resolveProviderKey(p); k != "" {
			req.Header.Set("Authorization", "Bearer "+k)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	setOpenRouterHeaders(req, cfg.SiteName)
	return req, nil
}

// orBatchSubmit is the POST /batches request shape: one endpoint shape and
// one model applied to every request; each item carries a custom_id echoed
// back in results.
type orBatchSubmit struct {
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	Requests []struct {
		CustomID string    `json:"custom_id"`
		Body     orRequest `json:"body"`
	} `json:"requests"`
}

type orBatchSubmitResp struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// submitRecapBatch POSTs one batch job; the returned id is durable state the
// next pass polls.
func submitRecapBatch(cfg Config, p Provider, excerpts []string) (string, error) {
	var sub orBatchSubmit
	sub.Endpoint = "/v1/chat/completions"
	sub.Model = p.Model
	sub.Requests = make([]struct {
		CustomID string    `json:"custom_id"`
		Body     orRequest `json:"body"`
	}, 0, len(excerpts))
	for i, ex := range excerpts {
		sub.Requests = append(sub.Requests, struct {
			CustomID string    `json:"custom_id"`
			Body     orRequest `json:"body"`
		}{
			CustomID: strconv.Itoa(i),
			Body: orRequest{Model: p.Model, Messages: []orMessage{
				{Role: "system", Content: []orContent{{Type: "text", Text: recapPrompt}}},
				{Role: "user", Content: []orContent{{Type: "text", Text: ex}}},
			}},
		})
	}
	payload, err := json.Marshal(sub)
	if err != nil {
		return "", err
	}
	req, err := newBatchRequest(cfg, p, "POST", batchesURL(p), payload)
	if err != nil {
		return "", err
	}
	resp, err := batchHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("batch submit api %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var out orBatchSubmitResp
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("batch submit response not valid JSON: %v", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("batch submit api error: %s", out.Error.Message)
	}
	if out.ID == "" {
		return "", fmt.Errorf("batch submit returned no id")
	}
	return out.ID, nil
}

type orBatchGetResp struct {
	Status  string `json:"status"`
	Results []struct {
		CustomID string `json:"custom_id"`
		// Exactly one of response/error is populated per result.
		Response *struct {
			StatusCode int             `json:"status_code"`
			Body       json.RawMessage `json:"body"`
		} `json:"response"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"results"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// batchResultText extracts the assistant text from one batch result body,
// tolerating both the string and content-parts message shapes providers use.
func batchResultText(raw json.RawMessage) string {
	var body struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Choices) == 0 {
		return ""
	}
	content := body.Choices[0].Message.Content
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			b.WriteString(part.Text)
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

// pollRecapBatch GETs the job status; completed jobs carry results inline.
func pollRecapBatch(cfg Config, p Provider, id string) (*orBatchGetResp, error) {
	req, err := newBatchRequest(cfg, p, "GET", batchesURL(p)+"/"+id, nil)
	if err != nil {
		return nil, err
	}
	resp, err := batchHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("batch poll api %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var out orBatchGetResp
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("batch poll response not valid JSON: %v", err)
	}
	return &out, nil
}

func batchTerminal(status string) bool {
	switch status {
	case "failed", "expired", "cancelled":
		return true
	}
	return false
}

// collectedRecap is one batch-written recap, returned so the invoking pass
// can render it immediately instead of waiting for the next view.
type collectedRecap struct {
	text    string
	quality *float64
}

// collectPendingBatch polls the stored batch and, when it completed, writes
// each result into agent_recaps keyed by the submit-time fingerprint — a
// transcript that grew mid-flight simply fails the next freshness check and
// is regenerated. Returns the collected recaps keyed by session path and
// true while a batch is still in flight (callers should not submit another).
func collectPendingBatch(db *sql.DB, cfg Config, p Provider, pend *pendingRecapBatch) (map[string]collectedRecap, bool) {
	got, err := pollRecapBatch(cfg, p, pend.ID)
	if err != nil {
		debugf(cfg, "recap batch poll %s: %v", pend.ID, err)
		return nil, true // transient poll failure — keep waiting, don't resubmit
	}
	if batchTerminal(got.Status) {
		// Back off before resubmitting — the same submission just failed.
		pend.ID = ""
		pend.Sessions = nil
		pend.LastAttempt = time.Now().Unix()
		savePendingBatch(db, pend)
		debugf(cfg, "recap batch %s ended %s", pend.ID, got.Status)
		return nil, false
	}
	if got.Status != "completed" {
		return nil, true // validating/in_progress/finalizing — still in flight
	}
	byID := make(map[string]batchSession, len(pend.Sessions))
	for _, s := range pend.Sessions {
		byID[s.CustomID] = s
	}
	// Same wall-clock budget as the inline pass: up to maxBatchRecaps Jev
	// quality calls run synchronously here, so past the deadline the rest
	// are written unscored rather than stalling the invoking UI load.
	deadline := time.Now().Add(45 * time.Second)
	collected := make(map[string]collectedRecap, len(got.Results))
	for _, r := range got.Results {
		s, ok := byID[r.CustomID]
		if !ok || r.Response == nil || r.Error != nil {
			continue
		}
		text := sanitizeRecap(batchResultText(r.Response.Body))
		if text == "" {
			continue // not cacheable — a later pass retries it
		}
		// Same quality scoring the inline path applies, minus regeneration —
		// a second batch for low scores isn't worth the complexity.
		var quality *float64
		if time.Now().Before(deadline) {
			if scores, _, err := decide(db, cfg, "agent_recap",
				boundState(s.State+"\nrecap: "+text, 2000),
				map[string]string{
					"quality": "Does this recap accurately and specifically describe what the session accomplished, without vagueness or invented detail?",
				}); err == nil {
				if q, ok := scores["quality"]; ok {
					quality = &q
				}
			}
		}
		sess := AgentSession{File: s.Path, Source: s.Source, Start: s.Start}
		fp := recapFingerprint{Mtime: s.Mtime, Size: s.Size}
		if err := putRecap(db, sess, fp, text, p.Model, s.Worthy, quality); err != nil {
			debugf(cfg, "recap batch cache write %s: %v", s.Path, err)
		}
		collected[s.Path] = collectedRecap{text: text, quality: quality}
	}
	clearPendingBatch(db)
	return collected, false
}

// attachRecapsBatch is the batch-mode counterpart of attachRecaps' generation
// loop: collect the pending batch if any, then bundle the day's uncached
// sessions into one submission. The Jev worthiness gate still runs inline —
// it's the cheap classifier, not the chat model — so only worthy sessions
// ever enter the batch.
func attachRecapsBatch(db *sql.DB, cfg Config, sessions []AgentSession, order []int, srcs []agentSource, p Provider) {
	var collected map[string]collectedRecap
	if pend, ok := loadPendingBatch(db); ok && pend.ID != "" {
		var inFlight bool
		collected, inFlight = collectPendingBatch(db, cfg, p, pend)
		if inFlight {
			return
		}
	}
	// Render what the collect just wrote and keep it out of the next
	// submission — order was built before the batch completed, so these
	// sessions still look uncached.
	for i := range sessions {
		if c, ok := collected[sessions[i].File]; ok {
			sessions[i].Recap = c.text
			sessions[i].RecapConfidence = c.quality
		}
	}
	// Post-collect: a batch may still be in flight (or just failed into
	// backoff). Don't stack a second submission on top.
	if pend, ok := loadPendingBatch(db); ok {
		if pend.ID != "" {
			return
		}
		if pend.LastAttempt > 0 && time.Since(time.Unix(pend.LastAttempt, 0)) < batchSubmitBackoff {
			return
		}
	}

	var excerpts []string
	var pendingSessions []batchSession
	deadline := time.Now().Add(45 * time.Second)
	for _, i := range order {
		if len(excerpts) >= maxBatchRecaps || time.Now().After(deadline) {
			break
		}
		if _, ok := collected[sessions[i].File]; ok {
			continue // the batch that just completed already covered this one
		}
		src := agentSourceNamed(srcs, sessions[i].Source)
		if src == nil {
			continue
		}
		beforeFP, beforeOK := src.Fingerprint(sessions[i])
		excerpt := src.Excerpt(sessions[i])
		if excerpt == "" {
			afterFP, afterOK := src.Fingerprint(sessions[i])
			if beforeOK && afterOK && beforeFP == afterFP {
				if err := putRecap(db, sessions[i], afterFP, "", "", nil, nil); err != nil {
					debugf(cfg, "recap cache write %s: %v", sessions[i].File, err)
				}
			}
			continue
		}
		// Worthiness gate inline — same decide call the inline path makes.
		var worthy *float64
		state := boundState("project: "+scrubText(sessions[i].Project)+"\ntitle: "+scrubText(sessions[i].Title)+
			"\nmessages: "+strconv.Itoa(sessions[i].Messages)+"\n"+excerpt, 2000)
		if scores, _, err := decide(db, cfg, "agent_recap", state, map[string]string{
			"worthy": "Is this a substantial coding session worth summarizing — real work with multiple exchanges, not a trivial probe or accidental run?",
		}); err == nil && scores != nil {
			if w, ok := scores["worthy"]; ok {
				worthy = &w
				if w < 0.4 {
					afterFP, afterOK := src.Fingerprint(sessions[i])
					if beforeOK && afterOK && beforeFP == afterFP {
						if err := putRecap(db, sessions[i], afterFP, "", p.Model, worthy, nil); err != nil {
							debugf(cfg, "recap cache write %s: %v", sessions[i].File, err)
						}
					}
					continue
				}
			}
		}
		afterFP, afterOK := src.Fingerprint(sessions[i])
		if !beforeOK || !afterOK || beforeFP != afterFP {
			continue // transcript moved mid-pass — retry next pass
		}
		excerpts = append(excerpts, excerpt)
		pendingSessions = append(pendingSessions, batchSession{
			CustomID: strconv.Itoa(len(excerpts) - 1),
			Path:     sessions[i].File,
			Source:   sessions[i].Source,
			Start:    sessions[i].Start,
			Mtime:    afterFP.Mtime,
			Size:     afterFP.Size,
			Worthy:   worthy,
			State:    state,
		})
	}
	if len(excerpts) == 0 {
		return
	}
	// Sole-submitter lease across the submit+save window. A second
	// recap-enabled command racing this pass loses the claim and returns
	// rather than orphaning a batch. A process that dies between the
	// provider accepting the POST and savePendingBatch leaves an
	// unreachable in-flight batch — it expires provider-side and the
	// resubmission after lease expiry costs one duplicate job, which is
	// the accepted residual without a provider-side idempotency key.
	token := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	if !claimBatchSubmit(db, token) {
		return
	}
	defer releaseBatchClaim(db, token)
	// Re-check under the lease: the state we validated above may have
	// changed while a peer held the slot.
	if pend, ok := loadPendingBatch(db); ok {
		if pend.ID != "" || (pend.LastAttempt > 0 && time.Since(time.Unix(pend.LastAttempt, 0)) < batchSubmitBackoff) {
			return
		}
	}
	pend := &pendingRecapBatch{LastAttempt: time.Now().Unix()}
	id, err := submitRecapBatch(cfg, p, excerpts)
	if err != nil {
		debugf(cfg, "recap batch submit: %v", err)
		savePendingBatch(db, pend) // LastAttempt backs off the retry
		return
	}
	pend.ID = id
	pend.SubmittedAt = pend.LastAttempt
	pend.Sessions = pendingSessions
	savePendingBatch(db, pend)
}
