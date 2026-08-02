// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BlogList from '@/pages/BlogList.vue'
import { posts } from '@/blog'
import { layoutStubs, makeRouter } from './test-utils'

describe('BlogList', () => {
  it('mounts and lists every compiled post', async () => {
    const router = await makeRouter('/blog', BlogList)
    const wrapper = mount(BlogList, {
      global: { plugins: [router], stubs: layoutStubs },
    })
    await flushPromises()
    expect(posts.length).toBeGreaterThan(0)
    for (const post of posts) {
      expect(wrapper.text()).toContain(post.title)
    }
    expect(wrapper.text()).toContain('min read')
    expect(wrapper.find('a[href="/blog/rss.xml"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
