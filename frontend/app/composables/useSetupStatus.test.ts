import { beforeEach, describe, expect, it, vi } from 'vitest'

describe('useSetupStatus', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.useRealTimers()
    ;(globalThis as any).useRuntimeConfig = () => ({
      public: {
        pocketbaseUrl: '',
      },
    })
  })

  it('caches setup status responses until invalidated', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ needsSetup: true, setupAllowed: true, reason: '', requiresBootstrapToken: true })
      .mockResolvedValueOnce({ needsSetup: false, setupAllowed: false, reason: 'already_configured', requiresBootstrapToken: false })

    ;(globalThis as any).$fetch = fetchMock

    const mod = await import('./useSetupStatus')

    const first = await mod.getInstanceSetupStatus()
    const second = await mod.getInstanceSetupStatus()

    expect(first?.needsSetup).toBe(true)
    expect(second?.needsSetup).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    mod.invalidateInstanceSetupStatus()

    const third = await mod.getInstanceSetupStatus()
    expect(third?.needsSetup).toBe(false)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
