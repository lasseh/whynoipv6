<br>
<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/images/github-logo-dark.webp">
    <img alt="Why No IPv6: Shame as a Service" src=".github/images/github-logo-light.webp">
  </picture>
</div>
<br>
<div align="center">

[![License](https://img.shields.io/github/license/lasseh/whynoipv6)](#license)
[![Website](https://img.shields.io/website?url=https%3A%2F%2Fwhynoipv6.com)](https://whynoipv6.com/)
[![Issues - whynoipv6](https://img.shields.io/github/issues/lasseh/whynoipv6)](https://github.com/lasseh/whynoipv6/issues)
[![GitHub status](https://img.shields.io/badge/GitHub_IPv6-Missing-red?logo=github)](https://whynoipv6.com/domains/github.com)

</div>
<br>
<p align="center">
    <a href="https://github.com/lasseh/whynoipv6/issues/new?assignees=lasseh&labels=bug&projects=&template=bug_report.md&title=%F0%9F%90%9B+Bug+Report%3A+">Report Bug</a>
    ·
    <a href="https://github.com/lasseh/whynoipv6/issues/new?assignees=lasseh&labels=enhancement&projects=&template=feature_request.md&title=%F0%9F%9A%80+Feature%3A+">Request Feature</a>
  </p>

## What is WhyNoIPv6.com?

WhyNoIPv6.com is a public, account-free measurement service that asks a simple question
about the internet's most popular domains: *"Does this site work over IPv6, and if not,
why not?"*

The crawler checks domains from Tranco's top-million ranking and community-maintained
campaigns. It measures IPv6 support across websites, authoritative DNS, and mail
infrastructure. The results are published as domain tier lists and adoption views by
country, network, provider, and campaign.

<div align="center">
  <img alt="The github.com report: apex and www missing IPv6, nameservers and mail supported, with a 90-day timeline" src=".github/images/github-status.png">
</div>

## Why is IPv6 important?

The unallocated IPv4 address pools are exhausted. Keeping the IPv4 internet growing now
depends on address sharing, address transfers, and increasingly complicated workarounds.
IPv6 provides the address space needed for continued growth without those constraints.

IPv6 is not new or experimental. Its first Standards Track specification was published
in 1998, and World IPv6 Launch took place in 2012. Major access networks, cloud providers,
and content platforms have supported it for years. Popular websites that still lack IPv6
are leaving the job unfinished. WhyNoIPv6 measures that gap and makes it public.

## How does it work?

Active domains are normally checked once every 24 hours:

- **What is checked:** The crawler looks for IPv6 records on the root domain, `www`,
  authoritative nameservers, and mail servers. It also tests whether the website answers
  over IPv6. Additional diagnostics cover DNSSEC, SPF, PTR, SMTP, TLS, response parity,
  and latency. For reachable sites, it discovers external resource hosts and checks their
  IPv6 records; it does not download and test every page resource.
- **Reliable results:** The root-domain and `www` checks normally query three public DNS
  resolvers and require at least two matching answers. After the first result is
  established, later changes must appear repeatedly before the public status changes.
  An error or one unreliable DNS response does not rewrite an established status.
- **Clear classifications:** Confirmed results place each domain in the **hero**,
  **partial**, **sinner**, or **inactive** tier. Domains remain **unknown** until there is
  enough evidence. Classification uses no numeric score or letter grade. A hero reaches
  **saint** status when its external resource-host check finds no IPv4-only dependency.

## What is Tranco?

The [Tranco list](https://tranco-list.eu/) is a research-oriented ranking of popular
domains. It combines several data sources and is designed to resist manipulation,
addressing problems found in older rankings such as Alexa. WhyNoIPv6 uses Tranco's
standard top-million list; it is a popularity ranking, not a direct traffic census.

## Campaigns

Campaigns track groups such as banks, government websites, or ISPs in a particular
country. They let anyone follow and shame the domains they care about beyond the Tranco
list. Campaign definitions are plain YAML files kept in the separate
[whynoipv6-campaign](https://github.com/lasseh/whynoipv6-campaign) repository. Follow that
repository's contribution instructions to submit one.

## The stack

The monorepo has three main source areas:

| Part | What it is |
| --- | --- |
| [`backend/`](backend/) | A Go module containing `api` (public HTTP API), `crawler` (scanning daemon), and `v6ctl` (operator CLI), backed by PostgreSQL and TimescaleDB |
| [`frontend/`](frontend/) | Vue 3 + TypeScript + Tailwind app for [whynoipv6.com](https://whynoipv6.com) |
| [`openapi/`](openapi/) | The OpenAPI 3 contract used to generate Go models and frontend TypeScript types for [api.whynoipv6.com](https://api.whynoipv6.com) |

- **[docs/architecture.md](docs/architecture.md)** — how the system fits together
- **[docs/deploy.md](docs/deploy.md)** — run it locally with Docker, deploy it for real
- **[docs/internals.md](docs/internals.md)** — a tour of the codebase
- **[docs/spec/](docs/spec/)** — the frozen build specification; later decisions live in [docs/adr/](docs/adr/)

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) to run the local stack
and [SECURITY.md](SECURITY.md) to report vulnerabilities.

## License

The code is licensed under [GPL-3.0](LICENSE). WhyNoIPv6 measurement data is published
under [CC-BY-NC-4.0](https://creativecommons.org/licenses/by-nc/4.0/). Data imported or
derived from third-party sources remains subject to the source's terms and attribution
requirements.

## Contributors

<a href="https://github.com/lasseh">
  <img src="https://github.com/lasseh.png?size=50" alt="lasseh" width="50" height="50">
</a>
<a href="https://github.com/aulonm">
  <img src="https://github.com/aulonm.png?size=50" alt="aulonm" width="50" height="50">
</a>
<a href="https://github.com/joms">
  <img src="https://github.com/joms.png?size=50" alt="joms" width="50" height="50">
</a>
<a href="https://github.com/sklirg">
  <img src="https://github.com/sklirg.png?size=50" alt="sklirg" width="50" height="50">
</a>
<a href="https://github.com/Foxboron">
  <img src="https://github.com/Foxboron.png?size=50" alt="Foxboron" width="50" height="50">
</a>
