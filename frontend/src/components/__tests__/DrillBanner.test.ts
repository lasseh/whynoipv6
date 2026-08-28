// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DrillBanner from '@/components/DrillBanner.vue'
import { drillBannerHeight } from '@/components/drill-banner-state'

// This banner is only on screen for eight days a month, so nobody meets it
// during ordinary development and a regression would sit unnoticed until the
// week it matters. Every phase is driven off a frozen clock instead.
//
// Storage is stubbed rather than borrowed: this environment does not provide
// localStorage at all, which is the same shape as the failure the component
// guards against in a locked-down browser, and leaving it undefined would mean
// the happy path never runs.
const RouterLink = { template: '<a><slot /></a>' }

function fakeStorage() {
  const values = new Map<string, string>()
  return {
    getItem: (k: string) => values.get(k) ?? null,
    setItem: (k: string, v: string) => void values.set(k, v),
    clear: () => values.clear(),
  }
}

function useStorage(storage: unknown) {
  vi.stubGlobal('localStorage', storage)
}

// jsdom ships no ResizeObserver, and the banner uses one to publish its height.
// The stub reports a fixed height on observe so the "how tall is the bar"
// contract can be asserted at all; jsdom lays nothing out, so a real one would
// only ever report 0.
const STUB_BAR_HEIGHT = 109
function stubResizeObserver() {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(private cb: () => void) {}
      observe(el: Element) {
        el.getBoundingClientRect = () => ({ height: STUB_BAR_HEIGHT }) as DOMRect
        this.cb()
      }
      disconnect() {}
      unobserve() {}
    },
  )
}

function render() {
  return mount(DrillBanner, { global: { stubs: { RouterLink } } })
}

beforeEach(() => {
  vi.useFakeTimers()
  useStorage(fakeStorage())
  stubResizeObserver()
  drillBannerHeight.value = 0
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('DrillBanner', () => {
  it('says nothing outside the notice period', () => {
    vi.setSystemTime(new Date('2026-09-20T12:00:00Z'))
    expect(render().text()).toBe('')
    expect(drillBannerHeight.value).toBe(0)
  })

  it('announces the window during the notice period, with its date', () => {
    vi.setSystemTime(new Date('2026-10-02T12:00:00Z'))
    const text = render().text()
    expect(text).toContain('Planned IPv4 outage on 6 October')
    expect(text).toContain('Over IPv6 nothing changes')
    expect(drillBannerHeight.value).toBe(STUB_BAR_HEIGHT)
  })

  // During a window an IPv4 visitor gets the 503 and never loads the SPA, so
  // this copy is only ever read by someone already on IPv6.
  it('addresses an IPv6 reader while the window is open', () => {
    vi.setSystemTime(new Date('2026-10-06T09:00:00Z'))
    const text = render().text()
    expect(text).toContain('IPv4 is switched off today')
    expect(text).toContain('you would not have noticed')
  })

  it('stays dismissed for this window but comes back for the next', async () => {
    const storage = fakeStorage()
    useStorage(storage)
    vi.setSystemTime(new Date('2026-10-02T12:00:00Z'))

    const first = render()
    await first.get('button').trigger('click')
    expect(first.text()).toBe('')
    expect(drillBannerHeight.value).toBe(0)
    expect(storage.getItem('wni6-ipv4-drill-dismissed')).toBe('2026-10-06')

    // Same window: still dismissed.
    expect(render().text()).toBe('')

    // Next month's window is a different key, so the notice returns.
    vi.setSystemTime(new Date('2026-11-02T12:00:00Z'))
    expect(render().text()).toContain('Planned IPv4 outage on 6 November')
  })

  it('still renders and dismisses when storage throws', async () => {
    useStorage({
      getItem: () => {
        throw new Error('denied')
      },
      setItem: () => {
        throw new Error('denied')
      },
    })
    vi.setSystemTime(new Date('2026-10-02T12:00:00Z'))

    const banner = render()
    expect(banner.text()).toContain('Planned IPv4 outage')
    await banner.get('button').trigger('click')
    expect(banner.text()).toBe('')
  })
})
