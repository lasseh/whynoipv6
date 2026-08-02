<script setup lang="ts">
import { onMounted, ref } from 'vue'
import CrossIcon from '@/components/icons/Cross.vue'
import { listShame } from '@/api'
import type { ShameItem } from '@/api'

const splitDomainShamers = ref<ShameItem[][]>([])
const randomTestimonial = ref<{
  statement: string
  name: string
  url: string
  urlTitle: string
  imageUrl: string
}>()

const testimonials = [
  {
    statement: "IPv6 is no longer an option, it's mandatory",
    name: 'Scott Hogg',
    url: 'https://hoggnet.com/',
    urlTitle: 'Hogg Networking',
    imageUrl: '/images/scott.webp',
  },
  {
    statement:
      "It's a shame some people still can't deploy a protocol that could buy its own beer, even in the US.",
    name: 'Ivan Pepelnjak',
    url: 'https://www.ipspace.net/',
    urlTitle: 'ipspace.net',
    imageUrl: '/images/ivan.webp',
  },
  // Add more testimonials here
]

function getRandomTestimonial() {
  const randomIndex = Math.floor(Math.random() * testimonials.length)
  randomTestimonial.value = testimonials[randomIndex]
}

async function getDomainShamers() {
  try {
    const response = await listShame()
    const items = response.items
    const midpoint = Math.ceil(items.length / 2)
    splitDomainShamers.value = [items.slice(0, midpoint), items.slice(midpoint)]
  } catch {
    splitDomainShamers.value = []
  }
}

onMounted(() => {
  void getDomainShamers()
  getRandomTestimonial()
})
</script>

<template>
  <section>
    <div class="max-w-6xl mx-auto px-4 sm:px-6">
      <div class="py-4 md:py-4">
        <!-- Items -->
        <div class="grid gap-20">
          <!-- Item -->
          <div class="md:grid md:grid-cols-12 md:gap-6 items-center">
            <!-- Image -->
            <div
              class="max-w-xl md:max-w-none md:w-full mx-auto md:col-span-5 lg:col-span-6 mb-8 md:mb-0 md:order-1"
            >
              <div class="relative">
                <img
                  class="hidden md:block md:max-w-none"
                  src="/images/WhyNoLogo.webp"
                  width="540"
                  height="520"
                  loading="lazy"
                  alt="Why No IPv6 logo"
                />
              </div>
            </div>
            <!-- Content -->
            <div class="max-w-xl md:max-w-none md:w-full mx-auto md:col-span-7 lg:col-span-6">
              <div class="md:pr-4 lg:pr-12 xl:pr-16">
                <h3 class="h3 mb-3">Top IPv6 Sinners</h3>
                <p class="text-base text-gray-400 mb-0">
                  The most visited websites in the world, without a single AAAA record among them.
                  IPv6 shipped in 1998; these domains are still thinking it over.
                </p>
                <p class="text-base text-gray-400 mb-4">Shame on them!</p>

                <div class="grid grid-cols-2 gap-4">
                  <ul
                    v-for="(list, listIndex) in splitDomainShamers"
                    :key="listIndex"
                    class="max-w-md space-y-1 list-inside text-gray-400"
                  >
                    <li v-for="item in list" :key="item.host" class="flex items-center">
                      <CrossIcon class="text-pink-600 mr-2" />
                      <RouterLink
                        :to="{ name: 'DomainDetail', params: { domain: item.host } }"
                        :title="item.reason ?? undefined"
                        >{{ item.host }}</RouterLink
                      >
                    </li>
                  </ul>
                </div>

                <!-- Testimonial -->
                <div v-if="randomTestimonial" class="flex items-start mt-8">
                  <img
                    :src="randomTestimonial.imageUrl"
                    alt=""
                    class="rounded-full shrink-0 mr-4"
                    width="40"
                    height="40"
                  />
                  <div>
                    <blockquote class="text-gray-400 italic m-0 mb-3">
                      "{{ randomTestimonial.statement }}"
                    </blockquote>
                    <div class="text-gray-700 font-medium">
                      <cite class="text-gray-200 not-italic">{{ randomTestimonial.name }}</cite>
                      -
                      <a
                        :href="randomTestimonial.url"
                        target="_blank"
                        class="text-fuchsia-600 hover:text-fuchsia-800 transition duration-150 ease-in-out"
                        >{{ randomTestimonial.urlTitle }}</a
                      >
                    </div>
                  </div>
                </div>
                <!-- End Testimonial -->
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
