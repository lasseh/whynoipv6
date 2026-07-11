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

// The detail accordion (DomainStatusCard) uses the stronger 600 shades the old
// site had there — tables keep 500 via statusTextClass.
export function statusCardTextClass(value: StatusValue): string {
  switch (value) {
    case 'supported':
      return 'text-emerald-600'
    case 'unsupported':
      return 'text-pink-600'
    case 'no_record':
      return 'text-amber-500'
    default:
      return 'text-zinc-600'
  }
}

export function statusCardBorderClass(value: StatusValue): string {
  switch (value) {
    case 'supported':
      return 'border-emerald-600'
    case 'unsupported':
      return 'border-pink-600'
    case 'no_record':
      return 'border-amber-500'
    default:
      return 'border-zinc-600'
  }
}

/** Tracker day-block fill — the old timeline shades; null days stay neutral. */
export function statusBlockClass(value: StatusValue): string {
  switch (value) {
    case 'supported':
      return 'bg-emerald-600'
    case 'unsupported':
      return 'bg-pink-600'
    case 'no_record':
      return 'bg-amber-500'
    case 'not_applicable':
      return 'bg-zinc-600'
    default:
      // Never confirmed / before the dimension's `since` — neutral.
      return 'bg-gray-800'
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
