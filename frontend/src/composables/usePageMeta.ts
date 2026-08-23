import type { Router } from 'vue-router'
import { SITE_NAME, withSiteName } from '@/site'

// Applies per-route `meta: { title, description }` (§9.6) — replaces the old
// site's imperative onMounted document.title writes.

function headTag(selector: string, create: () => HTMLElement): HTMLElement {
  let tag = document.head.querySelector<HTMLElement>(selector)
  if (!tag) {
    tag = create()
    document.head.appendChild(tag)
  }
  return tag
}

function removeHeadTag(selector: string): void {
  document.head.querySelector(selector)?.remove()
}

function setMetaProperty(property: string, content: string): void {
  headTag(`meta[property="${property}"]`, () => {
    const el = document.createElement('meta')
    el.setAttribute('property', property)
    return el
  }).setAttribute('content', content)
}

function setMetaName(name: string, content: string): void {
  headTag(`meta[name="${name}"]`, () => {
    const el = document.createElement('meta')
    el.setAttribute('name', name)
    return el
  }).setAttribute('content', content)
}

/**
 * A page's identity: the title stem (without the site name) and the
 * description. The runtime writer here and the build-time writer in
 * scripts/posts.ts render the same fields, so a prerendered head and a
 * hydrated one cannot disagree.
 */
export interface PageHead {
  /** Title stem; the site name is appended by the writer, never by callers. */
  title: string
  description: string
}

/** Every tag the title appears in, written together. */
function applyTitle(stem: string): void {
  const full = withSiteName(stem)
  document.title = full
  setMetaProperty('og:title', full)
  setMetaName('twitter:title', full)
}

/** Every tag the description appears in, written together. */
function applyDescription(description: string): void {
  setMetaName('description', description)
  setMetaProperty('og:description', description)
  setMetaName('twitter:description', description)
}

/**
 * The robots tag, which exists only to say noindex — its absence is the
 * indexable default, so the tag is removed rather than set to "index".
 * deploy/nginx sends X-Robots-Tag for the /not-found routes as well, because
 * this one is written by JS and a crawler that does not render never sees it;
 * the two must agree.
 */
function applyRobots(noindex: boolean): void {
  if (noindex) {
    setMetaName('robots', 'noindex')
  } else {
    removeHeadTag('meta[name="robots"]')
  }
}

/**
 * Mark the current page noindex from a component — for the states only the
 * API's answer reveals (an unknown country, a campaign that isn't there, a
 * failed fetch). Routes that are *always* unindexable declare
 * `meta: { noindex: true }` instead; both land on the same tag, and the
 * navigation guard clears it on the way out, so a page that sets it can
 * never leak the tag onto the next one.
 */
export function setPageNoindex(): void {
  applyRobots(true)
}

/**
 * Title *and* the share tags, for content whose real meta is only known
 * after load (blog posts). Blog posts are prerendered with correct per-post
 * tags (scripts/blog-plugin.ts), but the route guard would otherwise
 * overwrite them with the route's generic fallback the moment the app boots
 * — so a JS-executing crawler would read "Blog - Why No IPv6" off every
 * post. Restoring the whole set is what keeps the hydrated head equal to
 * the prerendered one.
 */
export function setPageMeta(title: string, description: string): void {
  applyTitle(title)
  applyDescription(description)
}

/**
 * Data-driven title for entity pages (domain, country, campaign, live
 * check) — call once the entity has loaded; the route's description stays,
 * because it already describes the page accurately and the entity name adds
 * nothing to it.
 *
 * It writes every tag the title appears in, not just document.title. Writing
 * a subset is what let og:title track the page while twitter:title kept
 * index.html's homepage value.
 */
export function setPageTitle(title: string): void {
  applyTitle(title)
}

export function installPageMeta(router: Router): void {
  router.beforeEach((to) => {
    // Route meta carries the already-suffixed title (it is also the static
    // fallback in index.html), so strip the suffix to get the stem
    // applyTitle re-appends. Both halves read SITE_NAME, so they agree.
    if (to.meta.title) {
      const suffix = ` - ${SITE_NAME}`
      applyTitle(
        to.meta.title.endsWith(suffix) ? to.meta.title.slice(0, -suffix.length) : to.meta.title,
      )
    }
    if (to.meta.description) applyDescription(to.meta.description)
    // Unconditional, unlike title/description: this has to clear the tag a
    // previous page set, not just apply the new route's value.
    applyRobots(to.meta.noindex === true)
    // Per-route canonical + og:url — every path claims itself, not the homepage.
    const url = `${location.origin}${to.path}`
    headTag('link[rel="canonical"]', () => {
      const el = document.createElement('link')
      el.setAttribute('rel', 'canonical')
      return el
    }).setAttribute('href', url)
    setMetaProperty('og:url', url)
  })
}
