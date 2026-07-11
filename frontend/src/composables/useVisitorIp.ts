import { ref } from 'vue'
import { getVisitorIP } from '@/api'

// GET /ip for the visitor banner (§9.5): warn iff family !== "ipv6" — no
// string sniffing. Fails silent: network error → no banner.
export function useVisitorIp() {
  const ip = ref<string | null>(null)
  const warn = ref(false)

  getVisitorIP()
    .then((res) => {
      ip.value = res.ip
      warn.value = res.family !== 'ipv6'
    })
    .catch(() => {
      warn.value = false
    })

  return { ip, warn }
}
