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

function unquote(value: string): string {
  const trimmed = value.trim()
  if (trimmed.length >= 2) {
    const first = trimmed[0]
    const last = trimmed[trimmed.length - 1]
    // Double-quoted values are serialized via JSON.stringify (so escapes
    // like a literal newline become `\n`) — decode with JSON.parse to
    // reverse that, not a plain slice, or escape sequences round-trip as
    // literal backslash text instead of the original character.
    if (first === '"' && last === '"') {
      try {
        return JSON.parse(trimmed)
      } catch {
        return trimmed.slice(1, -1)
      }
    }
    if (first === '\'' && last === '\'') {
      return trimmed.slice(1, -1)
    }
  }
  return trimmed
}

// Parses KEY=VALUE lines. Blank lines and lines starting with # are skipped.
// A line with no `=` or a key that isn't a valid env var identifier is
// reported as an error rather than silently dropped, so the caller can show
// it to the user instead of writing a truncated set of vars.
export function parseEnvFileContent(content: string): EnvFileParseResult {
  const vars: ParsedEnvLine[] = []
  const errors: EnvFileParseError[] = []
  const seen = new Set<string>()

  const lines = content.split(/\r?\n/)
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i] ?? ''
    const trimmed = raw.trim()
    if (trimmed === '' || trimmed.startsWith('#')) continue

    const eq = trimmed.indexOf('=')
    if (eq === -1) {
      errors.push({ line: i + 1, raw, message: 'expected KEY=VALUE' })
      continue
    }

    const key = trimmed.slice(0, eq).trim()
    const value = unquote(trimmed.slice(eq + 1))

    if (!KEY_PATTERN.test(key)) {
      errors.push({ line: i + 1, raw, message: `invalid key "${key}"` })
      continue
    }
    if (seen.has(key)) {
      errors.push({ line: i + 1, raw, message: `duplicate key "${key}"` })
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
    const needsQuotes = value !== value.trim() || value.includes('\n') || value.includes('#')
    return `${key}=${needsQuotes ? JSON.stringify(value) : value}`
  }).join('\n')
}
