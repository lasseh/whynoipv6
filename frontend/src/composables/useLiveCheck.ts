// The live-check machine (§10.1): POST /check → 202 → poll GET /check/{id}
// every 2 s to a terminal done|failed (a 200 on the POST is a dedupe
// envelope), plus the shareable-URL contract — the canonical URL is
// /check/{domain}, stored results load via GET /check/latest inside the
// 7 d TTL (auto-recheck past it), and legacy numeric /check/{id} links
// upgrade to the domain form. The page renders; this composable decides.
import { onMounted, onScopeDispose, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createCheck, getCheck, getLatestCheck, isCheckEnvelope } from '@/api'
import type { CheckEnvelope } from '@/api'
import { ApiProblem } from '@/api/problem'
import { extractHost } from '@/utils/host'

const POLL_MS = 2_000
const POLL_LIMIT = 60 // ~2 min; the engine's whole-scan budget is 90 s

export function useLiveCheck() {
  const route = useRoute()
  const router = useRouter()

  /** The form model — submit cleans it, loads canonicalize it. */
  const host = ref('')
  const envelope = ref<CheckEnvelope | null>(null)
  const problem = ref<ApiProblem | null>(null)
  const running = ref(false)
  const retryLeft = ref(0)

  // One controller + token per submission: a new submit (or unmount)
  // cancels the in-flight fetch and orphans the old poll loop.
  let controller: AbortController | null = null
  let pollTimer: ReturnType<typeof setTimeout> | null = null
  let retryTimer: ReturnType<typeof setInterval> | null = null
  let pollToken = 0

  function stopPolling() {
    pollToken++
    if (pollTimer !== null) clearTimeout(pollTimer)
    pollTimer = null
    controller?.abort()
    controller = null
  }

  onScopeDispose(() => {
    stopPolling()
    if (retryTimer !== null) clearInterval(retryTimer)
  })

  function startRetryCountdown(seconds: number) {
    retryLeft.value = seconds
    if (retryTimer !== null) clearInterval(retryTimer)
    retryTimer = setInterval(() => {
      retryLeft.value--
      if (retryLeft.value <= 0 && retryTimer !== null) {
        clearInterval(retryTimer)
        retryTimer = null
      }
    }, 1_000)
  }

  function fail(e: unknown) {
    const p = ApiProblem.from(e)
    problem.value = p
    running.value = false
    if (p.code === 'rate-limited') startRetryCountdown(p.retryAfter ?? 60)
  }

  function poll(id: number, attempt: number) {
    const token = pollToken
    pollTimer = setTimeout(async () => {
      if (token !== pollToken) return
      try {
        const env = await getCheck(id, controller?.signal)
        if (token !== pollToken) return
        envelope.value = env
        if (env.status === 'done' || env.status === 'failed') {
          running.value = false
          return
        }
        if (attempt >= POLL_LIMIT) {
          running.value = false
          problem.value = new ApiProblem(
            { title: 'Check timed out', detail: 'The scan is taking too long — try again later.' },
            0,
          )
          return
        }
        poll(id, attempt + 1)
      } catch (e) {
        if (token === pollToken) fail(e)
      }
    }, POLL_MS)
  }

  // activeTarget marks what this instance already handles, so the route
  // watcher ignores our own router.replace and only reacts to real
  // navigation (back/forward, links).
  let activeTarget: string | null = null

  function reflectHost(h: string) {
    activeTarget = h
    if (route.params.target !== h) {
      void router.replace(`/check/${h}`)
    }
  }

  // Returns the controller it installs so callers thread the signal locally
  // instead of asserting the module-level `controller!` is still theirs.
  function beginRequest(): AbortController {
    stopPolling()
    const c = new AbortController()
    controller = c
    envelope.value = null
    problem.value = null
    running.value = true
    return c
  }

  /** H3: a typo'd domain shouldn't lock the form for the whole scan. */
  function cancel() {
    stopPolling()
    running.value = false
    envelope.value = null
  }

  async function submit(target = extractHost(host.value)) {
    if (!target || running.value || retryLeft.value > 0) return
    host.value = target // show the cleaned host in the input
    const c = beginRequest()
    try {
      const res = await createCheck(target, c.signal)
      reflectHost(res.host)
      if (isCheckEnvelope(res)) {
        // Dedupe hit: a cached done envelope, no job to poll.
        envelope.value = res
        running.value = false
        return
      }
      poll(res.id, 1)
    } catch (e) {
      fail(e)
    }
  }

  // A /check/{domain} link: stored result inside the TTL, else recheck.
  async function loadByHost(h: string) {
    activeTarget = h
    host.value = h
    const c = beginRequest()
    try {
      const env = await getLatestCheck(h, c.signal)
      envelope.value = env
      host.value = env.host
      reflectHost(env.host) // canonicalized form may differ from the URL
      running.value = false
    } catch (e) {
      if (ApiProblem.from(e).code === 'not-found') {
        running.value = false
        void submit(h) // nothing stored within 7 days — recheck now
        return
      }
      fail(e)
    }
  }

  // A legacy /check/{id} link: load the job, then upgrade to the domain URL.
  async function loadByID(id: number) {
    const c = beginRequest()
    try {
      const env = await getCheck(id, c.signal)
      envelope.value = env
      host.value = env.host
      reflectHost(env.host)
      if (env.status === 'done' || env.status === 'failed') {
        running.value = false
        return
      }
      poll(id, 1)
    } catch (e) {
      const p = ApiProblem.from(e)
      if (p.code === 'not-found') {
        running.value = false
        problem.value = new ApiProblem(
          {
            title: 'Check not found',
            detail: 'This check link has expired or never existed — run a fresh check above.',
          },
          404,
        )
        return
      }
      fail(e)
    }
  }

  function loadTarget(raw: string) {
    activeTarget = raw
    if (/^\d+$/.test(raw)) {
      void loadByID(Number(raw))
    } else {
      void loadByHost(raw)
    }
  }

  function routeTarget(): string | null {
    const raw = route.params.target
    return typeof raw === 'string' && raw !== '' ? raw : null
  }

  onMounted(() => {
    const target = routeTarget()
    if (target !== null) loadTarget(target)
  })

  // Back/forward between two result links re-loads the target; navigating
  // to the bare /check (nav link) keeps whatever is on screen.
  watch(
    () => route.params.target,
    () => {
      const target = routeTarget()
      if (target !== null && target !== activeTarget) loadTarget(target)
    },
  )

  return { host, envelope, problem, running, retryLeft, submit, cancel }
}
