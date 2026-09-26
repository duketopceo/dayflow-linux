---
title: "chore: v1.2.0 release — version sync, merge, tag, marketplace revalidation"
date: 2026-09-26
type: chore
depth: standard
---

# chore: v1.2.0 release — version sync, merge, tag, marketplace revalidation

## Summary

The v1.2 feature work (agent recaps, goal streaks, WoW trends, split storage
caps) is complete on `feat/v12-deepening` behind PR #25, but every version
surface still declares 1.1.0 and the marketplace submission on issue #4992 is
stuck in a SHA-drift rejection loop. This plan takes the branch through a final
review-fix + version-bump commit, owner-gated merge, `v1.2.0` tag and release,
live redeploy, and a fresh marketplace validation + security-baseline request
bound to one frozen SHA.

## Problem Frame

Three separate clocks are out of sync:

- **Code** on the branch is v1.2-complete but `engine/main.go` and
  `manifest.json` both declare `1.1.0`; the deployed binary reports the same.
- **PR #25** is open and mergeable with green required CI, but CodeRabbit keeps
  returning `CHANGES_REQUESTED` — round 3 (2026-09-25) found two more findings
  introduced by the round-2 fixes.
- **Marketplace issue #4992** was validated at `105dd37` but master has moved
  repeatedly since; the maintainer requires one frozen commit, fresh
  validation, and a fresh security baseline. Four rejections so far were all
  SHA drift, not code findings.

The failure mode to engineer around: any push to master after the
revalidation request restarts the whole review. The plan therefore treats
freeze-and-hold as a first-class requirement, not a footnote.

## Assumptions (defaults taken — unconfirmed call-outs)

- Merge approval of #25 and the revalidation comment on #4992 remain
  **owner-gated** — the plan marks them as gates, not agent steps. Granting
  merge to the agent converts them without other plan changes.
- `preview.png` (dated 2026-09-11, v1.0-era) is refreshed by the owner from an
  interactive FullView on live data before the final merge, or consciously
  skipped — a stale preview ships inside the frozen SHA either way.
- Internal docs (`docs/plans/`, `docs/publish/`, `docs/HANDOFF-*.md`) stay in
  the tree — removing them does not change what the security review reads and
  they are committed history regardless.

## Requirements

- R1: every live version surface reports `1.2.0` — `dayflow --version`,
  `manifest.json`, and the version field emitted in engine JSON output.
- R2: the frozen tree carries no known actionable review findings — CodeRabbit
  round-3 comments are applied or consciously declined.
- R3: master contains the complete v1.2 tree at a single merge SHA.
- R4: tag `v1.2.0` and a GitHub release exist pointing at that SHA.
- R5: the live install on this machine runs the tagged build — binary,
  deployed plugin directory, and capture service verified.
- R6: #4992 carries a revalidation + security-baseline request naming the
  frozen SHA, and no commit lands on master while the review is pending.

---

## Key Technical Decisions

- **KTD1 — Bump the version on the feature branch, not post-merge.** The merge
  commit then IS the release tree: one SHA feeds the tag, the release, and the
  marketplace attestation. A post-merge version commit would create a second
  SHA and re-open the drift window.
- **KTD2 — Round-3 fixes and the version bump ride the same final push.** One
  CI run closes the review surface; serial pushes just invite another
  CodeRabbit round before merge.
- **KTD3 — Freeze means tag → revalidate-request → hold.** The SHA-drift
  rejection loop's only fix is zero pushes to master until the baseline lands.
  The hold is a rule of the plan, with an explicit escape (revert-and-refreeze)
  if a maintainer finding requires a code change.
- **KTD4 — Merge approval and the #4992 comment are owner gates.** Marked as
  gates in their units so execution stops cleanly there rather than taking an
  ungranted external action.
- **KTD5 — `preview.png` refresh must land on the branch before merge.** The
  marketplace card renders from the frozen tree; anything committed after the
  freeze is outside the reviewed SHA.

---

## Implementation Units

### U1. CodeRabbit round-3 findings

**Goal:** the two round-3 comments are resolved on the branch.
**Requirements:** R2. **Dependencies:** none.
**Files:** `FullView.qml`, `Settings.qml`.

**Approach:**

- `FullView.qml` — give the agents loader a per-run identity: an incrementing
  run counter set in `agentsLoad()`, captured by the stdout/exit/watchdog
  callbacks, ignored when stale. A watchdog-stopped process's late callbacks
  can then no longer clobber a newer run's `agentSessions`/`agentsError`/
  `agentsLoading` after `agentsTimedOut` was reset.
- `Settings.qml` — the retention `Row` now holds five fields sized for three
  columns and does not wrap; the legacy-cap control is clipped off-viewport.
  Reflow the container (wrap or re-bucket the fields) so every control is
  reachable.

**Patterns to follow:** the existing `agentsPending`/`agentsTimedOut` state
discipline in `FullView.qml`; `Flow` or a second `Row` matching how other
multi-row Settings sections are already structured.

**Test scenarios:**

- Render-only QML, no harness — verification is review-diff correctness plus a
  live check that the Agents pane still loads sessions and the Settings
  storage section shows all five controls inside the viewport.
- Regression check on the round-2 behavior: a timed-out agents run still shows
  the timeout message and is not overwritten by late stdout.

**Verification:** findings answered on the PR; engine suite unaffected
(`go test -count=1 ./...` stays green — no Go files touched).

### U2. Version metadata → 1.2.0

**Goal:** every live version declaration reads 1.2.0.
**Requirements:** R1. **Dependencies:** none.
**Files:** `engine/main.go` (`const version`), `manifest.json`.

**Approach:** set both literals to `1.2.0`. Sweep for any other live `1.1.0`
reference — historical `docs/plans/*.md` mentions are frozen history and stay.
Confirm nothing else interpolates the old value (the `version` const also
feeds engine JSON output around `engine/main.go:1219`).

**Test scenarios:**

- Build the binary and assert `--version` output equals `1.2.0`.
- `manifest.json` parses and carries `"version": "1.2.0"`.
- Grep confirms no live `1.1.0` literal outside `docs/plans/` history.

**Verification:** the built binary self-reports 1.2.0 and the manifest matches.

### U3. Pre-merge gate

**Goal:** the branch is in its final mergeable state — nothing lands after
this point except the merge itself.
**Requirements:** R2, R3. **Dependencies:** U1, U2.
**Files:** `preview.png` (conditional), PR #25 body.

**Approach:**

- Confirm required CI (`build-and-test`, GitGuardian, Socket) green on the
  final commit; CodeRabbit is advisory but its comments should show resolved
  or replied-to.
- Preview decision executed: owner captures a fresh FullView screenshot over
  v1.2 features (recaps/streak chip/WoW deltas) and it is committed to the
  branch, or the stale image is consciously accepted.
- PR body reflects reality — the deferred-findings/residuals section still
  names what was intentionally left.

**Owner gate:** approve and merge PR #25 (merge commit preserving the branch
commits — matches repo convention).

**Test expectation: none** — ops gate, no code change.

**Verification:** PR shows merged; `origin/master` HEAD is the merge commit
whose tree contains U1+U2.

### U4. Tag + GitHub release

**Goal:** the frozen SHA is named and published.
**Requirements:** R4. **Dependencies:** U3.
**Files:** none (git ref + GitHub release object).

**Approach:** tag `v1.2.0` at the merge commit (lightweight or annotated —
repo has used lightweight `v1.1.0` style), push the tag, create the GitHub
release with generated notes plus a short feature summary (recaps, streaks,
WoW trends, split storage caps, retention change). **From this point master
is frozen: no pushes until the marketplace baseline lands (KTD3).**

**Test expectation: none** — release ops.

**Verification:** `git tag` lists `v1.2.0` on the merge SHA; the GitHub
release page exists and renders notes.

### U5. Deploy + live verification

**Goal:** this machine runs the released build.
**Requirements:** R5. **Dependencies:** U4.
**Files:** deployed artifacts only — `~/.local/bin/dayflow`,
`~/.config/omarchy/plugins/io.github.duketopceo.dayflow/`,
`~/.config/dayflow/config.json` (unchanged — split caps already live).

**Approach:** build from the tagged commit, install the binary, sync the
plugin directory (`rsync --delete`, then `diff -rq` clean), restart
`dayflow-capture`, and smoke the v1.2 surfaces: `--version` prints 1.2.0,
`stats` shows the split caps (20480/10240), `agents` serves a cached recap,
`goal --json` carries streak, `weekly --json` carries `trends` with
`total_delta_minutes`/`shift_delta_count` populated.

**Test expectation: none** — deploy verification; engine suite already green.

**Verification:** every smoke check above reports the released values on the
live service.

### U6. Marketplace revalidation

**Goal:** #4992 is re-anchored to the frozen SHA and driven to a decision.
**Requirements:** R6. **Dependencies:** U4 (needs the frozen SHA to exist).
**Files:** none (issue comment + maintainer loop).

**Approach:**

- Post the revalidation request on #4992 naming the exact merge SHA and
  asking for fresh validation + security baseline — same mechanism as the
  prior successful triggers.
- Hold the freeze (KTD3): nothing pushes to master while validation is
  pending. If the review returns findings, decide per finding: fix-on-branch
  + new PR + re-freeze, or defer with a maintainer-visible note.
- Track the issue to a terminal state (approved, or findings applied and
  re-validated).

**Owner gate:** the comment posts under the owner's account — owner posts it
or explicitly grants it.

**Test expectation: none** — external process.

**Verification:** the issue shows a fresh validation/baseline against the
merge SHA, and the `needs-fixes`/`security-review-required` labels resolve.

---

## Scope Boundaries

**Deferred to follow-up work:** the intermittent `grab failed` capture bug
(Sep 16–17 frame loss); re-running the three failed summarize windows
(09-21 01:00, 09-22 02:30/02:45); slimming internal docs out of the deployed
plugin directory; a QML test harness.

**Non-goals:** packaging beyond tag + release (no AUR, no binary artifacts —
repo convention is "tag + verified install"), any new feature work, changes to
`retention_days` or cap values already live on this machine.

---

## Risks & Dependencies

- **SHA drift (repeat offender):** four prior marketplace rejections were all
  the attested SHA going stale. KTD1+KTD3 exist precisely for this; the
  residual risk is a fix-required finding mid-review, which forces
  revert-and-refreeze.
- **CodeRabbit nit-per-push pattern:** each push has produced one more round
  of minor findings. The merge decision accepts minor-only residual findings
  as owner choice — chasing zero forever is the drift risk in disguise.
- **`preview.png` needs a human:** capturing FullView requires the interactive
  Wayland session; an agent cannot produce it headlessly.
- **Review-state block:** `CHANGES_REQUESTED` may need the owner to dismiss or
  approve over CodeRabbit's review before merge, depending on branch
  protection.

## Sources & Research

- Verified live this session (2026-09-26): PR #25 `OPEN`/`MERGEABLE`/
  `CHANGES_REQUESTED` at `6b49bb7`; master at `f8a6290`; CodeRabbit round-3
  comments on `FullView.qml` + `Settings.qml`; #4992 open with
  `needs-fixes`/`security-review-required`, last maintainer note 2026-09-22
  naming the drift explicitly.
- Version literals: `engine/main.go` (`const version`, also feeds JSON
  output), `manifest.json`; all other `1.1.0` mentions are frozen plan docs.
- Prior release convention: `docs/plans/2026-09-16-0128-feat-dayflow-1-0-release-plan.md` — "tag + verified install, not packaging."
