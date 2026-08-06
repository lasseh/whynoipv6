// PLACEHOLDER DATA — DELETE THIS FILE WHEN THE ENDPOINTS EXIST.
//
// Every panel on /metrics that has no API endpoint yet reads its numbers from
// here, and renders a <SampleBadge> saying so. Nothing else in the app may
// import this module: `rg 'fixtures/metrics'` is the complete list of what is
// still fabricated, and deleting this file plus its importers is the whole
// cleanup.
//
// The values are real observations copied out of the dev crawler's database on
// 2026-08-06, so the shapes and magnitudes are honest. They are frozen; they do
// not track the crawler, and they go stale the moment the crawler runs again.
//
// What each block still needs on the backend:
//
//   tracked                      — count(*) over `domain` WHERE NOT disabled.
//                                  /stats/overview reports the ranked list only,
//                                  and unfiltered /domains answers with MaxRank
//                                  rather than a row count, so the size of the
//                                  whole tracked set is not currently served.
//   crawlerToday                 — an aggregate over `crawler_metrics`; the
//                                  table is internal telemetry, so the endpoint
//                                  must expose throughput only, never per-worker
//                                  or per-lease detail.
//   reverseDns / mail            — aggregates over domain.ptr_observed and
//                                  domain.smtp_observed. Both columns exist and
//                                  are populated; neither is summarised anywhere.
//   adoptionDelta                — a GROUP BY day over `changelog`. The rows are
//                                  already served per domain by /changelog; this
//                                  is the missing daily roll-up.
//   networkAdoption              — `stats_asn_daily`. /asns/{number}/stats serves
//                                  one network at a time, so a top-N chart would
//                                  need ten round trips; a multi-network series
//                                  endpoint is the cheaper fix.
//   hostingLeague                — a GROUP BY domain.hosting_provider. The column
//                                  exists; there is no /hosting resource at all.

/**
 * Everything the crawler keeps score on, not just the ranked list.
 *
 * `stats_global_daily.domains` counts rows carrying a Tranco rank. The rest of
 * the live set is domains that have since fallen off the list plus the ones
 * that were never on it, and it is not a rounding error:
 *
 *   ranked            989,808
 *   unranked          102,946   (94.4k ex-Tranco, 8,261 campaign,
 *                                161 curated, 281 parent links, 3 live checks)
 *   ------------------------
 *   live domains    1,092,754   = count(*) FROM domain WHERE NOT disabled
 *
 * 1,454 of those are subdomains; the other 1,091,300 are apexes.
 */
export const tracked = {
  domains: 1092754,
  ranked: 989808,
}

/**
 * Rolling 24-hour throughput and the newest observation timestamp.
 *
 * `checked` slightly exceeds `tracked.domains` because a host can be re-checked
 * inside the same 24 hours (a live check, a campaign refresh, a retry).
 */
export const crawlerToday = {
  checked: 1095290,
  latest: '2026-08-06T18:49:28Z',
}

/** Reverse DNS on the hosts that actually answer over IPv6. */
export const reverseDns = {
  v6Hosts: 387606,
  withPtr: 27340,
  withoutPtr: 359737,
}

/** Mail: an AAAA on the MX is not the same as an SMTP banner over IPv6. */
export const mail = {
  mxOverV6: 508582,
  answering: 317235,
  paperOnly: 27475,
}

/** Checks that flipped to supported, and away from it, per day. */
export const adoptionDelta: { day: string; gained: number; lost: number }[] = [
  { day: '2026-07-25', gained: 130, lost: 66 },
  { day: '2026-07-26', gained: 563, lost: 509 },
  { day: '2026-07-27', gained: 925, lost: 699 },
  { day: '2026-07-28', gained: 472, lost: 307 },
  { day: '2026-07-29', gained: 1803, lost: 1019 },
  { day: '2026-07-30', gained: 2521, lost: 1557 },
  { day: '2026-07-31', gained: 2013, lost: 1295 },
  { day: '2026-08-01', gained: 2114, lost: 1410 },
  { day: '2026-08-02', gained: 1765, lost: 1152 },
  { day: '2026-08-03', gained: 1261, lost: 981 },
  { day: '2026-08-04', gained: 2105, lost: 1204 },
  { day: '2026-08-05', gained: 3007, lost: 1091 },
]

/**
 * Share of each network's hosted domains answering over IPv6, per day (%).
 *
 * Keyed by AS number, never by name: `asn.name` is not unique, and grouping on
 * it silently averages unrelated networks together. Five separate ASNs are
 * called "Google LLC", six "Microsoft Corporation", four "Hetzner Online GmbH".
 *
 * The first crawl day is excluded because coverage was still ramping (AS13335
 * held 35,117 domains that day against 324,319 now), which would have shown as
 * a share swing that was really a denominator swing.
 *
 * Read the levels, not the slopes. Coverage is still expanding across this
 * whole window, so a network's line moving by a fraction of a point says more
 * about how many of its domains we had reached than about anything it
 * deployed. Nothing here moved more than 0.8pp in thirteen days.
 */
export const networkAdoption = {
  days: [
    '2026-07-25',
    '2026-07-26',
    '2026-07-27',
    '2026-07-28',
    '2026-07-29',
    '2026-07-30',
    '2026-07-31',
    '2026-08-01',
    '2026-08-02',
    '2026-08-03',
    '2026-08-04',
    '2026-08-05',
    '2026-08-06',
  ],
  networks: [
    {
      asn: 13335,
      name: 'Cloudflare, Inc.',
      share: [
        85.26, 85.85, 85.84, 85.81, 85.8, 85.81, 85.84, 85.85, 85.87, 85.83, 85.72, 85.71, 85.74,
      ],
    },
    {
      asn: 16509,
      name: 'Amazon.com, Inc.',
      share: [
        10.39, 10.2, 10.21, 10.21, 10.22, 10.21, 10.18, 10.18, 10.18, 10.23, 10.19, 10.18, 10.18,
      ],
    },
    {
      asn: 24940,
      name: 'Hetzner Online GmbH',
      share: [
        22.32, 22.29, 22.3, 22.32, 22.34, 22.34, 22.3, 22.23, 22.23, 22.17, 22.06, 22.01, 22.11,
      ],
    },
    {
      asn: 16276,
      name: 'OVH SAS',
      share: [
        15.04, 14.77, 14.74, 14.76, 14.76, 14.7, 14.42, 14.46, 14.51, 14.38, 14.33, 14.32, 14.29,
      ],
    },
    {
      asn: 209242,
      name: 'Cloudflare London, LLC',
      share: [0.76, 0.81, 0.82, 0.81, 0.84, 0.82, 0.86, 0.82, 0.82, 0.77, 0.8, 0.76, 0.77],
    },
    {
      asn: 8075,
      name: 'Microsoft Corporation',
      share: [4.7, 4.57, 4.56, 4.57, 4.58, 4.61, 4.65, 4.66, 4.67, 4.68, 4.65, 4.68, 4.69],
    },
    {
      asn: 14618,
      name: 'Amazon.com, Inc.',
      share: [4.87, 5.26, 5.27, 5.19, 5.21, 5.18, 5.16, 5.24, 5.21, 5.24, 5.25, 5.28, 5.26],
    },
  ],
}

/**
 * Apex IPv6 adoption grouped by the hosting provider behind the domain.
 *
 * `domain.hosting_provider` stores lowercase slugs ('cloudfront', 'aws'); the
 * display names here are what an endpoint would have to return, since a slug
 * is not a brand.
 */
export const hostingLeague: { name: string; domains: number; apexV6: number }[] = [
  { name: 'Cloudflare', domains: 373468, apexV6: 314296 },
  { name: 'AWS', domains: 83854, apexV6: 5875 },
  { name: 'Hetzner', domains: 23391, apexV6: 5192 },
  { name: 'OVH', domains: 20152, apexV6: 2846 },
  { name: 'Google', domains: 17234, apexV6: 3058 },
  { name: 'CloudFront', domains: 16116, apexV6: 4154 },
  { name: 'Azure', domains: 14837, apexV6: 793 },
  { name: 'Akamai', domains: 14085, apexV6: 3250 },
  { name: 'DigitalOcean', domains: 9918, apexV6: 672 },
  { name: 'Fastly', domains: 6677, apexV6: 758 },
  { name: 'Linode', domains: 5801, apexV6: 1124 },
]
