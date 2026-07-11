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

export function changelogMessage(
  item: Pick<ChangelogItem, 'host' | 'field' | 'old_value' | 'new_value'>,
): ChangelogMessage {
  const label = FIELD_LABELS[item.field]
  const fromNA = item.old_value === 'not_applicable'
  switch (item.new_value) {
    case 'supported':
      return {
        message: `${item.host} now supports IPv6 on ${label}`,
        colorClass: 'text-emerald-600',
      }
    case 'unsupported':
      return {
        message: fromNA
          ? `${item.host} started using ${label} — without IPv6`
          : `${item.host} lost IPv6 on ${label}`,
        colorClass: 'text-pink-600',
      }
    case 'no_record':
      return {
        message: fromNA
          ? `${item.host} started publishing ${label} — without IPv6 records`
          : `${item.host} no longer publishes records for ${label}`,
        colorClass: 'text-amber-500',
      }
    case 'not_applicable':
      return {
        message: `${item.host} no longer uses ${label}`,
        colorClass: 'text-zinc-600',
      }
  }
}
