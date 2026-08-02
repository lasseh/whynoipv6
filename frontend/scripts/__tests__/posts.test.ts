import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

import { BLOG_LIST_META } from '../blog-shared'
import { postJsonLd, renderPost, renderPosts, rewriteHead, rssXml, sitemapXml } from '../posts'

const SRC = `---
title: Test post
description: A description
date: 2026-08-02
---

Hello **world**.

[internal](/domains) and [external](https://example.com/) and [self](https://whynoipv6.com/faq).
`

describe('renderPost', () => {
  it('compiles frontmatter + markdown', () => {
    const post = renderPost('test-post.md', SRC)
    expect(post.meta).toMatchObject({
      slug: 'test-post',
      title: 'Test post',
      description: 'A description',
      date: '2026-08-02',
      minutes: 1,
    })
    expect(post.meta.image).toBeUndefined()
    expect(post.html).toContain('<strong>world</strong>')
  })

  it('keeps colons in values and strips surrounding quotes', () => {
    const post = renderPost('a.md', SRC.replace('title: Test post', 'title: "IPv6: a study"'))
    expect(post.meta.title).toBe('IPv6: a study')
  })

  it('opens external links in a new tab, leaves site links alone', () => {
    const { html } = renderPost('test-post.md', SRC)
    expect(html).toContain('<a href="/domains">internal</a>')
    expect(html).toContain('<a href="https://example.com/" target="_blank" rel="noopener">')
    // Absolute links to our own origin are not "external".
    expect(html).toContain('<a href="https://whynoipv6.com/faq">self</a>')
  })

  it.each([
    ['Bad File.md', /filename/],
    ['under_score.md', /filename/],
  ])('rejects filename %s', (name, want) => {
    expect(() => renderPost(name, SRC)).toThrow(want)
  })

  it('rejects missing required fields and bad dates', () => {
    expect(() => renderPost('a.md', SRC.replace('description: A description\n', ''))).toThrow(
      /missing "description"/,
    )
    expect(() => renderPost('a.md', SRC.replace('2026-08-02', '02.08.2026'))).toThrow(/YYYY-MM-DD/)
    expect(() => renderPost('a.md', 'no frontmatter')).toThrow(/frontmatter/)
  })
})

describe('renderPosts', () => {
  it('sorts newest first, slug tiebreak', () => {
    const posts = renderPosts([
      ['b.md', SRC],
      ['c.md', SRC.replace('2026-08-02', '2026-09-01')],
      ['a.md', SRC],
    ])
    expect(posts.map((p) => p.meta.slug)).toEqual(['c', 'a', 'b'])
  })
})

// The contract with the real shell: every tag rewriteHead expects must exist
// in index.html, or the build throws instead of shipping stale unfurls.
describe('rewriteHead', () => {
  const template = readFileSync(new URL('../../index.html', import.meta.url), 'utf8')
  const post = renderPost('test-post.md', SRC)

  it('rewrites the real index.html head for a post', () => {
    const html = rewriteHead(template, {
      path: '/blog/test-post',
      title: 'Test post - Why No IPv6',
      description: 'A & description',
      ogType: 'article',
      published: '2026-08-02',
      jsonLd: postJsonLd(post),
    })
    expect(html).toContain('<title>Test post - Why No IPv6</title>')
    expect(html).toContain('<meta name="description" content="A &amp; description" />')
    expect(html).toContain(
      '<meta property="og:url" content="https://whynoipv6.com/blog/test-post" />',
    )
    expect(html).toContain('<meta property="og:type" content="article" />')
    expect(html).toContain('<meta name="twitter:title" content="Test post - Why No IPv6" />')
    expect(html).toContain('<link rel="canonical" href="https://whynoipv6.com/blog/test-post" />')
    expect(html).toContain('<meta property="article:published_time" content="2026-08-02" />')
    expect(html).toContain('"@type":"BlogPosting"')
    // The static homepage values are gone, not duplicated.
    expect(html).not.toContain('content="Why No IPv6 scans the top million')
    expect(html.match(/property="og:title"/g)).toHaveLength(1)
    // No-image post keeps the site-wide sticker + summary card.
    expect(html).toContain('content="https://whynoipv6.com/images/WhyNoSticker.webp"')
    expect(html).toContain('<meta name="twitter:card" content="summary" />')
  })

  it('upgrades the card when the post has an image', () => {
    const html = rewriteHead(template, {
      path: '/blog/test-post',
      title: 'T',
      description: 'D',
      ogType: 'article',
      image: '/images/blog/chart.webp',
    })
    expect(html).toContain(
      '<meta property="og:image" content="https://whynoipv6.com/images/blog/chart.webp" />',
    )
    expect(html).toContain('<meta name="twitter:card" content="summary_large_image" />')
  })

  it('throws when the template lost an expected tag', () => {
    const broken = template.replace('property="og:title"', 'property="og:renamed"')
    expect(() =>
      rewriteHead(broken, { path: '/blog', title: 'T', description: 'D', ogType: 'website' }),
    ).toThrow(/og:title/)
  })
})

describe('feeds', () => {
  const posts = renderPosts([['qa-post.md', SRC.replace('title: Test post', 'title: Q&A time')]])

  it('rssXml escapes text and carries full CDATA content', () => {
    const rss = rssXml(posts)
    expect(rss).toContain('<title>Q&amp;A time</title>')
    expect(rss).toContain('<link>https://whynoipv6.com/blog/qa-post</link>')
    expect(rss).toContain('<guid isPermaLink="true">https://whynoipv6.com/blog/qa-post</guid>')
    expect(rss).toContain('<pubDate>Sun, 02 Aug 2026 00:00:00 GMT</pubDate>')
    expect(rss).toContain('<content:encoded><![CDATA[')
    expect(rss).toContain('<strong>world</strong>')
    expect(rss).toContain(BLOG_LIST_META.description.slice(0, 30))
    expect(rss).toContain('rel="self"')
  })

  it('sitemapXml lists entries with lastmod and priority', () => {
    const xml = sitemapXml([
      { path: '/blog/qa-post', lastmod: '2026-08-02T00:00:00+00:00', priority: '0.8' },
    ])
    expect(xml).toContain('<loc>https://whynoipv6.com/blog/qa-post</loc>')
    expect(xml).toContain('<lastmod>2026-08-02T00:00:00+00:00</lastmod>')
    expect(xml).toContain('<priority>0.8</priority>')
  })
})
