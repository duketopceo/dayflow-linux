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
config-gated and documented. Sampled frames go to the configured vision provider, TypeSafe Jev judge
calls carry small block/session descriptors to the decisions endpoint,
and agent-session recap generation sends a bounded, scrubbed transcript
excerpt to the chat provider — all config-gated (`jev_classification`,
`agent_recaps`). External
dependency: an OpenRouter API key (or any compatible chat-completions
endpoint, including local providers).

### Submission checklist

- [x] The repository is public and contains installation and removal instructions.
- [x] I have documented the plugin license and any external dependencies.
- [x] I confirm that I own or have permission to submit this plugin and its preview assets.
- [x] The plugin does not overwrite user configuration without explicit consent.
- [x] I understand that approval is for listing and is not a security review.
