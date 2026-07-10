# WhyNoIPv6 — docs

## Authoritative: the build spec

**[`spec/`](spec/)** is the single source of truth for the implementation. Start at
[`spec/00-overview.md`](spec/00-overview.md); the executable build plan is
[`spec/11-implementation-plan.md`](spec/11-implementation-plan.md). It is Round 3.0 —
clean root API, keyset pagination, RFC 9457, no legacy compat, no history import. If any
other document here disagrees with `spec/`, the spec wins.

## Current provenance (why the spec looks the way it does)

Kept because the spec cites them and they carry the rejected-alternatives reasoning the
spec doesn't restate. Read them for *why*, not *what*.

- **[`api-design-research.md`](api-design-research.md)** — the modern-API redesign (cited by
  every spec file's Round 3.0 header): versioning, pagination, resource model, and the 15
  resolved OPEN decisions (2026-07-09).
- **[`backend-design.md`](backend-design.md)** — the Round 2.0 design rationale, cited by the
  spec as "design §N" (phase plan, package layout, engine-adaptation contract). **Its API
  sections are OBSOLETE** — superseded by `spec/07-api.md` + `api-design-research.md` (see the
  banner at the top of that file).

## Active / forward-looking (pending work, not yet built)

- **[`feature-research.md`](feature-research.md)** — evidence-backed post-launch feature
  roadmap (State-of-IPv6 report, provider league tables, notification toolkit, …).
- **[`design-refresh-prompt.md`](design-refresh-prompt.md)** — the frontend redesign brief
  (the Vue app is rebuilt against the committed `openapi.yaml`; this explores its visual
  direction).

## History ([`history/`](history/))

Frozen artifacts of how we got here — no longer referenced by the build, kept for the record.

- `backend-research-brief.md` — the original product/requirements brief (the input to the whole design).
- `spec-readiness-review.md` — the two-pass audit (35 findings) folded into Round 2.0.
- `fable5-research-kickoff.md` — the kickoff prompt for the first research round.

## Agent scaffolding ([`agents/`](agents/))

Local agent conventions (issue tracker under `.scratch/`, triage labels, domain docs) —
see the repo-root `CLAUDE.md`.
