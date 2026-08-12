import { describe, expect, it } from 'vitest'
import { highlightLogLine } from './log-highlighter'

describe('highlightLogLine', () => {
  it('recognizes common structured log tokens', () => {
    const segments = highlightLogLine('2026-08-12T13:45:00Z ERROR request failed status=500')

    expect(segments).toEqual(expect.arrayContaining([
      expect.objectContaining({ text: '2026-08-12T13:45:00Z', kind: 'timestamp' }),
      expect.objectContaining({ text: 'ERROR', kind: 'level-error' }),
      expect.objectContaining({ text: '500', kind: 'number' }),
    ]))
  })

  it('highlights JSON keys, strings, URLs, and informational levels', () => {
    const segments = highlightLogLine('{"level":"info","url":"https://wireops.dev/docs"}')

    expect(segments.some(segment => segment.kind === 'json-key' && segment.text === '"level"')).toBe(true)
    expect(segments.some(segment => segment.kind === 'string' && segment.text === '"info"')).toBe(true)
    expect(segments.some(segment => segment.kind === 'url' && segment.text === 'https://wireops.dev/docs')).toBe(true)
  })

  it('marks every case-insensitive search occurrence without losing syntax color', () => {
    const segments = highlightLogLine('ERROR then error', 'error')
    const matches = segments.filter(segment => segment.match)

    expect(matches.map(segment => segment.text)).toEqual(['ERROR', 'error'])
    expect(matches.every(segment => segment.kind === 'level-error')).toBe(true)
  })

  it('returns text segments rather than rendered HTML', () => {
    const segments = highlightLogLine('<script>alert("x")</script>')
    expect(segments.map(segment => segment.text).join('')).toBe('<script>alert("x")</script>')
  })
})

