import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import GitlabConfigModal from './GitlabConfigModal.vue'

function setupGlobals(overrides: {
  listGitProviders?: ReturnType<typeof vi.fn>
  testGitlabIntegration?: ReturnType<typeof vi.fn>
} = {}) {
  for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive']) {
    vi.stubGlobal(key, (vue as any)[key])
  }

  const addToast = vi.fn()
  const listGitProviders = overrides.listGitProviders
    ?? vi.fn().mockResolvedValue([{ slug: 'gitlab', connected: true, account_login: 'octocat' }])
  const testGitlabIntegration = overrides.testGitlabIntegration
    ?? vi.fn().mockResolvedValue({ success: 'true' })

  vi.stubGlobal('useToast', () => ({ add: addToast }))
  vi.stubGlobal('useApi', () => ({ listGitProviders }))
  vi.stubGlobal('useIntegrations', () => ({ testGitlabIntegration }))

  return { addToast, listGitProviders, testGitlabIntegration }
}

describe('GitlabConfigModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('refreshStatus reflects a connected gitlab provider when opened', async () => {
    const { listGitProviders } = setupGlobals()

    const wrapper = mount(GitlabConfigModal, {
      props: { integration: { enabled: true }, open: false },
      shallow: true,
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(listGitProviders).toHaveBeenCalled()
    expect((wrapper.vm as any).connected).toBe(true)
    expect((wrapper.vm as any).accountLogin).toBe('octocat')
  })

  it('refreshStatus falls back to disconnected when the providers request fails', async () => {
    setupGlobals({ listGitProviders: vi.fn().mockRejectedValue(new Error('network error')) })

    const wrapper = mount(GitlabConfigModal, {
      props: { integration: { enabled: true }, open: false },
      shallow: true,
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect((wrapper.vm as any).connected).toBe(false)
    expect((wrapper.vm as any).loading).toBe(false)
  })

  it('testConnection shows a success toast on a successful test', async () => {
    const { addToast, testGitlabIntegration } = setupGlobals()

    const wrapper = mount(GitlabConfigModal, {
      props: { integration: { enabled: true }, open: true },
      shallow: true,
    })
    await flushPromises()

    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(testGitlabIntegration).toHaveBeenCalled()
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Connection successful', color: 'success' }))
  })

  it('testConnection shows a failure toast when testGitlabIntegration resolves an unsuccessful result', async () => {
    const { addToast } = setupGlobals({
      testGitlabIntegration: vi.fn().mockResolvedValue({ success: 'false', error: 'bad credentials' }),
    })

    const wrapper = mount(GitlabConfigModal, {
      props: { integration: { enabled: true }, open: true },
      shallow: true,
    })
    await flushPromises()

    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Connection failed', description: 'bad credentials', color: 'error' }))
    expect((wrapper.vm as any).testing).toBe(false)
  })

  it('testConnection shows an error toast when testGitlabIntegration rejects', async () => {
    const { addToast } = setupGlobals({ testGitlabIntegration: vi.fn().mockRejectedValue(new Error('boom')) })

    const wrapper = mount(GitlabConfigModal, {
      props: { integration: { enabled: true }, open: true },
      shallow: true,
    })
    await flushPromises()

    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Connection failed', description: 'boom', color: 'error' }))
    expect((wrapper.vm as any).testing).toBe(false)
  })
})
