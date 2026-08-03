---
title: IPv6 DNS is about to become the law. Well, a best practice.
description: The IETF is replacing its 2004 DNS transport guidance. Every zone MUST run two IPv6-reachable nameservers. One in five of the top million runs zero.
date: 2026-08-03
---

For twenty-one years, the official word on DNS over IPv6 has been
[RFC 3901](https://www.rfc-editor.org/info/rfc3901), a 2004 document written to
keep early IPv6 enthusiasm from breaking IPv4 resolution. Its successor,
[draft-ietf-dnsop-3901bis](https://datatracker.ietf.org/doc/draft-ietf-dnsop-3901bis/)
by Momoka Yamamoto and Tobias Fiebig, inverts the concern. It has cleared the
IESG as a Best Current Practice and sits in the RFC Editor's final review as we
write this; the number being passed around for it is RFC 10001, which would be
a suitably round monument. When it publishes, RFC 3901 is obsolete and the
requirement reads like this:

> Every DNS zone MUST be served by at least two authoritative DNS servers
> providing services via IPv4, and at least two providing services via IPv6,
> serving identical DNS data.

Not the website. Not the mail. The zone itself. Two servers, each address
family, same answers. This happens to be a thing we measure every day for the
Tranco top million, so here is the compliance report the internet did not ask
for.

## The scoreboard

Numbers frozen on the publish date, as usual. Of 991,440 tracked zones,
**206,345 (20.8%) have zero IPv6-capable nameservers**. Not one server short
of the requirement. Zero.

Against the actual two-server bar, measured across the 968,091 zones our
crawler passed in the last day:

- **77.8%** already meet it (two or more nameservers with AAAA records)
- **21.2%** have none at all
- **1.0%** sit at exactly one

That last line is the interesting one. Almost nobody half-deploys IPv6 DNS.
Either your DNS operator dual-stacked every server years ago, or nobody has
touched it since the zone was delegated. The top 1,000 does better, and still
14.2% of it fails the bar.

## Names, since that is what we do here

Zones with zero IPv6 nameservers include [akamai.net](/domains/akamai.net)
at rank 12, which is a content delivery network,
[twitter.com](/domains/twitter.com) and [x.com](/domains/x.com),
[samsung.com](/domains/samsung.com), [playstation.net](/domains/playstation.net),
and [t.co](/domains/t.co), the URL shortener for the site with no IPv6 either
way.

Better still: 20,403 zones serve their website over IPv6 while their DNS
remains IPv4-only. [europa.eu](/domains/europa.eu) leads that list at rank 113. The European Union has an official IPv6 strategy; its zone has no IPv6
nameservers. [un.org](/domains/un.org) is a few hundred ranks behind, in case
anyone hoped this was a regional problem. [hp.com](/domains/hp.com),
[intel.com](/domains/intel.com), and [cornell.edu](/domains/cornell.edu) round
out the category: sites that pass every check a visitor can see and fail the
one the resolver sees first.

## Whose checkbox is it

Compliance here is rarely something a domain owner does. It is a property of
whoever runs the nameservers. The league table for operators serving at least
5,000 of the top million:

| DNS operator        | zones   | with IPv6 nameservers |
| ------------------- | ------- | --------------------- |
| Cloudflare          | 358,975 | 100.0%                |
| Amazon Route 53     | 88,367  | 100.0%                |
| GoDaddy             | 41,846  | 97.2%                 |
| Alibaba Cloud DNS   | 16,682  | 100.0%                |
| Akamai Edge DNS     | 12,664  | 99.0%                 |
| Google Cloud DNS    | 12,437  | 100.0%                |
| Microsoft Azure DNS | 10,933  | 100.0%                |
| Namecheap           | 8,930   | 99.8%                 |
| Tencent DNSPod      | 6,718   | 86.2%                 |
| OVHcloud            | 6,497   | 99.1%                 |
| Network Solutions   | 5,556   | 0.1%                  |

Five operators sit at a flat 100.0%. If your zone is on any of them, you were
made compliant without being consulted. Akamai's customer DNS product is at
99.0%; Akamai's own akamai.net is at zero.

And then there is Network Solutions, the company that operated the .com
registry through the nineties, now running DNS for five and a half thousand
top-million domains. 0.1% of them have an IPv6 nameserver. The remaining
99.9% are about to be out of compliance with a Best Current Practice, as a
fleet, in one decision.

## The trajectory

Over the last ten days our [changelog](/changelog) recorded 1,552 zones
gaining their first IPv6 nameserver and 961 losing their last one. Net: +59
zones per day. At that rate, the 206,345-zone backlog clears in roughly nine
and a half years. The BCP should still be current.

## Method, briefly

We count AAAA records on a zone's NS hosts, up to four nameservers per zone.
That measures publication, not service: the RFC demands servers that answer
over IPv6, and we only check that they have an address to answer on, so real
non-compliance can only be higher than our numbers. In the other direction,
zones with more than four nameservers can hide their IPv6 ones past our cap;
the typical zone runs two or three in total, so that error is small. Full
methodology is on the [FAQ](/faq).

Credit where due: [Ching Chiao's post](https://www.linkedin.com/pulse/rfc-10001-just-made-ipv6-dns-requirement-most-zones-arent-ching-chiao-bblcc/)
flagged the compliance angle, with passive-DNS measurements putting the
whole namespace near 40% unresolvable over IPv6. The top million is the
well-lit end of the street.

Your own zone takes ten seconds to grade on the [live check](/check), or
`dig AAAA` your NS records and count the answers yourself. Two is the bar.
