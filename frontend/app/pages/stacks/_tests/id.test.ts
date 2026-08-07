import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import { ref } from 'vue'
import StackPage from '../[id].vue'

function setupGlobals() {
  // Nuxt auto-imports Vue's reactivity API globally; vitest doesn't, so the
  // page's <script setup> (which relies on that auto-import) needs it here.
  for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive', 'toRef', 'toRefs', 'shallowRef', 'unref']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  ;(globalThis as any).WORKER_STATUS = { ACTIVE: 'ACTIVE', OFFLINE: 'OFFLINE', REVOKED: 'REVOKED', PENDING: 'PENDING' }

  ;(globalThis as any).useRoute = () => ({ params: { id: 'stack-1' } })
  const navigateTo = vi.fn()
  ;(globalThis as any).navigateTo = navigateTo

  const getOne = vi.fn().mockResolvedValue({ id: 'stack-1', name: 'my-stack', repository: null })
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      baseURL: 'http://test',
      authStore: { token: 'tok' },
      collection: (name: string) => {
        if (name === 'stacks') return { getOne }
        if (name === 'sync_logs') return { getList: vi.fn().mockResolvedValue({ items: [] }) }
        return {}
      },
    },
  })

  ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
  ;(globalThis as any).useDeployStream = () => ({ lines: ref([]), connected: ref(false), error: ref(null) })
  ;(globalThis as any).useCopy = () => ({ copy: vi.fn() })
  ;(globalThis as any).useIntegrations = () => ({ getStackIntegrationActions: vi.fn().mockResolvedValue({}) })
  ;(globalThis as any).useValidation = () => ({ validateComposePath: vi.fn().mockReturnValue(''), validateComposeFile: vi.fn().mockReturnValue('') })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  ;(globalThis as any).useRepositoryPlatform = () => ({ platformIconUrl: vi.fn() })
  ;(globalThis as any).usePermissions = () => ({ canOperate: ref(true) })

  ;(globalThis as any).useApi = () => ({
    triggerSync: vi.fn(),
    triggerRollback: vi.fn(),
    forceRedeploy: vi.fn(),
    deleteStack: vi.fn(),
    getServices: vi.fn().mockResolvedValue([]),
    getComposeFile: vi.fn(),
    getWebhookUrl: vi.fn().mockResolvedValue(''),
    getContainerStats: vi.fn(),
    getContainerLogs: vi.fn(),
    getRepoCommits: vi.fn().mockResolvedValue([]),
    transferStack: vi.fn(),
    getWorkers: vi.fn().mockResolvedValue([]),
    stopContainer: vi.fn(),
    restartContainer: vi.fn(),
  })

  // Minimal useAsyncData stand-in: fetches immediately like Nuxt's real one
  // (immediate: true by default), and exposes each call's data/error refs
  // keyed by cache key so tests can drive them directly.
  const asyncDataStore: Record<string, { data: any, error: any, refresh: () => Promise<void> }> = {}
  ;(globalThis as any).useAsyncData = (key: string, fn: () => Promise<any>) => {
    const data = ref<any>(null)
    const error = ref<any>(null)
    const refresh = async () => {
      try {
        data.value = await fn()
        error.value = null
      } catch (e) {
        error.value = e
      }
    }
    refresh()
    const entry = { data, error, refresh }
    asyncDataStore[key] = entry
    return entry
  }

  return { navigateTo, getOne, asyncDataStore }
}

async function mountPage() {
  const wrapper = mount(StackPage, { shallow: true })
  await flushPromises()
  return wrapper
}

describe('stacks/[id].vue deferred 404 handling', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('navigates away immediately on a 404 while the delete modal is closed', async () => {
    const { navigateTo, asyncDataStore } = setupGlobals()
    await mountPage()

    asyncDataStore['stack_stack-1'].error.value = new Error('not found')
    await flushPromises()

    expect(navigateTo).toHaveBeenCalledWith('/stacks')
  })

  it('defers navigation on a 404 while the delete modal is open', async () => {
    const { navigateTo, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    ;(wrapper.vm as any).showDeleteModal = true
    await flushPromises()

    asyncDataStore['stack_stack-1'].error.value = new Error('not found')
    await flushPromises()

    expect(navigateTo).not.toHaveBeenCalled()
  })

  it('resumes navigating on the next 404 once the delete modal has closed', async () => {
    const { navigateTo, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    ;(wrapper.vm as any).showDeleteModal = true
    await flushPromises()

    asyncDataStore['stack_stack-1'].error.value = new Error('not found')
    await flushPromises()
    expect(navigateTo).not.toHaveBeenCalled()

    ;(wrapper.vm as any).showDeleteModal = false
    // A fresh background refetch 404s again once the modal is closed.
    asyncDataStore['stack_stack-1'].error.value = new Error('not found (again)')
    await flushPromises()

    expect(navigateTo).toHaveBeenCalledWith('/stacks')
  })

  it('navigates away when the delete modal is dismissed without a new error firing', async () => {
    const { navigateTo, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    ;(wrapper.vm as any).showDeleteModal = true
    await flushPromises()

    // Stack 404s in the background (e.g. deleted elsewhere) while the modal
    // is open — navigation is deferred.
    asyncDataStore['stack_stack-1'].error.value = new Error('not found')
    await flushPromises()
    expect(navigateTo).not.toHaveBeenCalled()

    // User cancels the modal instead of completing a delete — stackError
    // itself never changes again, only showDeleteModal does.
    ;(wrapper.vm as any).showDeleteModal = false
    await flushPromises()

    expect(navigateTo).toHaveBeenCalledWith('/stacks')
  })
})

describe('stacks/[id].vue force redeploy pause toggle', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function setupGlobalsWithForceRedeploy(forceRedeploy: ReturnType<typeof vi.fn>) {
    const globals = setupGlobals()
    ;(globalThis as any).useApi = () => ({
      triggerSync: vi.fn(),
      triggerRollback: vi.fn(),
      forceRedeploy,
      deleteStack: vi.fn(),
      getServices: vi.fn().mockResolvedValue([]),
      getComposeFile: vi.fn(),
      getWebhookUrl: vi.fn().mockResolvedValue(''),
      getContainerStats: vi.fn(),
      getContainerLogs: vi.fn(),
      getRepoCommits: vi.fn().mockResolvedValue([]),
      transferStack: vi.fn(),
      getWorkers: vi.fn().mockResolvedValue([]),
      stopContainer: vi.fn(),
      restartContainer: vi.fn(),
    })
    return globals
  }

  it('defaults pauseAfterRedeploy to true and sends it on a successful redeploy', async () => {
    const forceRedeploy = vi.fn().mockResolvedValue({})
    setupGlobalsWithForceRedeploy(forceRedeploy)
    const wrapper = await mountPage()

    expect((wrapper.vm as any).pauseAfterRedeploy).toBe(true)

    await (wrapper.vm as any).handleForceRedeploy()
    await flushPromises()

    expect(forceRedeploy).toHaveBeenCalledWith('stack-1', expect.objectContaining({ pause_after_redeploy: true }))
  })

  it('sends pause_after_redeploy: false when unchecked, then resets to true after success', async () => {
    const forceRedeploy = vi.fn().mockResolvedValue({})
    setupGlobalsWithForceRedeploy(forceRedeploy)
    const wrapper = await mountPage()

    ;(wrapper.vm as any).pauseAfterRedeploy = false
    await (wrapper.vm as any).handleForceRedeploy()
    await flushPromises()

    expect(forceRedeploy).toHaveBeenCalledWith('stack-1', expect.objectContaining({ pause_after_redeploy: false }))
    expect((wrapper.vm as any).pauseAfterRedeploy).toBe(true)
  })
})
