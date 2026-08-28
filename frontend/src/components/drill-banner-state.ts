import { ref } from 'vue'

// How much of the bottom edge DrillBanner is occupying, in px, or 0 when it is
// not showing. Notification.vue's toast is fixed to the bottom with no z-index,
// so during the notice week the banner would paint over it — and the visitor
// who sees both is exactly the one this whole feature is aimed at: someone on
// IPv4, on the home page, in the week before the site stops answering them.
// The toast lifts clear by this much instead of being covered.
//
// A measured height and not a boolean: the bar is one line on a desktop and
// three on a phone, so any offset picked from the markup is wrong at one of
// those widths. It was 80px, and on a 375px viewport the banner is 109px tall
// and swallowed the toast's last line.
export const drillBannerHeight = ref(0)
