// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BlogPost from '@/pages/BlogPost.vue'
import { posts } from '@/blog'
import { layoutStubs, makeRouter } from './test-utils'

describe('BlogPost', () => {
  it('renders a compiled post and sets the data-driven title', async () => {
    const first = posts[0]
    expect(first).toBeDefined()
    const router = await makeRouter('/blog/:slug([a-z0-9-]+)', BlogPost, `/blog/${first?.slug}`)
    const wrapper = mount(BlogPost, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    // The article body arrives via a real dynamic chunk import — poll past it.
    await vi.waitFor(() => expect(wrapper.text()).toContain(first?.title))
    expect(wrapper.text()).toContain('min read')
    // The compiled markdown body made it through v-html.
    expect(wrapper.find('.prose').exists()).toBe(true)
    expect(document.title).toBe(`${first?.title} - Why No IPv6`)
    wrapper.unmount()
  })

  it('shows the inline not-found for unknown slugs', async () => {
    const router = await makeRouter('/blog/:slug([a-z0-9-]+)', BlogPost, '/blog/no-such-post')
    const wrapper = mount(BlogPost, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('No post here')
    expect(wrapper.find('.prose').exists()).toBe(false)
    wrapper.unmount()
  })
})
