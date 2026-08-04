import { describe, it, expect } from 'vitest'
import { parseEnvFileContent, serializeEnvLines } from './envFileParser'

describe('parseEnvFileContent', () => {
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
})
