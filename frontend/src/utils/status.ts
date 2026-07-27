// The §7.2 status → presentation contract — the single source for
// visual-contract compliance. Never inline these ternaries in templates.
//
// One table, keyed surface → status. Shades vary by surface on purpose
// (tables use 500, the detail card the old site's stronger 600, the
// changelog dot 400/500), but the hue FAMILY per status never does:
// supported is always emerald, unsupported pink, no_record amber, the
// muted states zinc (status.test.ts pins this invariant). Tailwind needs
// literal class strings, so shades are spelled out rather than composed.
import type { StatusValue } from '@/api'

type StatusKey = 'supported' | 'unsupported' | 'no_record' | 'not_applicable' | 'unconfirmed'

const keyOf = (value: StatusValue): StatusKey => value ?? 'unconfirmed'

export const STATUS_CLASSES = {
  /** Table / status-icon text. */
  text: {
    supported: 'text-emerald-500',
    unsupported: 'text-pink-500',
    no_record: 'text-amber-500',
    not_applicable: 'text-zinc-600',
    unconfirmed: 'text-zinc-600',
  },
  /** Detail accordion (DomainStatusCard) text. */
  cardText: {
    supported: 'text-emerald-600',
    unsupported: 'text-pink-600',
    no_record: 'text-amber-500',
    not_applicable: 'text-zinc-600',
    unconfirmed: 'text-zinc-600',
  },
  /** Detail accordion border. */
  cardBorder: {
    supported: 'border-emerald-600',
    unsupported: 'border-pink-600',
    no_record: 'border-amber-500',
    not_applicable: 'border-zinc-600',
    unconfirmed: 'border-zinc-600',
  },
  /** Tracker day-block fill — the old timeline shades; null days stay neutral. */
  block: {
    supported: 'bg-emerald-600',
    unsupported: 'bg-pink-600',
    no_record: 'bg-amber-500',
    not_applicable: 'bg-zinc-600',
    unconfirmed: 'bg-gray-800',
  },
  /** Changelog message text (§7.4). */
  changelogText: {
    supported: 'text-emerald-600',
    unsupported: 'text-pink-600',
    no_record: 'text-amber-500',
    not_applicable: 'text-zinc-600',
    unconfirmed: 'text-zinc-600',
  },
  /** Changelog bullet dot (§7.4). */
  changelogDot: {
    supported: 'bg-emerald-500',
    unsupported: 'bg-pink-500',
    no_record: 'bg-amber-400',
    not_applicable: 'bg-zinc-500',
    unconfirmed: 'bg-zinc-500',
  },
} as const satisfies Record<string, Record<StatusKey, string>>

export type StatusSurface = keyof typeof STATUS_CLASSES

export function statusClass(surface: StatusSurface, value: StatusValue): string {
  return STATUS_CLASSES[surface][keyOf(value)]
}

export type StatusIconKind = 'check' | 'cross' | 'minus'

export function statusIcon(value: StatusValue): StatusIconKind {
  switch (value) {
    case 'supported':
      return 'check'
    case 'unsupported':
      return 'cross'
    default:
      return 'minus'
  }
}

export function statusTextClass(value: StatusValue): string {
  return statusClass('text', value)
}

export function statusCardTextClass(value: StatusValue): string {
  return statusClass('cardText', value)
}

export function statusCardBorderClass(value: StatusValue): string {
  return statusClass('cardBorder', value)
}

export function statusBlockClass(value: StatusValue): string {
  return statusClass('block', value)
}

// The 4-star rating trichotomy (§7.3): supported earns a filled emerald
// star; not_applicable a muted zinc one (neither earned nor missing — a
// no-MX domain is never penalized); everything else stays empty.
export type StarKind = 'filled' | 'muted' | 'empty'

export function statusStarKind(value: StatusValue): StarKind {
  if (value === 'supported') return 'filled'
  if (value === 'not_applicable') return 'muted'
  return 'empty'
}

export const STAR_CLASS: Record<StarKind, string> = {
  filled: 'text-emerald-600',
  muted: 'text-zinc-600',
  empty: 'text-gray-600',
}

export function statusLabel(value: StatusValue): string {
  switch (value) {
    case 'supported':
      return 'Success'
    case 'unsupported':
      return 'Missing'
    case 'no_record':
      return 'No Record'
    case 'not_applicable':
      return 'Not applicable'
    default:
      return 'Not yet checked'
  }
}

// The live-check raw-observation vocabulary (§10.1): the 4 public statuses
// plus partial/error/inconsistent, which never appear in confirmed state.
// Same hue families as §7.2; the three extra states stay deliberately muted.
export interface LiveStatusView {
  icon: StatusIconKind
  class: string
  label: string
}

const LIVE_STATUS: Record<string, LiveStatusView> = {
  supported: { icon: 'check', class: 'text-emerald-500', label: 'Supported' },
  partial: { icon: 'check', class: 'text-amber-500', label: 'Partial' },
  unsupported: { icon: 'cross', class: 'text-pink-500', label: 'Missing' },
  no_record: { icon: 'minus', class: 'text-amber-500', label: 'No record' },
  not_applicable: { icon: 'minus', class: 'text-zinc-600', label: 'Not applicable' },
  error: { icon: 'minus', class: 'text-zinc-600', label: 'Check error' },
  inconsistent: { icon: 'minus', class: 'text-amber-500', label: 'Resolvers disagreed' },
}

export function liveStatus(value: string | undefined): LiveStatusView {
  return (
    (value && LIVE_STATUS[value]) || { icon: 'minus', class: 'text-zinc-600', label: 'Not checked' }
  )
}

/** Hover tooltip on status icons — the old wording for the legacy three states. */
export function statusTooltip(value: StatusValue): string {
  switch (value) {
    case 'supported':
      return 'Supported'
    case 'unsupported':
      return 'Missing'
    case 'no_record':
      return 'No Records'
    case 'not_applicable':
      return 'Not applicable'
    default:
      return 'Not yet checked'
  }
}
