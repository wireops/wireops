import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { useServerVersion } from './useServerVersion'

function setupGlobals(canManageSettings: boolean, getSystemInfo: () => Promise<any>) {
  ;(globalThis as any).usePermissions = () => ({ canManageSettings: ref(canManageSettings) })
  ;(globalThis as any).useApi = () => ({ getSystemInfo })
  ;(globalThis as any).useAsyncData = (_key: string, fn: () => Promise<any>) => {
    const data = ref<any>(null)
    ;(async () => { data.value = await fn() })()
    return { data }
  }
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
})
