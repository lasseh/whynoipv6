import { computed } from 'vue'
import { getVisitorIP } from '@/api'
import { useResource } from '@/composables/useResource'

// GET /ip for the visitor banner (§9.5): warn iff family !== "ipv6" — no
// string sniffing. Fails silent: network error → no banner. Aborted on
// unmount so the fetch never outlives its scope.
//
// The fallback is what "fails silent" means here, and it is now stated
// rather than implied by an empty catch — this was the one copy of the
// pattern that wrote its refs with no aborted guard at all.
export function useVisitorIp() {
  const { data } = useResource((signal) => getVisitorIP(signal), { fallback: null })

  const ip = computed(() => data.value?.ip ?? null)
  const warn = computed(() => (data.value ? data.value.family !== 'ipv6' : false))

  return { ip, warn }
}
