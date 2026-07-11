import type { Router } from 'vue-router'

// Applies per-route `meta: { title, description }` (§9.6) — replaces the old
// site's imperative onMounted document.title writes.
export function installPageMeta(router: Router): void {
  router.beforeEach((to) => {
    if (to.meta.title) {
      document.title = to.meta.title
    }
    if (to.meta.description) {
      let tag = document.querySelector('meta[name="description"]')
      if (!tag) {
        tag = document.createElement('meta')
        tag.setAttribute('name', 'description')
        document.head.appendChild(tag)
      }
      tag.setAttribute('content', to.meta.description)
    }
  })
}
