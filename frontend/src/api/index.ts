// Narrow per-resource call helpers (§6.2) — the one seam pages call and tests
// stub. Envelopes are uniform (07 §2.4): item collections are
// { items, page, meta }, time series are { points, meta }.
import { get, post } from '@/api/client'
import type { GetQuery } from '@/api/client'
import type { components } from '@openapi/schema'

export type Schemas = components['schemas']

export type DomainSummary = Schemas['DomainSummary']
export type DomainDetail = Schemas['DomainDetail']
export type StatusValue = Schemas['IPv6Status'] | null
export type StatusBlock = Schemas['StatusBlock']
export type Dimension = Schemas['Dimension']
export type ChangelogItem = Schemas['ChangelogItem']
export type HistoryPoint = Schemas['HistoryPoint']
export type Country = Schemas['Country']
export type ASN = Schemas['ASN']
export type Provider = Schemas['Provider']
export type CampaignListItem = Schemas['CampaignListItem']
export type CampaignDetail = Schemas['CampaignDetail']
export type ShameItem = Schemas['ShameItem']
export type GlobalStatsPoint = Schemas['GlobalStatsPoint']
export type CrawlerStats = Schemas['CrawlerStats']
export type NetworkTrend = Schemas['NetworkTrend']
export type Page = Schemas['Page']
export type Meta = Schemas['Meta']

export const listSinners = (query?: GetQuery<'/sinners'>, signal?: AbortSignal) =>
  get('/sinners', { query, signal })

export const listHeroes = (query?: GetQuery<'/heroes'>, signal?: AbortSignal) =>
  get('/heroes', { query, signal })

export const listSaints = (query?: GetQuery<'/saints'>, signal?: AbortSignal) =>
  get('/saints', { query, signal })

export const listAlmostHeroes = (query?: GetQuery<'/almost-heroes'>, signal?: AbortSignal) =>
  get('/almost-heroes', { query, signal })

export const listShame = (signal?: AbortSignal) => get('/shame', { signal })

export const searchDomains = (query: GetQuery<'/domains'>, signal?: AbortSignal) =>
  get('/domains', { query, signal })

export const getDomain = (host: string, signal?: AbortSignal) =>
  get('/domains/{host}', { path: { host }, signal })

export const getDomainChangelog = (
  host: string,
  query?: GetQuery<'/domains/{host}/changelog'>,
  signal?: AbortSignal,
) => get('/domains/{host}/changelog', { path: { host }, query, signal })

export const getDomainHistory = (
  host: string,
  query?: GetQuery<'/domains/{host}/history'>,
  signal?: AbortSignal,
) => get('/domains/{host}/history', { path: { host }, query, signal })

export const listSubdomains = (
  host: string,
  query?: GetQuery<'/domains/{host}/subdomains'>,
  signal?: AbortSignal,
) => get('/domains/{host}/subdomains', { path: { host }, query, signal })

export const listCountries = (signal?: AbortSignal) => get('/countries', { signal })

export const getCountry = (code: string, signal?: AbortSignal) =>
  get('/countries/{code}', { path: { code }, signal })

export const listCountryDomains = (
  code: string,
  query?: GetQuery<'/countries/{code}/domains'>,
  signal?: AbortSignal,
) => get('/countries/{code}/domains', { path: { code }, query, signal })

export const listCampaigns = (query?: GetQuery<'/campaigns'>, signal?: AbortSignal) =>
  get('/campaigns', { query, signal })

export const getCampaign = (
  uuid: string,
  query?: GetQuery<'/campaigns/{uuid}'>,
  signal?: AbortSignal,
) => get('/campaigns/{uuid}', { path: { uuid }, query, signal })

export const getCampaignChangelog = (
  uuid: string,
  query?: GetQuery<'/campaigns/{uuid}/changelog'>,
  signal?: AbortSignal,
) => get('/campaigns/{uuid}/changelog', { path: { uuid }, query, signal })

export const getCampaignDomainChangelog = (
  uuid: string,
  host: string,
  query?: GetQuery<'/campaigns/{uuid}/domains/{host}/changelog'>,
  signal?: AbortSignal,
) => get('/campaigns/{uuid}/domains/{host}/changelog', { path: { uuid, host }, query, signal })

export const listChangelog = (query?: GetQuery<'/changelog'>, signal?: AbortSignal) =>
  get('/changelog', { query, signal })

export const getOverviewStats = (query?: GetQuery<'/stats/overview'>, signal?: AbortSignal) =>
  get('/stats/overview', { query, signal })

// Throughput, not a series: a single object with a sibling meta.
export const getCrawlerStats = (signal?: AbortSignal) => get('/stats/crawler', { signal })

// Grouped series — one box per network. Keyed on `asn`; `name` is not unique.
export const getNetworkStats = (query?: GetQuery<'/stats/networks'>, signal?: AbortSignal) =>
  get('/stats/networks', { query, signal })

export const listASNs = (query?: GetQuery<'/asns'>, signal?: AbortSignal) =>
  get('/asns', { query, signal })

export const listProviders = (query?: GetQuery<'/providers'>, signal?: AbortSignal) =>
  get('/providers', { query, signal })

export const getVisitorIP = (signal?: AbortSignal) => get('/ip', { signal })

// The live-check flow (§10.1): enqueue returns either a 202 accepted stub or
// a cached done envelope (dedupe); poll to a terminal done/failed status.
export type CheckAccepted = Schemas['CheckAccepted']
export type CheckEnvelope = Schemas['CheckEnvelope']

/** Discriminates the two createCheck success shapes; `cached` only exists on the envelope. */
export const isCheckEnvelope = (r: CheckAccepted | CheckEnvelope): r is CheckEnvelope =>
  'cached' in r

export const createCheck = (host: string, signal?: AbortSignal) =>
  post('/check', { host }, { signal })

export const getCheck = (id: number, signal?: AbortSignal) =>
  get('/check/{id}', { path: { id }, signal })

export const getLatestCheck = (host: string, signal?: AbortSignal) =>
  get('/check/latest', { query: { host }, signal })
