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

  it('ignores stale inflight responses after invalidation', async () => {
    let resolveFirst: ((value: any) => void) | undefined
    let resolveSecond: ((value: any) => void) | undefined

    const fetchMock = vi.fn()
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveSecond = resolve }))

    ;(globalThis as any).$fetch = fetchMock

    const mod = await import('./useSetupStatus')

    const firstRequest = mod.getInstanceSetupStatus()
    mod.invalidateInstanceSetupStatus()
    const secondRequest = mod.getInstanceSetupStatus()

    resolveFirst?.({ needsSetup: true, setupAllowed: true, reason: '', requiresBootstrapToken: true })
    await firstRequest

    resolveSecond?.({ needsSetup: false, setupAllowed: false, reason: 'already_configured', requiresBootstrapToken: false })
    const secondResult = await secondRequest

    expect(secondResult?.needsSetup).toBe(false)
    expect(fetchMock).toHaveBeenCalledTimes(2)

    const thirdRequest = await mod.getInstanceSetupStatus()
    expect(thirdRequest?.needsSetup).toBe(false)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('keeps the post-invalidation result when an older request resolves later', async () => {
    let resolveFirst: ((value: any) => void) | undefined
    let resolveSecond: ((value: any) => void) | undefined

    const fetchMock = vi.fn()
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveSecond = resolve }))
      .mockResolvedValueOnce({ needsSetup: true, setupAllowed: true, reason: 'unexpected_refetch', requiresBootstrapToken: true })

    ;(globalThis as any).$fetch = fetchMock

    const mod = await import('./useSetupStatus')

    const firstRequest = mod.getInstanceSetupStatus()
    mod.invalidateInstanceSetupStatus()
    const secondRequest = mod.getInstanceSetupStatus()

    resolveSecond?.({ needsSetup: false, setupAllowed: false, reason: 'already_configured', requiresBootstrapToken: false })
    const secondResult = await secondRequest

    resolveFirst?.({ needsSetup: true, setupAllowed: true, reason: '', requiresBootstrapToken: true })
    const firstResult = await firstRequest

    expect(secondResult?.needsSetup).toBe(false)
    expect(firstResult?.needsSetup).toBe(true)

    const cachedResult = await mod.getInstanceSetupStatus()
    expect(cachedResult?.needsSetup).toBe(false)
    expect(cachedResult?.reason).toBe('already_configured')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
