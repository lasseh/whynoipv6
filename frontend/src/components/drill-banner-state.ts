import { ref } from 'vue'

// Whether DrillBanner is currently occupying the bottom edge of the viewport.
// Notification.vue's toast is fixed at bottom-4 with no z-index, so during the
// notice week the banner would paint over it — and the visitor who sees both
// is exactly the one this whole feature is aimed at: someone on IPv4, on the
// home page, in the week before the site stops answering them. The toast lifts
// clear instead of being covered.
export const drillBannerVisible = ref(false)
