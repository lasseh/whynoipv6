# Site copy

Every user-visible string in the frontend, extracted verbatim from source, keyed so
it can be edited here and imported back.

**Extraction is verbatim.** Nothing here has been normalized, re-cased, or brought
into line with the voice guide. Curly apostrophes, em dashes, ellipsis characters,
and HTML entities appear exactly as the source has them, because an importer diffs
against source and a "helpful" fix here becomes an unreviewable diff there.
Deviations from the voice guide are catalogued in [Appendix A](#appendix-a--voice-discrepancies),
not silently corrected.

---

## How to read this file

Each block of copy carries an HTML comment naming its key and its source:

```markdown
<!-- key: example.section.element | src: frontend/src/partials/Example.vue:18-22 -->
IPv6 has been a standard since 1998. ...
```

The comment is invisible when rendered and trivially parseable. **Do not edit the
comment lines** — the key is the import target and the `src` anchor is how a change
finds its way back. Edit only the text beneath.

Keys are unique across the file, so an importer can key on them alone. The fenced block
above is the only example and carries a deliberately fake key; every real entry sits
outside a code fence.

### Notation

| In this file | In source | Notes |
|---|---|---|
| `**bold**` | `<span class="font-semibold a-gradient">` or `<strong>` | Gradient-accent emphasis |
| `[text](url)` | `<a href="url">` | External links carry `target="_blank"` **by default**; exceptions are noted inline |
| `[text](/path)` | `<router-link to="/path">` | Internal SPA link, never `target="_blank"` |
| `{name}` | `{{ expression }}` | Interpolated value. **Preserve these exactly** — dropping one silently corrupts the string on import |
| `⏎` | `<br />` | Hard line break |

### Whitespace

Vue collapses template whitespace, so a paragraph broken across five source lines
renders as one. Copy is recorded **as rendered**: one logical block per entry, line
wrapping in this file is not meaningful. A genuine line break is written `⏎`.

### Placeholders in this document

`{count}`, `{domain}`, `{percent}` and friends are live values. Some appear inside
template literals in source (`` `${fmtCompact(x)} carry a Tranco rank.` ``); those are
recorded with the placeholder in position and the surrounding text editable.

### Scope

This file covers **frontend copy only**. Deliberately excluded, and not silently
dropped — see [Appendix B](#appendix-b--out-of-scope-surfaces) for where each lives:

- Backend-served strings (badge SVG text, RFC 9457 problem titles, feed titles, `llms.txt`)
- Blog post bodies (already authored markdown in `frontend/src/content/blog/` — a copy
  here would create a second master)
- Chart axis tick values, country names, and other data-derived text

---

# Part 1 — Site chrome

Shared by every page.

## 1.1 Header

<!-- key: chrome.header.wordmark | src: frontend/src/partials/Header.vue:59 -->
Why No IPv6

<!-- key: chrome.header.wordmark_aria | src: frontend/src/partials/Header.vue:55 | a11y -->
Why No IPv6 home

**Navigation labels.** Each appears **twice** in source — once in the desktop nav,
once in the mobile nav. Editing a label here must update both anchors, or the two
menus diverge.

<!-- key: chrome.nav.domains | src: frontend/src/partials/Header.vue:75,186 -->
Domains

<!-- key: chrome.nav.campaigns | src: frontend/src/partials/Header.vue:85,193 -->
Campaigns

<!-- key: chrome.nav.countries | src: frontend/src/partials/Header.vue:95,200 -->
Countries

<!-- key: chrome.nav.livecheck | src: frontend/src/partials/Header.vue:105,207 -->
Live Check

<!-- key: chrome.nav.metrics | src: frontend/src/partials/Header.vue:115,214 -->
Metrics

<!-- key: chrome.nav.changelog | src: frontend/src/partials/Header.vue:125,221 -->
Changelog

<!-- key: chrome.nav.faq | src: frontend/src/partials/Header.vue:135,228 -->
FAQ

<!-- key: chrome.header.menu_sr | src: frontend/src/partials/Header.vue:162 | a11y -->
Menu

<!-- key: chrome.header.search_placeholder_hidden | src: frontend/src/partials/Header.vue:144 | invisible spacer, never shown -->
Search Domain

## 1.2 Footer

<!-- key: chrome.footer.blog_line | src: frontend/src/partials/Footer.vue:75-78 -->
Numbers with sentences on [the blog](/blog)

<!-- key: chrome.footer.hosting_line | src: frontend/src/partials/Footer.vue:81-88 -->
Hosted on native IPv6 co-location at [Blix Solutions](https://blix.com/) since 2008

<!-- key: chrome.footer.ipdata_line | src: frontend/src/partials/Footer.vue:91-97 -->
IP data by [IPinfo](https://ipinfo.io)

<!-- key: chrome.footer.aria_github | src: frontend/src/partials/Footer.vue:17 | a11y -->
GitHub

<!-- key: chrome.footer.aria_twitter | src: frontend/src/partials/Footer.vue:35 | a11y -->
Twitter

<!-- key: chrome.footer.aria_rss | src: frontend/src/partials/Footer.vue:52 | a11y -->
Blog RSS feed

## 1.3 Document head and share cards

<!-- key: head.title_default | src: frontend/index.html:52 -->
Why No IPv6

<!-- key: head.meta_description | src: frontend/index.html:27-30 -->
Why No IPv6 scans the top million domains daily (www, nameservers, mail) and names the giants still IPv4-only. Sinners, Heroes, Saints. Shame on them.

<!-- key: head.meta_author | src: frontend/index.html:31 -->
Lasse Haugen

<!-- key: head.og_title | src: frontend/index.html:33 -->
Why No IPv6: the web's biggest sites, still IPv4-only

<!-- key: head.og_description | src: frontend/index.html:34-37 -->
IPv4 ran out. We crawl the web's biggest sites daily and name the ones still IPv4-only: by rank, by country, by excuse. Redemption starts with an AAAA record.

<!-- key: head.twitter_title | src: frontend/index.html:42 -->
IPv6 Shame as a Service

<!-- key: head.twitter_description | src: frontend/index.html:43-46 | identical to head.og_description -->
IPv4 ran out. We crawl the web's biggest sites daily and name the ones still IPv4-only: by rank, by country, by excuse. Redemption starts with an AAAA record.

<!-- key: head.rss_link_title | src: frontend/index.html:20 -->
Why No IPv6 Blog

### Structured data (JSON-LD)

<!-- key: head.schema.website_name | src: frontend/index.html:62 -->
Why No IPv6

<!-- key: head.schema.website_description | src: frontend/index.html:63 -->
A daily who's who of the world's biggest websites still not on IPv6, ranked globally and by country, sorted into Sinners, Heroes, and Saints.

<!-- key: head.schema.org_name | src: frontend/index.html:77 -->
Why No IPv6

<!-- key: head.schema.dataset_name | src: frontend/index.html:84 -->
whynoipv6.com IPv6 adoption dataset (top 1M domains)

<!-- key: head.schema.dataset_description | src: frontend/index.html:85 -->
Confirmed IPv6 adoption state (base domain, www, nameservers, mail) for the top 1M websites, with per-country and per-campaign rollups. Attribution: Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco.

---

# Part 2 — Route titles and descriptions

Browser tab titles and `<meta name="description">` per route, also used for `og:` and
`twitter:` tags via `installPageMeta`. All live in `frontend/src/router.ts` except the
two blog routes, which live in `frontend/scripts/blog-shared.ts` so the prerendered
head and the runtime head cannot drift.

Several pages **override the title at runtime** once data loads; those are recorded in
their page section as `*.title_dynamic`.

| Route | Key prefix |
|---|---|
| `/` | `meta.home` |
| `/domains` | `meta.domains` |
| `/domains/:domain` | `meta.domain_detail` |
| `/domains/:domain/not-found` | `meta.domain_notfound` |
| `/search` | `meta.search` |
| `/check/:target?` | `meta.livecheck` |
| `/metrics` | `meta.metrics` |
| `/countries` | `meta.countries` |
| `/countries/:id` | `meta.country_detail` |
| `/campaigns` | `meta.campaigns` |
| `/campaigns/:uuid` | `meta.campaign_detail` |
| `/campaigns/:uuid/:domain` | `meta.campaign_domain` |
| `/campaigns/:uuid/:domain/not-found` | `meta.campaign_domain_notfound` |
| `/changelog` | `meta.changelog` |
| `/blog` | `meta.blog_list` |
| `/blog/:slug` | `meta.blog_post` |
| `/faq` | `meta.faq` |
| `*` | `meta.notfound` |

<!-- key: meta.home.title | src: frontend/src/router.ts:21 -->
Why No IPv6 - IPv6 Adoption Tracker

<!-- key: meta.home.description | src: frontend/src/router.ts:22-23 -->
Why No IPv6 scans the top million domains daily (www, nameservers, mail) and names the giants still IPv4-only. Sinners, Heroes, Saints. Shame on them.

<!-- key: meta.domains.title | src: frontend/src/router.ts:31 -->
Domain Leaderboard - Why No IPv6

<!-- key: meta.domains.description | src: frontend/src/router.ts:32-33 -->
Every domain we crawl, ranked by Tranco and checked for IPv6 (domain, www, nameservers, mail). Filter by tier: Sinners, Heroes, Saints.

<!-- key: meta.domain_notfound.title | src: frontend/src/router.ts:41 -->
Domain Not Found - Why No IPv6

<!-- key: meta.domain_notfound.description | src: frontend/src/router.ts:42-43 -->
This domain isn't in our database: not yet crawled, outside the Tranco top million, or a typo.

<!-- key: meta.domain_detail.title | src: frontend/src/router.ts:51 -->
Domain Details - Why No IPv6

<!-- key: meta.domain_detail.description | src: frontend/src/router.ts:52-53 -->
The complete IPv6 report card for this domain: AAAA for domain and www, nameservers, MX, and whether it actually answers over IPv6.

<!-- key: meta.search.title | src: frontend/src/router.ts:61 -->
Search Results - Why No IPv6

<!-- key: meta.search.description | src: frontend/src/router.ts:62 -->
Search the domains we crawl: who has IPv6 and who doesn't.

<!-- key: meta.livecheck.title | src: frontend/src/router.ts:70 -->
Live IPv6 Check - Why No IPv6

<!-- key: meta.livecheck.description | src: frontend/src/router.ts:71-72 -->
Run a live IPv6 check on any domain: AAAA records, nameservers, MX, and a real connection attempt over IPv6. Answers come from DNS, not our cache.

<!-- key: meta.metrics.title | src: frontend/src/router.ts:80 -->
IPv6 Adoption Metrics - Why No IPv6

<!-- key: meta.metrics.description | src: frontend/src/router.ts:81-82 -->
IPv6 adoption metrics for the top million domains, charted over time: how many publish AAAA records, how many don't, and how fast that's changing (slowly).

<!-- key: meta.countries.title | src: frontend/src/router.ts:90 -->
IPv6 Adoption by Country - Why No IPv6

<!-- key: meta.countries.description | src: frontend/src/router.ts:91-92 -->
IPv6 adoption ranked by country: who leads, who trails, and where the Sinners cluster. National pride, now measurable in AAAA records.

<!-- key: meta.country_detail.title | src: frontend/src/router.ts:100 -->
Country Details - Why No IPv6

<!-- key: meta.country_detail.description | src: frontend/src/router.ts:101-102 -->
How this country's top domains score on IPv6: adoption rate, the local Heroes, and the Sinners dragging the national average down.

<!-- key: meta.campaigns.title | src: frontend/src/router.ts:110 -->
Shame Campaigns - Why No IPv6

<!-- key: meta.campaigns.description | src: frontend/src/router.ts:111-112 -->
Reader-submitted lists of big-name domains, tracked daily until the AAAA records show up. Shame as a Service.

<!-- key: meta.campaign_detail.title | src: frontend/src/router.ts:120 -->
Campaign Details - Why No IPv6

<!-- key: meta.campaign_detail.description | src: frontend/src/router.ts:121-122 -->
Every domain in this campaign and its IPv6 status: who fixed it, who hasn't, and how the percentage is coming along.

<!-- key: meta.campaign_domain_notfound.title | src: frontend/src/router.ts:130 -->
Campaign Domain Not Found - Why No IPv6

<!-- key: meta.campaign_domain_notfound.description | src: frontend/src/router.ts:131-132 -->
This domain isn't tracked in this campaign. Either it was never on the list, or that's a typo.

<!-- key: meta.campaign_domain.title | src: frontend/src/router.ts:140 -->
Campaign Domain - Why No IPv6

<!-- key: meta.campaign_domain.description | src: frontend/src/router.ts:141-142 -->
The full IPv6 checklist for this campaign domain: AAAA, nameservers, mail, and whether it's helping or hurting the campaign's numbers.

<!-- key: meta.changelog.title | src: frontend/src/router.ts:150 -->
Changelog - Why No IPv6

<!-- key: meta.changelog.description | src: frontend/src/router.ts:151-152 -->
Who fixed their IPv6 and who broke it, day by day. Every AAAA record that appeared or quietly disappeared, pulled from the crawler's daily runs.

<!-- key: meta.blog_list.title | src: frontend/scripts/blog-shared.ts:38 -->
Blog - Why No IPv6

<!-- key: meta.blog_list.description | src: frontend/scripts/blog-shared.ts:39-40 | also the RSS channel description -->
Write-ups from the crawl data: adoption numbers, notable fixes, and excuses wearing thin. Every claim links to a live page you can check.

<!-- key: meta.blog_post.title | src: frontend/scripts/blog-shared.ts:46 | pre-load fallback; real post title swaps in -->
Blog - Why No IPv6

<!-- key: meta.blog_post.description | src: frontend/scripts/blog-shared.ts:47-48 | pre-load fallback -->
A write-up from the Why No IPv6 crawl: what the top-million data says about IPv6 adoption.

<!-- key: meta.faq.title | src: frontend/src/router.ts:174 -->
FAQ - Why No IPv6

<!-- key: meta.faq.description | src: frontend/src/router.ts:175-176 -->
How the crawler works, what the checks mean, and how to get your domain removed from the list. Short answer to that last one: start using IPv6.

<!-- key: meta.notfound.title | src: frontend/src/router.ts:210 -->
Page Not Found - Why No IPv6

<!-- key: meta.notfound.description | src: frontend/src/router.ts:211-212 -->
No route to this page. Unlike a missing AAAA record, this one probably isn't deliberate.

---

# Part 3 — Shared vocabulary

Label dictionaries. These are copy, and because they live in `.ts` files rather than
templates they are the easiest strings on the site to miss. One edit here changes
wording on many pages at once.

## 3.1 Confirmed status labels

The `§7.2` status vocabulary. Used by domain tables, the detail accordion, the tracker
tooltip, and status-icon screen-reader text.

<!-- key: vocab.status.supported | src: frontend/src/utils/status.ts:122 -->
Supported

<!-- key: vocab.status.unsupported | src: frontend/src/utils/status.ts:124 -->
Missing

<!-- key: vocab.status.no_record | src: frontend/src/utils/status.ts:126 -->
No record

<!-- key: vocab.status.not_applicable | src: frontend/src/utils/status.ts:128 -->
Not applicable

<!-- key: vocab.status.unknown | src: frontend/src/utils/status.ts:130 -->
Not yet checked

**Note.** `statusTooltip()` (`utils/status.ts:169-182`) duplicates all five strings
verbatim for the hover tooltip. An import must write both tables or the icon tooltip
and the row label will disagree.

## 3.2 Live-check status labels

The raw-observation vocabulary — the five above plus three states that never appear in
confirmed status.

<!-- key: vocab.live.supported | src: frontend/src/utils/status.ts:144 -->
Supported

<!-- key: vocab.live.partial | src: frontend/src/utils/status.ts:145 -->
Partial

<!-- key: vocab.live.unsupported | src: frontend/src/utils/status.ts:146 -->
Missing

<!-- key: vocab.live.no_record | src: frontend/src/utils/status.ts:147 -->
No record

<!-- key: vocab.live.not_applicable | src: frontend/src/utils/status.ts:148 -->
Not applicable

<!-- key: vocab.live.error | src: frontend/src/utils/status.ts:149 -->
Check error

<!-- key: vocab.live.inconsistent | src: frontend/src/utils/status.ts:150 -->
Resolvers disagreed

<!-- key: vocab.live.unchecked | src: frontend/src/utils/status.ts:155 | fallback for an unknown value -->
Not checked

<!-- key: vocab.live.no_result | src: frontend/src/utils/status.ts:164 | tracked domain whose observation was withheld -->
No result

## 3.3 Rating labels

Percent thresholds: Good ≥60, Medium ≥40, Bad below, Unknown when there is nothing to rate.

<!-- key: vocab.rating.good | src: frontend/src/utils/rating.ts:23 -->
Good

<!-- key: vocab.rating.medium | src: frontend/src/utils/rating.ts:31 -->
Medium

<!-- key: vocab.rating.bad | src: frontend/src/utils/rating.ts:38 -->
Bad

<!-- key: vocab.rating.unknown | src: frontend/src/utils/rating.ts:10 -->
Unknown

<!-- key: vocab.rating.prefix | src: frontend/src/components/RatingBadge.vue:60 | renders as "Rating: Good" -->
Rating: 

## 3.4 Tier labels

One row per tier drives the `/domains` tabs and the `/sinners`, `/heroes`, `/saints`,
`/almost-heroes` redirect routes. The slug is the URL; the label is the tab.

<!-- key: vocab.tier.sinners | src: frontend/src/tiers.ts:23 -->
Sinners

<!-- key: vocab.tier.heroes | src: frontend/src/tiers.ts:28 -->
Heroes

<!-- key: vocab.tier.saints | src: frontend/src/tiers.ts:33 -->
Saints

<!-- key: vocab.tier.almost_heroes | src: frontend/src/tiers.ts:38 | URL-only, no tab rendered -->
Almost Heroes

## 3.5 Changelog dimension labels

Substituted into the phrases in 3.6 as `{label}`.

<!-- key: vocab.field.base | src: frontend/src/utils/changelog.ts:8 -->
the base domain

<!-- key: vocab.field.www | src: frontend/src/utils/changelog.ts:9 -->
www

<!-- key: vocab.field.ns | src: frontend/src/utils/changelog.ts:10 -->
nameservers

<!-- key: vocab.field.mx | src: frontend/src/utils/changelog.ts:11 -->
mail

<!-- key: vocab.field.conn | src: frontend/src/utils/changelog.ts:12 -->
connectivity

<!-- key: vocab.field.resources | src: frontend/src/utils/changelog.ts:13 -->
page resources

## 3.6 Changelog phrases

Rendered as `{host} {phrase}` — the host is a separate link, so every phrase must read
as a verb phrase continuing from a hostname.

> **Mirrored in the backend.** `backend/internal/api/feed.go` renders the identical
> table for the Atom and JSON feeds, and goldens on both sides pin the wordings
> together. **An edit here is only half the change** — the Go table must move with it
> or the feed and the web page will describe the same event differently.

### Connectivity (`conn`)

<!-- key: phrase.conn.supported | src: frontend/src/utils/changelog.ts:42 -->
is now reachable over IPv6

<!-- key: phrase.conn.unsupported_from_na | src: frontend/src/utils/changelog.ts:47 -->
published IPv6 addresses — but connections fail

<!-- key: phrase.conn.unsupported | src: frontend/src/utils/changelog.ts:48 -->
is no longer reachable over IPv6

<!-- key: phrase.conn.not_applicable | src: frontend/src/utils/changelog.ts:52 | defensive; suppressed at write -->
has no IPv6 addresses left to test

### Page resources (`resources`)

<!-- key: phrase.resources.supported | src: frontend/src/utils/changelog.ts:60 -->
now loads all page resources over IPv6

<!-- key: phrase.resources.unsupported | src: frontend/src/utils/changelog.ts:62 -->
loads some page resources without IPv6

<!-- key: phrase.resources.not_applicable | src: frontend/src/utils/changelog.ts:65 | defensive; suppressed at write -->
no longer has its page resources checked

### Generic dimensions (`base`, `www`, `ns`, `mx`)

`{label}` comes from 3.5.

<!-- key: phrase.generic.supported | src: frontend/src/utils/changelog.ts:73 -->
now supports IPv6 on {label}

<!-- key: phrase.generic.unsupported_from_na | src: frontend/src/utils/changelog.ts:76 -->
started using {label} — without IPv6

<!-- key: phrase.generic.unsupported | src: frontend/src/utils/changelog.ts:76 -->
lost IPv6 on {label}

<!-- key: phrase.generic.no_record_from_na | src: frontend/src/utils/changelog.ts:82 -->
started publishing {label} — without IPv6 records

<!-- key: phrase.generic.no_record | src: frontend/src/utils/changelog.ts:83 -->
no longer publishes records for {label}

<!-- key: phrase.generic.not_applicable | src: frontend/src/utils/changelog.ts:87 -->
no longer uses {label}

---

# Part 4 — Shared components

Strings that belong to a component rather than a page. Labels a component receives
**as props** are recorded with the calling page instead, because that is where they
are written.

## 4.1 Domain table

Column headers and their hover tooltips.

<!-- key: comp.domaintable.col_rank | src: frontend/src/components/DomainTable.vue:39 -->
Rank

<!-- key: comp.domaintable.col_domain | src: frontend/src/components/DomainTable.vue:42 -->
Domain

<!-- key: comp.domaintable.col_apex | src: frontend/src/components/DomainTable.vue:45 -->
Apex

<!-- key: comp.domaintable.tip_apex | src: frontend/src/components/DomainTable.vue:45 -->
AAAA lookup at the zone apex: dig AAAA domain.com

<!-- key: comp.domaintable.col_www | src: frontend/src/components/DomainTable.vue:48 -->
WWW

<!-- key: comp.domaintable.tip_www | src: frontend/src/components/DomainTable.vue:48 -->
AAAA lookup for the www host: dig AAAA www.domain.com

<!-- key: comp.domaintable.col_mx | src: frontend/src/components/DomainTable.vue:53 -->
Mail (MX)

<!-- key: comp.domaintable.col_mx_short | src: frontend/src/components/DomainTable.vue:56 | mobile -->
MX

<!-- key: comp.domaintable.tip_mx | src: frontend/src/components/DomainTable.vue:52 -->
MX hosts for domain.com, each checked for an AAAA record

<!-- key: comp.domaintable.col_ns | src: frontend/src/components/DomainTable.vue:62 -->
Nameservers

<!-- key: comp.domaintable.col_ns_short | src: frontend/src/components/DomainTable.vue:65 | mobile -->
NS

<!-- key: comp.domaintable.tip_ns | src: frontend/src/components/DomainTable.vue:60-61 -->
Authoritative nameservers for domain.com, each checked for an AAAA record

<!-- key: comp.domaintable.col_ipv6only | src: frontend/src/components/DomainTable.vue:70 -->
IPv6 Only

<!-- key: comp.domaintable.col_ipv6only_short | src: frontend/src/components/DomainTable.vue:73 | mobile -->
V6

<!-- key: comp.domaintable.tip_ipv6only | src: frontend/src/components/DomainTable.vue:69 -->
Loads fully over an IPv6-only connection (site + page resources)

<!-- key: comp.domaintable.empty | src: frontend/src/components/DomainTable.vue:149 -->
No domains found

> **Casing caveat.** The `<thead>` carries a Tailwind `uppercase` class, so "IPv6 Only"
> renders as "IPV6 ONLY" and "Mail (MX)" as "MAIL (MX)". Editing the casing here will
> not change what ships; that needs the CSS class dropped.

## 4.2 Subdomain table

<!-- key: comp.subdomaintable.heading | src: frontend/src/components/SubdomainTable.vue:172 -->
Subdomains

<!-- key: comp.subdomaintable.blurb | src: frontend/src/components/SubdomainTable.vue:173-175 -->
Other hosts tracked under this domain, checked the same way. They do not affect its rating.

<!-- key: comp.subdomaintable.col_host | src: frontend/src/components/SubdomainTable.vue:184 -->
Host

<!-- key: comp.subdomaintable.col_ipv6 | src: frontend/src/components/SubdomainTable.vue:187 -->
IPv6

<!-- key: comp.subdomaintable.tip_ipv6 | src: frontend/src/components/SubdomainTable.vue:187 -->
AAAA lookup for this host: dig AAAA host

<!-- key: comp.subdomaintable.tip_ns | src: frontend/src/components/SubdomainTable.vue:191-192 -->
Authoritative nameservers for this subdomain, each checked for an AAAA record

<!-- key: comp.subdomaintable.tip_mx | src: frontend/src/components/SubdomainTable.vue:200 -->
MX hosts for this subdomain, each checked for an AAAA record

<!-- key: comp.subdomaintable.truncated | src: frontend/src/components/SubdomainTable.vue:254 -->
Showing the first {shown} of {total}.

<!-- key: comp.subdomaintable.suggest_link | src: frontend/src/components/SubdomainTable.vue:263 -->
Suggest a subdomain →

## 4.3 Domain status card

<!-- key: comp.statuscard.heading | src: frontend/src/components/DomainStatusCard.vue:163 -->
IPv6 status

<!-- key: comp.statuscard.row_ns | src: frontend/src/components/DomainStatusCard.vue:114 -->
Nameservers

<!-- key: comp.statuscard.row_mx | src: frontend/src/components/DomainStatusCard.vue:123 -->
Mail (MX)

<!-- key: comp.statuscard.row_ipv6only | src: frontend/src/components/DomainStatusCard.vue:136 -->
IPv6-only

<!-- key: comp.statuscard.desc_base_apex | src: frontend/src/components/DomainStatusCard.vue:97 -->
The apex domain publishes an AAAA record, cross-checked against three independent resolvers.

<!-- key: comp.statuscard.desc_base_subdomain | src: frontend/src/components/DomainStatusCard.vue:96 -->
This hostname publishes an AAAA record, cross-checked against three independent resolvers.

<!-- key: comp.statuscard.desc_www | src: frontend/src/components/DomainStatusCard.vue:108 -->
The www hostname publishes an AAAA record.

<!-- key: comp.statuscard.desc_ns | src: frontend/src/components/DomainStatusCard.vue:116 -->
The domain’s DNS is served by at least one IPv6-capable nameserver.

<!-- key: comp.statuscard.desc_mx | src: frontend/src/components/DomainStatusCard.vue:128 -->
Mail servers (MX) are reachable over IPv6 — or no mail is configured.

<!-- key: comp.statuscard.label_conn | src: frontend/src/components/DomainStatusCard.vue:141 -->
Reachability

<!-- key: comp.statuscard.desc_conn | src: frontend/src/components/DomainStatusCard.vue:142 -->
The site answers a real HTTP request over an IPv6-only connection.

<!-- key: comp.statuscard.label_resources | src: frontend/src/components/DomainStatusCard.vue:146 -->
Page resources

<!-- key: comp.statuscard.desc_resources | src: frontend/src/components/DomainStatusCard.vue:52 -->
Scripts, fonts, and images load from IPv6-capable hosts.

<!-- key: comp.statuscard.desc_resources_na_vacuous | src: frontend/src/components/DomainStatusCard.vue:55 -->
Not applicable: the page pulls no resources from external hosts.

<!-- key: comp.statuscard.desc_resources_na_unreachable | src: frontend/src/components/DomainStatusCard.vue:56 -->
Not applicable: the site isn’t reachable over IPv6, so page resources can’t be evaluated.

<!-- key: comp.statuscard.last_checked | src: frontend/src/components/DomainStatusCard.vue:253 -->
Last checked: {timestamp}

<!-- key: comp.statuscard.last_checked_never | src: frontend/src/components/DomainStatusCard.vue:155 | substituted for {timestamp} -->
never

<!-- key: comp.statuscard.faq_link | src: frontend/src/components/DomainStatusCard.vue:258 -->
How these checks work →

## 4.4 Informational card

Advisory checks that never affect a rating.

<!-- key: comp.infocard.heading | src: frontend/src/components/InformationalCard.vue:106 -->
Informational

<!-- key: comp.infocard.caveat | src: frontend/src/components/InformationalCard.vue:107 -->
Advisory — never affects the rating

<!-- key: comp.infocard.label_dnssec | src: frontend/src/components/InformationalCard.vue:50 -->
DNSSEC

<!-- key: comp.infocard.desc_dnssec | src: frontend/src/components/InformationalCard.vue:51 -->
The zone is signed and its chain of trust validates from the root.

<!-- key: comp.infocard.label_ptr | src: frontend/src/components/InformationalCard.vue:55 -->
Reverse DNS

<!-- key: comp.infocard.desc_ptr | src: frontend/src/components/InformationalCard.vue:56 -->
The domain’s IPv6 addresses resolve back to a hostname (PTR).

<!-- key: comp.infocard.label_smtp | src: frontend/src/components/InformationalCard.vue:60 -->
SMTP over IPv6

<!-- key: comp.infocard.desc_smtp | src: frontend/src/components/InformationalCard.vue:61 -->
A mail server presents its SMTP banner over an IPv6 connection.

<!-- key: comp.infocard.label_parity | src: frontend/src/components/InformationalCard.vue:65 -->
Content parity

<!-- key: comp.infocard.desc_parity | src: frontend/src/components/InformationalCard.vue:66 -->
The page served over IPv6 matches the one served over IPv4.

<!-- key: comp.infocard.latency_label | src: frontend/src/components/InformationalCard.vue:128 -->
Response time (TTFB)

<!-- key: comp.infocard.latency_values | src: frontend/src/components/InformationalCard.vue:130 -->
IPv4 {v4} · IPv6 {v6}

<!-- key: comp.infocard.verdict_par | src: frontend/src/components/InformationalCard.vue:87 -->
IPv6 is on par with IPv4

<!-- key: comp.infocard.verdict_faster | src: frontend/src/components/InformationalCard.vue:90 -->
IPv6 is {delta} ms faster

<!-- key: comp.infocard.verdict_slower | src: frontend/src/components/InformationalCard.vue:91 -->
IPv6 is {delta} ms slower

## 4.5 Changelog feed

<!-- key: comp.changelog.default_header | src: frontend/src/components/ChangelogTable.vue:288 | prop default; /changelog passes "" to suppress it -->
Changelog

<!-- key: comp.changelog.today | src: frontend/src/components/ChangelogTable.vue:300 -->
Today

<!-- key: comp.changelog.yesterday | src: frontend/src/components/ChangelogTable.vue:300 -->
Yesterday

<!-- key: comp.changelog.empty | src: frontend/src/components/ChangelogTable.vue:355 -->
No changes yet. Nothing fixed. To be fair, nothing broken either.

## 4.6 League table

<!-- key: comp.league.row_footnote | src: frontend/src/components/LeagueTable.vue:694 -->
{v6} of {total} domains answer over IPv6

<!-- key: comp.league.empty | src: frontend/src/components/LeagueTable.vue:645 -->
No providers matched. Try a shorter name.

<!-- key: comp.league.bar_aria | src: frontend/src/components/LeagueTable.vue:690 | a11y -->
{name} IPv6 share

## 4.7 Forms and controls

<!-- key: comp.searchform.label_sr | src: frontend/src/components/DomainSearchForm.vue:527 | a11y -->
Search domains

<!-- key: comp.searchform.placeholder | src: frontend/src/components/DomainSearchForm.vue:552 -->
Search domains

<!-- key: comp.searchform.button | src: frontend/src/components/DomainSearchForm.vue:560 -->
Search

<!-- key: comp.filterinput.label_default | src: frontend/src/components/FilterInput.vue:580 | a11y, prop default -->
Filter

<!-- key: comp.filterinput.placeholder_default | src: frontend/src/components/FilterInput.vue:580 | prop default -->
Filter…

<!-- key: comp.filterinput.button_aria | src: frontend/src/components/FilterInput.vue:600 | a11y -->
Apply filter

<!-- key: comp.pagination.previous | src: frontend/src/components/Pagination.vue:498 -->
Previous

<!-- key: comp.pagination.next | src: frontend/src/components/Pagination.vue:507 -->
Next

<!-- key: comp.pagination.nav_aria | src: frontend/src/components/Pagination.vue:490 | a11y -->
Pagination

## 4.8 Status glyphs, tracker, badges

<!-- key: comp.ratingstars.muted_tip | src: frontend/src/components/RatingStars.vue:805 | only the muted star explains itself -->
Not applicable

<!-- key: comp.tracker.axis_90 | src: frontend/src/components/Tracker.vue:946 -->
90 days ago

<!-- key: comp.tracker.axis_60 | src: frontend/src/components/Tracker.vue:947 -->
60 days ago

<!-- key: comp.tracker.axis_30 | src: frontend/src/components/Tracker.vue:948 -->
30 days ago

<!-- key: comp.tracker.axis_today | src: frontend/src/components/Tracker.vue:949 -->
Today

<!-- key: comp.tracker.block_aria | src: frontend/src/components/Tracker.vue:903 | a11y -->
{date} — {status}

<!-- key: comp.mandatebadge.label | src: frontend/src/components/MandateBadge.vue:381 -->
Mandate

<!-- key: comp.mandatebadge.tip_named | src: frontend/src/components/MandateBadge.vue:371 -->
Covered by a government IPv6 mandate: {names}

<!-- key: comp.mandatebadge.tip_generic | src: frontend/src/components/MandateBadge.vue:372 -->
Covered by a government IPv6 mandate

## 4.9 Chrome, errors, feedback

<!-- key: comp.breadcrumb.home | src: frontend/src/components/Breadcrumb.vue:1012 -->
Home

<!-- key: comp.breadcrumb.nav_aria | src: frontend/src/components/Breadcrumb.vue:994 | a11y -->
Breadcrumb

<!-- key: comp.spinner.sr | src: frontend/src/components/LoadingSpinner.vue:975 | a11y -->
Loading...

<!-- key: comp.apierror.fallback_title | src: frontend/src/api/problem.ts:46 | network failure, no problem+json body -->
Request failed

<!-- key: comp.apierror.status_title | src: frontend/src/api/problem.ts:23 | body carried no title -->
HTTP {status}

> The error card itself renders `problem.title` and `problem.detail` straight from the
> API's RFC 9457 response. Those strings are **backend copy** — see Appendix B.

## 4.10 Visitor IPv4 notification

Shown only when the visitor reached the site over IPv4.

<!-- key: comp.notification.title | src: frontend/src/components/Notification.vue:440 -->
No IPv6?

<!-- key: comp.notification.body | src: frontend/src/components/Notification.vue:442-443 -->
You're reading an IPv6 shame site over IPv4. Ask your ISP when they plan to catch up with 1998.

<!-- key: comp.notification.close_sr | src: frontend/src/components/Notification.vue:448 | a11y -->
Close

---

# Part 5 — Pages

## 5.1 Home (`/`)

Composed of five partials in order: hero, search bar, blog strip, sinners, domains.

### Hero

<!-- key: home.hero.title | src: frontend/src/partials/HomeSaaS.vue:11 -->
IPv6 Shame as a Service!

<!-- key: home.hero.body | src: frontend/src/partials/HomeSaaS.vue:17-22 -->
IPv6 has been a standard since 1998. IPv4 ran out of addresses more than a decade ago. The most-visited websites on the internet still haven't connected those two facts, so we keep score. Publicly, **until an AAAA record says otherwise.**

<!-- key: home.hero.how_title | src: frontend/src/partials/HomeSaaS.vue:35 -->
How does it work?

<!-- key: home.hero.how_body | src: frontend/src/partials/HomeSaaS.vue:36-40 -->
A crawler checks every domain daily: AAAA records for the site and its www, IPv6 on the nameservers and mail. Do it all and you're a Hero. Skip it and you're listed, Tranco rank and all. Anyone can start a campaign; the data does the rest.

<!-- key: home.hero.why_title | src: frontend/src/partials/HomeSaaS.vue:41 -->
Why shame?

<!-- key: home.hero.why_body | src: frontend/src/partials/HomeSaaS.vue:42-46 -->
The RFC is old enough to vote. The conference talks happened. World IPv6 Launch was in 2012. At some point the only tool left is a public list with your domain on it.

### Hero feature list

<!-- key: home.features.receipts_title | src: frontend/src/partials/HomeSaaS.vue:70-72 -->
Public receipts

<!-- key: home.features.receipts_body | src: frontend/src/partials/HomeSaaS.vue:73-76 -->
Every check is public and dated. A domain can ignore us, but it can't say it wasn't warned.

<!-- key: home.features.numbers_title | src: frontend/src/partials/HomeSaaS.vue:97-99 -->
Strength in numbers

<!-- key: home.features.numbers_body | src: frontend/src/partials/HomeSaaS.vue:100-103 -->
Compare notes with people who care about address space. Share findings, argue methodology, watch the adoption graph inch upward.

<!-- key: home.features.sinner_title | src: frontend/src/partials/HomeSaaS.vue:125-127 -->
Bring your own Sinner

<!-- key: home.features.sinner_body | src: frontend/src/partials/HomeSaaS.vue:128-131 -->
Found a big name still IPv4-only? Submit it as a campaign and the crawler takes it from there.

### Blog strip

<!-- key: home.blog.eyebrow | src: frontend/src/partials/HomeBlog.vue:21 -->
From the blog

### Top sinners

<!-- key: home.sinners.title | src: frontend/src/partials/HomeSinners.vue:83 -->
Top IPv6 Sinners

<!-- key: home.sinners.body | src: frontend/src/partials/HomeSinners.vue:84-87 -->
The most visited websites in the world, without a single AAAA record among them. IPv6 shipped in 1998; these domains are still thinking it over.

<!-- key: home.sinners.kicker | src: frontend/src/partials/HomeSinners.vue:88 -->
Shame on them!

<!-- key: home.sinners.logo_alt | src: frontend/src/partials/HomeSinners.vue:76 | a11y -->
Why No IPv6 logo

### Testimonials

One is picked at random per page load. Adding an entry means adding a row to the
`testimonials` array, not just a string here.

<!-- key: home.testimonial.hogg.statement | src: frontend/src/partials/HomeSinners.vue:18 -->
IPv6 is no longer an option, it's mandatory

<!-- key: home.testimonial.hogg.name | src: frontend/src/partials/HomeSinners.vue:19 -->
Scott Hogg

<!-- key: home.testimonial.hogg.url_title | src: frontend/src/partials/HomeSinners.vue:21 | links to https://hoggnet.com/ -->
Hogg Networking

<!-- key: home.testimonial.pepelnjak.statement | src: frontend/src/partials/HomeSinners.vue:26 -->
It's a shame some people still can't deploy a protocol that could buy its own beer, even in the US.

<!-- key: home.testimonial.pepelnjak.name | src: frontend/src/partials/HomeSinners.vue:28 -->
Ivan Pepelnjak

<!-- key: home.testimonial.pepelnjak.url_title | src: frontend/src/partials/HomeSinners.vue:29 | links to https://www.ipspace.net/ -->
ipspace.net

### Wall of shame

<!-- key: home.domains.title | src: frontend/src/partials/HomeDomains.vue:36 -->
Wall of Shame

<!-- key: home.domains.body1 | src: frontend/src/partials/HomeDomains.vue:37-40 -->
The Tranco top million, crawled daily: every domain's IPv6 support, or lack of it, on public display.

<!-- key: home.domains.body2 | src: frontend/src/partials/HomeDomains.vue:41-44 -->
Every domain listed here is missing an AAAA record. Nameserver IPv6 support is shown alongside; some manage one without the other.

<!-- key: home.domains.cta | src: frontend/src/partials/HomeDomains.vue:61 -->
View all domains

<!-- key: home.domains.nav_aria | src: frontend/src/partials/HomeDomains.vue:56 | a11y -->
Domain list

## 5.2 Domain leaderboard (`/domains`)

<!-- key: domains.title | src: frontend/src/pages/DomainList.vue:34 -->
The top million websites, judged by their AAAA records

<!-- key: domains.body | src: frontend/src/pages/DomainList.vue:35-41 -->
We check every domain in the Tranco top million for IPv6: apex, www, mail, nameservers. Deploy it everywhere and you're a Hero; the Saints bar adds serving your page resources over IPv6 too. No IPv6 at all makes you a Sinner: some of the internet's biggest names, still unreachable over a protocol standardized in 1998. The crawler re-checks daily. Redemption starts with an AAAA record.

Tab labels come from the tier table — see [3.4](#34-tier-labels).

## 5.3 Domain detail (`/domains/:domain`)

<!-- key: domain_detail.title_dynamic | src: frontend/src/pages/DomainDetail.vue:42 | replaces the route title once loaded -->
Does {host} support IPv6?

<!-- key: domain_detail.crumb | src: frontend/src/pages/DomainDetail.vue:31 -->
Domains

<!-- key: domain_detail.provider | src: frontend/src/pages/DomainDetail.vue:67-68 -->
Provider: {asn_name} (AS{asn_number})

<!-- key: domain_detail.subdomain_of | src: frontend/src/pages/DomainDetail.vue:70-71 -->
Subdomain of {parent}

<!-- key: domain_detail.rank | src: frontend/src/pages/DomainDetail.vue:84 -->
Rank: {rank}

The status card, informational card, subdomain table and changelog below it are shared
components — see [Part 4](#part-4--shared-components).

## 5.4 Domain not found (`/domains/:domain/not-found`)

<!-- key: domain_notfound.title | src: frontend/src/pages/DomainNotFound.vue:21 -->
Domain not found

<!-- key: domain_notfound.lede | src: frontend/src/pages/DomainNotFound.vue:25-27 -->
{domain} isn't in our database.

<!-- key: domain_notfound.subhead | src: frontend/src/pages/DomainNotFound.vue:28-30 -->
Either our crawler hasn't met it yet, or that's a typo. NXDOMAIN, basically.

<!-- key: domain_notfound.card1_title | src: frontend/src/pages/DomainNotFound.vue:54 -->
Not in the crawl

<!-- key: domain_notfound.card1_body | src: frontend/src/pages/DomainNotFound.vue:55-57 -->
No record of it in our data. The likely reasons:

<!-- key: domain_notfound.card1_item1 | src: frontend/src/pages/DomainNotFound.vue:59 -->
• Not yet picked up by the crawler

<!-- key: domain_notfound.card1_item2 | src: frontend/src/pages/DomainNotFound.vue:60 -->
• A typo in the domain name

<!-- key: domain_notfound.card1_item3 | src: frontend/src/pages/DomainNotFound.vue:61 -->
• Outside the Tranco top million

<!-- key: domain_notfound.card2_title | src: frontend/src/pages/DomainNotFound.vue:83 -->
Put it on the list

<!-- key: domain_notfound.card2_body | src: frontend/src/pages/DomainNotFound.vue:84-93 -->
Run a [live check](/check/{domain}) on it right now, or submit it through the community campaign and we'll start keeping score. Once merged, the crawler picks it up on its next run.

<!-- key: domain_notfound.card2_link | src: frontend/src/pages/DomainNotFound.vue:112 | https://github.com/lasseh/whynoipv6-campaign -->
Submit to campaign →

<!-- key: domain_notfound.cta_browse | src: frontend/src/pages/DomainNotFound.vue:124 -->
Browse domains

<!-- key: domain_notfound.cta_search | src: frontend/src/pages/DomainNotFound.vue:130 -->
Search domains

<!-- key: domain_notfound.cta_home | src: frontend/src/pages/DomainNotFound.vue:136 -->
Go home

## 5.5 Search (`/search`)

<!-- key: search.prompt_title | src: frontend/src/pages/Search.vue:79 -->
Search the domain index

<!-- key: search.prompt_body | src: frontend/src/pages/Search.vue:80-85 -->
Look up any tracked domain: Saint, Sinner, or something in between. Try [google](/search?q=google).

<!-- key: search.results_heading | src: frontend/src/pages/Search.vue:91 | &ldquo; &rdquo; entities in source -->
Results for “{query}”

<!-- key: search.empty | src: frontend/src/pages/Search.vue:101-105 -->
Nothing in the index by that name. [Run a live check on {query}](/check/{query}).

## 5.6 Live check (`/check/:target?`)

<!-- key: livecheck.title | src: frontend/src/pages/LiveCheck.vue:121 -->
Live IPv6 Check

<!-- key: livecheck.body | src: frontend/src/pages/LiveCheck.vue:122-126 -->
Runs a real scan from our crawler right now: DNS, mail, and an actual connection over IPv6. Results are live observations; the tracked, confirmed status updates on its own schedule, not yours.

<!-- key: livecheck.title_dynamic | src: frontend/src/pages/LiveCheck.vue:111 | replaces the route title once a result lands -->
{host} Live IPv6 Check

### Form

<!-- key: livecheck.input_label_sr | src: frontend/src/pages/LiveCheck.vue:131 | a11y -->
Domain

<!-- key: livecheck.input_placeholder | src: frontend/src/pages/LiveCheck.vue:144 -->
example.com

<!-- key: livecheck.submit | src: frontend/src/pages/LiveCheck.vue:153 -->
Check

<!-- key: livecheck.submit_waiting | src: frontend/src/pages/LiveCheck.vue:153 -->
Wait {seconds}s

<!-- key: livecheck.rate_limited | src: frontend/src/pages/LiveCheck.vue:163 -->
Rate limit reached. Next check in {seconds}s.

### Progress narration

Shown in sequence as the scan runs; the number is the elapsed-seconds threshold at
which each line takes over.

<!-- key: livecheck.stage.queued | src: frontend/src/pages/LiveCheck.vue:88 -->
Waiting in queue…

<!-- key: livecheck.stage.0s | src: frontend/src/pages/LiveCheck.vue:66 -->
Resolving DNS records — AAAA, nameservers, mail…

<!-- key: livecheck.stage.4s | src: frontend/src/pages/LiveCheck.vue:67 -->
Cross-checking three public resolvers; two must agree…

<!-- key: livecheck.stage.9s | src: frontend/src/pages/LiveCheck.vue:68 -->
Connecting to the site over IPv6 only…

<!-- key: livecheck.stage.16s | src: frontend/src/pages/LiveCheck.vue:69 -->
Checking mail servers and TLS over IPv6…

<!-- key: livecheck.stage.24s | src: frontend/src/pages/LiveCheck.vue:70 -->
Fetching the page and discovering its resources…

<!-- key: livecheck.stage.45s | src: frontend/src/pages/LiveCheck.vue:71 -->
Still working — slow targets can take up to 90 seconds…

<!-- key: livecheck.cancel | src: frontend/src/pages/LiveCheck.vue:191 -->
Cancel

### Failure

<!-- key: livecheck.failed_label | src: frontend/src/pages/LiveCheck.vue:201 -->
Check failed.

<!-- key: livecheck.failed_fallback | src: frontend/src/pages/LiveCheck.vue:203 | used when the API returns no reason -->
The scan could not complete — try again later.

### Result

<!-- key: livecheck.copy_link | src: frontend/src/pages/LiveCheck.vue:217 -->
Copy link

<!-- key: livecheck.copy_link_done | src: frontend/src/pages/LiveCheck.vue:217 -->
Copied

<!-- key: livecheck.live_badge | src: frontend/src/pages/LiveCheck.vue:221 -->
Live observation

<!-- key: livecheck.cached_note | src: frontend/src/pages/LiveCheck.vue:226-228 -->
Showing a stored result. A fresh check runs automatically once it's older than 7 days.

**Core check labels.** Render order is fixed by the array.

<!-- key: livecheck.check.base | src: frontend/src/pages/LiveCheck.vue:26 -->
Domain (AAAA)

<!-- key: livecheck.check.www | src: frontend/src/pages/LiveCheck.vue:27 -->
WWW (AAAA)

<!-- key: livecheck.check.ns | src: frontend/src/pages/LiveCheck.vue:28 -->
Nameservers

<!-- key: livecheck.check.mx | src: frontend/src/pages/LiveCheck.vue:29 -->
Mail (MX)

<!-- key: livecheck.check.conn | src: frontend/src/pages/LiveCheck.vue:30 -->
IPv6-only reachability

<!-- key: livecheck.check.resources | src: frontend/src/pages/LiveCheck.vue:31 -->
Page resources

<!-- key: livecheck.resources_note_vacuous | src: frontend/src/pages/LiveCheck.vue:57 -->
The page pulls no resources from external hosts.

<!-- key: livecheck.resources_note_unreachable | src: frontend/src/pages/LiveCheck.vue:58 -->
The site isn’t reachable over IPv6, so page resources can’t be evaluated.

**Informational check labels.**

<!-- key: livecheck.info_heading | src: frontend/src/pages/LiveCheck.vue:258 -->
Informational

<!-- key: livecheck.info.tls | src: frontend/src/pages/LiveCheck.vue:34 -->
TLS

<!-- key: livecheck.info.smtp | src: frontend/src/pages/LiveCheck.vue:35 -->
SMTP over IPv6

<!-- key: livecheck.info.parity | src: frontend/src/pages/LiveCheck.vue:36 -->
Content parity

<!-- key: livecheck.info.dnssec | src: frontend/src/pages/LiveCheck.vue:37 -->
DNSSEC

<!-- key: livecheck.info.ptr | src: frontend/src/pages/LiveCheck.vue:38 -->
Reverse DNS

<!-- key: livecheck.info.spf | src: frontend/src/pages/LiveCheck.vue:39 -->
SPF

<!-- key: livecheck.ttfb | src: frontend/src/pages/LiveCheck.vue:282-283 -->
TTFB: IPv4 {v4} · IPv6 {v6}

<!-- key: livecheck.tracked_status | src: frontend/src/pages/LiveCheck.vue:293 -->
Tracked status: {classification}

<!-- key: livecheck.saint_suffix | src: frontend/src/pages/LiveCheck.vue:297 -->
 · Saint

<!-- key: livecheck.history_link | src: frontend/src/pages/LiveCheck.vue:303 -->
Full history →

<!-- key: livecheck.checked_at | src: frontend/src/pages/LiveCheck.vue:308-313 -->
Checked {timestamp}

<!-- key: livecheck.checked_at_now | src: frontend/src/pages/LiveCheck.vue:312 | substituted for {timestamp} -->
just now

<!-- key: livecheck.scan_duration | src: frontend/src/pages/LiveCheck.vue:315 -->
 · scan took {seconds}s

## 5.7 Metrics (`/metrics`)

### Page header

<!-- key: metrics.title | src: frontend/src/pages/Metrics.vue:35 -->
Metrics

<!-- key: metrics.body | src: frontend/src/pages/Metrics.vue:36-39 -->
IPv6 adoption across the Tranco list, straight from the crawler. The line goes up, eventually.

<!-- key: metrics.tab.overview | src: frontend/src/pages/Metrics.vue:48 -->
Overview

<!-- key: metrics.tab.asn | src: frontend/src/pages/Metrics.vue:49 -->
Network Providers

### Overview tab

<!-- key: metrics.overview.heading | src: frontend/src/partials/MetricCrawler.vue:178 -->
IPv6 adoption, live from Why No IPv6

<!-- key: metrics.overview.lede | src: frontend/src/partials/MetricCrawler.vue:179-190 -->
Of the {domains} most-visited websites on the internet, only {hero_share} are fully IPv6-ready. IPv6 has been a standard since 1998. In the Tranco top 1000 the picture is no prettier: {top_heroes} have IPv6 enabled, and {top_nameserver} sit behind nameservers reachable over IPv6.

<!-- key: metrics.overview.context | src: frontend/src/partials/MetricCrawler.vue:191-195 -->
For context: IPv6 became a standard in 1998, and again in 2017, in case anyone missed it the first time. The numbers below are what nearly three decades of "we'll get to it" looks like. Every one of them moves the day someone publishes an AAAA record.

**Stat tiles.** Label and hint are passed from this partial into `StatTile`.

<!-- key: metrics.tile.tracked.label | src: frontend/src/partials/MetricCrawler.vue:204 -->
Domains tracked

<!-- key: metrics.tile.tracked.hint | src: frontend/src/partials/MetricCrawler.vue:205 -->
{ranked} carry a Tranco rank. The rest are campaign entries, curated lists, and domains that fell off it.

<!-- key: metrics.tile.apex.label | src: frontend/src/partials/MetricCrawler.vue:210 -->
Apex IPv6 adoption

<!-- key: metrics.tile.apex.hint | src: frontend/src/partials/MetricCrawler.vue:211 -->
{count} domains publish an AAAA record.

<!-- key: metrics.tile.top1000.label | src: frontend/src/partials/MetricCrawler.vue:215 -->
Top 1000 with IPv6

<!-- key: metrics.tile.top1000.hint | src: frontend/src/partials/MetricCrawler.vue:216 -->
{count} of them have IPv6 nameservers.

<!-- key: metrics.tile.heroes.label | src: frontend/src/partials/MetricCrawler.vue:220 -->
Heroes

<!-- key: metrics.tile.heroes.hint | src: frontend/src/partials/MetricCrawler.vue:221 -->
{count} are Saints: page resources over IPv6 too.

<!-- key: metrics.tile.sinners.label | src: frontend/src/partials/MetricCrawler.vue:226 -->
Sinners

<!-- key: metrics.tile.sinners.hint | src: frontend/src/partials/MetricCrawler.vue:227 -->
No AAAA anywhere. One DNS change from the exit.

<!-- key: metrics.tile.throughput.label | src: frontend/src/partials/MetricCrawler.vue:232 -->
Hosts checked today

<!-- key: metrics.tile.throughput.hint | src: frontend/src/partials/MetricCrawler.vue:150 -->
The whole set sweeps every 24 hours, re-checks on top.

<!-- key: metrics.tile.throughput.hint_stale | src: frontend/src/partials/MetricCrawler.vue:154 | appended when the last checkpoint is 3h+ old -->
 Last checkpoint {hours}h ago.

**Charts.**

<!-- key: metrics.chart.tiers.title | src: frontend/src/partials/MetricCrawler.vue:240 -->
Where domains sit, day by day

<!-- key: metrics.chart.tiers.description | src: frontend/src/partials/MetricCrawler.vue:241 -->
Every tracked domain lands in exactly one class, so the bands add up to the list.

<!-- key: metrics.chart.tiers.aria | src: frontend/src/partials/MetricCrawler.vue:248 | a11y -->
Stacked daily count of domains per classification

<!-- key: metrics.chart.dimensions.title | src: frontend/src/partials/MetricCrawler.vue:253 -->
IPv6 support by dimension

<!-- key: metrics.chart.dimensions.description | src: frontend/src/partials/MetricCrawler.vue:254 -->
The six checks, counted separately. Nameservers lead; the pages they point at do not.

<!-- key: metrics.chart.dimensions.aria | src: frontend/src/partials/MetricCrawler.vue:261 | a11y -->
Daily count of domains passing each IPv6 check

<!-- key: metrics.chart.delta.title | src: frontend/src/partials/MetricCrawler.vue:266 -->
IPv6 gained and lost per day

<!-- key: metrics.chart.delta.description | src: frontend/src/partials/MetricCrawler.vue:267 -->
Apex records that flipped to supported, against the ones that flipped back. Churn, not net movement: a domain can appear in both bars on the same day.

<!-- key: metrics.chart.delta.aria | src: frontend/src/partials/MetricCrawler.vue:274 | a11y -->
Daily count of checks gaining and losing IPv6 support

**Series legend labels.**

<!-- key: metrics.series.tier.heroes | src: frontend/src/partials/MetricCrawler.vue:104 -->
Heroes

<!-- key: metrics.series.tier.partial | src: frontend/src/partials/MetricCrawler.vue:105 -->
Partial

<!-- key: metrics.series.tier.sinners | src: frontend/src/partials/MetricCrawler.vue:106 -->
Sinners

<!-- key: metrics.series.tier.inactive | src: frontend/src/partials/MetricCrawler.vue:107 -->
Inactive

<!-- key: metrics.series.tier.unknown | src: frontend/src/partials/MetricCrawler.vue:108 -->
Unknown

<!-- key: metrics.series.dim.base | src: frontend/src/partials/MetricCrawler.vue:112 -->
Apex (AAAA)

<!-- key: metrics.series.dim.www | src: frontend/src/partials/MetricCrawler.vue:113 -->
WWW (AAAA)

<!-- key: metrics.series.dim.ns | src: frontend/src/partials/MetricCrawler.vue:114 -->
Nameservers

<!-- key: metrics.series.dim.mx | src: frontend/src/partials/MetricCrawler.vue:115 -->
Mail (MX)

<!-- key: metrics.series.dim.conn | src: frontend/src/partials/MetricCrawler.vue:116 -->
IPv6-only reachability

<!-- key: metrics.series.dim.resources | src: frontend/src/partials/MetricCrawler.vue:117 -->
Page resources

<!-- key: metrics.series.delta.gained | src: frontend/src/partials/MetricCrawler.vue:134 -->
Gained IPv6

<!-- key: metrics.series.delta.lost | src: frontend/src/partials/MetricCrawler.vue:135 -->
Lost IPv6

**Advisory section.**

<!-- key: metrics.advisory.heading | src: frontend/src/partials/MetricCrawler.vue:280 -->
Beyond a AAAA record

<!-- key: metrics.advisory.body | src: frontend/src/partials/MetricCrawler.vue:281-284 -->
An AAAA record is the entry fee, not the finish line. These three checks are advisory: they never change a rating, they just show how much of the deployment was finished.

<!-- key: metrics.advisory.ptr.label | src: frontend/src/partials/MetricCrawler.vue:290 -->
Reverse DNS on IPv6 hosts

<!-- key: metrics.advisory.ptr.hint | src: frontend/src/partials/MetricCrawler.vue:291 -->
{supported} of {graded} graded hosts answer a PTR lookup.

<!-- key: metrics.advisory.smtp.label | src: frontend/src/partials/MetricCrawler.vue:296 -->
Mail that answers over IPv6

<!-- key: metrics.advisory.smtp.hint | src: frontend/src/partials/MetricCrawler.vue:297 -->
{supported} of {graded} graded mail servers presented a banner.

<!-- key: metrics.advisory.paper.label | src: frontend/src/partials/MetricCrawler.vue:302 -->
Mail that only looks ready

<!-- key: metrics.advisory.paper.hint | src: frontend/src/partials/MetricCrawler.vue:303 -->
The MX has an AAAA record. Nothing answers on it.

### Network providers tab

<!-- key: metrics.asn.heading | src: frontend/src/partials/MetricASN.vue:312 -->
IPv6 by provider

<!-- key: metrics.asn.body | src: frontend/src/partials/MetricASN.vue:313-318 -->
Every domain we crawl lives on someone's network, resolves through someone's DNS, and is served off someone's platform. One default-on change at a big provider moves thousands of domains to dual stack overnight. These are the three registries that decide, and who in each has flipped the switch.

<!-- key: metrics.asn.entity.networks | src: frontend/src/partials/MetricASN.vue:35 -->
Networks

<!-- key: metrics.asn.entity.dns | src: frontend/src/partials/MetricASN.vue:36 -->
DNS

<!-- key: metrics.asn.entity.hosting | src: frontend/src/partials/MetricASN.vue:37 -->
Hosting & CDN

**Entity nouns and blurbs.** The noun is singularized into the scatter description, so
it must stay a plural noun that de-pluralizes by dropping a trailing `s`.

<!-- key: metrics.asn.noun.networks | src: frontend/src/partials/MetricASN.vue:213 -->
networks

<!-- key: metrics.asn.blurb.networks | src: frontend/src/partials/MetricASN.vue:214 -->
The autonomous systems the crawled domains actually resolve to.

<!-- key: metrics.asn.noun.dns | src: frontend/src/partials/MetricASN.vue:218 -->
DNS providers

<!-- key: metrics.asn.blurb.dns | src: frontend/src/partials/MetricASN.vue:219 -->
Who runs the zone, and whether the domains in it resolve to an AAAA.

<!-- key: metrics.asn.noun.hosting | src: frontend/src/partials/MetricASN.vue:223 -->
platforms

<!-- key: metrics.asn.blurb.hosting | src: frontend/src/partials/MetricASN.vue:224-225 -->
The platform serving the site, which is usually the one that decides. A league among the platforms we can attribute, not a market survey.

**Scatter panel.**

<!-- key: metrics.asn.scatter.title | src: frontend/src/partials/MetricASN.vue:329 -->
Size against adoption

<!-- key: metrics.asn.scatter.description | src: frontend/src/partials/MetricASN.vue:330 -->
Every {noun_singular} placed by how many domains it carries and how many answer over IPv6. Bottom right is the interesting corner: plenty of domains, no AAAA.

<!-- key: metrics.asn.scatter.aria | src: frontend/src/partials/MetricASN.vue:334 | a11y -->
IPv6 adoption against domains hosted, per {noun_singular}

<!-- key: metrics.asn.scatter.laggards | src: frontend/src/partials/MetricASN.vue:338 -->
{under} of the {total} plotted are under 5%.

<!-- key: metrics.asn.scatter.floor | src: frontend/src/partials/MetricASN.vue:340-342 | {dropped_note} is a conditional block, shown only when something was dropped; its own copy is "({dropped} here)" including the parentheses -->
Anything under {floor} domains is left off {dropped_note} — at that size a single dual-stack customer swings the percentage.

<!-- key: metrics.asn.scatter.axis_x | src: frontend/src/components/charts/ScatterChart.vue:168 -->
domains hosted (log scale)

<!-- key: metrics.asn.scatter.median | src: frontend/src/components/charts/ScatterChart.vue:191 | annotation on the median line -->
median {percent}

<!-- key: metrics.asn.scatter.tooltip | src: frontend/src/components/charts/ScatterChart.vue:230-231 -->
{count} domains · {percent} IPv6

**League panel.**

<!-- key: metrics.asn.league.title | src: frontend/src/partials/MetricASN.vue:349 -->
The league

<!-- key: metrics.asn.league.search_label | src: frontend/src/partials/MetricASN.vue:357 | a11y -->
Search

<!-- key: metrics.asn.league.search_placeholder | src: frontend/src/partials/MetricASN.vue:358 -->
Provider name…

<!-- key: metrics.asn.sort.total | src: frontend/src/partials/MetricASN.vue:370 -->
Most domains

<!-- key: metrics.asn.sort.v6 | src: frontend/src/partials/MetricASN.vue:371 -->
Most IPv6

<!-- key: metrics.asn.league.loading | src: frontend/src/partials/MetricASN.vue:379 -->
Loading…

<!-- key: metrics.asn.league.truncated | src: frontend/src/partials/MetricASN.vue:381 -->
Showing the top {limit} of {total}.

<!-- key: metrics.asn.league.truncated_hint | src: frontend/src/partials/MetricASN.vue:382 | networks tab only -->
Search to find a specific network.

**Small-multiples panel.**

<!-- key: metrics.asn.trend.title | src: frontend/src/partials/MetricASN.vue:390 -->
Network adoption, day by day

<!-- key: metrics.asn.trend.description | src: frontend/src/partials/MetricASN.vue:391 -->
One box per network, each scaled to itself. Read the levels rather than the slopes: coverage is still growing, so a line can move because we reached more of a network's domains, not because it deployed anything.

<!-- key: metrics.asn.trend.range | src: frontend/src/partials/MetricASN.vue:410 -->
{low} to {high} over {days} days

**Reverse DNS panel.**

<!-- key: metrics.asn.ptr.title | src: frontend/src/partials/MetricASN.vue:419 -->
Reverse DNS

<!-- key: metrics.asn.ptr.description | src: frontend/src/partials/MetricASN.vue:420-423 -->
Of the hosts that answer over IPv6, how many resolve back to a name. Mail servers and logging tools care; almost nobody else has noticed.

<!-- key: metrics.asn.ptr.caption | src: frontend/src/partials/MetricASN.vue:434 -->
of {total} IPv6 hosts resolve back to a name

<!-- key: metrics.asn.ptr.with | src: frontend/src/partials/MetricASN.vue:450 -->
{count} have a PTR record

<!-- key: metrics.asn.ptr.without | src: frontend/src/partials/MetricASN.vue:451 -->
{count} have none

## 5.8 Countries (`/countries`)

<!-- key: countries.title | src: frontend/src/pages/CountryList.vue:36 -->
IPv6 by Country

<!-- key: countries.body | src: frontend/src/pages/CountryList.vue:48-53 -->
IPv6 adoption, country by country. Each domain in the Tranco list is mapped to a country by GeoIP, then scored on who publishes an AAAA record and who doesn't. So this measures a country's most-visited websites, not its networks. Some countries are nearly done. Some haven't started. Pick yours and meet the local Sinners.

<!-- key: countries.card_label | src: frontend/src/pages/CountryList.vue:87 -->
IPv6 ready

## 5.9 Country detail (`/countries/:id`)

<!-- key: country_detail.title_dynamic | src: frontend/src/pages/CountryDetail.vue:53 | replaces the route title once loaded -->
IPv6 Adoption in {country}

<!-- key: country_detail.crumb | src: frontend/src/pages/CountryDetail.vue:65 -->
Countries

<!-- key: country_detail.not_found | src: frontend/src/pages/CountryDetail.vue:70-72 -->
Country not found. We go by ISO 3166 codes; check yours.

<!-- key: country_detail.tracked | src: frontend/src/pages/CountryDetail.vue:93 -->
{count} domains tracked

<!-- key: country_detail.ready | src: frontend/src/pages/CountryDetail.vue:96 -->
{count} IPv6-ready domains

<!-- key: country_detail.bar_label | src: frontend/src/pages/CountryDetail.vue:102 -->
IPv6 ready

<!-- key: country_detail.tab.sinners | src: frontend/src/pages/CountryDetail.vue:115 -->
Sinners

<!-- key: country_detail.tab.heroes | src: frontend/src/pages/CountryDetail.vue:116 -->
Heroes

## 5.10 Campaigns (`/campaigns`)

<!-- key: campaigns.title | src: frontend/src/pages/CampaignList.vue:33 -->
Campaigns

<!-- key: campaigns.body1 | src: frontend/src/pages/CampaignList.vue:61-66 -->
Campaigns are reader-submitted lists of domains with something in common: a country's banks, its ISPs, its government. Each list is crawled and scored as a group, with the same checks as everywhere else (AAAA, nameservers, mail). The percentage on each card is how many have actually deployed it.

<!-- key: campaigns.body2 | src: frontend/src/pages/CampaignList.vue:67-76 -->
Have a list of domains that should know better? Open an issue in the [campaign repo](https://github.com/lasseh/whynoipv6-campaign) and we'll put them on the scoreboard. Shame scales.

<!-- key: campaigns.create_title | src: frontend/src/pages/CampaignList.vue:46 | button title attribute -->
Start a campaign on GitHub

<!-- key: campaigns.create_label | src: frontend/src/pages/CampaignList.vue:54 | class="hidden" — not currently rendered -->
Start a campaign

<!-- key: campaigns.card_label | src: frontend/src/pages/CampaignList.vue:112 -->
IPv6 ready

## 5.11 Campaign detail (`/campaigns/:uuid`)

<!-- key: campaign_detail.title_dynamic | src: frontend/src/pages/CampaignDetail.vue:71 | replaces the route title once loaded -->
{campaign} IPv6 Campaign

<!-- key: campaign_detail.crumb | src: frontend/src/pages/CampaignDetail.vue:83 -->
Campaigns

<!-- key: campaign_detail.not_found | src: frontend/src/pages/CampaignDetail.vue:88-90 -->
Campaign not found. Wrong UUID or a stale link; nothing to shame here.

<!-- key: campaign_detail.count | src: frontend/src/pages/CampaignDetail.vue:117 -->
{count} domains

<!-- key: campaign_detail.percent | src: frontend/src/pages/CampaignDetail.vue:120 -->
{percent}% IPv6 ready

Campaign names and descriptions are data, not copy — they come from the campaign repo.

## 5.12 Campaign domain (`/campaigns/:uuid/:domain`)

<!-- key: campaign_domain.title_dynamic | src: frontend/src/pages/CampaignDomain.vue:49 | replaces the route title once loaded -->
Does {host} support IPv6?

<!-- key: campaign_domain.crumb | src: frontend/src/pages/CampaignDomain.vue:61 -->
Campaigns

<!-- key: campaign_domain.provider | src: frontend/src/pages/CampaignDomain.vue:79-80 -->
Provider: {asn_name} (AS{asn_number})

<!-- key: campaign_domain.subdomain_of | src: frontend/src/pages/CampaignDomain.vue:82-83 -->
Subdomain of {parent}

## 5.13 Changelog (`/changelog`)

<!-- key: changelog.title | src: frontend/src/pages/Changelog.vue:60 -->
Changelog

<!-- key: changelog.body | src: frontend/src/pages/Changelog.vue:61-63 -->
Who fixed their IPv6 and who broke it, confirmed by the crawler

<!-- key: changelog.tab.tranco | src: frontend/src/pages/Changelog.vue:70 -->
Tranco Top 1M

<!-- key: changelog.tab.campaign | src: frontend/src/pages/Changelog.vue:71 -->
Campaigns

Row phrasing comes from [3.6](#36-changelog-phrases).

## 5.14 Blog list (`/blog`)

<!-- key: blog_list.title | src: frontend/src/pages/BlogList.vue:15 -->
Blog

<!-- key: blog_list.body | src: frontend/src/pages/BlogList.vue:16-18 -->
Write-ups from the crawl data. The numbers do the talking; we hold the flashlight.

<!-- key: blog_list.rss | src: frontend/src/pages/BlogList.vue:39 -->
RSS feed

<!-- key: blog_list.readtime | src: frontend/src/pages/BlogList.vue:53 -->
{minutes} min read

## 5.15 Blog post (`/blog/:slug`)

<!-- key: blog_post.missing_title | src: frontend/src/pages/BlogPost.vue:66 -->
No post here

<!-- key: blog_post.missing_body | src: frontend/src/pages/BlogPost.vue:67-69 -->
Nothing is published at this URL. Everything we have written lives on the blog index.

<!-- key: blog_post.missing_cta | src: frontend/src/pages/BlogPost.vue:73 -->
All posts

<!-- key: blog_post.crumb | src: frontend/src/pages/BlogPost.vue:62 -->
Blog

<!-- key: blog_post.readtime | src: frontend/src/pages/BlogPost.vue:81 -->
{minutes} min read

<!-- key: blog_post.back | src: frontend/src/pages/BlogPost.vue:98 | &larr; entity in source -->
← All posts

<!-- key: blog_post.rss | src: frontend/src/pages/BlogPost.vue:119 -->
RSS feed

Post titles, descriptions and bodies live in `frontend/src/content/blog/*.md` — see
Appendix B.

## 5.16 FAQ (`/faq`)

Four sub-pages behind `?page=1..4`, switched by the sidebar. Each is a section here.

### Sidebar

<!-- key: faq.nav.page1 | src: frontend/src/pages/FAQ.vue:519 -->
Frequently Asked Questions

<!-- key: faq.nav.page2 | src: frontend/src/pages/FAQ.vue:538 -->
Rules and API

<!-- key: faq.nav.page3 | src: frontend/src/pages/FAQ.vue:557 -->
Resources

<!-- key: faq.nav.page4 | src: frontend/src/pages/FAQ.vue:576 -->
About

### Page 1 — Frequently Asked Questions

<!-- key: faq.p1.heading | src: frontend/src/pages/FAQ.vue:41 -->
Frequently Asked Questions

<!-- key: faq.p1.what.q | src: frontend/src/pages/FAQ.vue:45 -->
What is Why No IPv6?

<!-- key: faq.p1.what.a | src: frontend/src/pages/FAQ.vue:46-51 -->
Why No IPv6 crawls the Tranco top million every day, plus user-submitted campaigns, and checks each one for IPv6: the domain, www, nameservers, and mail. Then we sort the results into Sinners, Heroes, and Saints and publish the receipts.

<!-- key: faq.p1.why.q | src: frontend/src/pages/FAQ.vue:54 -->
Why does IPv6 matter?

<!-- key: faq.p1.why.a1 | src: frontend/src/pages/FAQ.vue:55-59 -->
IPv4 ran out. Not 'is running out': ran out. The registries held the funeral years ago. IPv6 is the address space the internet actually grew into. For a top-ranked site today, skipping it isn't an oversight. It's a choice.

<!-- key: faq.p1.why.a2 | src: frontend/src/pages/FAQ.vue:60-63 -->
Our part is simple: we watch the top million and publish who has IPv6 and who doesn't. Being on the second list is meant to be uncomfortable.

<!-- key: faq.p1.how.q | src: frontend/src/pages/FAQ.vue:66 -->
How does the site work?

<!-- key: faq.p1.how.a | src: frontend/src/pages/FAQ.vue:67-73 -->
Once a day the crawler walks the entire Tranco list and runs the same checks on every domain: AAAA records on the domain and www, IPv6 on the nameservers and mail servers, and a real HTTP connection over IPv6 to confirm the records aren't decorative. The results feed everything here: the tiers, the country stats, and the changelog.

<!-- key: faq.p1.tranco.q | src: frontend/src/pages/FAQ.vue:76 -->
Tranco?

<!-- key: faq.p1.tranco.a1 | src: frontend/src/pages/FAQ.vue:77-85 -->
The [Tranco List](https://tranco-list.eu/) ranks the top million domains by aggregating several traffic lists, which smooths out the noise and manipulation that made single-source rankings like Alexa easy to game. It's the ranking researchers actually use.

<!-- key: faq.p1.tranco.a2 | src: frontend/src/pages/FAQ.vue:86-89 -->
We use it because the rank is half the shame: 'top 100 site, zero AAAA records' only lands if the ranking is credible.

<!-- key: faq.p1.accuracy.q | src: frontend/src/pages/FAQ.vue:92 -->
How accurate is the data?

<!-- key: faq.p1.accuracy.a | src: frontend/src/pages/FAQ.vue:93-97 -->
Treat the data as indicative, not absolute. DNS propagation and CDNs that answer differently per anycast location can shift a result from one scan to the next. That's why a status only changes after three consecutive scans agree.

<!-- key: faq.p1.falsenegative.q | src: frontend/src/pages/FAQ.vue:100-102 -->
Why does a domain show as not supporting IPv6 when it does?

<!-- key: faq.p1.falsenegative.a1 | src: frontend/src/pages/FAQ.vue:103-108 -->
Usually DNS propagation lag, a CDN answering differently from our vantage point, or a server that didn't respond during that scan. A real fix sticks after three consecutive daily scans, so give it a few days. If it still looks wrong, contact us.

<!-- key: faq.p1.falsenegative.a2 | src: frontend/src/pages/FAQ.vue:109-112 -->
Also note the crawler verifies reachability: an AAAA record that doesn't answer over IPv6 still counts as unsupported.

### Page 2 — Rules, Frequency, and API Access

<!-- key: faq.p2.heading | src: frontend/src/pages/FAQ.vue:120 -->
Rules, Frequency, and API Access

<!-- key: faq.p2.section_crawler | src: frontend/src/pages/FAQ.vue:124 -->
Crawler

<!-- key: faq.p2.rules.q | src: frontend/src/pages/FAQ.vue:127 -->
Crawler Rules

<!-- key: faq.p2.rules.a1 | src: frontend/src/pages/FAQ.vue:128-132 -->
The crawler checks AAAA records on domain.com, www.domain.com, and the domain's NS and MX records. It also opens a real HTTP connection over IPv6 — publishing an AAAA record that doesn't answer won't fool anyone.

<!-- key: faq.p2.rules.a2 | src: frontend/src/pages/FAQ.vue:133-136 -->
The domain and www lookups go through three independent public resolvers, and two out of three must agree.

<!-- key: faq.p2.frequency.q | src: frontend/src/pages/FAQ.vue:139 -->
Crawler Frequency

<!-- key: faq.p2.frequency.a | src: frontend/src/pages/FAQ.vue:140-143 -->
Every domain is scanned once per day. A status only changes after 3 consecutive scans agree, so one flaky DNS answer won't flip your verdict.

<!-- key: faq.p2.na.q | src: frontend/src/pages/FAQ.vue:146 | curly quotes in source -->
What does “Not applicable” mean?

<!-- key: faq.p2.na.a1 | src: frontend/src/pages/FAQ.vue:147-149 -->
There was nothing to grade — it never counts against a domain.

<!-- key: faq.p2.na.a2 | src: frontend/src/pages/FAQ.vue:150-153 -->
For Mail (MX) it means the domain publishes no MX records: no mail service, nothing to check. A domain without mail can still become a Hero.

<!-- key: faq.p2.na.a3 | src: frontend/src/pages/FAQ.vue:154-159 -->
For Page resources it means one of two things: the page loads over IPv6 and pulls no resources from external hosts, or the site isn't reachable over IPv6 at all — then its resources can't be evaluated. The domain status card and the live check both spell out which one applies.

<!-- key: faq.p2.subdomains.q | src: frontend/src/pages/FAQ.vue:162 -->
Why does a domain list subdomains?

<!-- key: faq.p2.subdomains.a1 | src: frontend/src/pages/FAQ.vue:163-167 -->
An apex can score green while the part people actually use, the login portal or the API, is still IPv4 only. Anyone can list those hosts for a domain, and the crawler then checks them exactly like any other domain.

<!-- key: faq.p2.subdomains.a2 | src: frontend/src/pages/FAQ.vue:168-178 -->
Subdomain results are informational: they never change the parent domain's rating or any of the country and campaign numbers. What gets listed depends on who took the time to list it, and a domain should not score worse for having attentive users. Add one by opening a PR on the [campaign repo](https://github.com/lasseh/whynoipv6-campaign).

<!-- key: faq.p2.errors.q | src: frontend/src/pages/FAQ.vue:182 -->
Crawler Errors

<!-- key: faq.p2.errors.a | src: frontend/src/pages/FAQ.vue:183-185 -->
Found a bug in the crawler? PRs are welcome.

<!-- key: faq.p2.heroes.q | src: frontend/src/pages/FAQ.vue:188 -->
Heroes

<!-- key: faq.p2.heroes.a | src: frontend/src/pages/FAQ.vue:189-193 -->
Hero status takes IPv6 on domain.com, www.domain.com, and the nameservers. MX hosts need IPv6 too (or no MX at all), and the site has to actually answer over IPv6.

<!-- key: faq.p2.saints.q | src: frontend/src/pages/FAQ.vue:196 -->
Saints

<!-- key: faq.p2.saints.a | src: frontend/src/pages/FAQ.vue:197-200 -->
Saints are Heroes that also load all their page resources (scripts, fonts, images) over IPv6. The full package: the site works on an IPv6-only connection.

<!-- key: faq.p2.section_campaign | src: frontend/src/pages/FAQ.vue:203 -->
Campaign Crawler

<!-- key: faq.p2.createcampaign.q | src: frontend/src/pages/FAQ.vue:206 -->
How do I create my own campaign?

<!-- key: faq.p2.createcampaign.a | src: frontend/src/pages/FAQ.vue:207-215 -->
Open an issue on the [GitHub repo](https://github.com/lasseh/whynoipv6-campaign).

<!-- key: faq.p2.removal.q | src: frontend/src/pages/FAQ.vue:218-220 -->
How can I get my domain removed from the list?

<!-- key: faq.p2.removal.a | src: frontend/src/pages/FAQ.vue:221 -->
Yes, you can start using IPv6!

<!-- key: faq.p2.section_api | src: frontend/src/pages/FAQ.vue:224 -->
API

<!-- key: faq.p2.api.q | src: frontend/src/pages/FAQ.vue:227 -->
Can I get access to the API?

<!-- key: faq.p2.api.a1 | src: frontend/src/pages/FAQ.vue:228-232 | the API origin renders as accent text, not a link -->
Yes, the API is open — no key, no signup. Everything on this site is served from it, at **https://api.whynoipv6.com** (no version prefix).

<!-- key: faq.p2.api.a2 | src: frontend/src/pages/FAQ.vue:233-247 -->
Start with the [interactive docs](https://api.whynoipv6.com/docs), the raw [OpenAPI spec](https://api.whynoipv6.com/openapi.json), or — if you're pointing an LLM agent at the data — [llms.txt](https://api.whynoipv6.com/llms.txt).

<!-- key: faq.p2.dataset.q | src: frontend/src/pages/FAQ.vue:250 -->
Can I download the whole dataset?

<!-- key: faq.p2.dataset.a1 | src: frontend/src/pages/FAQ.vue:251-256 -->
Yes — daily snapshots (CSV and Parquet) are published at [api.whynoipv6.com/datasets](https://api.whynoipv6.com/datasets). Please don't paginate the whole API when a bulk file exists.

<!-- key: faq.p2.dataset.a2 | src: frontend/src/pages/FAQ.vue:257-260 -->
The data is licensed CC-BY-NC-4.0. Attribution: Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco.

<!-- key: faq.p2.badge.q | src: frontend/src/pages/FAQ.vue:263 -->
Is there a badge for my README?

<!-- key: faq.p2.badge.a | src: frontend/src/pages/FAQ.vue:264-266 -->
Every domain has an SVG status badge. Embed it in markdown:

<!-- key: faq.p2.badge.snippet | src: frontend/src/pages/FAQ.vue:267-269 | rendered in mono, not a link -->
![IPv6](https://api.whynoipv6.com/badge/yourdomain.com.svg)

<!-- key: faq.p2.feed.q | src: frontend/src/pages/FAQ.vue:272 -->
Can I follow changes as a feed?

<!-- key: faq.p2.feed.a | src: frontend/src/pages/FAQ.vue:273-288 -->
The changelog is available as [Atom](https://api.whynoipv6.com/changelog.atom) and [JSON Feed](https://api.whynoipv6.com/changelog.feed.json), with per-domain, per-country, and per-campaign variants — see the docs.

### Page 3 — Resources

Link lists. The link text is copy; the URLs are recorded so an importer can rebuild the
anchor. Links here **do not all** carry `target="_blank"` — the ones that don't are noted.

<!-- key: faq.p3.heading | src: frontend/src/pages/FAQ.vue:296 -->
Resources

<!-- key: faq.p3.ipv6.title | src: frontend/src/pages/FAQ.vue:300 -->
IPv6

<!-- key: faq.p3.ipv6.link1 | src: frontend/src/pages/FAQ.vue:302-307 | https://www.internetsociety.org/deploy360/ipv6/ -->
Internet Society IPv6

<!-- key: faq.p3.ipv6.link2 | src: frontend/src/pages/FAQ.vue:310-312 | https://ready.chair6.net/ -->
IPv6 Ready test

<!-- key: faq.p3.bestpractice.title | src: frontend/src/pages/FAQ.vue:316 -->
IPv6 Networking Best Practices

<!-- key: faq.p3.bestpractice.link1 | src: frontend/src/pages/FAQ.vue:318-322 | apnic.net blog; no target="_blank" -->
IPv6 Subnetting - Best Practices

<!-- key: faq.p3.bestpractice.link2 | src: frontend/src/pages/FAQ.vue:325-329 | internetsociety.org; no target="_blank" -->
IPv6 Security Considerations

<!-- key: faq.p3.community.title | src: frontend/src/pages/FAQ.vue:333 -->
Community and Forums

<!-- key: faq.p3.community.link1 | src: frontend/src/pages/FAQ.vue:335 | https://www.reddit.com/r/ipv6/; no target="_blank" -->
r/ipv6

<!-- key: faq.p3.community.link2 | src: frontend/src/pages/FAQ.vue:338 | https://www.ipv6forum.com/; no target="_blank" -->
IPv6 Forum

<!-- key: faq.p3.community.link3 | src: frontend/src/pages/FAQ.vue:341-342 | packetpushers.net; no target="_blank" -->
IPv6 Buzz Podcast

<!-- key: faq.p3.courses.title | src: frontend/src/pages/FAQ.vue:347 -->
Courses and Certifications

<!-- key: faq.p3.courses.link1 | src: frontend/src/pages/FAQ.vue:349-351 | https://ipv6.he.net/certification/ -->
Hurricane Electric IPv6 Certification Project

<!-- key: faq.p3.courses.link2 | src: frontend/src/pages/FAQ.vue:354-359 | https://www.coursera.org/projects/ip-address-v6 -->
Getting Started with IPv6 (Coursera)

<!-- key: faq.p3.reports.title | src: frontend/src/pages/FAQ.vue:363 -->
Reports and IPv6 Status

<!-- key: faq.p3.reports.link1 | src: frontend/src/pages/FAQ.vue:365-370 | https://bgp.he.net/ipv6-progress-report.cgi -->
Global IPv6 Deployment Progress Report

<!-- key: faq.p3.reports.link2 | src: frontend/src/pages/FAQ.vue:373-375 | https://www.worldipv6launch.org/ -->
World IPv6 Launch

<!-- key: faq.p3.reports.link3 | src: frontend/src/pages/FAQ.vue:378-383 | https://www.google.com/intl/en/ipv6/statistics.html -->
Google IPv6 Statistics

<!-- key: faq.p3.reports.link4 | src: frontend/src/pages/FAQ.vue:386-388 | https://www.vyncke.org/ipv6status/ -->
IPv6 Deployment Aggregated Status

<!-- key: faq.p3.reports.link5 | src: frontend/src/pages/FAQ.vue:391-393 | https://awsipv6.neveragain.de/ -->
AWS service endpoints by region and IPv6 support

<!-- key: faq.p3.stickers.title | src: frontend/src/pages/FAQ.vue:397 -->
Stickers

<!-- key: faq.p3.stickers.body1 | src: frontend/src/pages/FAQ.vue:398-401 -->
Fly the colors. Nothing says 'ask me about AAAA records' like a protocol sticker.

<!-- key: faq.p3.stickers.body2 | src: frontend/src/pages/FAQ.vue:402-405 -->
Put one on your laptop, your rack, or a Sinner's front door. Get permission for that last one.

<!-- key: faq.p3.stickers.order | src: frontend/src/pages/FAQ.vue:407 -->
Order yours:

<!-- key: faq.p3.stickers.small | src: frontend/src/pages/FAQ.vue:409-413 | stickermule item 14732767; no target="_blank" -->
Small (2.4" x 3")

<!-- key: faq.p3.stickers.medium | src: frontend/src/pages/FAQ.vue:415-419 | stickermule item 14732768; no target="_blank" -->
Medium (3.2" x 4")

<!-- key: faq.p3.stickers.alt | src: frontend/src/pages/FAQ.vue:429 | a11y -->
Why No IPv6 sticker

### Page 4 — About

<!-- key: faq.p4.heading | src: frontend/src/pages/FAQ.vue:440 -->
About

<!-- key: faq.p4.whoami.q | src: frontend/src/pages/FAQ.vue:444 -->
# whoami

<!-- key: faq.p4.whoami.a1 | src: frontend/src/pages/FAQ.vue:445-449 -->
I'm Lasse, a network engineer from Norway. By day I build and run networks; by night I run a crawler that shames billion-dollar companies who still won't publish an AAAA record.

<!-- key: faq.p4.whoami.a2 | src: frontend/src/pages/FAQ.vue:450-454 -->
None of it is personal. Any domain can walk off the Sinners list with one DNS change and a server that answers over IPv6; the crawler forgives after three clean scans.

<!-- key: faq.p4.whoami.a3 | src: frontend/src/pages/FAQ.vue:455-458 -->
The endgame is an empty Sinners list. IPv6 turns 30 in 2028. I'd like to be done before it turns 40.

<!-- key: faq.p4.contact.q | src: frontend/src/pages/FAQ.vue:461 -->
Contact

<!-- key: faq.p4.contact.twitter | src: frontend/src/pages/FAQ.vue:462-467 -->
Twitter / X: [@whynoipv6](https://twitter.com/WhyNoIPv6)

<!-- key: faq.p4.contact.email | src: frontend/src/pages/FAQ.vue:468-471 | accent text, deliberately not a mailto link -->
Email: **whynoipv6@protonmail.com**

<!-- key: faq.p4.status.q | src: frontend/src/pages/FAQ.vue:474 -->
Status page

<!-- key: faq.p4.status.a | src: frontend/src/pages/FAQ.vue:475-479 -->
Uptime and incident history: [status.whynoipv6.com](https://status.whynoipv6.com/)

<!-- key: faq.p4.supporters.q | src: frontend/src/pages/FAQ.vue:483 -->
Our Supporters

<!-- key: faq.p4.supporters.a | src: frontend/src/pages/FAQ.vue:484-487 -->
These organizations supported the site early on, back when it was one crawler and a grudge.

<!-- key: faq.p4.supporters.alt | src: frontend/src/pages/FAQ.vue:499 | a11y; links to https://blix.com/ -->
Blix

## 5.17 Page not found (catch-all)

<!-- key: notfound.eyebrow_code | src: frontend/src/pages/PageNotFound.vue:29-31 -->
404

<!-- key: notfound.eyebrow_label | src: frontend/src/pages/PageNotFound.vue:33-35 -->
NXDOMAIN

<!-- key: notfound.title | src: frontend/src/pages/PageNotFound.vue:38 -->
Shame on us. This page doesn't resolve.

<!-- key: notfound.body | src: frontend/src/pages/PageNotFound.vue:40-43 -->
We checked every record: A, AAAA, even MX. Unlike our Sinners, this URL has a valid excuse for being unreachable: it doesn't exist.

<!-- key: notfound.cta_home | src: frontend/src/pages/PageNotFound.vue:47 -->
Back to homepage

<!-- key: notfound.cta_check | src: frontend/src/pages/PageNotFound.vue:66 -->
Check a domain

<!-- key: notfound.records_label | src: frontend/src/pages/PageNotFound.vue:72 | rendered uppercase by CSS -->
WE LOOKED EVERYWHERE

<!-- key: notfound.records_list | src: frontend/src/pages/PageNotFound.vue:6 | list, not a sentence: one chip per value, quoted as in source -->
'A', 'AAAA', 'MX', 'CNAME', 'TXT'

<!-- key: notfound.artwork_alt | src: frontend/src/pages/PageNotFound.vue:22 | a11y -->
Woodcut nun ringing a bell in an empty cathedral

---

# Appendix A — Voice discrepancies

Recorded, not corrected. The voice guide lives in
`.scratch/frontend-copy-review/copy-review.md`; these are places where shipped copy
diverges from it. Fix them by editing the entries above, or leave them — but decide
knowingly rather than discovering it at import time.

## A.1 Em dashes

The guide bans em dashes from shipped copy ("machine-copy tell; write around them with
periods, commas, colons, or parentheses"), with one documented exception: the changelog
row phrases, where the dash is house style. Everything below is outside that exception.

| Key | Text |
|---|---|
| `comp.statuscard.desc_mx` | "reachable over IPv6 — or no mail is configured" |
| `comp.infocard.caveat` | "Advisory — never affects the rating" |
| `livecheck.failed_fallback` | "could not complete — try again later" |
| `livecheck.stage.0s` | "Resolving DNS records — AAAA, nameservers, mail…" |
| `livecheck.stage.45s` | "Still working — slow targets…" |
| `metrics.asn.scatter.floor` | "({dropped} here) — at that size…" |
| `faq.p2.rules.a1` | "over IPv6 — publishing an AAAA record…" |
| `faq.p2.na.a1` | "nothing to grade — it never counts…" |
| `faq.p2.na.a3` | "over IPv6 at all — then its resources…" |
| `faq.p2.api.a1` | "the API is open — no key, no signup" |
| `faq.p2.api.a2` | "or — if you're pointing an LLM agent at the data —" |
| `faq.p2.dataset.a1` | "Yes — daily snapshots…" |
| `faq.p2.feed.a` | "per-campaign variants — see the docs" |

Within the sanctioned exception (leave as is): `phrase.conn.unsupported_from_na`,
`phrase.generic.unsupported_from_na`, `phrase.generic.no_record_from_na`.

Also a dash-as-separator, structural rather than prose: `comp.tracker.block_aria`.

## A.2 Year references

The guide bans **current** calendar years (they go stale every January) but explicitly
permits historical dates. All year references in the copy are historical or forward
-looking, so none are violations — listed here only so a future edit does not
reintroduce a "this year" phrasing:

1998 (`home.hero.body`, `home.sinners.body`, `domains.body`, `metrics.overview.lede`,
`metrics.overview.context`, `comp.notification.body`), 2008 (`chrome.footer.hosting_line`),
2012 (`home.hero.why_body`), 2017 (`metrics.overview.context`), 2028 (`faq.p4.whoami.a3`).

## A.3 Mixed quote characters

Straight and typographic characters both ship. Preserved verbatim above; normalize
deliberately if at all.

- **Curly double quotes:** `faq.p2.na.q` — “Not applicable”
- **HTML entities:** `search.results_heading` (`&ldquo;`/`&rdquo;`), `blog_post.back` (`&larr;`), `home.blog.eyebrow` strip (`&rarr;`)
- **Curly apostrophes:** `comp.statuscard.desc_ns`, `comp.statuscard.desc_resources_na_unreachable`, `comp.infocard.desc_ptr`, `livecheck.resources_note_unreachable`
- **Straight apostrophes:** everywhere else, including the neighbouring `faq.p2.na.a3` which says "isn't" and "can't" with straight quotes while `livecheck.resources_note_unreachable` says the same sentence with curly ones

That last pair is a genuine inconsistency: the same sentence, two spellings.

## A.4 Duplicated strings

One concept, two source locations. An importer must write both or they drift.

| Concept | Keys |
|---|---|
| Nav labels | Desktop and mobile nav in `Header.vue` (see 1.1) |
| Status labels | `vocab.status.*` and the `statusTooltip()` copy at `utils/status.ts:169-182` |
| Changelog phrases | `phrase.*` here and the Go table in `backend/internal/api/feed.go` |
| Share description | `head.og_description` and `head.twitter_description` |
| "IPv6 ready" bar label | `countries.card_label`, `country_detail.bar_label`, `campaigns.card_label` |
| "Nameservers" / "Mail (MX)" / "Page resources" | Appear as check labels in `livecheck.check.*`, `metrics.series.dim.*`, and `comp.statuscard.*` |

## A.5 Casing fought by CSS

`comp.domaintable.*` headers and `notfound.records_label` are uppercased by a Tailwind
class. Editing their casing here changes nothing on screen.

---

# Appendix B — Out-of-scope surfaces

Text that appears on or around the site but is not frontend copy. Named so the omission
is explicit rather than an oversight.

| Surface | Where it lives |
|---|---|
| RFC 9457 problem titles and details (rendered by `ApiError.vue`) | Backend, `backend/internal/api/` |
| Domain status badge SVG text | Backend, `backend/internal/api/badge.go` + goldens in `testdata/badge/` |
| Atom / JSON Feed channel titles and entry text | Backend, `backend/internal/api/feed.go` — note its changelog phrases mirror [3.6](#36-changelog-phrases) |
| `llms.txt`, `/docs`, `/openapi.json` | Backend, generated from `openapi/openapi.yaml` |
| Blog post titles, descriptions, bodies | `frontend/src/content/blog/*.md` (frontmatter + markdown) |
| Campaign names and descriptions | Data, from the `whynoipv6-campaign` repo |
| Country names | Data, from the API |
| Provider / ASN names | Data, from the API |
| Chart axis tick labels | Derived from data by `fmtAxisDate` / `fmtCompact` |
| `robots.txt`, `sitemap.xml` | `frontend/public/` and build scripts |
