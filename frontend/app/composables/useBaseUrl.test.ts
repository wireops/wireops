import { describe, expect, it } from 'vitest'
import { resolveBackendBaseUrlFromLocation } from './useBaseUrl'

describe('resolveBackendBaseUrl', () => {
  it('returns the configured backend URL when provided', () => {
    expect(resolveBackendBaseUrlFromLocation('https://wireops.example.com/')).toBe('https://wireops.example.com')
  })

  it('maps local Nuxt dev origin to PocketBase default port', () => {
    expect(resolveBackendBaseUrlFromLocation('', {
      origin: 'http://localhost:3000',
      protocol: 'http:',
      hostname: 'localhost',
      port: '3000',
    })).toBe('http://localhost:8090')
  })

  it('keeps same-origin fallback outside local dev port mapping', () => {
    expect(resolveBackendBaseUrlFromLocation('', {
      origin: 'https://wireops.example.com',
      protocol: 'https:',
      hostname: 'wireops.example.com',
      port: '',
    })).toBe('https://wireops.example.com')
  })
})
