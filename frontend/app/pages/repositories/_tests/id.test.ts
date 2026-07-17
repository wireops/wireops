import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import { ref } from 'vue'
import RepositoryPage from '../[id].vue'

function setupGlobals(repo: Record<string, any>, customPost = vi.fn().mockResolvedValue({})) {
  for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  ;(globalThis as any).useRoute = () => ({ params: { id: repo.id } })
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: () => ({
        getOne: vi.fn().mockResolvedValue(repo),
      }),
    },
  })

  const addToast = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: addToast })
  ;(globalThis as any).useCopy = () => ({ copy: vi.fn() })
  ;(globalThis as any).useRepositoryPlatform = () => ({
    platformIconUrl: () => null,
    PLATFORM_OPTIONS: [],
  })
  ;(globalThis as any).usePermissions = () => ({ canManageRepos: ref(true) })
  ;(globalThis as any).useApi = () => ({
    getRepoCommits: vi.fn().mockResolvedValue([]),
    customPost,
  })

  ;(globalThis as any).useAsyncData = (_key: string, fn: () => Promise<any>) => {
    const data = ref<any>(null)
    const refresh = async () => { data.value = await fn() }
    refresh()
    return { data, refresh }
  }

  return { addToast, customPost }
}

describe('repositories/[id].vue rotateSopsKey flow', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('requires a second call to confirm before rotating', async () => {
    const repo = { id: 'repo-1', name: 'my-repo', sops_age_public_key: 'age1abc' }
    const { customPost } = setupGlobals(repo)

    const wrapper = mount(RepositoryPage, { shallow: true })
    await flushPromises()

    expect((wrapper.vm as any).confirmingRotate).toBe(false)

    await (wrapper.vm as any).rotateSopsKey()
    expect((wrapper.vm as any).confirmingRotate).toBe(true)
    expect(customPost).not.toHaveBeenCalled()
  })

  it('rotates the key and shows a warning toast on confirm', async () => {
    const repo = { id: 'repo-1', name: 'my-repo', sops_age_public_key: 'age1abc' }
    const { addToast, customPost } = setupGlobals(repo)

    const wrapper = mount(RepositoryPage, { shallow: true })
    await flushPromises()

    ;(wrapper.vm as any).confirmingRotate = true
    await (wrapper.vm as any).rotateSopsKey()
    await flushPromises()

    expect(customPost).toHaveBeenCalledWith('/api/custom/repositories/repo-1/sops-rotate-key')
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'SOPS age key rotated', color: 'warning' }))
    expect((wrapper.vm as any).confirmingRotate).toBe(false)
    expect((wrapper.vm as any).rotating).toBe(false)
  })

  it('shows an error toast when rotation fails', async () => {
    const repo = { id: 'repo-1', name: 'my-repo', sops_age_public_key: 'age1abc' }
    const failingPost = vi.fn().mockRejectedValue({ data: { error: 'server exploded' } })
    const { addToast } = setupGlobals(repo, failingPost)

    const wrapper = mount(RepositoryPage, { shallow: true })
    await flushPromises()

    ;(wrapper.vm as any).confirmingRotate = true
    await (wrapper.vm as any).rotateSopsKey()
    await flushPromises()

    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Rotation failed', description: 'server exploded', color: 'error' }))
    expect((wrapper.vm as any).confirmingRotate).toBe(false)
    expect((wrapper.vm as any).rotating).toBe(false)
  })
})
