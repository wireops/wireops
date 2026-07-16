import { describe, expect, it } from 'vitest'
import { parseAnsiLine } from './ansi'

describe('parseAnsiLine', () => {
  it('returns a single plain segment for text with no escape codes', () => {
    expect(parseAnsiLine('Pulling image')).toEqual([{ text: 'Pulling image', color: undefined, bold: false }])
  })

  it('applies color for a foreground SGR code and resets after code 0', () => {
    const segments = parseAnsiLine('\x1b[32mdone\x1b[0m plain')
    expect(segments).toEqual([
      { text: 'done', color: '#4ade80', bold: false },
      { text: ' plain', color: undefined, bold: false },
    ])
  })

  it('tracks bold state independently of color', () => {
    const segments = parseAnsiLine('\x1b[1m\x1b[31merror\x1b[22m still red')
    expect(segments).toEqual([
      { text: 'error', color: '#f87171', bold: true },
      { text: ' still red', color: '#f87171', bold: false },
    ])
  })

  it('drops non-SGR CSI sequences without leaking raw escape bytes', () => {
    const segments = parseAnsiLine('\x1b[2Kclearing line\x1b[1Grestart')
    expect(segments.map(s => s.text).join('')).toBe('clearing linerestart')
    for (const s of segments) {
      expect(s.text).not.toContain('\x1b')
    }
  })

  it('ignores unrecognized SGR codes and keeps prior state', () => {
    const segments = parseAnsiLine('\x1b[32mgreen\x1b[999munaffected')
    expect(segments).toEqual([
      { text: 'green', color: '#4ade80', bold: false },
      { text: 'unaffected', color: '#4ade80', bold: false },
    ])
  })

  it('handles an empty line', () => {
    expect(parseAnsiLine('')).toEqual([{ text: '', color: undefined, bold: false }])
  })
})
