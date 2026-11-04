# 2026-10-23-001-fix-frame-integrity-and-summarizer-reliability

**Status:** proposed
**Source:** data audit + live repair, 2026-10-23

## What the audit found

- 45,990 `frames` rows. **5 files on disk are 0 bytes** while the DB declares
  36,593 / 47,031 / 41,160 bytes for them. All 5 sit on three days
  (2026-10-03, 10-09, 10-13).
- Those 5 poisoned exactly **3 blocks**. One 0-byte frame is enough to fail a
  15-minute window: the payload builder base64s the empty file into a data-url
  and the provider returns
  `400 {"error":{"message":"Invalid image data-url"}}`.
- **Reproduced, not guessed.** Requeued the three windows as
  `status='failed', attempts=0, triaged=0` and ran the summarize unit: all three
  failed again with the identical 400. That rules out a transient network fault
  and confirms a data defect.
- **Repair that worked:** delete the 5 worthless frame rows, requeue, re-summarize.
  All three flipped to `done`. DB went from 4 dead + 3 failed to
  **1,869 done / 0 dead / 0 failed**.
  Pre-mutation backup: `~/.local/share/dayflow/backups/dayflow-pre-requeue-<stamp>.db`
  (taken with `sqlite3 .backup`, not `cp`, because WAL).
- Separately: `api_calls` on `google/gemma-4-31b-it` shows **1,716 ok / 263 error
  (13.3%)**, max latency **550s on errors** vs 181s on successes.
- `events` shows **1,215 `block_dead`** over 42 days, yet only 4 blocks were ever
  simultaneously dead. The terminal state is being produced and re-produced.

## Work items

### R1 - Never write a 0-byte frame

**Why:** 5 short writes poisoned 3 windows. A truncated write is indistinguishable
from a valid capture until a provider rejects it minutes later, with an error that
names the payload rather than the write.

**Do:** write the JPEG to a temp path, fsync, verify `size > 0`, then rename into
`frames/<date>/`. On a short write, do not insert a `frames` row at all; emit
`capture_error` with the reason.

**Accept:** killing the daemon mid-write, or suspending the laptop, leaves no
0-byte `.jpg` under `frames/`, and the failure surfaces as a `capture_error` event.

**Files:** `engine/capture.go` (`captureOnce` write path)

### R2 - Block assembly must skip invalid frames

**Why:** one bad frame fails a window in which 17 of 18 frames were fine. The
window is recoverable; the current builder makes it unrecoverable.

**Do:** before building the payload, drop frames that are missing, 0 bytes, or fail
a JPEG magic check (`FF D8 FF`). Log the skip count and summarize from the rest.

**Accept:** a window containing a 0-byte frame summarizes from the remaining frames
and reports how many it skipped.

**Files:** the summarize payload builder

### R3 - Summarizer client: retry with backoff, and a real timeout

**Why:** a 13.3% error rate spends the three-attempt budget on transient faults and
then lands the block in a terminal state. Max 550s on error against 181s max on
success means the timeout sits well outside the normal latency tail, so hung
connections are waited out instead of retried.

**Do:** retry 429/5xx/timeout/DNS with bounded exponential backoff. Set the request
timeout below the observed success tail (180-240s, not 550s). Treat a truncated or
non-JSON body as retryable too.

**Accept:** a synthetic timeout or DNS fault recovers within one run; `api_calls`
error rate drops below 2%.

**Files:** the summarize HTTP client

### R4 - `dead` must not be terminal without an alarm

**Why:** `blockExists` counts `done` and `dead` as existing, so a dead window is
invisible to every later run and stays dead forever. 1,215 `block_dead` events show
this happening routinely; it is silent by design.

**Do:** requeue dead windows on a schedule (only those that still have frames), or
alert when a block enters `dead`. Silence is the current failure mode.

**Accept:** a deliberately planted 0-byte frame produces an alert, not silence.

**Files:** engine state machine, `engine/notify.go`

## Recorded, not fixing yet

- **Capture efficiency:** 131,003 `capture_deduped` vs 50,526 `capture_saved` -
  69% of ticks are discarded after a full decode. A prefilter (byte length +
  prefix hash) before decode would cut most of that cost.
- **`agent_ingest`:** 55,334 rows, no retention.
- **`journal_entries` (0 rows) and `day_goals` (1 row):** dead tables. Build or drop.
- **Frames retention:** 13 GB / 42 days, about 113 GB/year, against **777 GB free**.
  Monitor, not urgent - do not treat this as a priority.
- **Minor:** `auto_vacuum=0` with 811 free pages; `user_version` is 0 while
  `schema_migrations` says 5. Pick one versioning convention.
