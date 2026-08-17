# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

- Email: [whynoipv6@protonmail.com](mailto:whynoipv6@protonmail.com)
- Or use [GitHub's private vulnerability reporting](https://github.com/lasseh/whynoipv6/security/advisories/new)
  on this repository.

A useful report includes: what you found, where (URL, endpoint, or file), steps to
reproduce, and what you think the impact is. A working proof of concept beats a
scanner screenshot. The site's machine-readable policy lives at
[whynoipv6.com/.well-known/security.txt](https://whynoipv6.com/.well-known/security.txt).

## What to expect

- Acknowledgement within **5 business days**, usually sooner.
- Updates as triage and the fix progress, and a heads-up before anything is published.
- Coordinated disclosure: we'll agree on a timeline together, defaulting to **90 days**
  from report to public disclosure. If you want credit, you'll get it — in the fix's
  release notes and the advisory.
- This is an open-source side project: there is **no bug bounty**. What we offer is a
  fast fix, public credit, and gratitude.

## Scope

- The public website (`whynoipv6.com`, `www.whynoipv6.com`)
- The public API (`api.whynoipv6.com`), including the datasets downloads
- The crawler and its infrastructure (`crawler.whynoipv6.com`)
- The code in this repository (API server, crawler, operator CLI, frontend)

The service is public and anonymous by design — there are no accounts, sessions, or
personal data. Reports that meaningfully affect the integrity of published
measurements (e.g. ways to spoof a domain's IPv6 status, SSRF through the checker,
or cache poisoning of the resolvers) are very much in scope.

## Out of scope

- **The IPv6 status of a listed domain.** A domain showing as a sinner is a
  measurement, not a vulnerability — see the [FAQ](https://whynoipv6.com/faq) for
  how confirmation works and what to do if a result looks wrong.
- Vulnerabilities in the listed domains themselves. We measure them; we don't run them.
- The crawler visiting your site — that's what it does. See
  [whynoipv6.com/bot](https://whynoipv6.com/bot) for what it sends and how to verify it.
- Volumetric denial of service and rate-limit exhaustion testing.
- Raw automated-scanner output without a demonstrated impact.
- Missing security headers or TLS-configuration nitpicks on their own, unless you can
  show an actual exploit path.
- Social engineering of the maintainer or hosting providers.

## Safe harbor

Good-faith security research within the scope above is welcome. If you make a
reasonable effort to avoid privacy violations, data destruction, and service
degradation, and you report what you find as described here, we will not pursue
legal action or report you to law enforcement for it. Testing that knocks the
service over is not good faith — if in doubt, ask first.

## Supported versions

Only the latest deployed version (the `main` branch) is supported. The site runs a
rolling deployment of `main`; there are no maintained release branches.
