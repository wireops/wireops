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

  const getOne = vi.fn().mockResolvedValue({ id: 'stack-1', name: 'my-stack', repository: null, status: 'active' })
  const updateStack = vi.fn().mockResolvedValue({})
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      baseURL: 'http://test',
      authStore: { token: 'tok' },
      collection: (name: string) => {
        if (name === 'stacks') return { getOne, update: updateStack }
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
  const toastAdd = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: toastAdd })
  ;(globalThis as any).useRepositoryPlatform = () => ({ platformIconUrl: vi.fn() })
  ;(globalThis as any).usePermissions = () => ({ canOperate: ref(true) })

  const api = {
    triggerSync: vi.fn(),
    triggerRollback: vi.fn(),
    forceRedeploy: vi.fn(),
    setRenderOverrides: vi.fn(),
    clearRenderOverrides: vi.fn(),
    getRenderOverridesDiff: vi.fn().mockResolvedValue({ git: {} }),
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
  }
  ;(globalThis as any).useApi = () => api

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

  return { navigateTo, getOne, updateStack, toastAdd, api, asyncDataStore }
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
    const api = { ...globals.api, forceRedeploy }
    ;(globalThis as any).useApi = () => api
    return { ...globals, api }
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

describe('stacks/[id].vue stack operations and override helpers', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('blocks sync while the assigned worker is offline and explains why', async () => {
    const { toastAdd, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore.workers_for_stacks.data.value = [{ id: 'worker-1', status: 'OFFLINE' }]
    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', worker: 'worker-1', status: 'active' }
    await flushPromises()

    ;(wrapper.vm as any).openSyncModal()

    expect((wrapper.vm as any).showSyncModal).toBe(false)
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Sync unavailable', description: expect.stringContaining('Worker offline') }))
  })

  it('triggers a sync online and reports API failures', async () => {
    const { api, toastAdd, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore.workers_for_stacks.data.value = [{ id: 'worker-1', status: 'ACTIVE' }]
    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', worker: 'worker-1', status: 'active' }
    await flushPromises()

    await (wrapper.vm as any).handleSync()
    expect(api.triggerSync).toHaveBeenCalledWith('stack-1')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Sync triggered', color: 'success' })

    api.triggerSync.mockRejectedValueOnce(new Error('worker unavailable'))
    await (wrapper.vm as any).handleSync()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'worker unavailable', color: 'error' })
  })

  it('normalizes, validates, and previews render-time overrides', async () => {
    const { asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore['stack_stack-1'].data.value = {
      id: 'stack-1', name: 'my-stack', status: 'active',
      render_overrides: {
        web: { image: 'nginx:latest', ports: ['8080:80'], networks: ['edge'], scale: 2 },
      },
    }
    await flushPromises()

    expect((wrapper.vm as any).joinList(['one', 'two'])).toBe('one, two')
    expect((wrapper.vm as any).joinList('not-a-list')).toBe('')
    expect((wrapper.vm as any).splitList(' one, , two ')).toEqual(['one', 'two'])
    expect((wrapper.vm as any).splitList(' , ')).toBeUndefined()
    expect((wrapper.vm as any).scaleValidationError('abc')).toBe('Scale must be a whole number')
    expect((wrapper.vm as any).scaleValidationError('101')).toBe('Scale must be between 0 and 100')
    expect((wrapper.vm as any).scaleValidationError('0')).toBe('')

    ;(wrapper.vm as any).overridesForm = { web: { image: '', ports: '', networks: '', scale: '' } }
    ;(wrapper.vm as any).adjustOverrideScale('web', -1)
    expect((wrapper.vm as any).overridesForm.web.scale).toBe('0')
    ;(wrapper.vm as any).adjustOverrideScale('web', 200)
    expect((wrapper.vm as any).overridesForm.web.scale).toBe('100')

    ;(wrapper.vm as any).renderOverridesGit = { web: { image: 'nginx:1.25', ports: ['80:80'], networks: ['internal'], scale: 1 } }
    const lines = (wrapper.vm as any).renderOverridesDiffLines.map((line: any) => line.text)
    expect(lines).toContain('-    image: nginx:1.25')
    expect(lines).toContain('+    image: nginx:latest')
    expect(lines).toContain('+      - 8080:80')
    expect(lines).toContain('+    scale: 2')
  })

  it('applies valid overrides, rejects invalid input, and clears persisted overrides', async () => {
    const { api, toastAdd, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore['stack_stack-1'].data.value = {
      id: 'stack-1', name: 'my-stack', status: 'active',
      containers_list: [{ name: 'web' }],
    }
    await flushPromises()

    ;(wrapper.vm as any).overridesForm = { web: { image: ' nginx:1.25 ', ports: '8080:80', networks: 'edge', scale: '3' } }
    await (wrapper.vm as any).handleApplyOverrides()
    expect(api.setRenderOverrides).toHaveBeenCalledWith('stack-1', { web: { image: 'nginx:1.25', ports: ['8080:80'], networks: ['edge'], scale: 3 } })
    expect((wrapper.vm as any).activeTab).toBe('logs')

    ;(wrapper.vm as any).overridesForm = { web: { image: '', ports: '', networks: '', scale: 'bad' } }
    await (wrapper.vm as any).handleApplyOverrides()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'web: Scale must be a whole number', color: 'warning' })

    ;(wrapper.vm as any).overridesForm = { web: { image: '', ports: '', networks: '', scale: '0' } }
    await (wrapper.vm as any).handleApplyOverrides()
    expect(api.setRenderOverrides).toHaveBeenCalledWith('stack-1', { web: { scale: 0 } })

    ;(wrapper.vm as any).overridesForm = {
      web: { image: '', ports: '', networks: '', scale: '2' },
      stale: { image: 'ghost:latest', ports: '', networks: '', scale: '' },
    }
    await (wrapper.vm as any).handleApplyOverrides()
    expect(api.setRenderOverrides).toHaveBeenCalledWith('stack-1', { web: { scale: 2 } })

    await (wrapper.vm as any).handleClearOverrides()
    expect(api.clearRenderOverrides).toHaveBeenCalledWith('stack-1')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Render overrides cleared, reverting to Git state', color: 'info' })
  })

  it('rolls back, pauses, and resumes a stack through the expected APIs', async () => {
    const { api, updateStack, toastAdd, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', status: 'active' }
    await flushPromises()

    ;(wrapper.vm as any).rollbackSha = 'abc123'
    await (wrapper.vm as any).handleRollback()
    expect(api.triggerRollback).toHaveBeenCalledWith('stack-1', 'abc123')
    expect((wrapper.vm as any).showRollbackModal).toBe(false)
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Rollback triggered — stack will be paused', color: 'warning' })

    await (wrapper.vm as any).togglePause()
    expect((wrapper.vm as any).showPauseModal).toBe(true)
    await (wrapper.vm as any).confirmPause()
    expect(updateStack).toHaveBeenCalledWith('stack-1', { status: 'paused' })

    asyncDataStore['stack_stack-1'].data.value.status = 'paused'
    await (wrapper.vm as any).togglePause()
    expect(updateStack).toHaveBeenLastCalledWith('stack-1', { status: 'active' })
  })

  it('loads running service metrics while ignoring unavailable container stats', async () => {
    const { api } = setupGlobals()
    api.getServices.mockResolvedValue([
      { service_name: 'api', container_id: 'running', status: 'running' },
      { service_name: 'worker', container_id: 'stopped', status: 'exited' },
    ])
    api.getContainerStats.mockResolvedValue({ cpu_percent: 12 })
    const wrapper = await mountPage()

    await (wrapper.vm as any).loadServices()
    await flushPromises()
    expect((wrapper.vm as any).services).toHaveLength(2)
    expect(api.getContainerStats).toHaveBeenCalledWith('stack-1', 'running')
    expect((wrapper.vm as any).containerStats.running).toEqual({ cpu_percent: 12 })

    api.getServices.mockRejectedValueOnce(new Error('offline'))
    await (wrapper.vm as any).loadServices()
    expect((wrapper.vm as any).services).toEqual([])
  })
})
