---
title: IPv6 DNS is now the law. Well, a best practice.
description: The IETF has replaced its DNS transport guidance from 2004. Every zone MUST use two IPv6-reachable nameservers. One in five of the top million uses none.
date: 2026-08-12
---

Since 2004, the guidance for DNS over IPv6 has been [RFC 3901](https://www.rfc-editor.org/info/rfc3901). It was written to make sure early IPv6 deployments did not break DNS for IPv4 users. Its replacement addresses the opposite problem: DNS that still cannot be reached over IPv6. The new requirement says:

> To prevent DNS name space partitioning, at least two IPv4-reachable and
> two IPv6-reachable name servers MUST be configured for a zone. A single
> name server that is reachable over both IPv4 and IPv6 counts once per
> address family.

This is not about the website or its mail server. It is about the zone's authoritative DNS. Each zone needs at least two nameservers reachable over IPv4 and two reachable over IPv6. Dual-stack servers count toward both requirements.

The MUST comes from [RFC 10001](https://www.rfc-editor.org/rfc/rfc10001.html), by Momoka Yamamoto and Tobias Fiebig. A suitably round number for the occasion. The RFC is a Best Current Practice and replaces RFC 3901 as BCP 91. Same category, new guidance. RFC 3901 is now obsolete.

We measure this every day for the Tranco top million. Here is the compliance report the internet did not ask for.

## The scoreboard

These numbers are a snapshot from the publication date. We track 988,163 zones. **Of those, 204,942 have no IPv6-capable nameservers.** That is 20.7%. They are not one server short of the requirement. They have none.

The requirement is two servers. During the previous 24 hours, our crawler graded 969,109 zones against that requirement:

- **78.1%** meet it, with AAAA records on two or more nameservers
- **20.9%** have no IPv6 nameservers
- **1.0%** have exactly one

The last number is the interesting one. Almost nobody deploys IPv6 DNS halfway. DNS operators tend to enable IPv6 across their nameservers or not at all. The top 1,000 domains do better, but 14.3% still fail to meet the requirement.

## Naming names, since that is what we do here

The zero-server club includes some familiar names. [akamai.net](/domains/akamai.net) is in it at rank 14. Akamai sells content delivery services. [twitter.com](/domains/twitter.com), [x.com](/domains/x.com), and their URL shortener [t.co](/domains/t.co) are also in it. So are [samsung.com](/domains/samsung.com) and [playstation.net](/domains/playstation.net).

Another 21,117 zones serve their websites over IPv6 while keeping their DNS on IPv4 only. [europa.eu](/domains/europa.eu) leads that list at rank 115. The European Union has an official IPv6 strategy, but its own zone has no IPv6 nameservers. [un.org](/domains/un.org) appears a few hundred ranks later, so this is not just a regional problem. [hp.com](/domains/hp.com), [intel.com](/domains/intel.com), and [cornell.edu](/domains/cornell.edu) are on the list too. These sites pass the IPv6 check a visitor can see, but fail the DNS check their resolver performs first.

## Who controls this?

The domain owner rarely configures this directly. IPv6 support usually depends on the company that operates the nameservers. Here is the table for DNS operators serving at least 5,000 domains in the top million:

| DNS operator        | zones   | with at least one IPv6 nameserver |
| ------------------- | ------- | --------------------------------- |
| Cloudflare          | 364,065 | 100.0%                            |
| Amazon Route 53     | 87,573  | 100.0%                            |
| GoDaddy             | 41,676  | 97.3%                             |
| Alibaba Cloud DNS   | 16,207  | 99.9%                             |
| Akamai Edge DNS     | 13,364  | 99.0%                             |
| Google Cloud DNS    | 12,306  | 100.0%                            |
| Microsoft Azure DNS | 10,778  | 100.0%                            |
| Namecheap           | 10,413  | 99.8%                             |
| Tencent DNSPod      | 6,458   | 86.7%                             |
| OVHcloud            | 6,456   | 98.9%                             |
| Network Solutions   | 5,343   | 0.0%                              |

Four operators round to 100.0%. That is rounding, not perfection: between them, 92 zones out of nearly half a million still have no IPv6 nameserver. If your zone uses one of these operators, IPv6 was probably enabled without you having to ask. Akamai's DNS service for customers reaches 99.0%. Akamai's own akamai.net remains at zero.

Then there is Network Solutions. It operated the .com registry during the 1990s. Today it provides DNS for 5,343 domains in the top million, and not one has an IPv6 nameserver. One operator-level decision leaves the entire group outside the Best Current Practice.

## The trajectory

During the last ten days, our [changelog](/changelog) recorded 1,223 zones gaining their first IPv6 nameserver and 923 losing their last. That is a net gain of 30 zones per day. The previous ten days averaged 48, so treat this as weather rather than climate. At the slower rate, clearing the backlog of 204,942 zones would take about 19 years. At the faster rate, it would take 12. The BCP should still be current by then.

## The advice is eight years old

Scott Hogg gave operators [the same advice](https://hoggnet.com/blogs/news/why-you-should-dual-stack-your-dns-nameservers) in 2018. Start IPv6 at the internet edge, where public nameservers already sit. Make those servers dual-stack. If the parent zone publishes IPv4 glue for your NS records, publish IPv6 glue alongside it. He concluded that running both protocols on nameservers "is strongly recommended and is a task that is on the critical path to IPv6 deployment."

What was strongly recommended is now a MUST. The work itself is not new.

## Method, briefly

We check up to four nameservers per zone and count the NS hosts with AAAA records. The RFC also RECOMMENDS that every hostname in an NS record have both an A and a AAAA record.

The MUST is stricter: the nameservers must actually answer over IPv6. We check whether a server has an IPv6 address, not whether it responds, so the true rate of non-compliance may be higher than our figures. We can also undercount IPv6 support when a zone has more than four nameservers and its IPv6-capable servers fall beyond our limit. Most zones use two or three nameservers, so this error should be small. The full methodology is on the [FAQ](/faq).

Credit where it is due: [Ching Chiao's post](https://www.linkedin.com/pulse/rfc-10001-just-made-ipv6-dns-requirement-most-zones-arent-ching-chiao-bblcc/) highlighted the compliance issue. Its passive DNS measurements found that nearly 40% of the entire namespace could not be resolved over IPv6. That is worse than our result because we only measure the top million domains, where someone is more likely to be paid to maintain the zone.

You can grade your own zone in ten seconds with the [live check](/check). Or use `dig AAAA` on your NS hostnames and count the answers yourself. You need at least two.
