---
title: IPv6 DNS is now the law. Well, a best practice.
description: The IETF has replaced its 2004 DNS transport guidance. Every zone MUST run two IPv6-reachable nameservers. One in five of the top million runs zero.
date: 2026-08-12
---

The rule for DNS over IPv6 was [RFC 3901](https://www.rfc-editor.org/info/rfc3901), from 2004. It was written to keep early IPv6 enthusiasm from breaking IPv4 resolution. Its replacement worries about the opposite. The new requirement:

> To prevent DNS name space partitioning, at least two IPv4-reachable and
> two IPv6-reachable name servers MUST be configured for a zone. A single
> name server that is reachable over both IPv4 and IPv6 counts once per
> address family.

Not the website. Not the mail. The zone itself. Two servers, each address family, same answers.

The MUST is [RFC 10001](https://www.rfc-editor.org/rfc/rfc10001.html), by Momoka Yamamoto and Tobias Fiebig. A suitably round monument. It is a Best Current Practice, and it takes over BCP 91, the label RFC 3901 has carried since 2004. Same shelf, new text. RFC 3901 is obsolete.

We happen to measure exactly this, every day, for the Tranco top million. So here is the compliance report the internet did not ask for.

## The scoreboard

Numbers frozen on the publish date, as usual. We track 988,163 zones. **204,942 of them have zero IPv6-capable nameservers.** That is 20.7%. Not one server short of the requirement. Zero.

The actual bar is two servers. Our crawler graded 969,109 zones against it in the last day:

- **78.1%** already meet it (two or more nameservers with AAAA records)
- **20.9%** have none at all
- **1.0%** sit at exactly one

That last line is the interesting one. Almost nobody half-deploys IPv6 DNS. Either your DNS operator dual-stacked every server years ago, or nobody has touched the zone since delegation. The top 1,000 does better. Still, 14.3% of it fails the bar.

## Names, since that is what we do here

The zero-server club has names. [akamai.net](/domains/akamai.net) is in it, at rank 14. Akamai sells content delivery. [twitter.com](/domains/twitter.com) and [x.com](/domains/x.com) are in it too. So is [t.co](/domains/t.co), the URL shortener for the site with no IPv6 either way. [samsung.com](/domains/samsung.com) and [playstation.net](/domains/playstation.net) fill out the roster.

Better still: 21,117 zones serve their website over IPv6 while their DNS remains IPv4-only. [europa.eu](/domains/europa.eu) leads that list at rank 115. The European Union has an official IPv6 strategy. Its zone has no IPv6 nameservers. [un.org](/domains/un.org) is a few hundred ranks behind, in case anyone hoped this was a regional problem. [hp.com](/domains/hp.com), [intel.com](/domains/intel.com), and [cornell.edu](/domains/cornell.edu) are in there too. These sites pass every check a visitor can see. They fail the one the resolver sees first.

## Whose checkbox is it

Compliance here is rarely something a domain owner does. It is a property of whoever runs the nameservers. The league table for operators serving at least 5,000 of the top million:

| DNS operator        | zones   | with IPv6 nameservers |
| ------------------- | ------- | --------------------- |
| Cloudflare          | 364,065 | 100.0%                |
| Amazon Route 53     | 87,573  | 100.0%                |
| GoDaddy             | 41,676  | 97.3%                 |
| Alibaba Cloud DNS   | 16,207  | 99.9%                 |
| Akamai Edge DNS     | 13,364  | 99.0%                 |
| Google Cloud DNS    | 12,306  | 100.0%                |
| Microsoft Azure DNS | 10,778  | 100.0%                |
| Namecheap           | 10,413  | 99.8%                 |
| Tencent DNSPod      | 6,458   | 86.7%                 |
| OVHcloud            | 6,456   | 98.9%                 |
| Network Solutions   | 5,343   | 0.0%                  |

Four operators round to 100.0%. That is rounding, not perfection: between them, 92 zones out of nearly half a million still have no IPv6 nameserver. If your zone is on any of them, you were made compliant without being consulted. Akamai's customer DNS product is at 99.0%. Akamai's own akamai.net is at zero.

Then there is Network Solutions. It operated the .com registry through the nineties. Today it runs DNS for 5,343 top-million domains. Not one of them has an IPv6 nameserver. The whole fleet is out of compliance with a Best Current Practice. In one decision.

## The trajectory

Over the last ten days, our [changelog](/changelog) recorded 1,223 zones gaining their first IPv6 nameserver. It recorded 923 losing their last one. Net: +30 zones per day. The ten days before those ran at +48, so treat the trend as weather rather than climate. At the slower rate the 204,942-zone backlog clears in about nineteen years. At the faster one, twelve. The BCP should still be current.

## The advice is eight years old

Scott Hogg wrote [the operator version of this](https://hoggnet.com/blogs/news/why-you-should-dual-stack-your-dns-nameservers) in 2018. Start IPv6 at the internet edge, because that is where the public nameservers already sit. Dual-stack them. Then, if the parent zone holds IPv4 glue for your NS records, publish IPv6 glue beside it. His closing verdict was that running both protocols on your nameservers "is strongly recommended and is a task that is on the critical path to IPv6 deployment."

Strongly recommended is now MUST. The RFC asks for nothing that was not already on the list.

## Method, briefly

We count AAAA records on a zone's NS hosts, up to four nameservers per zone. That is the RFC's other line, the one saying it is RECOMMENDED that every name used in an NS record have both an A and a AAAA. The MUST is stricter. It wants servers that answer over IPv6, and we only check that they have an address to answer on, so real non-compliance can only be higher than our numbers. In the other direction, zones with more than four nameservers can hide their IPv6 ones past our cap. The typical zone runs two or three, so that error is small. Full methodology is on the [FAQ](/faq).

Credit where due: [Ching Chiao's post](https://www.linkedin.com/pulse/rfc-10001-just-made-ipv6-dns-requirement-most-zones-arent-ching-chiao-bblcc/) flagged the compliance angle. Its passive-DNS measurements put the whole namespace near 40% unresolvable over IPv6. Worse than our number, because we only count the top million. Those are the zones somebody is paid to maintain.

Your own zone takes ten seconds to grade on the [live check](/check). Or `dig AAAA` your NS records and count the answers yourself. Two is the bar.
