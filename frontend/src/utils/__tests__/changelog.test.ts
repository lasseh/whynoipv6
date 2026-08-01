import { describe, expect, it } from 'vitest'
import { changelogParts, FIELD_LABELS } from '@/utils/changelog'
import { statusClass } from '@/utils/status'

// Golden table for §7.4 — these wordings are the trust surface shared with the
// server's feed serializer; change them only alongside the spec.
describe('changelogParts', () => {
  const goldens = [
    {
      field: 'base',
      old_value: 'unsupported',
      new_value: 'supported',
      message: 'example.com now supports IPv6 on the base domain',
      colorClass: 'text-emerald-600',
    },
    {
      field: 'www',
      old_value: 'not_applicable',
      new_value: 'supported',
      message: 'example.com now supports IPv6 on www',
      colorClass: 'text-emerald-600',
    },
    {
      field: 'www',
      old_value: 'not_applicable',
      new_value: 'unsupported',
      message: 'example.com started using www — without IPv6',
      colorClass: 'text-pink-600',
    },
    {
      field: 'ns',
      old_value: 'supported',
      new_value: 'unsupported',
      message: 'example.com lost IPv6 on nameservers',
      colorClass: 'text-pink-600',
    },
    {
      field: 'mx',
      old_value: 'not_applicable',
      new_value: 'no_record',
      message: 'example.com started publishing mail — without IPv6 records',
      colorClass: 'text-amber-500',
    },
    {
      field: 'mx',
      old_value: 'supported',
      new_value: 'no_record',
      message: 'example.com no longer publishes records for mail',
      colorClass: 'text-amber-500',
    },
    {
      field: 'conn',
      old_value: 'unsupported',
      new_value: 'supported',
      message: 'example.com is now reachable over IPv6',
      colorClass: 'text-emerald-600',
    },
    {
      field: 'conn',
      old_value: 'supported',
      new_value: 'unsupported',
      message: 'example.com is no longer reachable over IPv6',
      colorClass: 'text-pink-600',
    },
    {
      field: 'conn',
      old_value: 'not_applicable',
      new_value: 'unsupported',
      message: 'example.com published IPv6 addresses — but connections fail',
      colorClass: 'text-pink-600',
    },
    {
      field: 'conn',
      old_value: 'unsupported',
      new_value: 'not_applicable',
      message: 'example.com has no IPv6 addresses left to test',
      colorClass: 'text-zinc-600',
    },
    {
      field: 'resources',
      old_value: 'unsupported',
      new_value: 'supported',
      message: 'example.com now loads all page resources over IPv6',
      colorClass: 'text-emerald-600',
    },
    {
      field: 'resources',
      old_value: 'supported',
      new_value: 'unsupported',
      message: 'example.com loads some page resources without IPv6',
      colorClass: 'text-pink-600',
    },
  ] as const

  it.each(goldens)('$field $old_value → $new_value', (g) => {
    const parts = changelogParts({
      field: g.field,
      old_value: g.old_value,
      new_value: g.new_value,
    })
    expect(`example.com ${parts.phrase}`).toBe(g.message)
    expect(statusClass('changelogText', g.new_value)).toBe(g.colorClass)
  })

  it('labels all six dimensions', () => {
    expect(FIELD_LABELS).toEqual({
      base: 'the base domain',
      www: 'www',
      ns: 'nameservers',
      mx: 'mail',
      conn: 'connectivity',
      resources: 'page resources',
    })
  })
})
