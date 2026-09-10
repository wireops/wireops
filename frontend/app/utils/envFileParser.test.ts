import { describe, it, expect } from 'vitest'
import { parseEnvFileContent, serializeEnvLines } from './envFileParser'

describe('parseEnvFileContent', () => {
  it('imports quoted multiline JSON, preserving literal private-key escapes', () => {
    const value = '{\n  "type": "service_account",\n  "private_key": "FAKE\\nKEY\\n"\n}\n'
    const { vars, errors } = parseEnvFileContent(`GCP='${value}'\nNEXT=ok`)
    expect(errors).toEqual([])
    expect(vars).toEqual([{ key: 'GCP', value }, { key: 'NEXT', value: 'ok' }])
    expect(JSON.parse(vars[0]!.value).private_key).toBe('FAKE\nKEY\n')
  })

  it('preserves CRLF and blank lines inside quotes and accepts closing comments', () => {
    expect(parseEnvFileContent('A="first\r\n\r\nlast\r\n" # comment\r\nB=ok').vars)
      .toEqual([{ key: 'A', value: 'first\r\n\r\nlast\r\n' }, { key: 'B', value: 'ok' }])
  })

  it('decodes escapes once without interpolating dollar expressions', () => {
    const input = String.raw`A="line\nnext\\n\t\"\$TOKEN \u0041"`
    expect(parseEnvFileContent(input)).toEqual({ vars: [{ key: 'A', value: 'line\nnext\\n\t"$TOKEN A' }], errors: [] })
    expect(parseEnvFileContent(String.raw`A='Let\'s keep \n literal'`).vars[0]!.value).toBe("Let's keep \\n literal")
  })

  it('reports an unterminated value at its opening line without importing its fragments', () => {
    const result = parseEnvFileContent('GOOD=ok\nBAD="first\nFAKE=value')
    expect(result.vars).toEqual([{ key: 'GOOD', value: 'ok' }])
    expect(result.errors).toMatchObject([{ line: 2, message: 'unterminated quoted value' }])
  })

  it('retains physical line numbers after multiline values', () => {
    expect(parseEnvFileContent('A="first\nlast"\nA=duplicate').errors).toMatchObject([{ line: 3, message: 'duplicate key "A"' }])
    expect(parseEnvFileContent('A="ok" garbage').errors).toMatchObject([{ line: 1, message: 'unexpected text after quoted value' }])
  })
  it('parses KEY=value lines', () => {
    const { vars, errors } = parseEnvFileContent('FOO=bar\nBAZ=qux')
    expect(errors).toEqual([])
    expect(vars).toEqual([{ key: 'FOO', value: 'bar' }, { key: 'BAZ', value: 'qux' }])
  })

  it('unquotes single and double quoted values', () => {
    const { vars } = parseEnvFileContent('A="hello world"\nB=\'single quoted\'')
    expect(vars).toEqual([{ key: 'A', value: 'hello world' }, { key: 'B', value: 'single quoted' }])
  })

  it('ignores blank lines and #-comments', () => {
    const { vars, errors } = parseEnvFileContent('# a comment\n\nFOO=bar\n   \n# another\nBAZ=qux')
    expect(errors).toEqual([])
    expect(vars).toEqual([{ key: 'FOO', value: 'bar' }, { key: 'BAZ', value: 'qux' }])
  })

  it('handles an empty value', () => {
    const { vars, errors } = parseEnvFileContent('EMPTY=')
    expect(errors).toEqual([])
    expect(vars).toEqual([{ key: 'EMPTY', value: '' }])
  })

  it('reports a malformed line with no =', () => {
    const { vars, errors } = parseEnvFileContent('NOVALUE')
    expect(vars).toEqual([])
    expect(errors).toEqual([{ line: 1, raw: 'NOVALUE', message: 'expected KEY=VALUE' }])
  })

  it('reports an invalid key', () => {
    const { vars, errors } = parseEnvFileContent('1BAD=x')
    expect(vars).toEqual([])
    expect(errors).toHaveLength(1)
    expect(errors[0]!.message).toContain('invalid key')
  })

  it('reports a duplicate key', () => {
    const { vars, errors } = parseEnvFileContent('FOO=1\nFOO=2')
    expect(vars).toEqual([{ key: 'FOO', value: '1' }])
    expect(errors).toHaveLength(1)
    expect(errors[0]!.message).toContain('duplicate key')
  })

  it('trims whitespace around key and unquoted value', () => {
    const { vars } = parseEnvFileContent('  FOO  =  bar  ')
    expect(vars).toEqual([{ key: 'FOO', value: 'bar' }])
  })
})

describe('serializeEnvLines', () => {
  it.each(['\r', '\r\n', '\n\n', '  first\nlast  ', '"quotes"\n\\literal\\n $DOLLAR', "single'\nquote"])(
    'round-trips special multiline content %j', (value) => {
      const vars = [{ key: 'VALUE', value }]
      expect(parseEnvFileContent(serializeEnvLines(vars))).toEqual({ vars, errors: [] })
    },
  )
  it('round-trips plain values', () => {
    const content = serializeEnvLines([{ key: 'FOO', value: 'bar' }])
    expect(content).toBe('FOO=bar')
    expect(parseEnvFileContent(content).vars).toEqual([{ key: 'FOO', value: 'bar' }])
  })

  it('quotes a value with leading/trailing whitespace so it round-trips', () => {
    const content = serializeEnvLines([{ key: 'FOO', value: '  spaced  ' }])
    const { vars, errors } = parseEnvFileContent(content)
    expect(errors).toEqual([])
    expect(vars).toEqual([{ key: 'FOO', value: '  spaced  ' }])
  })

  it('round-trips a value containing a newline', () => {
    const content = serializeEnvLines([{ key: 'FOO', value: 'line1\nline2' }])
    const { vars, errors } = parseEnvFileContent(content)
    expect(errors).toEqual([])
    expect(vars).toEqual([{ key: 'FOO', value: 'line1\nline2' }])
  })

  it('round-trips a value containing a #', () => {
    const content = serializeEnvLines([{ key: 'FOO', value: 'a#b' }])
    const { vars, errors } = parseEnvFileContent(content)
    expect(errors).toEqual([])
    expect(vars).toEqual([{ key: 'FOO', value: 'a#b' }])
  })

  it('round-trips a value that already starts/ends with a quote character', () => {
    const content = serializeEnvLines([{ key: 'FOO', value: '"quoted"' }])
    const { vars, errors } = parseEnvFileContent(content)
    expect(errors).toEqual([])
    expect(vars).toEqual([{ key: 'FOO', value: '"quoted"' }])
  })
})
