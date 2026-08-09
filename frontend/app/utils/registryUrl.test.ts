import { describe, expect, it } from 'vitest'
import { isValidRegistryUrl } from './registryUrl'

describe('isValidRegistryUrl', () => {
  it.each([
    'ghcr.io',
    'docker.io',
    'registry.example.com',
    'registry.example.com:5000',
    'localhost:5000',
    'https://ghcr.io',
    'https://registry.example.com:5000',
    'http://localhost:5000',
    '  ghcr.io  ',
  ])('accepts %s', (value) => {
    expect(isValidRegistryUrl(value)).toBe(true)
  })

  it.each([
    '',
    '   ',
    'not a valid url',
    'https://',
    '://ghcr.io',
    'ghcr.io with spaces',
    '   ',
  ])('rejects %s', (value) => {
    expect(isValidRegistryUrl(value)).toBe(false)
  })
})
