// Narrow per-resource call helpers (§6.2) — the one seam pages call and tests
// stub. Envelopes are uniform (07 §2.4): item collections are
// { items, page, meta }, time series are { points, meta }.
import { get } from '@/api/client'
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
export type CampaignListItem = Schemas['CampaignListItem']
export type CampaignDetail = Schemas['CampaignDetail']
export type ShameItem = Schemas['ShameItem']
export type GlobalStatsPoint = Schemas['GlobalStatsPoint']
export type Page = Schemas['Page']
export type Meta = Schemas['Meta']

export const listSinners = (query?: GetQuery<'/sinners'>, signal?: AbortSignal) =>
  get('/sinners', { query, signal })

export const listHeroes = (query?: GetQuery<'/heroes'>, signal?: AbortSignal) =>
  get('/heroes', { query, signal })

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

export const listASNs = (query?: GetQuery<'/asns'>, signal?: AbortSignal) =>
  get('/asns', { query, signal })

export const getVisitorIP = (signal?: AbortSignal) => get('/ip', { signal })
