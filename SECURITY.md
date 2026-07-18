# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

- Email: [whynoipv6@protonmail.com](mailto:whynoipv6@protonmail.com)
  (PGP key: https://whynoipv6.com/.well-known/pgp-key.txt)
- Or use GitHub's private vulnerability reporting on this repository.

You'll get a response within a few days. The site's machine-readable policy lives at
[whynoipv6.com/.well-known/security.txt](https://whynoipv6.com/.well-known/security.txt).

## Scope

- The public API (`api.whynoipv6.com`) and website (`whynoipv6.com`)
- The code in this repository (API server, crawler, operator CLI, frontend)

The service is public and anonymous by design — there are no accounts, sessions, or
personal data. Reports that meaningfully affect the integrity of published
measurements (e.g. ways to spoof a domain's IPv6 status, SSRF through the checker,
or cache poisoning of the resolvers) are very much in scope.

## Supported versions

Only the latest deployed version (the `main` branch) is supported.
