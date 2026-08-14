<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const mobileNav = ref<HTMLElement | null>(null)
const mobileNavOpen = ref(false)

// One list drives both the desktop and mobile menus.
const NAV_LINKS = [
  { to: '/domains', label: 'Domains' },
  { to: '/campaigns', label: 'Campaigns' },
  { to: '/countries', label: 'Countries' },
  { to: '/check', label: 'Live Check' },
  { to: '/metrics', label: 'Metrics' },
  { to: '/changelog', label: 'Changelog' },
  { to: '/faq', label: 'FAQ' },
]

// Computed style based on mobileNavOpen and mobileNav refs
const mobileNavStyle = computed(() => ({
  maxHeight: mobileNavOpen.value ? `${mobileNav.value?.scrollHeight}px` : '0',
  opacity: mobileNavOpen.value ? 1 : 0.8,
}))

// Close the mobile nav on click outside
const clickOutside = (e: Event) => {
  if (
    !mobileNavOpen.value ||
    mobileNav.value?.contains(e.target as Node) ||
    document.querySelector('.hamburger')?.contains(e.target as Node)
  )
    return
  mobileNavOpen.value = false
}
// Close the mobile nav on escape key press
const keyPress = (event: KeyboardEvent) => {
  if (!mobileNavOpen.value || event.key !== 'Escape') return
  mobileNavOpen.value = false
}

// Underline the active route
const isActiveRoute = (basePath: string): boolean => {
  return route.path.startsWith(basePath)
}

// Lifecycle hooks
onMounted(() => {
  document.addEventListener('click', clickOutside)
  document.addEventListener('keydown', keyPress)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', clickOutside)
  document.removeEventListener('keydown', keyPress)
})
</script>

<template>
  <header class="absolute w-full z-30">
    <div class="max-w-6xl mx-auto px-4 sm:px-6">
      <div class="flex items-center justify-between h-20">
        <!-- Site branding -->
        <div class="shrink-0 mr-4">
          <!-- Logo -->
          <router-link to="/" class="block" aria-label="Why No IPv6 home">
            <div
              class="bg-gradient-to-r bg-clip-text text-2xl font-bold text-transparent from-fuchsia-500 to-fuchsia-700"
            >
              Why No IPv6
            </div>
          </router-link>
        </div>

        <!-- Desktop navigation -->
        <nav class="hidden md:flex md:grow">
          <!-- Desktop menu links -->
          <ul class="flex grow justify-center flex-wrap items-center">
            <li v-for="link in NAV_LINKS" :key="link.to">
              <router-link
                :to="link.to"
                :class="[
                  isActiveRoute(link.to) ? 'underline' : '',
                  'text-gray-300 hover:text-gray-200 px-4 py-2 flex items-center transition duration-150 ease-in-out font-bold',
                ]"
                >{{ link.label }}</router-link
              >
            </li>
          </ul>

          <!-- Hidden button to keep menu placement -->
          <ul class="flex grow justify-end flex-wrap items-center">
            <li class="invisible">
              <!-- Placeholder with same styling as the button -->
              <div class="btn-sm text-white bg-fuchsia-700 hover:bg-fuchsia-800 ml-3 opacity-0">
                Search Domain
              </div>
            </li>
          </ul>
        </nav>
        <!-- End Desktop nav -->

        <!-- Mobile menu -->
        <div class="md:hidden">
          <!-- Hamburger button -->
          <button
            class="hamburger"
            :class="{ active: mobileNavOpen }"
            aria-controls="mobile-nav"
            :aria-expanded="mobileNavOpen"
            @click="mobileNavOpen = !mobileNavOpen"
          >
            <span class="sr-only">Menu</span>
            <svg
              class="w-6 h-6 fill-current text-gray-300 hover:text-gray-200 transition duration-150 ease-in-out"
              viewBox="0 0 24 24"
              xmlns="http://www.w3.org/2000/svg"
            >
              <rect y="4" width="24" height="2" rx="1" />
              <rect y="11" width="24" height="2" rx="1" />
              <rect y="18" width="24" height="2" rx="1" />
            </svg>
          </button>

          <!-- Mobile navigation -->
          <!-- inert removes the clipped menu from the tab order and the
               accessibility tree; max-height only hides it visually. -->
          <nav
            id="mobile-nav"
            ref="mobileNav"
            :inert="!mobileNavOpen"
            class="absolute top-full z-20 left-0 w-full px-4 sm:px-6 overflow-hidden transition-all duration-300 ease-in-out"
            :style="mobileNavStyle"
          >
            <ul class="bg-gray-800 px-4 py-2 border border-gray-700">
              <li v-for="link in NAV_LINKS" :key="link.to">
                <router-link
                  :to="link.to"
                  class="flex font-medium text-gray-200 hover:text-gray-200 py-2"
                  >{{ link.label }}</router-link
                >
              </li>
            </ul>
          </nav>
        </div>
      </div>
    </div>
  </header>
</template>
