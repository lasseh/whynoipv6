// RFC 9457 problem+json parsing (§6.3). Every non-2xx API response converts
// into an ApiProblem; call sites discriminate with `instanceof` + `code` (the
// type-URI tail, e.g. "not-found"), never by re-parsing bodies.

interface ProblemBody {
  type?: string
  title?: string
  status?: number
  detail?: string
  instance?: string
  retry_after?: number
}

export class ApiProblem extends Error {
  readonly type: string
  readonly title: string
  readonly status: number
  readonly detail: string | null
  /** Seconds until a rate-limited window frees; null otherwise. */
  readonly retryAfter: number | null

  constructor(body: ProblemBody, fallbackStatus: number) {
    const title = body.title ?? `HTTP ${fallbackStatus}`
    super(body.detail ? `${title}: ${body.detail}` : title)
    this.name = 'ApiProblem'
    this.type = body.type ?? 'about:blank'
    this.title = title
    this.status = body.status ?? fallbackStatus
    this.detail = body.detail ?? null
    this.retryAfter = body.retry_after ?? null
  }

  /** The type-URI tail: https://whynoipv6.com/problems/not-found → "not-found". */
  get code(): string {
    const tail = this.type.split('/').at(-1)
    return tail || 'unknown'
  }

  static async fromResponse(res: Response): Promise<ApiProblem> {
    const body: ProblemBody | null = await res.json().catch(() => null)
    return new ApiProblem(body ?? { title: res.statusText }, res.status)
  }
}
