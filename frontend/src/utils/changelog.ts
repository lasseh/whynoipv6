// Structured changelog rows → human message + color (§7.4). The template is
// keyed on (field, old_value, new_value) — old_value matters: a host going
// not_applicable → unsupported never *had* IPv6 to lose.
import type { ChangelogItem, Dimension } from '@/api'

export const FIELD_LABELS: Record<Dimension, string> = {
  base: 'the base domain',
  www: 'www',
  ns: 'nameservers',
  mx: 'e-mail',
  conn: 'connectivity',
  resources: 'page resources',
}

export interface ChangelogMessage {
  message: string
  colorClass: string
}

export interface ChangelogParts {
  /** Verb phrase after the host — the feed renders the host separately as the link. */
  phrase: string
  /** Status-dot background, same hue table as colorClass (§7.4). */
  dotClass: string
}

/**
 * The §7.4 wording split at the host boundary: message = `${host} ${phrase}`.
 * conn and resources are derived dimensions with bespoke reachability
 * wording — the generic "{host} verb {label}" template misdescribes them.
 * The backend feed serializer (internal/api/feed.go) renders the identical
 * table; goldens on both sides pin the wordings together.
 */
export function changelogParts(
  item: Pick<ChangelogItem, 'field' | 'old_value' | 'new_value'>,
): ChangelogParts {
  const fromNA = item.old_value === 'not_applicable'
  if (item.field === 'conn') {
    switch (item.new_value) {
      case 'supported':
        return { phrase: 'is now reachable over IPv6', dotClass: 'bg-emerald-500' }
      case 'unsupported':
        return {
          phrase: fromNA
            ? 'published IPv6 addresses — but connections fail'
            : 'is no longer reachable over IPv6',
          dotClass: 'bg-pink-500',
        }
      default: // not_applicable — suppressed at write (03 §11); defensive only
        return { phrase: 'has no IPv6 addresses left to test', dotClass: 'bg-zinc-500' }
    }
  }
  if (item.field === 'resources') {
    switch (item.new_value) {
      case 'supported':
        return { phrase: 'now loads all page resources over IPv6', dotClass: 'bg-emerald-500' }
      case 'unsupported':
        return { phrase: 'loads some page resources without IPv6', dotClass: 'bg-pink-500' }
      default: // not_applicable — suppressed at write (03 §11); defensive only
        return { phrase: 'no longer has its page resources checked', dotClass: 'bg-zinc-500' }
    }
  }
  const label = FIELD_LABELS[item.field]
  switch (item.new_value) {
    case 'supported':
      return { phrase: `now supports IPv6 on ${label}`, dotClass: 'bg-emerald-500' }
    case 'unsupported':
      return {
        phrase: fromNA ? `started using ${label} — without IPv6` : `lost IPv6 on ${label}`,
        dotClass: 'bg-pink-500',
      }
    case 'no_record':
      return {
        phrase: fromNA
          ? `started publishing ${label} — without IPv6 records`
          : `no longer publishes records for ${label}`,
        dotClass: 'bg-amber-400',
      }
    case 'not_applicable':
      return { phrase: `no longer uses ${label}`, dotClass: 'bg-zinc-500' }
  }
}

const TEXT_COLORS: Record<ChangelogItem['new_value'], string> = {
  supported: 'text-emerald-600',
  unsupported: 'text-pink-600',
  no_record: 'text-amber-500',
  not_applicable: 'text-zinc-600',
}

export function changelogMessage(
  item: Pick<ChangelogItem, 'host' | 'field' | 'old_value' | 'new_value'>,
): ChangelogMessage {
  return {
    message: `${item.host} ${changelogParts(item).phrase}`,
    colorClass: TEXT_COLORS[item.new_value],
  }
}
