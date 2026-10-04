---
title: Full Local Model Support - Plan
type: feat
date: 2026-10-03
topic: local-models
---

# Full Local Model Support - Plan

## Summary

Make every model call site in dayflow routable to a local OpenAI-compatible endpoint — vision, chat, recaps, briefing, and the Jev decisions layer — so the journal can run with zero OpenRouter dependency and zero egress. Vision/chat already route locally via `api_base_url` and `task_provider`; the only hardcoded cloud surface is the decisions endpoint, which this plan unhardcodes and extends with a chat-transport fallback so no external decisions service is required.

## Problem Frame

`decide()` in `engine/decisions.go` POSTs to a hardcoded `decisionsURL` (`https://openrouter.ai/api/alpha/decisions`, test-overridable var). Every category/productive/quality/merge judgment and every agent-recap worthiness/quality score egresses through it. Today the only way to run fully local is `jev_classification: false`, which deletes the entire calibrated layer rather than localizing it.

Probe result (this session): the existing `jev-shim` on `127.0.0.1:8931` is **not** a drop-in decisions backend — it answers `{score|choice, confidence}` on a 0–2 scale under a wisp-specific system prompt, while `decide()` requires `{noul: 0–1}` keyed answers. It would silently return empty results. Reusing it means rewriting wisp's tool for dayflow — rejected; the fallback below needs no shim at all.

## Requirements

- R1. `decide()` targets a configurable endpoint; the OpenRouter alpha URL stays the default.
- R2. When the configured endpoint is an ordinary OpenAI-compatible chat endpoint, `decide()` compiles the noul questions into one chat call and parses a JSON score map — same `map[string]float64` contract, no external decisions service needed.
- R3. Non-OpenRouter endpoints never receive an `Authorization` header unless a key is explicitly configured, and never receive OpenRouter attribution headers.
- R4. `DisableJudges` / `jev_classification: false` suppression is unchanged; callers' nil-score fallbacks are unchanged.
- R5. A "fully local" configuration (local vision + local decisions + local chat/recap providers) results in zero network egress, verifiable by `llm_calls`/`api_calls` showing only localhost URLs.
- R6. Docs teach the local recipe (Ollama / llama.cpp) and the shim finding is recorded.

## Key Technical Decisions

- **KTD1 — `decisions_url` config field, not a provider.** The decisions endpoint is a distinct API contract (`{model,state,questions}→{answers:{q:{noul}}}`), not a chat-completions provider; modeling it as a `Provider` would force fake fields. A plain config string (empty = OpenRouter default) keeps the seam honest and test-stubbing trivial — `decisionsURL` var is replaced by `cfg.DecisionsURL` resolution with the same default.
- **KTD2 — transport selection by URL shape.** If `decisions_url` resolves to a `*/chat/completions` URL (or a `*/v1` base, normalized by the same `normalizeAPIBaseURL` used for providers), `decide()` uses the chat fallback (KTD3); anything else POSTs the current decisions contract. One field, two transports — no second flag to keep in sync, and hosted mirrors of the real decisions API still work.
- **KTD3 — noul-over-chat fallback.** The chat path renders one user message: the state, then each question's instructions verbatim, then a strict output contract — `{"<key>": <0-1 float>, ...}` — with `temperature: 0` (matching the shim's and Jev's deterministic posture). Response is parsed leniently (strip think-tags, take first JSON object — same parse discipline as `batchResultText`/`jev-shim`), each value clamped to `[0,1]`. Parse failure or missing keys → return the subset that did parse; total failure → error so callers keep heuristic fallback. `model` comes from a new optional `decisions_model` field falling back to `classification_model`, so the local slug (e.g. `qwen3:4b-instruct`, `ornith`) and the Jev slug don't collide.
- **KTD4 — auth and attribution follow the endpoint, not the key.** On chat/decisions transports pointing off OpenRouter: no `Authorization` header unless `decisions_api_key` is explicitly set (new optional field; empty = none — mirrors `providerNeedsAuth` semantics), and `setOpenRouterHeaders` is skipped entirely. This mirrors the existing `providerUsesOpenRouterHeaders` rule: attribution only on OpenRouter-routed calls.
- **KTD5 — logging keeps the `judge:` task name.** `llm_calls` rows stay `judge:<kind>` with provider recorded as `decisions_url` host or `local-chat` so `dayflow usage` can show Jev locality; this is how R5's egress audit gets verified.

## Scope Boundaries

### Deferred to Follow-Up Work

- Rewriting `jev-shim` to speak the real decisions contract (it's wisp's tool; if wisp wants Jev-parity it can adopt the same noul shape later — the chat fallback makes this unnecessary for dayflow).
- A Settings-toggle for "fully local mode" that flips every route at once — convenience layer, deferred until the config proves out.
- Local-model quality evaluation vs `typesafe/jev-1.13` (does a 4B local judge match calibrated scores?) — worth measuring, not blocking.

### Non-goals

- Changing the `noul` contract or score semantics.
- Bundling/downloading local models (Ollama/llama.cpp management stays the user's).
- Batching decisions calls (they're already one call per judgment site).

## Implementation Units

### U1. `decisions_url` config + transport seam

**Goal:** the decisions endpoint becomes configurable with a chat-transport branch.

**Requirements:** R1, R3, R4

**Dependencies:** none

**Files:**
- `engine/config.go` — `DecisionsURL string \`json:"decisions_url"\`` (empty = default), `DecisionsModel string \`json:"decisions_model,omitempty"\``, `DecisionsAPIKey string \`json:"decisions_api_key,omitempty"\``; defaults in `DefaultConfig()`/`fillDefaults`; `config set`/`config patch` cases
- `engine/decisions.go` — replace the `decisionsURL` package var with `cfg.DecisionsURL` resolution (empty → OpenRouter alpha URL); keep a `decisionsURLOverride` test seam or have tests set `cfg.DecisionsURL` (prefer the latter — kills the global)

**Approach:** `decisionsEndpoint(cfg) (url string, isChat bool)` — normalize with `normalizeAPIBaseURL`; `isChat = strings.HasSuffix(url, "/chat/completions")` (accept `/v1` base too, appending `/chat/completions`). Non-OpenRouter URLs skip `setOpenRouterHeaders`; `Authorization` only when `DecisionsAPIKey` non-empty — on OpenRouter the existing `jevAPIKey` chain still applies.

**Patterns to follow:** `normalizeAPIBaseURL` + `providerChatURL`/`providerUsesOpenRouterHeaders`/`providerNeedsAuth` in `engine/provider.go` — the same "endpoint shape implies behavior" rules.

**Test scenarios:**
- Empty `decisions_url` → requests hit the OpenRouter alpha path (existing tests keep passing by setting the field instead of the var).
- `decisions_url` = custom `https://mirror.example/decisions` → decisions-shaped POST to that URL, `Authorization` present only with `decisions_api_key`, no `HTTP-Referer`/`X-Title`.
- `decisions_url` = `http://127.0.0.1:8091/v1` → normalized to `…/v1/chat/completions`, routed to chat transport.
- `DisableJudges` / `jev_classification:false` → zero calls, `nil` scores (unchanged).
- Error path: non-200 / bad JSON on either transport → error returned, `llm_calls` logs `judge:` failure (unchanged contract).

### U2. noul-over-chat transport

**Goal:** `decide()` answers noul questions via one local chat call when the endpoint is chat-shaped.

**Requirements:** R2, R3, R4

**Dependencies:** U1

**Files:**
- `engine/decisions.go` — `decideViaChat(db, cfg, kind, state, questions, endpoint)` building the prompt and parsing the response; `decide()` dispatches on `isChat` from U1

**Approach:** single user message — `state`, then `Q "<key>": <instructions>` per question, then: `Answer each question with a float 0-1 in one strict JSON object keyed by question name. Output JSON only.` Parse: strip `<think>…</think>`, extract first `{…}` object, per-key `float64` clamp `[0,1]`; tolerate missing keys (return the parsed subset — an empty-but-valid map is still a judgment, distinct from error). `temperature: 0`, model from `decisions_model` → `classification_model` → `defaultJevModel`. Key ordering sorted for deterministic prompts.

**Patterns to follow:** `orRequest`/`callProviderChat` message shapes in `engine/provider.go`; the strip-then-parse discipline in `batchResultText` (engine/agents_recap_batch.go) and `~/bin/jev-shim`'s `_parse_answers`.

**Test scenarios:**
- Happy path: stub chat endpoint returns `{"worthy":0.9,"quality":0.2}` → `decide()` yields `{worthy:0.9, quality:0.2}`, model logged as the local slug.
- Think-tag noise: `…<think>blah</think> {"worthy":0.7} trailing text` → parses 0.7.
- Prose-only / malformed JSON response → error (caller fallback path preserved).
- Missing key: response has `{worthy}` but not `quality` → map contains only `worthy` (no fabricated 0).
- Out-of-range values (`-1`, `42`) → clamped to `[0,1]`.
- Integration: `judgeBlock` against the chat stub picks `argmax(cat_*)` category + productive bool correctly (proves the full block path, not just `decide`).

### U3. Zero-egress verification + docs

**Goal:** prove and document the fully-local recipe.

**Requirements:** R5, R6

**Dependencies:** U1, U2

**Files:**
- `README.md` — config rows for `decisions_url`/`decisions_model`/`decisions_api_key`; a "Fully local" recipe (vision `api_base_url` → Ollama, `decisions_url` → local chat endpoint, `task_provider` → local providers)
- `PRIVACY.md` — update the Jev egress row: classification egresses only when the decisions endpoint is non-local
- `docs/research/macos-agents-section.md` or a short new note under `docs/research/` — record the jev-shim contract incompatibility finding
- `engine/decisions_test.go` (or `decisions_local_test.go`) — egress assertions below

**Approach:** a test asserting the fully-local config produces zero non-localhost requests — stub the provider layer the way existing tests stub `openRouterURL`, configure all-local endpoints, run a `decide()` + a `judgeBlock`, assert no request left the loopback stubs and `llm_calls` records only local providers.

**Test scenarios:**
- Configured fully local → `llm_calls` rows reference only `127.0.0.1`/localhost providers; stub counters show zero OpenRouter hits.
- `decisions_url` unset + OpenRouter key present → OR path unchanged (regression).

**Verification:**
- `go test ./engine` green; `go vet` clean.
- Live: point `decisions_url` at `http://127.0.0.1:8091/v1` (llama-jev, qwen3-4b) → `dayflow status`/`summarize --now` produces judgments with zero WAN traffic (verify via `llm_calls` provider field and `api_calls`).

## Risks & Dependencies

- **Local judge quality.** A 4B local model scoring `noul` questions won't match calibrated `typesafe/jev-1.13` — mitigated by keeping scores advisory (all existing consumers already treat missing/low scores as fallback signals; nothing hard-depends on Jev accuracy). Doc notes the quality trade-off; a real comparison is deferred.
- **Prompt-format drift.** qwen3/Ornith variants may wrap JSON in think-tags or prose — the lenient parser covers the common cases; a hard-fail still lands on the heuristic fallback, so the blast radius is "Jev silent" not "wrong verdict."
- **`classification_model` reused as local slug** — if a user sets `decisions_url` local but leaves `classification_model` as `typesafe/jev-1.13`, the local endpoint gets a nonsense model name; `decisions_model` exists so they don't collide, and docs call this out.
- The **`classification` TaskProvider key** already exists in `providerForTask`'s keychain (`jevAPIKey` consults it) — no routing-table changes needed; docs should mention `task_provider.classification` can point the *key resolution* at a provider, though `decisions_url` is the real knob.

## Sources / Research

- Probe this session: `jev-shim` source (`~/bin/jev-shim`) — contract `{score|choice, confidence}` 0–2 scale, wisp system prompt; service currently inactive. Verdict: incompatible, not reused.
- OpenRouter decisions contract: `engine/decisions.go` (noul questions, `answers.{q}.noul` 0–1, Authorization Bearer, attribution headers).
- Local endpoints available on this machine: Ollama `:11434` (Ornith, CPU), llama.cpp `:8080` (Ornith-35B GPU), `:8091` (qwen3-4b, wisp decisions), `:8931` (jev-shim).
