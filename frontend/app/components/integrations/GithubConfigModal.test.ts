import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import GithubConfigModal from './GithubConfigModal.vue'

function setupGlobals(overrides: {
  listGitProviders?: ReturnType<typeof vi.fn>
  testGithubIntegration?: ReturnType<typeof vi.fn>
} = {}) {
  for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  const addToast = vi.fn()
  const listGitProviders = overrides.listGitProviders
    ?? vi.fn().mockResolvedValue([{ slug: 'github', connected: true, account_login: 'octocat' }])
  const testGithubIntegration = overrides.testGithubIntegration
    ?? vi.fn().mockResolvedValue({ success: 'true' })

  ;(globalThis as any).useToast = () => ({ add: addToast })
  ;(globalThis as any).useApi = () => ({ listGitProviders })
  ;(globalThis as any).useIntegrations = () => ({ testGithubIntegration })

  return { addToast, listGitProviders, testGithubIntegration }
}

describe('GithubConfigModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('refreshStatus reflects a connected github provider when opened', async () => {
    const { listGitProviders } = setupGlobals()

    const wrapper = mount(GithubConfigModal, {
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

    const wrapper = mount(GithubConfigModal, {
      props: { integration: { enabled: true }, open: false },
      shallow: true,
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect((wrapper.vm as any).connected).toBe(false)
    expect((wrapper.vm as any).loading).toBe(false)
  })

  it('testConnection shows a success toast on a successful test', async () => {
    const { addToast, testGithubIntegration } = setupGlobals()

    const wrapper = mount(GithubConfigModal, {
      props: { integration: { enabled: true }, open: true },
      shallow: true,
    })
    await flushPromises()

    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(testGithubIntegration).toHaveBeenCalled()
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Connection successful', color: 'success' }))
  })

  it('testConnection shows an error toast when testGithubIntegration rejects', async () => {
    const { addToast } = setupGlobals({ testGithubIntegration: vi.fn().mockRejectedValue(new Error('boom')) })

    const wrapper = mount(GithubConfigModal, {
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
