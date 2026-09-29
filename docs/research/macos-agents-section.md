# Research: macOS Dayflow "Agents" section vs the port's AgentsPane

Date: 2026-09-29. Method: web research only (no upstream checkout on this
machine; upstream = `github.com/JerryZLiu/Dayflow`, SwiftUI, currently ~v2.2.x).

## Confidence

- **Verified**: upstream repo, upstream feature existence (asserted by our own
  parity plans: `docs/plans/2026-09-05-001-...roadmap...` line 53 defers
  "Native agent recap UI for Codex/Claude CLI sessions"; `docs/plans/2026-09-14-2351`
  calls it "Agents/Fable-style session recap" — i.e., upstream recaps exist for
  Claude Code + Codex transcripts only).
- **Strong inference**: the standalone `github.com/JerryZLiu/AgentPlayback`
  (MIT, "from the makers of Dayflow", brand-links dayflow.so) is the same
  author's implementation of this exact feature — Claude Code + Codex session
  logs rendered on a 24h dial. The in-app Agents section shares this design.
  File-level Swift paths could not be confirmed via search indexing.
- **Unknown**: exact upstream SwiftUI file names; whether in-app Agents uses
  LLM-generated recap text (likely — the app has `TextProviderActions`
  freeform generation via `LLMService.swift`).

## What upstream's Agents section shows

Sources scanned: `~/.claude/projects/**/*.jsonl` and
`~/.codex/sessions/YYYY/MM/DD/*.jsonl` only (confirmed by AgentPlayback's
scanner, `scripts/scan-day.mjs`, and by our parity plan naming upstream's
sources "Codex/Claude CLI sessions").

Layout (AgentPlayback design, the same author's agents view):
- **24-hour clock/vinyl dial** as the centerpiece — each agent session is an
  arc ("thread") around the dial; arcs are segmented into **"Agent working"**
  vs **"Agent waiting"** ranges (`blockedRanges` in the day JSON) so you can
  see at a glance when the agent was autonomous vs blocked on your input.
  Parallel agents stack as concentric grooves.
- **Per-day summary panel**: "Total hours worked by agents", task count,
  project count, total waiting time.
- **Project donut**: per-project/category hour breakdown (`day.donut`),
  human-readable task labels (`day.labels`), colors assigned by
  `projectColorOrder` = projects ranked by trailing-30-day cost so colors
  stay stable across days.
- **Cost/tokens**: `day.tokens.cost.{openai,claude}` estimated from a bundled
  `model-prices-cache.json`, with `byCategory`/per-provider breakdowns; the
  month calendar cells are heat-tinted by cost.
- **Month calendar popover** behind the date label: per-day agent count +
  cost heat; ‹ › day navigation in the header.
- **Skins**: paper / vinyl / electric picker (glass hidden behind a query
  flag) — the dial is heavily art-directed ("grooves", "record notch").
- Subagent/non-task threads render `dotted` (de-emphasized).
- Interaction: day nav + calendar picker + hover/click callouts on arcs;
  refresh via background rescan (`POST /api/scan`, ~5min cooldown).
- Tagline/copy: "Are you running too few agents, or too many?" / "when each
  one ran, when it was blocked on you, and what it cost."

Data flow upstream: scan transcripts → per-day JSON (`day-YYYY-MM-DD.json` +
`days.json` index under `~/.agentplayback/data`); no API keys needed for the
dial itself — waiting/working is derived from transcript turn structure, cost
from token counts × bundled prices.

## Port baseline

- `AgentsPane.qml`: flat chronological list of session cards — colored source
  badge (claude=amber, else blue), project name, `hh:mm–hh:mm · N msgs`,
  title line, italic one-line `recap` + `recap_confidence%`. DayNavRow +
  BusyBar + error line + empty-state text + per-source drift notes + recaps
  opt-in hint. No grouping, no dial, no cost, no waiting-state.
- `engine/agents.go`: 5 sources via `agentSource` seam —
  claude (`~/.claude/projects`), codex (`~/.codex/sessions`), opencode
  (`~/.local/share/opencode/opencode*.db`), devin (`~/.local/share/devin/cli`
  sessions.db + ATIF transcripts), cursor (`~/.config/Cursor/User/
  globalStorage/state.vscdb`). Per-source scan status + drift flags.
- `engine/agents_recap.go`: opt-in (`agent_recaps` config) one-sentence recap
  per session — Jev worthiness gate (<0.4 → cached "unworthy"), chat-model
  generation (≤20 words), Jev quality gate with one grounded regeneration;
  ≤2 KB scrubbed excerpt (first/last user msg + last assistant; home paths +
  secrets redacted); fingerprint-cached; 8-recap cap + 45s deadline per pass.

## Feature diff

The "AgentPlayback" column reflects the inferred macOS sibling app
(AgentPlayback), not verified in-app Dayflow behavior — the exact Dayflow
SwiftUI UI is unknown from research alone, so "present" there means
"present in AgentPlayback's public surface".

| Feature | AgentPlayback (macOS) | Port | Verdict |
|---|---|---|---|
| Session list w/ badge, project, times, msg count | present | present | parity |
| Per-session LLM recap | present (Claude/Codex) | present, + worthiness/quality gates + opt-in + egress scrubbing | port better |
| Sources | 2 (claude, codex) | 5 (+ opencode, devin, cursor) | port better |
| Store drift/unavailable surfacing | unknown/likely absent | present (sources[] status + drift) | port better |
| Working-vs-waiting segmentation | present (blockedRanges arcs) | absent | upstream better |
| 24h dial/vinyl visualization | present | absent | upstream better |
| Day summary strip (agent-hours, tasks, waiting) | present | absent | upstream better |
| Project/task grouping + stable colors | present (donut + labels + projectColorOrder) | absent (flat list) | upstream better |
| Token/cost estimates | present (model-prices cache) | absent | upstream better |
| Month calendar w/ cost heat | present | absent (DayNavRow only) | upstream better |
| Parallelism/"am I the bottleneck" framing | present (the point of the dial) | absent | upstream better |

## Worth porting, ranked

1. **Working-vs-waiting segmentation** — the signature upstream insight
   ("spot when you're the bottleneck"). Derivable from transcript turn
   timestamps we already parse (assistant-active runs vs gaps awaiting a
   user turn). Expose `active_ranges`/`waiting_ranges` on AgentSession;
   render as two-tone spans. Medium effort, highest information gain.
2. **Day summary strip** — total agent-hours, session count, waiting time
   for the viewed day. Cheap aggregation over existing sessions; big "at a
   glance" win. Do together with #1 since waiting needs the same ranges.
3. **Project grouping + stable colors** — group sessions under project
   headers (we already emit `project`); pick badge colors per project or
   keep per-source. Low effort.
4. **Linear 24h swimlane (dial-lite)** — the full vinyl dial is heavy
   art-direction + a 3D/Canvas project; a horizontal 24h lane per session
   (or one lane per source) with working/waiting segments gets ~80% of the
   readability in plain QML Rectangles. Do this before any circular dial.
5. **Token/cost estimate** — needs per-source token extraction + a bundled
   price table; medium effort, medium value (nice for the calendar heat
   later).
6. **Month calendar heat (agents/day, cost/day)** — small extension once
   day summaries exist; could tint CalendarPicker cells.
7. ~~Full vinyl/three.js dial~~ — port's QML-native constraint makes this
   disproportionately expensive; skip unless #4 proves the metaphor lands.

## Sources

- https://github.com/JerryZLiu/Dayflow
- https://github.com/JerryZLiu/AgentPlayback (README + `bin/cli.js`,
  `scripts/scan-day.mjs`, `package.json`, UI overlay/calendar/summary code)
- https://whnex.com/items/49456949 (author's Show HN describing the
  clock-dial rationale)
- Port: `AgentsPane.qml`, `engine/agents.go`, `engine/agents_recap.go`,
  `engine/agents_{opencode,devin,cursor}.go`, `docs/agent-contract.md`
  (get_agent_sessions contract), `docs/plans/2026-09-14-2351-...` (upstream
  "Agents/Fable-style session recap" reference), `docs/plans/2026-09-27-001-...`
  (5-source extension rationale).
