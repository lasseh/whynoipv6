import { describe, expect, it } from 'vitest'
import { extractHost } from '@/utils/host'

// The paste-anything input rule: URL-shaped input reduces to its hostname;
// plain hosts pass through; garbage flows on to the API's validator.
describe('extractHost', () => {
  const cases: [string, string][] = [
    ['vg.no', 'vg.no'],
    ['  vg.no  ', 'vg.no'],
    ['http://vg.no/', 'vg.no'],
    ['https://www.vg.no/path?q=1#frag', 'www.vg.no'],
    ['HTTPS://VG.NO/path', 'vg.no'],
    ['vg.no/some/path', 'vg.no'],
    ['vg.no:8080', 'vg.no'],
    ['ftp://files.example.com/pub', 'files.example.com'],
    ['user:pass@vg.no', 'vg.no'],
    ['', ''],
    ['   ', ''],
    // Unparsable input passes through for the API to reject with a reason.
    ['not a domain!', 'not a domain!'],
  ]
  it.each(cases)('%j → %j', (raw, want) => {
    expect(extractHost(raw)).toBe(want)
  })
})
