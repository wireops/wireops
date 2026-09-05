import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { useServerVersion } from './useServerVersion'

function setupGlobals(canManageSettings: boolean, getSystemInfo: () => Promise<any>) {
  vi.stubGlobal('usePermissions', () => ({ canManageSettings: ref(canManageSettings) }))
  vi.stubGlobal('useApi', () => ({ getSystemInfo }))
  vi.stubGlobal('useAsyncData', (_key: string, fn: () => Promise<any>) => {
    const data = ref<any>(null)
    ;(async () => { data.value = await fn() })()
    return { data }
  })
}

describe('useServerVersion', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the server version when the caller can manage settings', async () => {
    setupGlobals(true, vi.fn().mockResolvedValue({ version: '1.5.0' }))
    const { serverVersion } = useServerVersion()
    await flushPromises()
    expect(serverVersion.value).toBe('1.5.0')
  })

  it('does not fetch and stays null when the caller lacks manage-settings permission', async () => {
    const getSystemInfo = vi.fn().mockResolvedValue({ version: '1.5.0' })
    setupGlobals(false, getSystemInfo)
    const { serverVersion } = useServerVersion()
    await flushPromises()
    expect(getSystemInfo).not.toHaveBeenCalled()
    expect(serverVersion.value).toBeNull()
  })

  it('degrades to null instead of throwing when the request fails', async () => {
    setupGlobals(true, vi.fn().mockRejectedValue(new Error('network error')))
    const { serverVersion } = useServerVersion()
    await flushPromises()
    expect(serverVersion.value).toBeNull()
  })

  it('treats a missing version field as null', async () => {
    setupGlobals(true, vi.fn().mockResolvedValue({ version: '' }))
    const { serverVersion } = useServerVersion()
    await flushPromises()
    expect(serverVersion.value).toBeNull()
  })

  it('does not leak a cached authorized version to a later unauthorized caller', async () => {
    // Real useAsyncData shares its cache by key across every caller in the
    // app - unlike setupGlobals()'s per-call ref, this mock reuses the same
    // entry for a repeated key so the test can catch a stale-permission leak.
    const canManageSettingsRef = ref(true)
    const getSystemInfo = vi.fn().mockResolvedValue({ version: '1.5.0' })
    vi.stubGlobal('usePermissions', () => ({ canManageSettings: canManageSettingsRef }))
    vi.stubGlobal('useApi', () => ({ getSystemInfo }))
    const cache = new Map<string, { data: ReturnType<typeof ref> }>()
    vi.stubGlobal('useAsyncData', (key: string, fn: () => Promise<any>) => {
      const cached = cache.get(key)
      if (cached) return cached
      const entry = { data: ref<any>(null) }
      cache.set(key, entry)
      ;(async () => { entry.data.value = await fn() })()
      return entry
    })

    const authorized = useServerVersion()
    await flushPromises()
    expect(authorized.serverVersion.value).toBe('1.5.0')

    canManageSettingsRef.value = false
    const unauthorized = useServerVersion()
    await flushPromises()
    expect(unauthorized.serverVersion.value).toBeNull()
  })
})
