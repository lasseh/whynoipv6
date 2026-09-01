# WhyNoIPv6 — docs

## Start here

- **[`architecture.md`](architecture.md)** — how the system fits together (components,
  the crawl → confirm → publish pipeline, design decisions).
- **[`deploy.md`](deploy.md)** — run it locally with Docker Compose, the full `v6ctl`
  tool catalog, and the production layout (systemd, nginx, backups).
- **[`internals.md`](internals.md)** — the codebase tour: package map, life of a scan,
  DB layer, conventions, frontend structure.

## What is authoritative

**The shipped code is.** For any question about current behavior, read the code — then
[`adr/`](adr/) for decisions taken since launch, then the three docs above.

[`spec/`](spec/) is the **frozen design record** that drove the build, not a live
contract. It is thorough and still worth reading for *why* — section numbers are cited
from ~150 source-file headers (`db/query/*.sql` → `05-schema.md`, `internal/checker/*`
→ `01-engine.md`, `deploy/**` → `09-ops.md`), so those citations stay resolvable. But
where the spec and the code disagree, the code is right and the spec is stale. Do not
"fix" code to match it.

Start at [`spec/00-overview.md`](spec/00-overview.md) for the glossary and sizing
constants — the two things it still single-sources.

## Decisions ([`adr/`](adr/))

Post-spec architectural decisions, one file per decision. Anything decided after the
spec froze lands here, not in `spec/`.

## Runbooks ([`runbooks/`](runbooks/))

Operational procedures:
[`cloudflare-origin-cert.md`](runbooks/cloudflare-origin-cert.md) (dashboard + vault
steps for the origin certificate),
[`frontier-surgery.md`](runbooks/frontier-surgery.md) (bulk `next_check_at` /
lifecycle repair), [`timescale-jobs.md`](runbooks/timescale-jobs.md) (compression,
retention, continuous aggregates), [`unbound.md`](runbooks/unbound.md) (the two local
recursors).

## Active / forward-looking

- **[`feature-research.md`](feature-research.md)** — evidence-backed post-launch feature
  roadmap (State-of-IPv6 report, provider league tables, notification toolkit, …).

## Agent scaffolding ([`agents/`](agents/))

[`issue-tracker.md`](agents/issue-tracker.md) — the local `.scratch/<slug>/` ticket
convention and its triage vocabulary. See the repo-root `CLAUDE.md`.
