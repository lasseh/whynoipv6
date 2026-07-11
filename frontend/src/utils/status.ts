// The §7.2 status → icon/color mapping — the single source for visual-contract
// compliance. Never inline these ternaries in templates.
import type { StatusValue } from '@/api'

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
  switch (value) {
    case 'supported':
      return 'text-emerald-500'
    case 'unsupported':
      return 'text-pink-500'
    case 'no_record':
      return 'text-amber-500'
    default:
      // not_applicable and null (never confirmed) — the muted states.
      return 'text-zinc-600'
  }
}

export function statusBorderClass(value: StatusValue): string {
  switch (value) {
    case 'supported':
      return 'border-emerald-500'
    case 'unsupported':
      return 'border-pink-500'
    case 'no_record':
      return 'border-amber-500'
    default:
      return 'border-zinc-600'
  }
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

/** Tooltip for the two muted states; null for the legacy three. */
export function statusTooltip(value: StatusValue): string | null {
  if (value === 'not_applicable') return 'Not applicable'
  if (value === null) return 'Not yet checked'
  return null
}
