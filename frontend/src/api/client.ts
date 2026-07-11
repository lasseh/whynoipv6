// The one typed-fetch wrapper (§6.1) — vendored because the openapi-fetch
// package is maintenance-mode. Typed end-to-end against the generated `paths`
// from openapi/schema.ts; the frontend never hand-writes a response interface.
import type { paths } from '@openapi/schema'
import { ApiProblem } from '@/api/problem'

type GetOp<P extends keyof paths> = paths[P] extends { get: infer O } ? O : never
type SuccessJson<O> = O extends { responses: { 200: { content: { 'application/json': infer J } } } }
  ? J
  : never

/** Paths with a JSON-returning GET (excludes .atom / .svg / CSV-only shapes). */
export type GetPath = {
  [P in keyof paths]: SuccessJson<GetOp<P>> extends never ? never : P
}[keyof paths]

export type GetQuery<P extends GetPath> =
  GetOp<P> extends { parameters: { query?: infer Q } } ? NonNullable<Q> : never

type PathParamsOf<O> = O extends { parameters: { path: infer PP extends object } } ? PP : never

type GetOptions<O> = { signal?: AbortSignal | undefined } & (PathParamsOf<O> extends never
  ? { path?: undefined }
  : { path: PathParamsOf<O> }) &
  (O extends { parameters: { query?: infer Q } }
    ? { query?: NonNullable<Q> | undefined }
    : { query?: undefined })

const BASE = import.meta.env.VITE_API_URL ?? ''
const TIMEOUT_MS = 15_000

function buildURL(path: string, pathParams?: object, query?: object): string {
  const filled = path.replace(/\{(\w+)\}/g, (_, key: string) =>
    encodeURIComponent(String((pathParams as Record<string, string | number>)[key])),
  )
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(query ?? {})) {
    if (v !== undefined && v !== null && v !== '') params.set(k, String(v))
  }
  const qs = params.toString()
  return qs ? `${BASE}${filled}?${qs}` : `${BASE}${filled}`
}

function withTimeout(signal?: AbortSignal): AbortSignal {
  const timeout = AbortSignal.timeout(TIMEOUT_MS)
  return signal ? AbortSignal.any([signal, timeout]) : timeout
}

async function handle<T>(res: Response): Promise<T> {
  if (!res.ok) throw await ApiProblem.fromResponse(res)
  return res.json() as Promise<T>
}

export async function get<P extends GetPath>(
  path: P,
  opts: GetOptions<GetOp<P>>,
): Promise<SuccessJson<GetOp<P>>> {
  const res = await fetch(buildURL(path, opts.path, opts.query), {
    headers: { Accept: 'application/json' },
    signal: withTimeout(opts.signal),
  })
  return handle(res)
}

type PostOp<P extends keyof paths> = paths[P] extends { post: infer O } ? O : never
type PostPath = { [P in keyof paths]: PostOp<P> extends never ? never : P }[keyof paths]
type PostBody<O> = O extends { requestBody?: { content: { 'application/json': infer B } } }
  ? B
  : never
type PostJson<O> = O extends { responses: { 202: { content: { 'application/json': infer J } } } }
  ? J
  : O extends { responses: { 200: { content: { 'application/json': infer J } } } }
    ? J
    : never

export async function post<P extends PostPath>(
  path: P,
  body: PostBody<PostOp<P>>,
  signal?: AbortSignal,
): Promise<PostJson<PostOp<P>>> {
  const res = await fetch(buildURL(path), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(body),
    signal: withTimeout(signal),
  })
  return handle(res)
}
