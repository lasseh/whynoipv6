import { describe, expect, it } from 'vitest'
import { domainPageHead } from '@/utils/domain-head'
import { domainDetail } from '@/pages/__tests__/test-utils'
import type { DomainDetail, Schemas } from '@/api'

type Status = Schemas['IPv6Status'] | null

/** The fixture with the four prose dimensions set explicitly. */
function domain(
  over: Partial<DomainDetail>,
  dims: Partial<Record<'base' | 'www' | 'ns' | 'mx', Status>> = {},
): DomainDetail {
  const value = (v: Status) => ({ value: v, since: null })
  return {
    ...domainDetail,
    ...over,
    status: {
      ...domainDetail.status,
      base: value(dims.base ?? null),
      www: value(dims.www ?? null),
      ns: value(dims.ns ?? null),
      mx: value(dims.mx ?? null),
    },
  }
}

const all = { base: 'supported', www: 'supported', ns: 'supported', mx: 'supported' } as const

describe('domainPageHead', () => {
  it('asks the long-tail question in the title', () => {
    expect(domainPageHead(domain({ host: 'vg.no' }, all)).title).toBe('Does vg.no support IPv6?')
  })

  // The description is the whole point: before this, ~15k crawled domain pages
  // shipped one shared sentence, which is what "Crawled - currently not
  // indexed" is made of. Each row below must read as its own page.
  const cases: [string, DomainDetail, string][] = [
    [
      'saint outranks the classification phrase',
      domain({ host: 'google.com', classification: 'hero', saint: true }, all),
      'google.com is an IPv6 saint. IPv6 on the apex, www, nameservers, and mail.',
    ],
    [
      'hero without sainthood',
      domain({ host: 'facebook.com', classification: 'hero' }, all),
      'facebook.com supports IPv6. IPv6 on the apex, www, nameservers, and mail.',
    ],
    [
      'partial splits the two lists',
      domain({ host: 'cloudflare.com', classification: 'partial' }, { ...all, mx: 'unsupported' }),
      'cloudflare.com has partial IPv6. IPv6 on the apex, www, and nameservers, but not mail.',
    ],
    [
      'two misses join with or, not and',
      domain(
        { host: 'x.com', classification: 'partial' },
        { ...all, ns: 'no_record', mx: 'unsupported' },
      ),
      'x.com has partial IPv6. IPv6 on the apex and www, but not nameservers or mail.',
    ],
    [
      // The almost-heroes cohort: IPv6 everywhere but the apex. The ladder is
      // strict, so these classify as sinners — reading the phrase off the
      // classification alone shipped "openai.com has no IPv6. IPv6 on www,
      // nameservers, and mail, but not the apex", caught against live data.
      'a sinner that passes something is "not IPv6 ready", never "has no IPv6"',
      domain(
        { host: 'openai.com', classification: 'sinner' },
        {
          base: 'unsupported',
          www: 'supported',
          ns: 'supported',
          mx: 'supported',
        },
      ),
      'openai.com is not IPv6 ready. IPv6 on www, nameservers, and mail, but not the apex.',
    ],
    [
      'sinner names what is missing rather than repeating the verdict',
      domain(
        { host: 'nrk.no', classification: 'sinner' },
        {
          base: 'unsupported',
          www: 'unsupported',
          ns: 'unsupported',
          mx: 'unsupported',
        },
      ),
      'nrk.no has no IPv6. Missing on the apex, www, nameservers, and mail.',
    ],
    [
      // not_applicable is neither earned nor missing (a no-mail domain is
      // never penalized), so it must not appear in either list.
      'not_applicable is omitted, not counted against the domain',
      domain(
        { host: 'quiet.example', classification: 'hero' },
        {
          base: 'supported',
          www: 'supported',
          ns: 'supported',
          mx: 'not_applicable',
        },
      ),
      'quiet.example supports IPv6. IPv6 on the apex, www, and nameservers.',
    ],
    [
      // The engine never looks up www.<subdomain>, so naming it would invent a
      // hostname that was never checked.
      'a subdomain never mentions www',
      domain(
        { host: 'api.example.com', kind: 'subdomain', classification: 'partial' },
        {
          base: 'supported',
          ns: 'unsupported',
          mx: 'not_applicable',
        },
      ),
      'api.example.com has partial IPv6. IPv6 on the apex, but not nameservers.',
    ],
    [
      'unconfirmed dimensions drop the detail clause entirely',
      domain({ host: 'fresh.example', classification: 'sinner' }, {}),
      'fresh.example has no IPv6.',
    ],
    [
      'inactive gets its own phrasing',
      domain({ host: 'gone.example', classification: 'inactive' }, { base: 'unsupported' }),
      'gone.example is not responding. Missing on the apex.',
    ],
  ]

  it.each(cases)('%s', (_name, d, expected) => {
    expect(domainPageHead(d).description).toBe(expected)
  })

  // Nothing confirmed means no verdict to state; the caller keeps the route's
  // generic description rather than the util inventing one.
  it('returns a null description for an unclassified domain', () => {
    const head = domainPageHead(domain({ host: 'new.example', classification: 'unknown' }, all))
    expect(head.description).toBeNull()
    expect(head.title).toBe('Does new.example support IPv6?')
  })

  // Google truncates the snippet around 160 characters, and the host is the
  // only unbounded part.
  it('stays inside the snippet budget for a long hostname', () => {
    const host = `${'sub.'.repeat(8)}example.co.uk`
    const head = domainPageHead(
      domain({ host, classification: 'partial' }, { ...all, mx: 'unsupported' }),
    )
    expect(head.description!.length).toBeLessThanOrEqual(160)
  })
})
