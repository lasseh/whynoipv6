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
 * Rolling 24-hour throughput and the newest observation timestamp.
 *
 * `checked` counts every host the crawler swept, which is a larger population
 * than stats_global_daily's `domains`: that series covers the ranked list
 * (989,808 live rows with a rank), while the crawler also works through
 * 102,946 unranked subdomains and campaign entries. The tile labels have to
 * keep saying which population they mean.
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

/** Share of each network's hosted domains answering over IPv6, per day (%). */
export const networkAdoption = {
  days: [
    '2026-07-24',
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
      name: 'Cloudflare, Inc.',
      share: [
        85.59, 85.26, 85.85, 85.84, 85.81, 85.8, 85.81, 85.84, 85.85, 85.87, 85.83, 85.72, 85.71,
        85.74,
      ],
    },
    {
      name: 'Hetzner Online GmbH',
      share: [
        27.17, 22.32, 22.29, 22.3, 22.32, 22.34, 22.34, 22.3, 22.23, 22.23, 22.17, 22.06, 22.01,
        22.11,
      ],
    },
    {
      name: 'OVH SAS',
      share: [
        13.91, 15.04, 14.77, 14.74, 14.76, 14.76, 14.7, 14.42, 14.46, 14.51, 14.38, 14.33, 14.32,
        14.29,
      ],
    },
    {
      name: 'Google LLC',
      share: [
        13.98, 11.69, 11.29, 11.31, 11.34, 11.32, 11.42, 11.4, 11.44, 11.47, 11.53, 11.44, 11.39,
        11.35,
      ],
    },
    {
      name: 'Amazon.com, Inc.',
      share: [
        11.09, 10.39, 10.2, 10.21, 5.19, 10.22, 10.21, 10.18, 5.24, 5.21, 10.23, 10.19, 10.18,
        10.18,
      ],
    },
    {
      name: 'Microsoft Corporation',
      share: [6.94, 4.7, 4.57, 4.56, 4.57, 4.58, 4.61, 4.65, 4.66, 4.67, 4.68, 4.65, 4.68, 4.69],
    },
    {
      name: 'Cloudflare London, LLC',
      share: [0.73, 0.76, 0.81, 0.82, 0.81, 0.84, 0.82, 0.86, 0.82, 0.82, 0.77, 0.8, 0.76, 0.77],
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
