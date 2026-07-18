import { describe, expect, it } from 'vitest'
import {
  STATUS_CLASSES,
  statusCardBorderClass,
  statusCardTextClass,
  statusIcon,
  statusLabel,
  statusTextClass,
  statusTooltip,
} from '@/utils/status'

// All five §7.2 states → icon/class/label/tooltip. Tables use the 500 shades
// (text), the detail accordion the old site's stronger 600 (cardText/border).
describe('status maps', () => {
  const table = [
    {
      value: 'supported',
      icon: 'check',
      text: 'text-emerald-500',
      cardText: 'text-emerald-600',
      border: 'border-emerald-600',
      label: 'Success',
      tooltip: 'Supported',
    },
    {
      value: 'unsupported',
      icon: 'cross',
      text: 'text-pink-500',
      cardText: 'text-pink-600',
      border: 'border-pink-600',
      label: 'Missing',
      tooltip: 'Missing',
    },
    {
      value: 'no_record',
      icon: 'minus',
      text: 'text-amber-500',
      cardText: 'text-amber-500',
      border: 'border-amber-500',
      label: 'No Record',
      tooltip: 'No Records',
    },
    {
      value: 'not_applicable',
      icon: 'minus',
      text: 'text-zinc-600',
      cardText: 'text-zinc-600',
      border: 'border-zinc-600',
      label: 'Not applicable',
      tooltip: 'Not applicable',
    },
    {
      value: null,
      icon: 'minus',
      text: 'text-zinc-600',
      cardText: 'text-zinc-600',
      border: 'border-zinc-600',
      label: 'Not yet checked',
      tooltip: 'Not yet checked',
    },
  ] as const

  it.each(table)('$value', (row) => {
    expect(statusIcon(row.value)).toBe(row.icon)
    expect(statusTextClass(row.value)).toBe(row.text)
    expect(statusCardTextClass(row.value)).toBe(row.cardText)
    expect(statusCardBorderClass(row.value)).toBe(row.border)
    expect(statusLabel(row.value)).toBe(row.label)
    expect(statusTooltip(row.value)).toBe(row.tooltip)
  })
})

// The hue-family invariant: shades vary by surface on purpose, but the
// family per status never does — supported is always emerald, unsupported
// pink, no_record amber, the muted states zinc/gray. A new status value or
// surface added to STATUS_CLASSES must declare its family here.
describe('status hue families', () => {
  const FAMILY: Record<string, string[]> = {
    supported: ['emerald'],
    unsupported: ['pink'],
    no_record: ['amber'],
    not_applicable: ['zinc'],
    unconfirmed: ['zinc', 'gray'],
  }

  it('every surface class stays inside its status family', () => {
    for (const [surface, classes] of Object.entries(STATUS_CLASSES)) {
      for (const [status, cls] of Object.entries(classes)) {
        const families = FAMILY[status]
        expect(families, `unknown status ${status}`).toBeDefined()
        expect(
          families!.some((f) => cls.includes(`-${f}-`)),
          `${surface}.${status} = ${cls}, want family ${families!.join('/')}`,
        ).toBe(true)
      }
    }
  })
})
