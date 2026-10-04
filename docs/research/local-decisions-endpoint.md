# Research: local decisions endpoints for Jev judgments

Date: 2026-10-03. Scope: finding recorded while implementing
`docs/plans/2026-10-03-001-feat-full-local-model-support-plan.md`.

## Finding: a decisions-API shim is not a drop-in backend

A locally running "jev-shim" service (an HTTP shim that emulates a
judgment/decisions API for another tool) was probed as a candidate backend
for `decide()`. **Verdict: contract-incompatible — not integrated.**

- Shim answers `{score|choice, confidence}` on a **0–2 scale** under its own
  tool-specific system prompt.
- `decide()` requires the OpenRouter decisions contract:
  `{model, state, questions:{key:{instructions, type:"noul"}}}` →
  `{answers:{key:{type:"noul", noul:<0-1 float>}}}`.
- Wired in as-is, the shim would parse to an empty answers map — a silent
  total no-op that looks like "Jev has no opinion", not a misconfiguration.
- Reusing it means rewriting the other tool's service for dayflow's
  contract — rejected. The `decisions_url` chat transport lands the same
  outcome with no shim at all.

## What shipped instead

`decisions_url` selects a transport by URL shape (normalized the same way
as `api_base_url`):

| `decisions_url` shape | Transport | Request |
|---|---|---|
| empty | OpenRouter decisions API (default) | `{model,state,questions}` contract |
| `*/chat/completions`, or a base that normalizes to `*/v1` | chat transport | one user message, `{"key": 0-1}` JSON reply |
| anything else | decisions contract, mirrored | `{model,state,questions}` contract |

The chat path renders state + each question's instructions, asks for one
strict `{"<key>": <0-1 float>}` object at `temperature: 0`, strips
`<think>…</think>` noise, takes the first JSON object, clamps each score to
`[0,1]`, and tolerates missing keys — total parse failure errors out so
callers keep their heuristic fallbacks.

## Caveats recorded

- A small local judge (e.g. a 4B chat model) will not match calibrated
  `typesafe/jev-1.13` scores. Missing/failed judgments fall back to
  heuristics, but a valid score is applied — so a weak judge degrades
  verdict quality rather than failing safe. A real quality comparison is
  deferred follow-up.
- `decisions_model` exists so a local slug does not collide with the Jev
  slug that `classification_model` carries.
- Non-OpenRouter endpoints get no `Authorization` header unless
  `decisions_api_key` is set, and never get OpenRouter attribution headers.
