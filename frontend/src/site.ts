// The site's identity, spelled once. Imported by the runtime head writer
// (src/composables/usePageMeta.ts) and by the build-time one
// (scripts/blog-shared.ts), so the two cannot disagree about what a page
// title looks like.
export const SITE_NAME = 'Why No IPv6'

/** A page title as it is actually rendered: stem plus the site name. */
export function withSiteName(stem: string): string {
  return `${stem} - ${SITE_NAME}`
}
