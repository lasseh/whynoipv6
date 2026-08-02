// Typed access to the compiled blog content (scripts/blog-plugin.ts). The
// `?meta` glob is eager but carries frontmatter only, so list/teaser
// surfaces cost a few strings in the main chunk; the full article HTML
// stays in a lazy per-post chunk that only BlogPost loads.
import type { Post, PostMeta } from '../scripts/blog-shared'
import { comparePostMeta } from '../scripts/blog-shared'

export type { Post, PostMeta }

const metaModules = import.meta.glob<PostMeta>('./content/blog/*.md', {
  eager: true,
  query: '?meta',
  import: 'default',
})

/** Every post's metadata, newest first. */
export const posts: PostMeta[] = Object.values(metaModules).sort(comparePostMeta)

const contentModules = import.meta.glob<{ default: Post }>('./content/blog/*.md')

// Glob keys are paths; the filename is the slug (scripts/posts.ts pins this).
const loaders = new Map(
  Object.entries(contentModules).map(([path, load]) => [
    path.replace(/^.*\//, '').replace(/\.md$/, ''),
    load,
  ]),
)

/** Load the full post for a slug, or null when no such post is compiled in. */
export async function loadPost(slug: string): Promise<Post | null> {
  const load = loaders.get(slug)
  if (!load) return null
  return (await load()).default
}
