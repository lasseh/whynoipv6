# 0001 — GeoIP attribution via IPinfo Lite (not MaxMind GeoLite2)

- **Status:** Accepted
- **Date:** 2026-07-11
- **Deciders:** project owner
- **Touches:** `internal/geoip`, `cmd/v6ctl` (`geoip update`), `compose.yaml`, 06-ingest §6, 09-ops §11, 12-frontend §9.4

## Context

The crawler attributes each scanned host to a **country** and an **ASN** (06-ingest §6). It reads this from a local mmdb file at scan-commit time — offline, with an hourly mtime-based hot reload — because per-IP HTTP lookups across the Tranco top-1M are infeasible on both latency and rate limits.

The original design used **MaxMind GeoLite2** (two files: `GeoLite2-ASN.mmdb` + `GeoLite2-Country.mmdb`), provisioned by the distro `geoipupdate` package with a MaxMind account ID + license key. Friction: a mandatory account/license-key + EULA, a distro package and `/etc/GeoIP.conf` to template, and two files to keep in sync. The owner prefers IPinfo (already used in other projects).

The project uses **only country-level and ASN** attribution — never city-level, which is the only dimension where GeoIP providers materially differ. At country + ASN granularity the major providers are comparable.

## Decision

Switch GeoIP attribution to the free **IPinfo Lite** database.

- **Data:** one combined file `ipinfo_lite.mmdb` (country + ASN, IPv4+IPv6), refreshed daily.
- **Reader:** `github.com/oschwald/maxminddb-golang/v2` (generic mmdb decoder) — **not** `geoip2-golang` (MaxMind-schema-specific) and **not** `ipinfo/go` (an HTTP API client with no offline mmdb support). Fields consumed: `asn` (textual `"AS13335"`, parsed to uint), `as_name`, `country_code`.
- **Provisioning:** a new `v6ctl geoip update` command downloads the file into `GEOIP_PATH` atomically (temp → mmdb-verify → rename), authenticated with `IPINFO_TOKEN` via an `Authorization` header (never the URL). It backs both the dev compose `geoip-init` service and the prod `v6ctl-geoip-update.timer` (daily), replacing `geoipupdate` (09-ops §11).

The `IPMeta` seam (`ASN`/`CountryCode`) and the entire attribution algorithm (ccTLD-wins-over-GeoIP, ASN auto-registration, sentinels) are unchanged; only the data source and reader change.

## Consequences

**Positive**

- Quality on par for country + ASN (the only dimensions used).
- No MaxMind account/license-key/EULA; no distro package. One free token.
- One file instead of two — the geoip reader collapses from dual readers/mtimes to one.
- Daily updates (vs GeoLite2's twice-weekly); enrichment available (`as_domain`).

**Negative / obligations**

- **Attribution required.** IPinfo Lite is **CC BY-SA 4.0** — the frontend footer must credit IPinfo with a link (12-frontend §9.4).
- **License interaction to watch.** The site declares its own output **CC-BY-NC-4.0**; CC BY-SA and CC BY-NC are not directly compatible. The derived country/ASN columns redistributed via the API/datasets could implicate the ShareAlike term. Flagged as an open policy item — not resolved here, and not legal advice.
- A network fetch is now part of GeoIP provisioning (previously the distro package's job); mitigated by the atomic install + mmdb verify.

## Alternatives considered

- **Keep MaxMind GeoLite2.** Rejected: the owner dislikes the account/license-key/EULA model; no quality benefit at country/ASN granularity.
- **`ipinfo/go` official library.** Rejected: it is an HTTP API client only — no offline mmdb reading — which does not fit the offline, hot-reloaded scan-commit path.
- **IPinfo HTTP API per-IP.** Rejected on scale: infeasible across the top-1M crawl (latency + rate limits).
