// Build-time blog pipeline: markdown file → compiled Post, plus the static
// artifacts derived from the post set (prerendered heads, RSS, sitemap).
// Pure string-in/string-out — no fs, no Vite types — so the vitest suite can
// pin every transform. Callers: scripts/blog-plugin.ts (dev transform + build
// prerender) and scripts/__tests__/posts.test.ts.
import MarkdownIt from 'markdown-it'

import type { Post, PostMeta } from './blog-shared'
import { BLOG_LIST_META, ORIGIN, comparePostMeta } from './blog-shared'

// ---------------------------------------------------------------------------
// Markdown → HTML

const md = new MarkdownIt({ html: true, linkify: true })

// External links open a new tab (the site's outbound-link convention);
// site-absolute paths stay plain anchors — BlogPost's click delegation
// routes them through the SPA.
const defaultLinkOpen =
  md.renderer.rules.link_open ??
  ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options))
md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  const token = tokens[idx]
  const href = String(token?.attrGet('href') ?? '')
  if (/^https?:\/\//.test(href) && !href.startsWith(ORIGIN)) {
    token?.attrSet('target', '_blank')
    token?.attrSet('rel', 'noopener')
  }
  return defaultLinkOpen(tokens, idx, options, env, self)
}

// ---------------------------------------------------------------------------
// Frontmatter

const FRONTMATTER = /^---\n([\s\S]*?)\n---\n/

// `key: value` lines only — three required string fields don't need YAML.
// Values may be bare or single/double-quoted; colons in values are fine
// (split on the first one).
function parseFrontmatter(src: string): { fields: Record<string, string>; body: string } {
  const match = FRONTMATTER.exec(src)
  if (!match || match[1] === undefined) {
    throw new Error('missing frontmatter block (--- ... ---)')
  }
  const fields: Record<string, string> = {}
  for (const line of match[1].split('\n')) {
    if (!line.trim()) continue
    const colon = line.indexOf(':')
    if (colon === -1) throw new Error(`malformed frontmatter line: ${JSON.stringify(line)}`)
    const key = line.slice(0, colon).trim()
    const raw = line.slice(colon + 1).trim()
    fields[key] = raw.replace(/^"(.*)"$/, '$1').replace(/^'(.*)'$/, '$1')
  }
  return { fields, body: src.slice(match[0].length) }
}

// ---------------------------------------------------------------------------
// Post assembly

const SLUG = /^[a-z0-9][a-z0-9-]*$/
const DATE = /^\d{4}-\d{2}-\d{2}$/

/**
 * Compile one markdown source into a Post. `fileName` is the basename
 * (`<slug>.md`) — the filename IS the slug, so URLs are decided in the
 * repo, not in frontmatter. Throws (failing the build) on anything
 * malformed: bad slugs, missing fields, unparseable dates.
 */
export function renderPost(fileName: string, src: string): Post {
  const slug = fileName.replace(/\.md$/, '')
  if (!SLUG.test(slug)) {
    throw new Error(`blog: ${fileName}: filename must be <slug>.md with [a-z0-9-] only`)
  }
  if (slug === 'index') {
    // dist/blog/index.html is the list page — a post named index.md would
    // silently overwrite it.
    throw new Error(`blog: ${fileName}: "index" is reserved for the list page`)
  }
  let fields: Record<string, string>
  let body: string
  try {
    ;({ fields, body } = parseFrontmatter(src))
  } catch (err) {
    throw new Error(`blog: ${fileName}: ${err instanceof Error ? err.message : String(err)}`, {
      cause: err,
    })
  }
  for (const key of ['title', 'description', 'date'] as const) {
    if (!fields[key]) throw new Error(`blog: ${fileName}: frontmatter is missing "${key}"`)
  }
  const date = fields.date ?? ''
  if (!DATE.test(date) || Number.isNaN(Date.parse(`${date}T00:00:00Z`))) {
    throw new Error(`blog: ${fileName}: date must be YYYY-MM-DD, got ${JSON.stringify(date)}`)
  }
  const words = body.split(/\s+/).filter(Boolean).length
  const meta: PostMeta = {
    slug,
    title: fields.title ?? '',
    description: fields.description ?? '',
    date,
    minutes: Math.max(1, Math.round(words / 200)),
    ...(fields.image ? { image: fields.image } : {}),
  }
  return { meta, html: md.render(body) }
}

/** Compile a set of (fileName, src) pairs, newest first. */
export function renderPosts(sources: [string, string][]): Post[] {
  return sources
    .map(([fileName, src]) => renderPost(fileName, src))
    .sort((a, b) => comparePostMeta(a.meta, b.meta))
}

// ---------------------------------------------------------------------------
// Prerendered heads

export interface PageHead {
  /** Site-absolute path — canonical + og:url + twitter:url. */
  path: string
  /** Full document title (already suffixed). */
  title: string
  description: string
  ogType: 'website' | 'article'
  /** Site-absolute share image; upgrades twitter:card to summary_large_image. */
  image?: string
  /** ISO date → article:published_time (articles only). */
  published?: string
  jsonLd?: object
}

export function escapeHtml(s: string): string {
  return s.replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] ?? c,
  )
}

function replaceTag(html: string, what: string, pattern: RegExp, replacement: string): string {
  if (!pattern.test(html)) {
    throw new Error(`blog prerender: ${what} not found in index.html — update scripts/posts.ts`)
  }
  // Function replacement: content may contain `$`, which string replacements
  // would expand as a substitution pattern.
  return html.replace(pattern, () => replacement)
}

const metaTag = (attr: 'name' | 'property', key: string) =>
  new RegExp(`<meta\\s[^>]*${attr}="${key}"[^>]*/>`)

/**
 * Rewrite the built index.html's head for one page: swap the static
 * title/description/OG/Twitter block (§9.6) for page-specific values and
 * append canonical + JSON-LD. Throws when an expected tag is missing, so an
 * index.html edit that breaks the contract fails the build instead of
 * shipping stale unfurls.
 */
export function rewriteHead(template: string, page: PageHead): string {
  const url = `${ORIGIN}${page.path}`
  const title = escapeHtml(page.title)
  const desc = escapeHtml(page.description)
  let html = template
  html = replaceTag(html, '<title>', /<title>[\s\S]*?<\/title>/, `<title>${title}</title>`)
  html = replaceTag(
    html,
    'meta description',
    metaTag('name', 'description'),
    `<meta name="description" content="${desc}" />`,
  )
  const pairs: [string, 'name' | 'property', string][] = [
    ['og:title', 'property', `<meta property="og:title" content="${title}" />`],
    ['og:description', 'property', `<meta property="og:description" content="${desc}" />`],
    ['og:type', 'property', `<meta property="og:type" content="${page.ogType}" />`],
    ['og:url', 'property', `<meta property="og:url" content="${url}" />`],
    ['twitter:title', 'name', `<meta name="twitter:title" content="${title}" />`],
    ['twitter:description', 'name', `<meta name="twitter:description" content="${desc}" />`],
    ['twitter:url', 'name', `<meta name="twitter:url" content="${url}" />`],
  ]
  if (page.image) {
    const image = `${ORIGIN}${page.image}`
    pairs.push(
      ['og:image', 'property', `<meta property="og:image" content="${image}" />`],
      ['twitter:image', 'name', `<meta name="twitter:image" content="${image}" />`],
      ['twitter:card', 'name', `<meta name="twitter:card" content="summary_large_image" />`],
    )
  }
  for (const [key, attr, replacement] of pairs) {
    html = replaceTag(html, key, metaTag(attr, key), replacement)
  }
  const extra = [
    `<link rel="canonical" href="${url}" />`,
    ...(page.ogType === 'article' && page.published
      ? [`<meta property="article:published_time" content="${page.published}" />`]
      : []),
    ...(page.jsonLd
      ? [
          `<script type="application/ld+json">${JSON.stringify(page.jsonLd).replace(/</g, '\\u003c')}</script>`,
        ]
      : []),
  ]
  return replaceTag(html, '</head>', /<\/head>/, `${extra.join('\n    ')}\n  </head>`)
}

const BLOG_ID = `${ORIGIN}/blog#blog`

export function postJsonLd(post: Post): object {
  const url = `${ORIGIN}/blog/${post.meta.slug}`
  return {
    '@context': 'https://schema.org',
    '@type': 'BlogPosting',
    '@id': `${url}#post`,
    headline: post.meta.title,
    description: post.meta.description,
    datePublished: post.meta.date,
    url,
    mainEntityOfPage: url,
    image: `${ORIGIN}${post.meta.image ?? '/images/WhyNoSticker.webp'}`,
    author: { '@type': 'Person', name: 'Lasse Haugen', url: ORIGIN },
    publisher: { '@id': `${ORIGIN}/#org` },
    isPartOf: { '@type': 'Blog', '@id': BLOG_ID },
  }
}

export function blogJsonLd(posts: Post[]): object {
  return {
    '@context': 'https://schema.org',
    '@type': 'Blog',
    '@id': BLOG_ID,
    name: 'Why No IPv6 Blog',
    url: `${ORIGIN}/blog`,
    description: BLOG_LIST_META.description,
    publisher: { '@id': `${ORIGIN}/#org` },
    blogPost: posts.map((p) => ({ '@id': `${ORIGIN}/blog/${p.meta.slug}#post` })),
  }
}

// ---------------------------------------------------------------------------
// Feeds

const rfc822 = (date: string) => new Date(`${date}T00:00:00Z`).toUTCString()

/** Full-content RSS 2.0 feed at /blog/rss.xml, newest first. */
export function rssXml(posts: Post[]): string {
  const newest = posts[0]
  const items = posts
    .map((p) => {
      const url = `${ORIGIN}/blog/${p.meta.slug}`
      // CDATA-safe: a literal ]]> inside content would end the section early.
      const content = p.html.split(']]>').join(']]]]><![CDATA[>')
      return `    <item>
      <title>${escapeHtml(p.meta.title)}</title>
      <link>${url}</link>
      <guid isPermaLink="true">${url}</guid>
      <pubDate>${rfc822(p.meta.date)}</pubDate>
      <description>${escapeHtml(p.meta.description)}</description>
      <content:encoded><![CDATA[${content}]]></content:encoded>
    </item>`
    })
    .join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>Why No IPv6 Blog</title>
    <link>${ORIGIN}/blog</link>
    <description>${escapeHtml(BLOG_LIST_META.description)}</description>
    <language>en</language>
    ${newest ? `<lastBuildDate>${rfc822(newest.meta.date)}</lastBuildDate>` : ''}
    <atom:link href="${ORIGIN}/blog/rss.xml" rel="self" type="application/rss+xml" />
${items}
  </channel>
</rss>
`
}

// ---------------------------------------------------------------------------
// Sitemap

export interface SitemapEntry {
  path: string
  /** Full ISO timestamp. */
  lastmod: string
  priority: string
}

/** Replaces the hand-frozen public/sitemap.xml — regenerated every build. */
export function sitemapXml(entries: SitemapEntry[]): string {
  const urls = entries
    .map(
      (e) => `   <url>
      <loc>${ORIGIN}${escapeHtml(e.path)}</loc>
      <lastmod>${e.lastmod}</lastmod>
      <priority>${e.priority}</priority>
   </url>`,
    )
    .join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls}
</urlset>
`
}
