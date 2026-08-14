<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useVisitorIp } from '@/composables/useVisitorIp'

// Visitor IPv6 banner (§9.5): GET /ip, warn iff family !== "ipv6" — no string
// sniffing. Auto-hides after 15 s or shortly after the user scrolls; a failed
// lookup shows nothing.
const { warn } = useVisitorIp()

const showNotification = ref(false)
let autoHideTimeout: ReturnType<typeof setTimeout> | undefined

watch(warn, (isWarn) => {
  if (!isWarn) return
  // A pre-warn scroll may have armed the 1 s hide timer; clear it so the
  // banner gets its full 15 s.
  clearTimeout(autoHideTimeout)
  showNotification.value = true
  autoHideTimeout = setTimeout(() => {
    showNotification.value = false
  }, 15000)
})

const hideNotification = () => {
  clearTimeout(autoHideTimeout)
  autoHideTimeout = setTimeout(() => {
    showNotification.value = false
  }, 1000)
}

onMounted(() => {
  window.addEventListener('scroll', hideNotification, { passive: true })
})

onUnmounted(() => {
  clearTimeout(autoHideTimeout)
  window.removeEventListener('scroll', hideNotification)
})
</script>

<template>
  <transition name="fade">
    <div v-if="showNotification" role="alert">
      <div
        class="flex-col w-full max-w-lg px-4 py-2 fixed bottom-4 right-4 flex gap-4 rounded-sm text-sm bg-zinc-800 shadow-lg border border-zinc-700 text-slate-300"
      >
        <div class="flex w-full justify-between items-start">
          <div class="flex">
            <svg
              class="w-4 h-4 shrink-0 fill-current text-amber-500 mt-[3px] mr-3"
              viewBox="0 0 16 16"
            >
              <path
                d="M8 0C3.6 0 0 3.6 0 8s3.6 8 8 8 8-3.6 8-8-3.6-8-8-8zm0 12c-.6 0-1-.4-1-1s.4-1 1-1 1 .4 1 1-.4 1-1 1zm1-3H7V4h2v5z"
              />
            </svg>
            <div>
              <div class="font-medium text-slate-200 mb-1">No IPv6?!</div>
              <div>You're reading an IPv6 shame site over IPv4. Ask your ISP why.</div>
            </div>
          </div>
          <button class="opacity-70 hover:opacity-80 ml-3 mt-[3px]" @click="hideNotification">
            <div class="sr-only">Close</div>
            <svg class="w-4 h-4 fill-current">
              <path
                d="M7.95 6.536l4.242-4.243a1 1 0 111.415 1.414L9.364 7.95l4.243 4.242a1 1 0 11-1.415 1.415L7.95 9.364l-4.243 4.243a1 1 0 01-1.414-1.415L6.536 7.95 2.293 3.707a1 1 0 011.414-1.414L7.95 6.536z"
              />
            </svg>
          </button>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 1s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
