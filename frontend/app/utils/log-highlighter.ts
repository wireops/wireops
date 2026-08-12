export type LogTokenKind =
  | 'plain'
  | 'timestamp'
  | 'level-error'
  | 'level-warn'
  | 'level-info'
  | 'level-debug'
  | 'json-key'
  | 'string'
  | 'number'
  | 'url'

export interface LogSegment {
  text: string
  kind: LogTokenKind
  match: boolean
}

interface TokenRange {
  start: number
  end: number
  kind: LogTokenKind
  priority: number
}

const TOKEN_PATTERNS: { kind: LogTokenKind; pattern: RegExp; priority: number }[] = [
  {
    kind: 'timestamp',
    pattern: /(?:^|\s)(?:\d{4}-\d{2}-\d{2}[T ][0-9:.+-]+Z?|\d{2}:\d{2}:\d{2}(?:\.\d+)?)(?=\s|$)/g,
    priority: 0,
  },
  { kind: 'url', pattern: /https?:\/\/[^\s"'<>]+/gi, priority: 1 },
  { kind: 'json-key', pattern: /"(?:\\.|[^"\\])*"(?=\s*:)/g, priority: 2 },
  { kind: 'string', pattern: /"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'/g, priority: 3 },
  { kind: 'level-error', pattern: /\b(?:error|err|fatal|panic|critical)\b/gi, priority: 4 },
  { kind: 'level-warn', pattern: /\b(?:warn|warning)\b/gi, priority: 4 },
  { kind: 'level-info', pattern: /\b(?:info|notice|success)\b/gi, priority: 4 },
  { kind: 'level-debug', pattern: /\b(?:debug|trace)\b/gi, priority: 4 },
  { kind: 'number', pattern: /\b-?\d+(?:\.\d+)?\b/g, priority: 5 },
]

function syntaxRanges(line: string): TokenRange[] {
  const candidates: TokenRange[] = []

  for (const token of TOKEN_PATTERNS) {
    token.pattern.lastIndex = 0
    let match: RegExpExecArray | null
    while ((match = token.pattern.exec(line)) !== null) {
      let start = match.index
      let text = match[0]

      // Timestamp matching permits leading whitespace so it can remain
      // anchored without coloring the indentation itself.
      if (token.kind === 'timestamp' && /^\s/.test(text)) {
        const whitespace = text.match(/^\s+/)?.[0].length || 0
        start += whitespace
        text = text.slice(whitespace)
      }

      candidates.push({ start, end: start + text.length, kind: token.kind, priority: token.priority })
      if (match[0].length === 0) token.pattern.lastIndex++
    }
  }

  // Resolve overlap by semantic priority first. For example, a URL inside a
  // quoted JSON value should still be recognizable as a URL, while the
  // numbers inside an ISO timestamp should remain part of the timestamp.
  candidates.sort((a, b) => a.priority - b.priority || a.start - b.start || b.end - a.end)

  const accepted: TokenRange[] = []
  for (const candidate of candidates) {
    if (!accepted.some(range => candidate.start < range.end && candidate.end > range.start)) {
      accepted.push(candidate)
    }
  }
  return accepted.sort((a, b) => a.start - b.start)
}

function queryRanges(line: string, query: string): { start: number; end: number }[] {
  const needle = query.trim().toLocaleLowerCase()
  if (!needle) return []

  const haystack = line.toLocaleLowerCase()
  const ranges: { start: number; end: number }[] = []
  let offset = 0
  while (offset <= haystack.length - needle.length) {
    const start = haystack.indexOf(needle, offset)
    if (start === -1) break
    ranges.push({ start, end: start + needle.length })
    offset = start + Math.max(needle.length, 1)
  }
  return ranges
}

/**
 * Tokenizes a log line without generating HTML. The renderer can safely use
 * text interpolation while applying syntax colors and search highlights.
 */
export function highlightLogLine(line: string, query = ''): LogSegment[] {
  const syntax = syntaxRanges(line)
  const matches = queryRanges(line, query)
  const boundaries = new Set([0, line.length])

  for (const range of [...syntax, ...matches]) {
    boundaries.add(range.start)
    boundaries.add(range.end)
  }

  const points = [...boundaries].sort((a, b) => a - b)
  const segments: LogSegment[] = []
  for (let i = 0; i < points.length - 1; i++) {
    const start = points[i]!
    const end = points[i + 1]!
    if (end <= start) continue
    const syntaxRange = syntax.find(range => start >= range.start && end <= range.end)
    const isMatch = matches.some(range => start >= range.start && end <= range.end)
    segments.push({
      text: line.slice(start, end),
      kind: syntaxRange?.kind || 'plain',
      match: isMatch,
    })
  }

  return segments.length ? segments : [{ text: '', kind: 'plain', match: false }]
}
