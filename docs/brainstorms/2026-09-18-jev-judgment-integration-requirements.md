# Jev judgment integration — requirements

Date: 2026-09-18
Status: scoped, confirmed — full takeover in one tranche

## What we're building

Integrate TypeSafe's Jev decision model across Dayflow: every judgment the
app makes today moves to calibrated `noul` calls on OpenRouter's decisions
API. The chat model keeps only true generation (titles, summaries, standup
prose, chat). Jev's calibrated scores become first-class data, surfaced in
the UI — not just plumbing.

Verified contract (live-tested 2026-09-18):

- Endpoint: `https://openrouter.ai/api/alpha/decisions`
- Request: `{model: "jev-latest", state: <string>, questions: {<key>: {type: "noul", instructions: <statement>}}}`
- Response: `{answers: {<key>: {noul: 0..1}}, usage: {cost}}`; resolves to `typesafe/jev-1.13-*`
- Cost: ~$0.00002 per multi-question call — ~96 blocks/day ≪ $0.01/day
- `typesafe/jev-latest` on chat-completions is NOT valid — decisions API only. Decision-shaped tasks only; no generation, no images.

## Integrations (all seven, one tranche)

In priority order — if the tranche balloons, cut from the bottom:

1. **Category takeover** — per-block jev judgment produces category +
   confidence stored on the block. `engine/summarize.go`'s chat prompt drops
   category/`productive` selection; keeps titles, summaries, activities.
   Categories judged as a batch of `noul` questions per block.
2. **Merge judgment** — "same activity as the previous block?" feeds
   `mergeSpans` beyond title/app+category equality.
3. **Summary quality gate** — "is this title/summary specific and accurate?"
   One regeneration retry on fail; persistent low-confidence flagged on the
   block.
4. **Standup worthiness** — per-block score drives standup inclusion instead
   of dumping every block.
5. **Dead-block triage** — "is this failure retryable vs terminal?" —
   retryables auto-requeue once; terminal stays `dead` (pairs with the
   flagged-failure rendering shipped in the maintenance tranche).
6. **Forecast confidence** — calibrated "does tomorrow plausibly resemble
   the learned pattern?" shown in `dayflow forecast` output and FullView.
7. **Context-shift salience** — "was this app switch a real context change?"
   filters sankey edges so the Context pane shows meaningful shifts, not
   noise.

Plus the infrastructure: `engine/decisions.go` — a bounded-timeout,
degrade-safe client; and the UI surface — block confidence on Today pane /
panel, forecast's calibrated %, visible low-confidence flag.

## Fallback contract

Jev unreachable or erroring → current behavior, always: categories from the
chat call's inline output, merges by the existing heuristic, standup shows
all blocks, forecast shows the heuristic estimate without a confidence
number. Jev is never a hard dependency; absence degrades, never breaks.

## User-facing behavior

- Every new summarized block carries `category` + calibrated confidence.
- `dayflow forecast` and FullView's forecast show a calibrated probability.
- Low-confidence blocks are visibly flagged (Today pane, panel, MCP data).
- `dayflow usage`/MCP `get_usage` breakdown records jev calls under their
  own task name (existing ledger shape — no special casing).
- UI scores are informative, not noisy: shown where a number means
  something (confidence, forecast), not a score on every pixel.

## Non-goals

- Replacing or retraining generative calls (titles, summaries, standup,
  chat, agent recaps stay on the chat model).
- Image/frame judgments — decisions API is text-only.
- Marketplace submission; macOS parity; new product features beyond the
  judgment takeover.
- Scores on pre-existing historical blocks (backfill is opt-in tooling if
  wanted later, not required).

## Success criteria

- New blocks show calibrated category + confidence end-to-end (engine →
  MCP → CLI → QML).
- Forecast reports a calibrated probability, not just the heuristic mix.
- A deliberately low-quality summary is caught by the quality gate
  (regeneration or flag).
- With OpenRouter unreachable, capture+summarize still completes with
  pre-jev behavior and zero new errors.
- `go test ./...` + `go vet ./...` green; live smoke on a real block.

## Assumptions / open calls for planning

- Score persistence shape (new `blocks` columns vs a judgments ledger) —
  planning decides; blocks already carry `status`/`error` precedent.
- Where judgments batch (inside the existing summarize sweep vs a dedicated
  pass) — planning decides; must not serialize the sweep on network latency.
- Whether jev calls ride the existing `api_calls`/`llm_calls` ledgers under
  task `judge`/`decide` (preferred — inherits the U4 breakdown for free) or
  a separate ledger.
- `jev-latest` as the configured model ID (alpha endpoint resolves it);
  fall back to pinned `typesafe/jev-1.13-20260917` if `latest` drifts.
