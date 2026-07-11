import { describe, expect, it } from 'vitest'
import {
  statusBorderClass,
  statusIcon,
  statusLabel,
  statusTextClass,
  statusTooltip,
} from '@/utils/status'

// All five §7.2 states → icon/class/label/tooltip.
describe('status maps', () => {
  const table = [
    {
      value: 'supported',
      icon: 'check',
      text: 'text-emerald-500',
      border: 'border-emerald-500',
      label: 'Success',
      tooltip: 'Supported',
    },
    {
      value: 'unsupported',
      icon: 'cross',
      text: 'text-pink-500',
      border: 'border-pink-500',
      label: 'Missing',
      tooltip: 'Missing',
    },
    {
      value: 'no_record',
      icon: 'minus',
      text: 'text-amber-500',
      border: 'border-amber-500',
      label: 'No Record',
      tooltip: 'No Records',
    },
    {
      value: 'not_applicable',
      icon: 'minus',
      text: 'text-zinc-600',
      border: 'border-zinc-600',
      label: 'Not applicable',
      tooltip: 'Not applicable',
    },
    {
      value: null,
      icon: 'minus',
      text: 'text-zinc-600',
      border: 'border-zinc-600',
      label: 'Not yet checked',
      tooltip: 'Not yet checked',
    },
  ] as const

  it.each(table)('$value', (row) => {
    expect(statusIcon(row.value)).toBe(row.icon)
    expect(statusTextClass(row.value)).toBe(row.text)
    expect(statusBorderClass(row.value)).toBe(row.border)
    expect(statusLabel(row.value)).toBe(row.label)
    expect(statusTooltip(row.value)).toBe(row.tooltip)
  })
})
