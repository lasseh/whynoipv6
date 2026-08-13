# whynoipv6-new

## Agent skills

### Issue tracker

Agent issues live as local markdown under `.scratch/<feature-slug>/`, not on GitHub. The
repo publishes to `lasseh/whynoipv6`, whose issues are the public bug/feature surface;
agent working tickets stay local and PRs are not a triage surface. See
`docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary — needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context — one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
