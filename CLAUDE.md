# whynoipv6-new

## Agent skills

### Issue tracker

Agent issues live as local markdown under `.scratch/<feature-slug>/`, not on GitHub. The
repo publishes to `lasseh/whynoipv6`, whose issues are the public bug/feature surface;
agent working tickets stay local and PRs are not a triage surface. Conventions and the
triage-status vocabulary: `docs/agents/issue-tracker.md`.

### Domain docs

Single-context — one `CONTEXT.md` + `docs/adr/` at the repo root. Read `CONTEXT.md` and
the ADRs touching your area before exploring; use the glossary's terms in your output,
and surface a conflict with an existing ADR rather than silently overriding it.
