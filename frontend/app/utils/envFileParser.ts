// Parses the KEY=VALUE textarea format shared by the bulk env-var editor and
// the .env import flow. Deliberately permissive about quoting (a bare value
// round-trips as-is) since the server is the source of truth for what's a
// valid key — this only needs to catch malformed lines before they're sent.

export interface ParsedEnvLine {
  key: string
  value: string
}

export interface EnvFileParseError {
  line: number
  raw: string
  message: string
}

export interface EnvFileParseResult {
  vars: ParsedEnvLine[]
  errors: EnvFileParseError[]
}

const KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/

// A pending (not-yet-persisted) env var row, as edited in a creation wizard
// before the owning stack/job exists — see EnvironmentVariablesPendingEditor.vue.
export interface PendingEnvVar {
  key: string
  value: string
  secret: boolean
  secret_provider: string
}

export function isValidEnvKey(key: string): boolean {
  return KEY_PATTERN.test(key)
}

// Decode quoted input without expanding $VARIABLE expressions. Double quotes
// accept the existing JSON escapes as well as Compose's escaped dollar sign.
function decodeQuoted(value: string, quote: string): string {
  if (quote === "'") return value.replace(/\\'/g, "'")
  return value.replace(/\\(u[0-9a-fA-F]{4}|[\\"/$nrtbf])/g, (_match, escape: string) => {
    if (escape.startsWith('u')) return String.fromCharCode(Number.parseInt(escape.slice(1), 16))
    const escapes: Record<string, string> = { n: '\n', r: '\r', t: '\t', b: '\b', f: '\f' }
    return escapes[escape] ?? escape
  })
}

export function parseEnvFileContent(content: string): EnvFileParseResult {
  const vars: ParsedEnvLine[] = []
  const errors: EnvFileParseError[] = []
  const seen = new Set<string>()
  // Keep CR characters inside quoted values; only trim outside their quotes.
  const lines = content.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i] ?? ''
    const line = i + 1
    const trimmed = raw.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const eq = raw.indexOf('=')
    if (eq === -1) {
      errors.push({ line, raw, message: 'expected KEY=VALUE' })
      continue
    }
    const key = raw.slice(0, eq).trim()
    if (!KEY_PATTERN.test(key)) {
      errors.push({ line, raw, message: `invalid key "${key}"` })
      continue
    }
    let source = raw.slice(eq + 1).trimStart()
    let value = source.trim()
    const quote = source[0]
    if (quote === '"' || quote === "'") {
      let cursor = 1
      let closed = false
      while (!closed) {
        for (; cursor < source.length; cursor++) {
          if (source[cursor] === '\\' && (quote === '"' || source[cursor + 1] === "'")) {
            cursor++
          } else if (source[cursor] === quote) {
            closed = true
            break
          }
        }
        if (closed || i + 1 >= lines.length) break
        source += '\n' + lines[++i]
      }
      if (!closed) {
        errors.push({ line, raw, message: 'unterminated quoted value' })
        continue
      }
      const tail = source.slice(cursor + 1).trim()
      if (tail && !tail.startsWith('#')) {
        errors.push({ line, raw, message: 'unexpected text after quoted value' })
        continue
      }
      value = decodeQuoted(source.slice(1, cursor), quote)
    }
    if (seen.has(key)) {
      errors.push({ line, raw, message: `duplicate key "${key}"` })
      continue
    }
    seen.add(key)
    vars.push({ key, value })
  }
  return { vars, errors }
}

// Serializes vars back to KEY=VALUE lines for prefilling the bulk editor
// textarea. Quotes a value only when it contains characters that would
// otherwise change the parsed result (leading/trailing whitespace, a literal
// newline, or a `#` that could be mistaken for a comment start).
export function serializeEnvLines(vars: ParsedEnvLine[]): string {
  return vars.map(({ key, value }) => {
    const startsOrEndsQuoted = value.length >= 1 && (
      value[0] === '"' || value[0] === '\'' || value[value.length - 1] === '"' || value[value.length - 1] === '\''
    )
    const needsQuotes = value !== value.trim() || /[\r\n]/.test(value) || value.includes('#') || startsOrEndsQuoted
    return `${key}=${needsQuotes ? JSON.stringify(value) : value}`
  }).join('\n')
}
