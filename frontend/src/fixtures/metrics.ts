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
//   hostingLeague                — a GROUP BY domain.hosting_provider. The column
//                                  exists; there is no /hosting resource at all.

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
