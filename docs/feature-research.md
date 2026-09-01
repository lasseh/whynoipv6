# WhyNoIPv6 — Feature Research (Deep-Research Round)

**Status:** Research round 1 (2026-07-06), **audited against the shipped code
2026-09-01** — of the top ten, four shipped, two are partial and four were never
built. See "Shipped status" below before treating any item as a to-do.
**Method:** multi-agent deep research (5 search angles, 22 primary sources fetched,
106 claims extracted, 25 adversarially verified with 3-vote panels — 24 confirmed,
1 refuted). Every top-10 item traces to at least one unanimously verified claim.
**Scope guard:** everything below respects the hard constraints — public/anonymous
(no accounts), no scores/grades (3-state + ladder only), Tranco-only, non-commercial.

---

## Shipped status (audited 2026-09-01)

| # | Feature | Status | Evidence |
|---|---|---|---|
| 1 | Embeddable badge | **shipped** | `/badge/{host}.svg` + `/badge/{host}.json` |
| 2 | Hall of Fame + provider league tables | **shipped** | `/heroes`, `/saints`, `/almost-heroes`; `/providers`, `/providers/{id}/domains`, `/hosting`, `/asns`; `LeagueTable.vue` on `/metrics?filter=asn` |
| 3 | Fix guides + retest-now | **not built** | No fix-guide content keyed off `class_flags`. The `/check/:target` live-check page exists, but no red domain page offers a retest button or a per-failure guide |
| 4 | Timelines + compare-two-dates | **partial** | Timelines shipped (`/countries/{code}/stats`, `/campaigns/{uuid}/stats`, `/stats/changes`, `/domains/{host}/history`). The API takes `from`/`to` on `/changelog`, but no UI exposes a two-date compare |
| 5 | Citable datasets | **shipped** (no DOI) | `/datasets` manifest; `datapackage.json`, `DICTIONARY.md`, `SHA256SUMS`, CC-BY-NC-4.0, Tranco list-ID provenance in `internal/export`. Zenodo DOI stays deferred-on-demand per 07-api §5.3 |
| 6 | State of IPv6 report | **not built** | No generator in `v6ctl`. The blog (`/blog`) became the editorial channel instead |
| 7 | CSV export | **shipped** | `?format=csv` / `text/csv` on the country, ASN, provider and hosting list endpoints |
| 8 | Methodology + criteria changelog | **partial** | The ladder, Hero and Saint criteria are written up in the FAQ (`FaqRulesApi.vue`). The designed `GET /methodology` with a structured `criteria_changelog[]` (07-api §1016) was never built |
| 9 | Notification toolkit | **not built** | No RDAP, `security.txt` or RFC 2142 contact discovery; no letter templates |
| 10 | Mail/MX recognition track | **not built** | MX is a column in the domain table; there is no mail-hero list or mail-specific view |

Second tier: **Atom/JSON feeds shipped and went further than proposed** — Atom *and*
JSON Feed 1.1, global plus per-domain, per-country and per-campaign (10 feed
endpoints). `/mandates` **shipped in the API** but has no frontend route, so there is
no /mandates page. Social cards are **partial**: a static `og:image` share card plus
per-page `og:title` via `usePageMeta`, not per-domain rendered status cards.
`llms.txt` shipped (`frontend/public/llms.txt`) despite being filed as "defer".

Untouched: 3, 6, 9 and 10. Two of those (3 fix guides, 9 one-shot outreach) are what
the research rated the highest-conversion levers of the whole set, and 10 (the mail
track) is among the cheapest — the schema already carries the MX and SMTP dimensions.

---

## The headline insight: reparability beats shame

The strongest cross-cutting finding is behavioral, not a feature: **shaming converts
into fixes only when the shamed party sees a reparable path.** The Leach & Cidam
meta-analysis (90 samples, N=12,364) shows reparability flips the sign of shame's
effect (g=+.47 when reparable vs g=−.34 when not); Renaud et al. (NSPW 2021) argue
unreparable shame produces avoidance, not action. And "transparency alone moves
operators" is an unmeasured hope (PrivacyScore's own words), while notification RCTs
(USENIX Sec '16/'21) show one-shot, well-framed notifications produce real but modest
remediation — and **repeat notifications produce zero additional effect**.

Design translation for "Shame as a Service": every sinner/partial page must pair the
shame with (a) a per-failure fix guide, (b) a public retest-now button, and (c) a
visible route to Hero/Gold. The shame list is the amplifier; reparability is the
conversion mechanism.

Comparable-site evidence (internet.nl Hall of Fame + provider tracks, securityheaders'
deliberately attainable criteria, SSL Pulse's 13 years of citable aggregates, shields.io
serving 1.6B badges/month, EU Commission and Dutch CBS republishing internet.nl data)
all points the same way: **positive recognition, embeddable artifacts, and open
reusable data are what give measurement sites reach** — the shame angle rides on top.

---

## Top 10 (evidence-backed, prioritized)

| # | Feature | User value | Maintainer value | Cost | Backend fit |
|---|---|---|---|---|---|
| 1 | **Embeddable per-domain SVG status badge** (`/badge/{domain}.svg` — hero/gold/partial/sinner), domain-locked policy, auto-updating with confirmed status | Free green trophy for operators; a red badge is a self-inflicted nudge | Viral distribution — shields.io proves the pattern at 1.6B images/month; every badge is a backlink | **S** | Already sketched in design §5.2 — promote from "optional" to committed. Serve from confirmed status; cache aggressively |
| 2 | **Segmented Hall of Fame + provider league tables** — hero/gold lists plus per-DNS-provider and per-hosting-ASN tracks ("X of this provider's Y domains are v6-ready", binary inclusion, published criteria) | Operators pick providers by v6 support; provider table answers "who should I move to" | Highest-leverage pressure: one provider fixing defaults unlocks thousands of domains (internet.nl runs exactly this with 79 hosters) | **M** | ASN data exists; NS-provider mapping needs a `ns_host → provider` table derived from NS-host data already collected |
| 3 | **Per-failure-mode fix guides + retest-now on every red page** — one guide per `class_flag` (`broken_v6`, `www_missing`, `ns_missing`, `mail_missing`, `resources_v4only`) + sinner, with provider-specific how-tos; retest button = existing live-check queue | The evidence-backed conversion lever — turns shame into a to-do list | Fewer "why is my domain red" mails; credibility; the thing that makes the shame defensible | **S–M** | Guides are static content keyed off `class_flags`; retest reuses `check_job` for already-tracked domains (priority re-scan) |
| 4 | **Public campaign/country timelines + compare-two-dates view** — trend graph per campaign/country, and "what changed between date A and B" (who went green) | Campaigners track and screenshot progress; journalists get before/after | Auto-generates "who went green this week" press material from the confirmed changelog | **M** | `stats_*_daily` tables + changelog already designed; needs a diff endpoint + frontend view |
| 5 | **Stable documented API + versioned, citable datasets** — semantic-versioned dataset schema, citation format, optionally DOI-minted snapshots via Zenodo | Researchers/journalists get durable, citable data | The **biggest reach lever found**: EU Commission, Dutch CBS, ISOC Portugal republish internet.nl data — third parties become your amplifiers | **S** (on top of designed §5.4) | Datasets + OpenAPI already designed; add DICTIONARY versioning, citation blurb, Zenodo upload in the export job |
| 6 | **Annual/monthly "State of IPv6" aggregate report** — citable snapshot: adoption by country, ASN, TLD across the Tranco 1M | Reference statistics for talks, papers, articles | Recurring press cycle; SSL Pulse held this role for TLS for 13 years and is now stale — the seat is open | **M** | Generated from `stats_*` tables; mostly editorial template + a generator in `v6ctl` |
| 7 | **CSV export + stable shareable URL on every list view** (campaign, country, ASN, search results) | Sysadmins paste the spreadsheet into the internal case for enabling v6 | Near-free amplification; internet.nl dashboard ships exactly this | **S** | `?format=csv` content negotiation on existing list endpoints |
| 8 | **Published, changelogged classification criteria** — a /methodology page stating the exact ladder + a public log of every rule change | Clear, achievable target ("what do I need for Gold") | Trust. securityheaders publicly recalibrated its criteria toward attainability — tune Gold the same way, in the open | **S** | The ladder is already deterministic; render it + keep a criteria-changes page |
| 9 | **One-shot notification toolkit for campaigners** — pre-written, well-framed letter templates + contact discovery (RDAP, security.txt, RFC 2142 addresses) per domain; user sends it themselves | RCT-proven outreach (+11pp remediation from good WHOIS letters; framing matters more than channel) | Remediation wins to publicize — while the site itself never spams (one-shot, human-sent, never scheduled) | **S–M** | Static templates + a contact-discovery endpoint; no sending infrastructure, stays anonymous |
| 10 | **Separate MX/email recognition track** — "mail heroes" list + mail-specific shame view from the MX/SMTP data already collected | Mail admins get their own target and trophy | A second shaming/hero dimension for free; mirrors internet.nl's website/email split | **S** | `mx`/`smtp` dimensions already in the schema; two list endpoints + views |

---

## Second tier — promising, but no verified evidence found

The research explicitly flagged these directions as *unverified* (no surviving claims —
absence of evidence, not evidence against). My own read on each:

- **Per-domain/campaign/country RSS/Atom feeds of confirmed changes.** No measured
  adoption evidence anywhere — but it is the *only* account-free push channel, costs
  almost nothing (render the changelog as Atom), and fits the trustworthy-changelog
  design perfectly. I'd build it despite the missing evidence (cost S).
- **Government-mandate compliance tracking** (US OMB M-21-07, EU, Norwegian DSS —
  the campaign for which already exists). Open question whether regulators would
  consume third-party data; cheap first step: a `mandate` tag on campaigns + a
  /mandates page with citations (cost S). Revisit after launch.
- **OG/social-share cards per domain** (share a sinner page → rendered status card).
  Standard practice, unverified impact; pairs naturally with the badge renderer
  (cost M).
- **Funding: NLnet and the RIPE Community Projects Fund** both fund exactly this
  category of non-commercial internet-measurement work (both were fetched as primary
  sources). A grant application is maintainer-value with zero product conflict;
  donations link is compatible with anonymity. Worth pursuing independently of features.
- **Browser extension / CLI tool, llms.txt/AI-readable data.** Defer — the CLI is a
  thin API client anyone can write once the API is stable; llms.txt belongs to the
  separate SEO/AI-content brief.

---

## What the evidence says NOT to do

- **No automated/repeat notifications.** The RCT evidence is unambiguous: repeat
  notifications yield zero additional remediation, and nagging burns goodwill.
- **Don't expect listing alone to move operators.** Publication is an amplifier;
  the measured levers are reparability and one-shot framed outreach.
- **Don't make Gold aspirational-unreachable.** securityheaders had to walk back
  maximalist criteria; attainability is a design property, tune it publicly.
- **Don't rank providers with scores** — binary inclusion + counts keeps the
  no-scores constraint intact (internet.nl's hoster track works exactly this way).

## Caveats from verification

- The internet.nl dashboard patterns (batch scans, timelines, publishing) are proven
  in an **account-based** product; the anonymous transfer (public campaign lists +
  link-based sharing) is a design inference.
- The shame-backfires thesis carried a 2-1 vote and extrapolates individual
  psychology to organizations — treat as design caution, not proof.
- Notification effect sizes (56.6%/76.3%) came from a GDPR legal-liability context
  with a one-line fix; IPv6 remediation will run far lower.
- One claim was refuted and excluded: that internet.nl's batch API has "hundreds of
  active users" (traction unverified; the feature exists in production regardless).

## Suggested build order (historical — phases 4–6 have shipped)

_Kept as the record of the original sequencing. The phases below are done; what is
still open is the "not built" and "partial" set in the audit at the top._

Nearly everything rides on already-designed machinery (confirmed status, changelog,
stats tables, live-check queue, datasets):

1. **With phase 4 (API):** #7 CSV/stable URLs, #8 methodology page, #1 badges.
2. **With phase 5 (classification surfacing):** #3 fix guides + retest, #10 mail track.
3. **With phase 6 (public features):** #5 citable datasets/API polish, #4 timelines +
   compare, RSS feeds (second tier).
4. **Post-launch editorial cadence:** #6 State of IPv6 report, #2 provider league
   tables, #9 campaigner notification toolkit.
