// Vite side of the blog (12-frontend.md §14): compiles src/content/blog/*.md
// into JS modules at build time (no markdown runtime ships to the client),
// then prerenders the static surface after the bundle is written.
//
// Two module shapes per file, so the list ships without the article bodies:
//   x.md?meta  →  export default PostMeta          (eager, main chunk)
//   x.md       →  export default Post              (lazy, per-post chunk)
//
// closeBundle writes into dist/: blog/<slug>.html per post plus blog.html
// and blog/index.html for the list (real head tags for the no-JS crawlers
// social unfurls depend on), blog/rss.xml, and sitemap.xml. Flat .html
// files, not <slug>/index.html dirs: nginx 301-appends a slash to a
// directory URL, and the canonical slashless /blog/<slug> must 200
// directly — try_files' $uri.html serves it without a redirect.
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { basename, join, resolve } from 'node:path'

import type { Plugin, ResolvedConfig } from 'vite'

import { BLOG_LIST_META, ORIGIN } from './blog-shared'
import type { Post } from './blog-shared'
import {
  blogJsonLd,
  postJsonLd,
  renderPost,
  renderPosts,
  rewriteHead,
  rssXml,
  sitemapXml,
} from './posts'

const CONTENT_DIR = 'src/content/blog'

// The §5 route set for the sitemap: same paths and priorities as the retired
// hand-maintained public/sitemap.xml, plus /check (missing there), /blog, and
// /bot (the crawler-identification page the bot's User-Agent URL points at).
const STATIC_ROUTES: [string, string][] = [
  ['/', '0.8'],
  ['/domains', '1.0'],
  ['/search', '1.0'],
  ['/check', '1.0'],
  ['/metrics', '1.0'],
  ['/countries', '1.0'],
  ['/campaigns', '1.0'],
  ['/changelog', '1.0'],
  ['/faq', '1.0'],
  ['/blog', '0.8'],
  ['/bot', '0.5'],
]

function readPosts(root: string): Post[] {
  const dir = resolve(root, CONTENT_DIR)
  const files = readdirSync(dir).filter((f) => f.endsWith('.md'))
  return renderPosts(files.map((f) => [f, readFileSync(join(dir, f), 'utf8')]))
}

export function blogPlugin(): Plugin {
  let config: ResolvedConfig
  return {
    name: 'whynoipv6:blog',
    enforce: 'pre',

    configResolved(resolved) {
      config = resolved
    },

    transform(src, id) {
      const [file, query] = id.split('?')
      if (!file || !file.endsWith('.md') || !file.includes(`/${CONTENT_DIR}/`)) return null
      const post = renderPost(basename(file), src)
      const value = query === 'meta' ? post.meta : post
      return { code: `export default ${JSON.stringify(value)}`, map: null }
    },

    closeBundle() {
      // The dev server also closes its plugin container — only a real build
      // has a dist/ to prerender into.
      if (config.command !== 'build') return
      const outDir = resolve(config.root, config.build.outDir)
      const template = readFileSync(join(outDir, 'index.html'), 'utf8')
      const posts = readPosts(config.root)
      const buildTime = new Date().toISOString()

      mkdirSync(join(outDir, 'blog'), { recursive: true })
      for (const post of posts) {
        writeFileSync(
          join(outDir, 'blog', `${post.meta.slug}.html`),
          rewriteHead(template, {
            path: `/blog/${post.meta.slug}`,
            title: `${post.meta.title} - Why No IPv6`,
            description: post.meta.description,
            ogType: 'article',
            published: post.meta.date,
            jsonLd: postJsonLd(post),
            ...(post.meta.image ? { image: post.meta.image } : {}),
          }),
        )
      }

      // The list twice: blog.html answers /blog, blog/index.html answers
      // /blog/ — both 200, both canonicalized to /blog.
      const list = rewriteHead(template, {
        path: '/blog',
        title: BLOG_LIST_META.title,
        description: BLOG_LIST_META.description,
        ogType: 'website',
        jsonLd: blogJsonLd(posts),
      })
      writeFileSync(join(outDir, 'blog.html'), list)
      writeFileSync(join(outDir, 'blog', 'index.html'), list)
      writeFileSync(join(outDir, 'blog', 'rss.xml'), rssXml(posts))
      writeFileSync(
        join(outDir, 'sitemap.xml'),
        sitemapXml([
          ...STATIC_ROUTES.map(([path, priority]) => ({ path, lastmod: buildTime, priority })),
          ...posts.map((p) => ({
            path: `/blog/${p.meta.slug}`,
            lastmod: `${p.meta.date}T00:00:00+00:00`,
            priority: '0.8',
          })),
        ]),
      )
      config.logger.info(
        `blog: prerendered ${posts.length} post(s) + index, rss.xml, sitemap.xml → ${ORIGIN}/blog`,
      )
    },
  }
}
