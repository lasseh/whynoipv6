import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// public/ipv4-unavailable.html is the body of the 503 served during a planned
// IPv4 outage. It is read by a visitor for whom *every other request to this
// origin is also answered with 503* — no stylesheet, no font, no image, no
// script will load. It is also subject to the production CSP, which is
// script-src 'self' with no nonce, so an inline script would be blocked even
// outside a window.
//
// Those two constraints are invisible when the file is opened in a browser
// normally, and only bite on the one day a month the page exists for. So they
// are pinned here instead: a well-meaning `<img src="/images/...">` would
// otherwise ship a broken page and nobody would find out until the 6th.
const html = readFileSync(resolve(__dirname, '../../public/ipv4-unavailable.html'), 'utf8')

describe('the IPv4 outage helper page', () => {
  it('loads no subresources at all', () => {
    // src= on any element, CSS url() references and @import are the ways a
    // subresource sneaks in.
    expect(html).not.toMatch(/\ssrc\s*=/i)
    expect(html).not.toMatch(/url\(/i)
    expect(html).not.toMatch(/@import/i)
    expect(html).not.toMatch(/<link\b/i)
  })

  it('carries no script, which the CSP would block anyway', () => {
    expect(html).not.toMatch(/<script\b/i)
    expect(html).not.toMatch(/\son[a-z]+\s*=/i)
  })

  it('links only off-site, because this origin is unreachable for its reader', () => {
    const hrefs = [...html.matchAll(/href="([^"]+)"/g)].map((m) => m[1])
    expect(hrefs.length).toBeGreaterThan(0)
    for (const href of hrefs) {
      expect(href, `${href} is not reachable during a window`).toMatch(/^https:\/\//)
      expect(href).not.toMatch(/^https:\/\/([a-z0-9-]+\.)*whynoipv6\.com/)
    }
  })

  it('says what it is and stays out of the index', () => {
    // Prettier formats this file, so the tag may or may not self-close.
    expect(html).toMatch(/<meta name="robots" content="noindex"\s*\/?>/)
    expect(html).toMatch(/IPv4/)
  })
})
