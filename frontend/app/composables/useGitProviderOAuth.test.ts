import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

function makePopup() {
  return { closed: false, close: vi.fn() }
}

describe('useGitProviderOAuth', () => {
  let getGitProviderAuthorizeUrl: ReturnType<typeof vi.fn>
  let windowOpenSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.resetModules()
    getGitProviderAuthorizeUrl = vi.fn().mockResolvedValue({ url: 'https://github.com/login/oauth/authorize' })
    ;(globalThis as any).useApi = () => ({ getGitProviderAuthorizeUrl })
  })

  afterEach(() => {
    windowOpenSpy?.mockRestore()
    vi.useRealTimers()
  })

  it('resolves the credentials when the popup posts a matching success message', async () => {
    const popup = makePopup()
    windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(popup as any)

    const { useGitProviderOAuth } = await import('./useGitProviderOAuth')
    const { connect } = useGitProviderOAuth()

    const resultPromise = connect('github')
    await vi.waitFor(() => expect(windowOpenSpy).toHaveBeenCalled())
    window.dispatchEvent(new MessageEvent('message', {
      source: popup as any,
      data: { type: 'wireops-git-oauth', ok: true, keyId: 'key-1', login: 'octocat' },
    }))

    await expect(resultPromise).resolves.toEqual({ keyId: 'key-1', login: 'octocat' })
    expect(popup.close).toHaveBeenCalledTimes(1)
    expect(getGitProviderAuthorizeUrl).toHaveBeenCalledWith('github')
  })

  it('resolves null immediately when the popup is blocked', async () => {
    windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(null)

    const { useGitProviderOAuth } = await import('./useGitProviderOAuth')
    const { connect } = useGitProviderOAuth()

    await expect(connect('github')).resolves.toBeNull()
  })

  it('ignores messages from other windows and message types', async () => {
    const popup = makePopup()
    const otherWindow = {}
    windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(popup as any)

    const { useGitProviderOAuth } = await import('./useGitProviderOAuth')
    const { connect } = useGitProviderOAuth()

    const resultPromise = connect('github')
    await vi.waitFor(() => expect(windowOpenSpy).toHaveBeenCalled())
    window.dispatchEvent(new MessageEvent('message', {
      source: otherWindow as any,
      data: { type: 'wireops-git-oauth', ok: true, keyId: 'key-1', login: 'octocat' },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      source: popup as any,
      data: { type: 'some-other-message' },
    }))
    window.dispatchEvent(new MessageEvent('message', {
      source: popup as any,
      data: { type: 'wireops-git-oauth', ok: false },
    }))

    await expect(resultPromise).resolves.toBeNull()
  })

  it('resolves null when the popup is closed manually before completing the flow', async () => {
    vi.useFakeTimers()
    const popup = makePopup()
    windowOpenSpy = vi.spyOn(window, 'open').mockReturnValue(popup as any)

    const { useGitProviderOAuth } = await import('./useGitProviderOAuth')
    const { connect } = useGitProviderOAuth()

    const resultPromise = connect('github')
    await Promise.resolve()
    await Promise.resolve()
    popup.closed = true
    await vi.advanceTimersByTimeAsync(500)

    await expect(resultPromise).resolves.toBeNull()
  })
})
