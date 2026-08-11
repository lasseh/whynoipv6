# Site text, for proofreading

Everything a visitor reads, in the order they meet it. Plain text only — for the keyed
version with source line numbers, see `docs/copy.md`.

`{like_this}` is a live value the site fills in. **Bold** is accent-coloured text. Links
are shown as links.

---

# Site identity

**Browser tab (default):** Why No IPv6

**Search-result description:** Why No IPv6 scans the top million domains daily (www, nameservers, mail) and names the giants still IPv4-only. Sinners, Heroes, Saints. Shame on them.

**Author:** Lasse Haugen

### When a link is shared

**Facebook / LinkedIn title:** Why No IPv6: the web's biggest sites, still IPv4-only

**Facebook / LinkedIn description:** IPv4 ran out. We crawl the web's biggest sites daily and name the ones still IPv4-only: by rank, by country, by excuse. Redemption starts with an AAAA record.

**Twitter title:** IPv6 Shame as a Service

**Twitter description:** IPv4 ran out. We crawl the web's biggest sites daily and name the ones still IPv4-only: by rank, by country, by excuse. Redemption starts with an AAAA record.

### For search engines and AI (structured data, rarely seen by people)

**Site description:** A daily who's who of the world's biggest websites still not on IPv6, ranked globally and by country, sorted into Sinners, Heroes, and Saints.

**Dataset name:** whynoipv6.com IPv6 adoption dataset (top 1M domains)

**Dataset description:** Confirmed IPv6 adoption state (base domain, www, nameservers, mail) for the top 1M websites, with per-country and per-campaign rollups. Attribution: Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco.

**Blog feed title:** Why No IPv6 Blog

---

# Header and footer

**Wordmark:** Why No IPv6

**Menu:** Domains · Campaigns · Countries · Live Check · Metrics · Changelog · FAQ

### Footer

Numbers with sentences on [the blog](/blog)

Hosted on native IPv6 co-location at [Blix Solutions](https://blix.com/) since 2008

IP data by [IPinfo](https://ipinfo.io)

---

# Home

## Hero

# IPv6 Shame as a Service!

IPv6 has been a standard since 1998. IPv4 ran out of addresses more than a decade ago. The most-visited websites on the internet still haven't connected those two facts, so we keep score. Publicly, **until an AAAA record says otherwise.**

### How does it work?

A crawler checks every domain daily: AAAA records for the site and its www, IPv6 on the nameservers and mail. Do it all and you're a Hero. Skip it and you're listed, Tranco rank and all. Anyone can start a campaign; the data does the rest.

### Why shame?

The RFC is old enough to vote. The conference talks happened. World IPv6 Launch was in 2012. At some point the only tool left is a public list with your domain on it.

## Three selling points

**Public receipts** — Every check is public and dated. A domain can ignore us, but it can't say it wasn't warned.

**Strength in numbers** — Compare notes with people who care about address space. Share findings, argue methodology, watch the adoption graph inch upward.

**Bring your own Sinner** — Found a big name still IPv4-only? Submit it as a campaign and the crawler takes it from there.

## Search bar

Placeholder: Search domains · Button: Search

## Blog strip

Label: From the blog

## Top IPv6 Sinners

The most visited websites in the world, without a single AAAA record among them. IPv6 shipped in 1998; these domains are still thinking it over.

Shame on them!

### Testimonials (one shown at random)

> "IPv6 is no longer an option, it's mandatory"
> — Scott Hogg, [Hogg Networking](https://hoggnet.com/)

> "It's a shame some people still can't deploy a protocol that could buy its own beer, even in the US."
> — Ivan Pepelnjak, [ipspace.net](https://www.ipspace.net/)

## Wall of Shame

The Tranco top million, crawled daily: every domain's IPv6 support, or lack of it, on public display.

Every domain listed here is missing an AAAA record. Nameserver IPv6 support is shown alongside; some manage one without the other.

Button: View all domains

## Pop-up shown to visitors on IPv4

**No IPv6?**

You're reading an IPv6 shame site over IPv4. Ask your ISP when they plan to catch up with 1998.

---

# Domains

# The top million websites, judged by their AAAA records

We check every domain in the Tranco top million for IPv6: apex, www, mail, nameservers. Deploy it everywhere and you're a Hero; the Saints bar adds serving your page resources over IPv6 too. No IPv6 at all makes you a Sinner: some of the internet's biggest names, still unreachable over a protocol standardized in 1998. The crawler re-checks daily. Redemption starts with an AAAA record.

**Tabs:** Sinners · Heroes · Saints

(A fourth tier, Almost Heroes, exists at `/almost-heroes` but has no tab yet.)

## The domain table

**Columns:** Rank · Domain · Apex · WWW · Mail (MX) · Nameservers · IPv6 Only

On narrow screens the last four shorten to: MX · NS · V6

**Column tooltips**

- Apex — AAAA lookup at the zone apex: dig AAAA domain.com
- WWW — AAAA lookup for the www host: dig AAAA www.domain.com
- Mail (MX) — MX hosts for domain.com, each checked for an AAAA record
- Nameservers — Authoritative nameservers for domain.com, each checked for an AAAA record
- IPv6 Only — Loads fully over an IPv6-only connection (site + page resources)

**When nothing matches:** No domains found

**Buttons:** Previous · Next

---

# A single domain

Tab title becomes: Does {domain} support IPv6?

Provider: {provider} (AS{number})

Subdomain of {parent}

Rank: {rank}

## IPv6 status

Rows: {the domain} · www.{the domain} · Nameservers · Mail (MX) · IPv6-only

Each row expands to a 90-day timeline and an explanation:

- **The domain** — The apex domain publishes an AAAA record, cross-checked against three independent resolvers.
- **A subdomain** — This hostname publishes an AAAA record, cross-checked against three independent resolvers.
- **www** — The www hostname publishes an AAAA record.
- **Nameservers** — The domain’s DNS is served by at least one IPv6-capable nameserver.
- **Mail (MX)** — Mail servers (MX) are reachable over IPv6 — or no mail is configured.
- **Reachability** — The site answers a real HTTP request over an IPv6-only connection.
- **Page resources** — Scripts, fonts, and images load from IPv6-capable hosts.

When page resources can't be graded, that last line becomes one of:

- Not applicable: the page pulls no resources from external hosts.
- Not applicable: the site isn’t reachable over IPv6, so page resources can’t be evaluated.

Footer: Last checked: {time} · [How these checks work →](/faq?page=2)

When a domain has never been checked, "{time}" reads: never

Timeline labels: 90 days ago / 60 days ago / 30 days ago · Today

## Informational panel

**Informational** — Advisory — never affects the rating

- **DNSSEC** — The zone is signed and its chain of trust validates from the root.
- **Reverse DNS** — The domain’s IPv6 addresses resolve back to a hostname (PTR).
- **SMTP over IPv6** — A mail server presents its SMTP banner over an IPv6 connection.
- **Content parity** — The page served over IPv6 matches the one served over IPv4.

Response time (TTFB) — IPv4 {v4} · IPv6 {v6}

Verdict, one of: IPv6 is on par with IPv4 / IPv6 is {n} ms faster / IPv6 is {n} ms slower

## Subdomains

Other hosts tracked under this domain, checked the same way. They do not affect its rating.

**Columns:** Host · IPv6 · Nameservers · Mail (MX) · IPv6 Only

**Column tooltips**

- IPv6 — AAAA lookup for this host: dig AAAA host
- Nameservers — Authoritative nameservers for this subdomain, each checked for an AAAA record
- Mail (MX) — MX hosts for this subdomain, each checked for an AAAA record

Showing the first {n} of {total}.

[Suggest a subdomain →](https://github.com/lasseh/whynoipv6-campaign)

## Changelog under the domain

Heading: Changelog · Day headings: Today / Yesterday / the date

**When empty:** No changes yet. Nothing fixed. To be fair, nothing broken either.

## Government mandate badge

Badge: Mandate

Tooltip: Covered by a government IPv6 mandate: {names}

Without names: Covered by a government IPv6 mandate

---

# Search

## Before you search

**Search the domain index**

Look up any tracked domain: Saint, Sinner, or something in between. Try [google](/search?q=google).

## After

Results for "{query}"

**When nothing matches:** Nothing in the index by that name. [Run a live check on {query}](/check/{query}).

---

# Live Check

# Live IPv6 Check

Runs a real scan from our crawler right now: DNS, mail, and an actual connection over IPv6. Results are live observations; the tracked, confirmed status updates on its own schedule, not yours.

Placeholder: example.com · Button: Check

Once a result loads, the tab title becomes: {domain} Live IPv6 Check

## While it runs

Shown in order as the scan progresses:

1. Waiting in queue…
2. Resolving DNS records — AAAA, nameservers, mail…
3. Cross-checking three public resolvers; two must agree…
4. Connecting to the site over IPv6 only…
5. Checking mail servers and TLS over IPv6…
6. Fetching the page and discovering its resources…
7. Still working — slow targets can take up to 90 seconds…

Button: Cancel

## When it goes wrong

**Rate limited:** Rate limit reached. Next check in {n}s. (button reads: Wait {n}s)

**Failed:** Check failed. {reason}

When there is no reason: The scan could not complete — try again later.

**Timed out:** Check timed out — The scan is taking too long — try again later.

**Expired link:** Check not found — This check link has expired or never existed — run a fresh check above.

## The result

Buttons: Copy link (becomes Copied) · Badge: Live observation

Showing a stored result. A fresh check runs automatically once it's older than 7 days.

**The six main checks:** Domain (AAAA) · WWW (AAAA) · Nameservers · Mail (MX) · IPv6-only reachability · Page resources

Notes under Page resources, when it can't be graded:

- The page pulls no resources from external hosts.
- The site isn’t reachable over IPv6, so page resources can’t be evaluated.

**Informational:** TLS · SMTP over IPv6 · Content parity · DNSSEC · Reverse DNS · SPF

TTFB: IPv4 {v4} · IPv6 {v6}

Tracked status: {status} · Saint — [Full history →](/domains/{domain})

Checked {time} · scan took {n}s

When just run, "{time}" reads: just now

---

# Metrics

# Metrics

IPv6 adoption across the Tranco list, straight from the crawler. The line goes up, eventually.

**Tabs:** Overview · Network Providers

## Overview

### IPv6 adoption, live from Why No IPv6

Of the {total} most-visited websites on the internet, only {percent} are fully IPv6-ready. IPv6 has been a standard since 1998. In the Tranco top 1000 the picture is no prettier: {n} have IPv6 enabled, and {n} sit behind nameservers reachable over IPv6.

For context: IPv6 became a standard in 1998, and again in 2017, in case anyone missed it the first time. The numbers below are what nearly three decades of "we'll get to it" looks like. Every one of them moves the day someone publishes an AAAA record.

### The six headline numbers

**Domains tracked** — {n} carry a Tranco rank. The rest are campaign entries, curated lists, and domains that fell off it.

**Apex IPv6 adoption** — {n} domains publish an AAAA record.

**Top 1000 with IPv6** — {n} of them have IPv6 nameservers.

**Heroes** — {n} are Saints: page resources over IPv6 too.

**Sinners** — No AAAA anywhere. One DNS change from the exit.

**Hosts checked today** — The whole set sweeps every 24 hours, re-checks on top.

When the crawler has been quiet, that last one gains: Last checkpoint {n}h ago.

### The three charts

**Where domains sit, day by day** — Every tracked domain lands in exactly one class, so the bands add up to the list.
*Bands:* Heroes · Partial · Sinners · Inactive · Unknown

**IPv6 support by dimension** — The six checks, counted separately. Nameservers lead; the pages they point at do not.
*Lines:* Apex (AAAA) · WWW (AAAA) · Nameservers · Mail (MX) · IPv6-only reachability · Page resources

**IPv6 gained and lost per day** — Apex records that flipped to supported, against the ones that flipped back. Churn, not net movement: a domain can appear in both bars on the same day.
*Bars:* Gained IPv6 · Lost IPv6

### Beyond a AAAA record

An AAAA record is the entry fee, not the finish line. These three checks are advisory: they never change a rating, they just show how much of the deployment was finished.

**Reverse DNS on IPv6 hosts** — {n} of {total} graded hosts answer a PTR lookup.

**Mail that answers over IPv6** — {n} of {total} graded mail servers presented a banner.

**Mail that only looks ready** — The MX has an AAAA record. Nothing answers on it.

## Network Providers

## IPv6 by provider

Every domain we crawl lives on someone's network, resolves through someone's DNS, and is served off someone's platform. One default-on change at a big provider moves thousands of domains to dual stack overnight. These are the three registries that decide, and who in each has flipped the switch.

**Tabs:** Networks · DNS · Hosting & CDN

Each tab describes itself:

- **Networks** — The autonomous systems the crawled domains actually resolve to.
- **DNS** — Who runs the zone, and whether the domains in it resolve to an AAAA.
- **Hosting & CDN** — The platform serving the site, which is usually the one that decides. A league among the platforms we can attribute, not a market survey.

### Size against adoption

Every {network} placed by how many domains it carries and how many answer over IPv6. Bottom right is the interesting corner: plenty of domains, no AAAA.

{n} of the {total} plotted are under 5%. Anything under {n} domains is left off ({n} here) — at that size a single dual-stack customer swings the percentage.

Axis: domains hosted (log scale) · Median line: median {percent}

Hover: {n} domains · {percent} IPv6

### The league

Sort buttons: Most domains · Most IPv6 · Search box: Provider name…

Each row reads: {n} of {total} domains answer over IPv6

Showing the top {n} of {total}. Search to find a specific network.

While loading: Loading…

**When nothing matches:** No providers matched. Try a shorter name.

### Network adoption, day by day

One box per network, each scaled to itself. Read the levels rather than the slopes: coverage is still growing, so a line can move because we reached more of a network's domains, not because it deployed anything.

Under each box: {low} to {high} over {n} days

### Reverse DNS

Of the hosts that answer over IPv6, how many resolve back to a name. Mail servers and logging tools care; almost nobody else has noticed.

{percent} of {total} IPv6 hosts resolve back to a name

{n} have a PTR record · {n} have none

---

# Countries

# IPv6 by Country

IPv6 adoption, country by country. Each domain in the Tranco list is mapped to a country by GeoIP, then scored on who publishes an AAAA record and who doesn't. So this measures a country's most-visited websites, not its networks. Some countries are nearly done. Some haven't started. Pick yours and meet the local Sinners.

Each card shows: IPv6 ready — {percent}%

Filter box placeholder: Filter…

## A single country

Tab title becomes: IPv6 Adoption in {country}

{n} domains tracked · {n} IPv6-ready domains · IPv6 ready {percent}%

**Tabs:** Sinners · Heroes

**When the code is wrong:** Country not found. We go by ISO 3166 codes; check yours.

---

# Campaigns

# Campaigns

Campaigns are reader-submitted lists of domains with something in common: a country's banks, its ISPs, its government. Each list is crawled and scored as a group, with the same checks as everywhere else (AAAA, nameservers, mail). The percentage on each card is how many have actually deployed it.

Have a list of domains that should know better? Open an issue in the [campaign repo](https://github.com/lasseh/whynoipv6-campaign) and we'll put them on the scoreboard. Shame scales.

Button tooltip: Start a campaign on GitHub

Each card shows: IPv6 ready — {percent}%

## A single campaign

Tab title becomes: {campaign} IPv6 Campaign

{n} domains · {percent}% IPv6 ready

**When the link is bad:** Campaign not found. Wrong UUID or a stale link; nothing to shame here.

---

# Changelog

# Changelog

Who fixed their IPv6 and who broke it, confirmed by the crawler

**Tabs:** Tranco Top 1M · Campaigns

## How each line reads

Every line is the domain name followed by what happened to it. The same wording is used
in the Atom and JSON feeds.

**The domain, www, nameservers, or mail** — where "{it}" is one of: the base domain / www / nameservers / mail

- {domain} now supports IPv6 on {it}
- {domain} lost IPv6 on {it}
- {domain} started using {it} — without IPv6
- {domain} no longer publishes records for {it}
- {domain} started publishing {it} — without IPv6 records
- {domain} no longer uses {it}

**Reachability**

- {domain} is now reachable over IPv6
- {domain} is no longer reachable over IPv6
- {domain} published IPv6 addresses — but connections fail
- {domain} has no IPv6 addresses left to test

**Page resources**

- {domain} now loads all page resources over IPv6
- {domain} loads some page resources without IPv6
- {domain} no longer has its page resources checked

---

# Blog

# Blog

Write-ups from the crawl data. The numbers do the talking; we hold the flashlight.

Each entry shows: {date} · {n} min read

Link: RSS feed

## A missing post

**No post here**

Nothing is published at this URL. Everything we have written lives on the blog index.

Button: All posts · Back link: ← All posts

*Post titles, summaries and bodies are written in the blog files themselves, not here.*

---

# FAQ

Four tabs: Frequently Asked Questions · Rules and API · Resources · About

## Tab 1 — Frequently Asked Questions

**What is Why No IPv6?**

Why No IPv6 crawls the Tranco top million every day, plus user-submitted campaigns, and checks each one for IPv6: the domain, www, nameservers, and mail. Then we sort the results into Sinners, Heroes, and Saints and publish the receipts.

**Why does IPv6 matter?**

IPv4 ran out. Not 'is running out': ran out. The registries held the funeral years ago. IPv6 is the address space the internet actually grew into. For a top-ranked site today, skipping it isn't an oversight. It's a choice.

Our part is simple: we watch the top million and publish who has IPv6 and who doesn't. Being on the second list is meant to be uncomfortable.

**How does the site work?**

Once a day the crawler walks the entire Tranco list and runs the same checks on every domain: AAAA records on the domain and www, IPv6 on the nameservers and mail servers, and a real HTTP connection over IPv6 to confirm the records aren't decorative. The results feed everything here: the tiers, the country stats, and the changelog.

**Tranco?**

The [Tranco List](https://tranco-list.eu/) ranks the top million domains by aggregating several traffic lists, which smooths out the noise and manipulation that made single-source rankings like Alexa easy to game. It's the ranking researchers actually use.

We use it because the rank is half the shame: 'top 100 site, zero AAAA records' only lands if the ranking is credible.

**How accurate is the data?**

Treat the data as indicative, not absolute. DNS propagation and CDNs that answer differently per anycast location can shift a result from one scan to the next. That's why a status only changes after three consecutive scans agree.

**Why does a domain show as not supporting IPv6 when it does?**

Usually DNS propagation lag, a CDN answering differently from our vantage point, or a server that didn't respond during that scan. A real fix sticks after three consecutive daily scans, so give it a few days. If it still looks wrong, contact us.

Also note the crawler verifies reachability: an AAAA record that doesn't answer over IPv6 still counts as unsupported.

## Tab 2 — Rules, Frequency, and API Access

### Crawler

**Crawler Rules**

The crawler checks AAAA records on domain.com, www.domain.com, and the domain's NS and MX records. It also opens a real HTTP connection over IPv6 — publishing an AAAA record that doesn't answer won't fool anyone.

The domain and www lookups go through three independent public resolvers, and two out of three must agree.

**Crawler Frequency**

Every domain is scanned once per day. A status only changes after 3 consecutive scans agree, so one flaky DNS answer won't flip your verdict.

**What does “Not applicable” mean?**

There was nothing to grade — it never counts against a domain.

For Mail (MX) it means the domain publishes no MX records: no mail service, nothing to check. A domain without mail can still become a Hero.

For Page resources it means one of two things: the page loads over IPv6 and pulls no resources from external hosts, or the site isn't reachable over IPv6 at all — then its resources can't be evaluated. The domain status card and the live check both spell out which one applies.

**Why does a domain list subdomains?**

An apex can score green while the part people actually use, the login portal or the API, is still IPv4 only. Anyone can list those hosts for a domain, and the crawler then checks them exactly like any other domain.

Subdomain results are informational: they never change the parent domain's rating or any of the country and campaign numbers. What gets listed depends on who took the time to list it, and a domain should not score worse for having attentive users. Add one by opening a PR on the [campaign repo](https://github.com/lasseh/whynoipv6-campaign).

**Crawler Errors**

Found a bug in the crawler? PRs are welcome.

**Heroes**

Hero status takes IPv6 on domain.com, www.domain.com, and the nameservers. MX hosts need IPv6 too (or no MX at all), and the site has to actually answer over IPv6.

**Saints**

Saints are Heroes that also load all their page resources (scripts, fonts, images) over IPv6. The full package: the site works on an IPv6-only connection.

### Campaign Crawler

**How do I create my own campaign?**

Open an issue on the [GitHub repo](https://github.com/lasseh/whynoipv6-campaign).

**How can I get my domain removed from the list?**

Yes, you can start using IPv6!

### API

**Can I get access to the API?**

Yes, the API is open — no key, no signup. Everything on this site is served from it, at **https://api.whynoipv6.com** (no version prefix).

Start with the [interactive docs](https://api.whynoipv6.com/docs), the raw [OpenAPI spec](https://api.whynoipv6.com/openapi.json), or — if you're pointing an LLM agent at the data — [llms.txt](https://api.whynoipv6.com/llms.txt).

**Can I download the whole dataset?**

Yes — daily snapshots (CSV and Parquet) are published at [api.whynoipv6.com/datasets](https://api.whynoipv6.com/datasets). Please don't paginate the whole API when a bulk file exists.

The data is licensed CC-BY-NC-4.0. Attribution: Data: whynoipv6.com (CC-BY-NC-4.0). Ranks: Tranco.

**Is there a badge for my README?**

Every domain has an SVG status badge. Embed it in markdown:

`![IPv6](https://api.whynoipv6.com/badge/yourdomain.com.svg)`

**Can I follow changes as a feed?**

The changelog is available as [Atom](https://api.whynoipv6.com/changelog.atom) and [JSON Feed](https://api.whynoipv6.com/changelog.feed.json), with per-domain, per-country, and per-campaign variants — see the docs.

## Tab 3 — Resources

**IPv6**
- [Internet Society IPv6](https://www.internetsociety.org/deploy360/ipv6/)
- [IPv6 Ready test](https://ready.chair6.net/)

**IPv6 Networking Best Practices**
- [IPv6 Subnetting - Best Practices](https://blog.apnic.net/2023/04/04/ipv6-architecture-and-subnetting-guide-for-network-engineers-and-operators/)
- [IPv6 Security Considerations](https://www.internetsociety.org/deploy360/ipv6/security/)

**Community and Forums**
- [r/ipv6](https://www.reddit.com/r/ipv6/)
- [IPv6 Forum](https://www.ipv6forum.com/)
- [IPv6 Buzz Podcast](https://packetpushers.net/podcasts/ipv6-buzz/)

**Courses and Certifications**
- [Hurricane Electric IPv6 Certification Project](https://ipv6.he.net/certification/)
- [Getting Started with IPv6 (Coursera)](https://www.coursera.org/projects/ip-address-v6)

**Reports and IPv6 Status**
- [Global IPv6 Deployment Progress Report](https://bgp.he.net/ipv6-progress-report.cgi)
- [World IPv6 Launch](https://www.worldipv6launch.org/)
- [Google IPv6 Statistics](https://www.google.com/intl/en/ipv6/statistics.html)
- [IPv6 Deployment Aggregated Status](https://www.vyncke.org/ipv6status/)
- [AWS service endpoints by region and IPv6 support](https://awsipv6.neveragain.de/)

**Stickers**

Fly the colors. Nothing says 'ask me about AAAA records' like a protocol sticker.

Put one on your laptop, your rack, or a Sinner's front door. Get permission for that last one.

Order yours:
- [Small (2.4" x 3")](https://www.stickermule.com/u/89ea0892a27fc29/item/14732767)
- [Medium (3.2" x 4")](https://www.stickermule.com/u/89ea0892a27fc29/item/14732768)

## Tab 4 — About

**# whoami**

I'm Lasse, a network engineer from Norway. By day I build and run networks; by night I run a crawler that shames billion-dollar companies who still won't publish an AAAA record.

None of it is personal. Any domain can walk off the Sinners list with one DNS change and a server that answers over IPv6; the crawler forgives after three clean scans.

The endgame is an empty Sinners list. IPv6 turns 30 in 2028. I'd like to be done before it turns 40.

**Contact**

Twitter / X: [@whynoipv6](https://twitter.com/WhyNoIPv6)

Email: **whynoipv6@protonmail.com**

**Status page**

Uptime and incident history: [status.whynoipv6.com](https://status.whynoipv6.com/)

**Our Supporters**

These organizations supported the site early on, back when it was one crawler and a grudge.

---

# Error pages

## Page not found

404 — NXDOMAIN

# Shame on us. This page doesn't resolve.

We checked every record: A, AAAA, even MX. Unlike our Sinners, this URL has a valid excuse for being unreachable: it doesn't exist.

Buttons: Back to homepage · Check a domain

WE LOOKED EVERYWHERE — A · AAAA · MX · CNAME · TXT

## Domain not found

# Domain not found

{domain} isn't in our database.

Either our crawler hasn't met it yet, or that's a typo. NXDOMAIN, basically.

**Not in the crawl**

No record of it in our data. The likely reasons:
- Not yet picked up by the crawler
- A typo in the domain name
- Outside the Tranco top million

**Put it on the list**

Run a [live check](/check/{domain}) on it right now, or submit it through the community campaign and we'll start keeping score. Once merged, the crawler picks it up on its next run.

Link: Submit to campaign →

Buttons: Browse domains · Search domains · Go home

When no domain was given, "{domain}" reads: unknown.invalid

## When the API is down

The error card shows a title and explanation sent by the API. If the network itself
fails, the title reads: Request failed

---

# The words we grade with

These appear all over the site, so a change here changes many pages at once.

## Status of a check

| Word | Means |
|---|---|
| Supported | It has IPv6 |
| Missing | It doesn't |
| No record | Nothing published at all |
| Not applicable | Nothing to grade; never counts against a domain |
| Not yet checked | The crawler hasn't got there |

The live check adds four more: **Partial** · **Check error** · **Resolvers disagreed** · **Not checked**, and shows **No result** when a tracked domain's observation was withheld.

## Overall rating

Rating: **Good** (60% and up) · **Medium** (40% and up) · **Bad** (below) · **Unknown** (nothing to rate)

## Tiers

Sinners · Heroes · Saints · Almost Heroes

---

# Small print

Text most people never see — screen-reader labels, image descriptions, and hidden form
labels. Worth a read anyway, since it's what blind visitors hear.

**Image descriptions:** Why No IPv6 logo · Why No IPv6 sticker · Blix · Woodcut nun ringing a bell in an empty cathedral

**Link and button labels:** Why No IPv6 home · GitHub · Twitter · Blog RSS feed · Menu · Close · Apply filter · Breadcrumb · Pagination · Domain list · Home

**Hidden form labels:** Search domains · Domain · Filter · Search

**Loading:** Loading...

**Chart descriptions:**
- Stacked daily count of domains per classification
- Daily count of domains passing each IPv6 check
- Daily count of checks gaining and losing IPv6 support
- IPv6 adoption against domains hosted, per {network}
- {provider} IPv6 share

**Timeline blocks:** {date} — {status}

**Rating star tooltip:** Not applicable

---

# Already known, no need to flag

Three things are reproduced above exactly as they ship, so you'll see them and wonder:

**Em dashes.** The voice guide bans them, but around a dozen shipped anyway — in the FAQ's API and crawler answers, the informational panel's "Advisory — never affects the rating", and the live-check errors. The changelog phrases ("published IPv6 addresses — but connections fail") keep theirs on purpose.

**Curly vs straight apostrophes.** Mixed. The clearest case: "the site isn’t reachable over IPv6, so page resources can’t be evaluated" appears in two places, curly on the live check and in the status card, straight in the FAQ answer just above it. Same sentence, two spellings.

**Repeated strings.** Some text appears in more than one place and must change in both: the menu labels (desktop and mobile), the status words (row label and hover tooltip), the changelog phrases (site and RSS/JSON feeds), the Facebook and Twitter share descriptions, and "IPv6 ready" on the country, campaign and detail bars. `docs/copy.md` lists every pair.

Everything else is fair game — say what reads wrong.
