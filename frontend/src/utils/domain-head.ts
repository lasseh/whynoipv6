// The title + meta description for the two domain report surfaces
// (DomainDetail, CampaignDomain), built from the loaded entity.
//
// One function rather than a string in each page: the two surfaces already
// carried the same hand-written title and the comments on both said "mirrored
// by" the other, which is the drift hazard spelled out. Now they cannot
// disagree.
//
// Why the description is data-driven at all: every /domains/:domain page used
// to ship the route's one generic description, so ~15k crawled pages differed
// only in a <title> and a table body. Google filed them as "Crawled -
// currently not indexed" (GSC coverage, 2026-08-23). The dimension read-out
// below is what makes each page's summary its own.
import type { DomainDetail, Dimension, StatusValue } from '@/api'

// Only the four AAAA-record dimensions read as prose. conn and resources are
// deliberately absent: they fold into `ipv6_only` (ADR 0002) and spelling them
// out here would restate the fold in a second vocabulary.
//
// A subdomain has no www dimension — the engine never looks up
// www.<subdomain>, so naming it would invent a hostname (matching the row
// suppression in DomainStatusCard).
const PROSE_DIMENSIONS: [Dimension, string][] = [
  ['base', 'the apex'],
  ['www', 'www'],
  ['ns', 'nameservers'],
  ['mx', 'mail'],
]

/** "a, b, and c" — Oxford comma, matching the site's copy. */
function list(parts: string[], conjunction: 'and' | 'or'): string {
  if (parts.length <= 1) return parts[0] ?? ''
  if (parts.length === 2) return `${parts[0]} ${conjunction} ${parts[1]}`
  return `${parts.slice(0, -1).join(', ')}, ${conjunction} ${parts.at(-1)}`
}

// `unsupported` and `no_record` both mean "no IPv6 here" and read the same in
// a sentence; not_applicable and unconfirmed (null) are neither earned nor
// missing, so they are left out rather than counted against the domain.
const verdictOf = (value: StatusValue): 'yes' | 'no' | null =>
  value === 'supported' ? 'yes' : value === 'unsupported' || value === 'no_record' ? 'no' : null

/**
 * The verdict clause. `anySupported` is what keeps it honest: the
 * classification ladder is strict (hero needs base + www + ns + conn), so a
 * domain with IPv6 everywhere except the apex classifies as a sinner — the
 * "almost-heroes" cohort, tens of thousands of the top million. Reading the
 * phrase off the classification alone produced "openai.com has no IPv6. IPv6
 * on www, nameservers, and mail, but not the apex", which contradicts itself
 * in the same breath. A sinner that passes something is not IPv6 *ready*,
 * which is true of every sinner and safe next to any list of what works.
 *
 * Returns null when nothing is confirmed: no verdict to state, so the caller
 * keeps the route's generic description rather than inventing one.
 */
function phraseFor(domain: DomainDetail, anySupported: boolean): string | null {
  if (domain.saint) return 'is an IPv6 saint'
  switch (domain.classification) {
    case 'hero':
      return 'supports IPv6'
    case 'partial':
      return 'has partial IPv6'
    case 'sinner':
      return anySupported ? 'is not IPv6 ready' : 'has no IPv6'
    case 'inactive':
      return 'is not responding'
    case 'unknown':
      return null
  }
}

/**
 * The head for one domain report. The title is always the long-tail question;
 * `description` is null when the domain has nothing confirmed to summarize,
 * and the caller keeps the route's generic one rather than inventing a verdict.
 */
export function domainPageHead(domain: DomainDetail): {
  title: string
  description: string | null
} {
  const title = `Does ${domain.host} support IPv6?`

  const yes: string[] = []
  const no: string[] = []
  for (const [dim, label] of PROSE_DIMENSIONS) {
    if (dim === 'www' && domain.kind === 'subdomain') continue
    const verdict = verdictOf(domain.status[dim].value)
    if (verdict === 'yes') yes.push(label)
    else if (verdict === 'no') no.push(label)
  }

  const phrase = phraseFor(domain, yes.length > 0)
  if (phrase === null) return { title, description: null }

  const detail =
    yes.length && no.length
      ? ` IPv6 on ${list(yes, 'and')}, but not ${list(no, 'or')}.`
      : yes.length
        ? ` IPv6 on ${list(yes, 'and')}.`
        : no.length
          ? ` Missing on ${list(no, 'and')}.`
          : ''

  return { title, description: `${domain.host} ${phrase}.${detail}` }
}
