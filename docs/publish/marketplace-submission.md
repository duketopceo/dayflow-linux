# Marketplace submission draft — Dayflow

Status: **draft, owner-gated.** Do not submit without explicit approval —
the marketplace creates a real public issue and snapshots the exact commit.

## Pre-submit checklist (run right before submitting)

- [ ] `omarchy plugin validate .` — clean
- [ ] Decide whether `preview.png` needs a refresh: open Full view on a day
      with real data (timeline + Jev flags visible), screenshot it, replace
      `preview.png`, commit. Optional — current preview is representative.
- [ ] Note the HEAD commit — approval binds to the exact validated commit;
      merging anything else first means the listing shows "Update unverified".

## Submit command

```bash
gh issue create \
  --repo omacom/omarchy-plugin-marketplace \
  --title "[Plugin]: Dayflow" \
  --body-file docs/publish/submission-body.md
```

## Issue body (verbatim, keep all six headings in order)

The body below is duplicated into `submission-body.md` so `--body-file` reads
a clean file. If you edit one, edit both.

---

### Repository URL

https://github.com/duketopceo/dayflow-linux

### Category

Productivity

### Tags

ai, hyprland, quickshell

### Suggest a missing tag

local-first

### Maintainer notes

Dayflow is a local-first automatic work journal: it screenshots the desktop
every 10s, dedupes frames, and summarizes activity into a timeline via an
OpenRouter vision model (any provider endpoint works — vision, summary, and
classification tasks are independently routable). TypeSafe Jev (OpenRouter
decisions API) provides calibrated category/judgment calls; all egress is
config-gated and documented. Frames and the SQLite journal never leave the
machine except sampled frames sent to the configured model. External
dependency: an OpenRouter API key (or any compatible chat-completions
endpoint, including local providers).

### Submission checklist

- [x] The repository is public and contains installation and removal instructions.
- [x] I have documented the plugin license and any external dependencies.
- [x] I confirm that I own or have permission to submit this plugin and its preview assets.
- [x] The plugin does not overwrite user configuration without explicit consent.
- [x] I understand that approval is for listing and is not a security review.

---

## After submitting

- The marketplace bot posts one validation comment + one Automated Security
  Baseline comment; a `passed` baseline becomes Verified automatically once
  a maintainer applies `approved-and-verified`.
- If validation doesn't fire, edit the issue (re-runs detection) — verify
  the `[Plugin]:` title prefix and all six headings are intact.
- To retry after fixes: fix in a new commit, edit the issue, do not open a
  duplicate.
- Later commits to the repo show "Update unverified" on the listing until
  rescanned — plan merge cadence accordingly.
