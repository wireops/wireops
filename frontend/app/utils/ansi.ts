// Minimal ANSI SGR (Select Graphic Rendition) parser for docker/compose CLI
// output. Only color/bold/reset codes are interpreted — cursor movement,
// clear-line, and other non-SGR CSI sequences are stripped since they have
// no meaning in a static, line-buffered log view.
export interface AnsiSegment {
  text: string
  color?: string
  bold?: boolean
}

// Classic 16-color xterm palette, tuned for readability on a fixed dark
// terminal background (our live-output panel doesn't follow light/dark
// theme, so these values don't need a light-mode counterpart).
const ANSI_COLORS: Record<number, string> = {
  30: '#6b7280', // black -> mid gray (true black is invisible on a dark bg)
  31: '#f87171',
  32: '#4ade80',
  33: '#facc15',
  34: '#60a5fa',
  35: '#e879f9',
  36: '#22d3ee',
  37: '#e5e7eb',
  90: '#9ca3af',
  91: '#fca5a5',
  92: '#86efac',
  93: '#fde047',
  94: '#93c5fd',
  95: '#f0abfc',
  96: '#67e8f9',
  97: '#f9fafb',
}

// Matches CSI sequences in general (ESC [ ... <final byte>), so unsupported
// ones (cursor moves, clear-line, etc.) are still consumed and dropped
// instead of leaking raw escape bytes into the rendered text.
// eslint-disable-next-line no-control-regex -- the ESC (0x1b) control char is the CSI sequence itself, not accidental
const CSI_SEQUENCE = /\x1b\[([0-9;]*)([a-zA-Z])/g

/**
 * Splits a single line of ANSI-colored text into styled segments. Non-SGR
 * CSI sequences are discarded; unrecognized SGR codes are ignored (segment
 * keeps whatever color/bold state was already active).
 */
export function parseAnsiLine(line: string): AnsiSegment[] {
  const segments: AnsiSegment[] = []
  let color: string | undefined
  let bold = false
  let lastIndex = 0

  CSI_SEQUENCE.lastIndex = 0
  let match: RegExpExecArray | null
  while ((match = CSI_SEQUENCE.exec(line)) !== null) {
    const text = line.slice(lastIndex, match.index)
    if (text) segments.push({ text, color, bold })
    lastIndex = CSI_SEQUENCE.lastIndex

    const [, params, final] = match
    if (final === 'm') {
      const codes = params ? params.split(';').map(Number) : [0]
      for (const code of codes) {
        if (code === 0) {
          color = undefined
          bold = false
        } else if (code === 1) {
          bold = true
        } else if (code === 22) {
          bold = false
        } else if (code === 39) {
          color = undefined
        } else if (ANSI_COLORS[code]) {
          color = ANSI_COLORS[code]
        }
      }
    }
    // Non-'m' CSI sequences (cursor/clear/etc.) are simply dropped.
  }

  const rest = line.slice(lastIndex)
  if (rest || segments.length === 0) segments.push({ text: rest, color, bold })

  return segments
}
