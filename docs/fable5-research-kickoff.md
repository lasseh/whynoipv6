# Fable 5 — Research Round 1 kickoff prompt

Paste the block below into a fresh Fable 5 session running in the `whynoipv6-new` repo
(with read access to the sibling repos under `~/code/go/src/github.com/lasseh/`).

---

You are the senior architect for a from-scratch rewrite of **WhyNoIPv6** — a public
advocacy site ("Shame as a Service" for IPv6, whynoipv6.com) that scans the world's
most-visited websites and community-submitted campaign lists, measures IPv6 support,
tracks changes over time, and publishes heroes vs. sinners by domain, country, and ASN.
This rewrite is **backend-first**; the crawler and its scanning logic are the heart of it.

**Your task this round is RESEARCH AND DESIGN — do NOT write implementation code.** Produce
a detailed design research report that recommends *how* to build the new backend, with
concrete tradeoffs, that we will iterate on.

**Step 1 — read the source of truth.** Read `docs/backend-research-brief.md` in this repo
**in full** — it contains all product decisions, hard constraints, the crawler/consensus
design, classification rules, data-model asks, features, repo layout, and (in §10) the
exact deliverable definition. Treat it as authoritative. Then read the existing code it
references (paths in §1): the production `whynoipv6` backend, `claude/whynoipv6-team/backend`
(the v2 TimescaleDB/hexagonal rebuild), `v6audit/internal/checker` (the rich checker engine
to lift, minus scoring), the `claude/whynoipv6/prompts` specs + `REVIEW-REPORT.md`, and the
`whynoipv6-campaign` YAML format. Study real code before proposing anything.

**Hard constraints (do not relitigate — see brief §2, §2.5):**
- Public, anonymous — NO accounts/auth/orgs/billing.
- 3-state status model (`supported`/`unsupported`/`no_record`/`not_applicable`) — **NO A–F grade, NO 0–100 score.** Classification is the deterministic hero/partial/sinner ladder in §5.5.
- Data source = **Tranco only**, top 1M, eTLD+1 (no third-party lists, no subdomains from Tranco).
- Newest stack: current Go, Postgres 18 + TimescaleDB, pgx/v5, slog, chi, cobra, sqlc.
- Two-repo layout: `whynoipv6` code monorepo (backend + frontend + openapi), `whynoipv6-campaign` stays separate.

**Method (required):**
- For **every** major recommendation, state the alternative you rejected and **why**.
- **Cite the existing-code paths** you'd lift verbatim vs. adapt.
- Do additional **web research** where the brief flags it (Tranco ingestion/permalink API, TimescaleDB compression/retention/continuous-aggregate patterns, DNS resolver-load strategy, Go DNS/HTTP-over-IPv6 at scale).
- Where you can't resolve something, **flag it as an open decision** with options + a recommendation — this is round 1, meant to be iterated.

**Deliverable:** a markdown design report saved under `docs/` (propose a filename), structured
exactly per **§10** of the brief (executive summary; crawler design incl. consensus,
check set, cadence, resolver-load split, throughput math; data-source plan; full proposed
DB schema incl. confirmed-status/changelog-trust, stats/continuous-aggregates, domain-entity
model, service-domain + disabled-domain lifecycle, #23 resources; API surface; package/binary
layout; campaign automation; phased implementation plan; open risks & decisions; explicit
tradeoffs). Be thorough, opinionated, and honest about cost and complexity.

Begin by reading `docs/backend-research-brief.md` and the referenced repos, then produce the report.
