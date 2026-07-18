# WhyNoIPv6 — docs

## Start here

- **[`architecture.md`](architecture.md)** — how the system fits together (components,
  the crawl → confirm → publish pipeline, design decisions).
- **[`deploy.md`](deploy.md)** — run it locally with Docker Compose, the full `v6ctl`
  tool catalog, and the production layout (systemd, nginx, backups).
- **[`internals.md`](internals.md)** — the codebase tour: package map, life of a scan,
  DB layer, conventions, frontend structure.

## Authoritative: the build spec

**[`spec/`](spec/)** is the single source of truth for the implementation. Start at
[`spec/00-overview.md`](spec/00-overview.md). It is Round 3.0 — clean root API, keyset
pagination, RFC 9457, no legacy compat, no history import. If any other document here
disagrees with `spec/`, the spec wins.

## Decisions ([`adr/`](adr/))

Post-spec architectural decisions, one file per decision. Anything decided after the
spec froze lands here, not in `spec/`.

## Runbooks ([`runbooks/`](runbooks/))

Operational procedures: [`cutover.md`](runbooks/cutover.md) (the production DNS-flip
checklist — build gate green, **production cutover still pending**),
[`frontier-surgery.md`](runbooks/frontier-surgery.md),
[`timescale-jobs.md`](runbooks/timescale-jobs.md), [`unbound.md`](runbooks/unbound.md).

## Active / forward-looking

- **[`feature-research.md`](feature-research.md)** — evidence-backed post-launch feature
  roadmap (State-of-IPv6 report, provider league tables, notification toolkit, …).

## History ([`history/`](history/))

Frozen artifacts of how we got here — no longer referenced by the build, kept for the
record.

- `backend-research-brief.md` — the original product/requirements brief (the input to the whole design).
- `backend-design.md` — the Round 2.0 design rationale (its API sections are obsolete; superseded by `spec/07-api.md`).
- `api-design-research.md` — the modern-API redesign research and the 15 resolved OPEN decisions (2026-07-09); cited by every spec file's Round 3.0 header.
- `design-refresh-prompt.md` — the frontend redesign brief (redesign shipped).
- `spec-readiness-review.md` — the two-pass audit (35 findings) folded into Round 2.0.
- `fable5-research-kickoff.md` — the kickoff prompt for the first research round.
- `gates/` — recorded build-phase gate evidence (P0–P2 + P1 spikes).

## Agent scaffolding ([`agents/`](agents/))

Local agent conventions (issue tracker under `.scratch/`, triage labels, domain docs) —
see the repo-root `CLAUDE.md`.
