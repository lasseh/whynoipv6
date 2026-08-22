// The blog contract shared across the seam: `src/blog.ts` (runtime accessors),
// `src/router.ts` (route meta), and `scripts/posts.ts` (compile + prerender)
// all import from here so the prerendered head and the SPA's runtime meta can
// never drift apart. Pure types and constants only — anything heavier (fs,
// markdown-it) lives in scripts/posts.ts and must not leak into the app bundle.

// The suffix comes from the same module the runtime head writer reads, so a
// prerendered title and a hydrated one cannot drift apart.
import { SITE_NAME, withSiteName } from '../src/site'

export { SITE_NAME, withSiteName }
export const ORIGIN = 'https://whynoipv6.com'

/** Frontmatter-derived post metadata — everything the list/teaser surfaces
 * ship without paying for the rendered HTML. */
export interface PostMeta {
  /** URL segment, from the filename: `src/content/blog/<slug>.md`. */
  slug: string
  title: string
  description: string
  /** Publish date, `YYYY-MM-DD`. List order, RSS pubDate, sitemap lastmod. */
  date: string
  /** Rounded reading time at ~200 wpm, never below 1. */
  minutes: number
  /** Optional share image (site-absolute path) — overrides the og default. */
  image?: string
}

/** A compiled post: metadata plus the markdown rendered to HTML. */
export interface Post {
  meta: PostMeta
  html: string
}

/** Newest first; slug tiebreak keeps same-day posts deterministic. */
export function comparePostMeta(a: PostMeta, b: PostMeta): number {
  return b.date.localeCompare(a.date) || a.slug.localeCompare(b.slug)
}

// Route meta (§9.6) for the two blog routes — also the prerendered head for
// /blog and the RSS channel description.
export const BLOG_LIST_META = {
  title: withSiteName('Blog'),
  description: 'Write-ups from the crawl data: adoption numbers, notable changes, and methodology.',
}

// Pre-load fallback for /blog/:slug — the page swaps in the real title once
// the post chunk loads; crawlers get the real one from the prerendered head.
export const BLOG_POST_META = {
  title: withSiteName('Blog'),
  description:
    'A write-up from the Why No IPv6 crawl and what the top-million data says about adoption.',
}
