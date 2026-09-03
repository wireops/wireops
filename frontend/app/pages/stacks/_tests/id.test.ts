import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import { h, ref } from 'vue'
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
  const asyncDataStore: Record<string, { data: any, error: any, refresh: ReturnType<typeof vi.fn> }> = {}
  ;(globalThis as any).useAsyncData = (key: string, fn: () => Promise<any>) => {
    const data = ref<any>(null)
    const error = ref<any>(null)
    const refresh = vi.fn(async () => {
      try {
        data.value = await fn()
        error.value = null
      } catch (e) {
        error.value = e
      }
    })
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

  function mountPageWithServicesCardSpy() {
    const refreshSpy = vi.fn()
    const wrapper = mount(StackPage, {
      shallow: true,
      global: {
        stubs: {
          StackServicesCard: {
            setup(_props: unknown, { expose }: { expose: (exposed: Record<string, unknown>) => void }) {
              expose({ refresh: refreshSpy })
              return () => h('div')
            },
          },
        },
      },
    })
    return { wrapper, refreshSpy }
  }

  it('reloads the stack, services card, and render-overrides diff once an in-flight sync finishes', async () => {
    const { api, getOne, asyncDataStore } = setupGlobals()
    const { refreshSpy } = mountPageWithServicesCardSpy()
    await flushPromises()
    asyncDataStore['stack_stack-1'].data.value = {
      id: 'stack-1', name: 'my-stack', status: 'active', render_overrides: { web: { scale: 2 } },
    }
    await flushPromises()

    const getOneCallsBefore = getOne.mock.calls.length
    const refreshSpyCallsBefore = refreshSpy.mock.calls.length
    const getRenderOverridesDiffCallsBefore = api.getRenderOverridesDiff.mock.calls.length

    // Sync starts: a sync_logs row with status 'running' appears.
    asyncDataStore['logs_stack-1'].data.value = { items: [{ id: 'log-1', status: 'running' }] }
    await flushPromises()

    expect(getOne.mock.calls.length).toBe(getOneCallsBefore)
    expect(refreshSpy.mock.calls.length).toBe(refreshSpyCallsBefore)

    // Sync finishes: the same row settles to a terminal status.
    asyncDataStore['logs_stack-1'].data.value = { items: [{ id: 'log-1', status: 'success' }] }
    await flushPromises()

    expect(getOne.mock.calls.length).toBeGreaterThan(getOneCallsBefore)
    expect(refreshSpy.mock.calls.length).toBeGreaterThan(refreshSpyCallsBefore)
    expect(api.getRenderOverridesDiff.mock.calls.length).toBeGreaterThan(getRenderOverridesDiffCallsBefore)
  })

  it('does not reload on the initial (non-running) sync_logs load', async () => {
    const { getOne, asyncDataStore } = setupGlobals()
    const { refreshSpy } = mountPageWithServicesCardSpy()
    await flushPromises()
    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', status: 'active' }
    await flushPromises()

    const getOneCallsBefore = getOne.mock.calls.length
    const refreshSpyCallsBefore = refreshSpy.mock.calls.length

    asyncDataStore['logs_stack-1'].data.value = { items: [{ id: 'log-1', status: 'success' }] }
    await flushPromises()

    expect(getOne.mock.calls.length).toBe(getOneCallsBefore)
    expect(refreshSpy.mock.calls.length).toBe(refreshSpyCallsBefore)
  })
})

describe('stacks/[id].vue additional handler coverage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('explains why sync is disabled for every non-online worker state', async () => {
    const { asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()

    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', worker: 'worker-1', status: 'active' }

    asyncDataStore.workers_for_stacks.data.value = [{ id: 'worker-1', status: 'OFFLINE' }]
    await flushPromises()
    expect((wrapper.vm as any).syncDisabledReason).toContain('Worker offline')

    asyncDataStore.workers_for_stacks.data.value = [{ id: 'worker-1', status: 'REVOKED' }]
    await flushPromises()
    expect((wrapper.vm as any).syncDisabledReason).toContain('Worker revoked')

    asyncDataStore.workers_for_stacks.data.value = [{ id: 'worker-1', status: 'DEGRADED' }]
    await flushPromises()
    expect((wrapper.vm as any).syncDisabledReason).toContain('Docker unreachable')

    asyncDataStore.workers_for_stacks.data.value = []
    await flushPromises()
    expect((wrapper.vm as any).syncDisabledReason).toContain('Worker unavailable')
  })

  it('toggles a timeline entry open and closed', async () => {
    setupGlobals()
    const wrapper = await mountPage()

    const openEvent = { target: { open: true } } as unknown as Event
    ;(wrapper.vm as any).toggleTimeline('log-1', openEvent)
    expect((wrapper.vm as any).expandedTimelineLogIds.has('log-1')).toBe(true)

    const closeEvent = { target: { open: false } } as unknown as Event
    ;(wrapper.vm as any).toggleTimeline('log-1', closeEvent)
    expect((wrapper.vm as any).expandedTimelineLogIds.has('log-1')).toBe(false)
  })

  it('opens the container logs, action confirmation, and terminal modals', async () => {
    setupGlobals()
    const wrapper = await mountPage()

    ;(wrapper.vm as any).openContainerLogs('c1', 'web')
    expect((wrapper.vm as any).showLogsModal).toBe(true)
    expect((wrapper.vm as any).logsContainerName).toBe('web')

    ;(wrapper.vm as any).openContainerLogs('c1', '')
    expect((wrapper.vm as any).logsContainerName).toBe('c1')

    ;(wrapper.vm as any).openContainerActionModal('c1', 'web', 'stop')
    expect((wrapper.vm as any).showContainerConfirmModal).toBe(true)
    expect((wrapper.vm as any).containerActionState).toEqual({ id: 'c1', name: 'web', action: 'stop' })

    ;(wrapper.vm as any).openTerminalModal('c1', 'web')
    expect((wrapper.vm as any).showTerminalModal).toBe(true)
    expect((wrapper.vm as any).terminalState).toEqual({ id: 'c1', name: 'web' })
  })

  it('executes a bulk container action, reporting full success and partial failure', async () => {
    const { api, toastAdd } = setupGlobals()
    const wrapper = await mountPage()

    ;(wrapper.vm as any).handleBulkContainerAction({
      containers: [{ containerId: 'c1', containerName: 'web' }],
      action: 'restart',
      subject: 'web',
    })
    expect((wrapper.vm as any).showBulkActionModal).toBe(true)
    expect((wrapper.vm as any).bulkActionTitle).toBe('Restart web?')

    api.restartContainer.mockResolvedValue({})
    await (wrapper.vm as any).executeBulkContainerAction()
    expect(api.restartContainer).toHaveBeenCalledWith('stack-1', 'c1')
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Restarted 1 container' }))
    expect((wrapper.vm as any).showBulkActionModal).toBe(false)

    ;(wrapper.vm as any).handleBulkContainerAction({
      containers: [
        { containerId: 'c1', containerName: 'web' },
        { containerId: 'c2', containerName: 'db' },
      ],
      action: 'stop',
      subject: 'all containers',
    })
    api.stopContainer.mockResolvedValueOnce({}).mockRejectedValueOnce(new Error('boom'))
    await (wrapper.vm as any).executeBulkContainerAction()
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: '1 succeeded, 1 failed', description: 'db' }))
  })

  it('loads repo commits only when the stack has a repository, clearing on failure', async () => {
    const { api, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()

    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', status: 'active' }
    await flushPromises()
    expect(api.getRepoCommits).not.toHaveBeenCalled()

    api.getRepoCommits.mockResolvedValue([{ sha: 'abcdef1234567', message: 'a'.repeat(60), author: 'me', date: '2024-01-01' }])
    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', status: 'active', repository: 'repo-1' }
    await flushPromises()
    expect(api.getRepoCommits).toHaveBeenCalledWith('repo-1')
    expect((wrapper.vm as any).commitOptions[0].label).toContain('abcdef1')
    expect((wrapper.vm as any).commitOptions[0].label).toContain('...')

    api.getRepoCommits.mockRejectedValueOnce(new Error('nope'))
    await (wrapper.vm as any).loadRepoCommits()
    expect((wrapper.vm as any).repoCommits).toEqual([])
  })

  it('opens the compose viewer and reports a failure toast', async () => {
    const { api, toastAdd } = setupGlobals()
    const wrapper = await mountPage()

    api.getComposeFile.mockResolvedValue({ content: 'services: {}', filename: 'docker-compose.yml' })
    await (wrapper.vm as any).openComposeViewer()
    expect((wrapper.vm as any).showComposeModal).toBe(true)
    expect((wrapper.vm as any).composeContent).toBe('services: {}')

    api.getComposeFile.mockRejectedValueOnce(new Error('missing file'))
    ;(wrapper.vm as any).showComposeModal = false
    await (wrapper.vm as any).openComposeViewer()
    expect((wrapper.vm as any).showComposeModal).toBe(false)
    expect(toastAdd).toHaveBeenCalledWith({ title: 'missing file', color: 'error' })
  })

  it('starts editing, and saves for both wireops-managed and regular stacks', async () => {
    const { updateStack, asyncDataStore } = setupGlobals()
    ;(globalThis as any).useValidation = () => ({
      validateComposePath: (v: string) => (v ? '' : 'Compose path is required'),
      validateComposeFile: (v: string) => (v ? '' : 'Compose file is required'),
    })
    const wrapper = await mountPage()

    asyncDataStore['stack_stack-1'].data.value = {
      id: 'stack-1', name: 'my-stack', status: 'active', worker: 'worker-1', config_source: 'wireops_file',
    }
    await flushPromises()

    ;(wrapper.vm as any).startEdit()
    expect((wrapper.vm as any).editing).toBe(true)
    expect((wrapper.vm as any).editForm.name).toBe('my-stack')

    await (wrapper.vm as any).saveEdit()
    expect(updateStack).toHaveBeenCalledWith('stack-1', { name: 'my-stack', worker: 'worker-1' })
    expect((wrapper.vm as any).editing).toBe(false)

    asyncDataStore['stack_stack-1'].data.value = {
      id: 'stack-1', name: 'my-stack', status: 'active', worker: 'worker-1', compose_path: 'a', compose_file: 'b', group: 'g',
    }
    await flushPromises()
    ;(wrapper.vm as any).startEdit()
    ;(wrapper.vm as any).editForm.compose_path = ''
    await (wrapper.vm as any).saveEdit()
    expect((wrapper.vm as any).editErrors.compose_path).toBeTruthy()
    expect((wrapper.vm as any).editing).toBe(true)

    updateStack.mockRejectedValueOnce(new Error('save failed'))
    ;(wrapper.vm as any).editForm.compose_path = 'valid/path'
    await (wrapper.vm as any).saveEdit()
    expect((wrapper.vm as any).editing).toBe(true)
  })

  it('generates and saves a webhook secret, reporting failures', async () => {
    const { updateStack, toastAdd } = setupGlobals()
    const wrapper = await mountPage()

    await (wrapper.vm as any).saveWebhookSecret()
    expect(updateStack).not.toHaveBeenCalled()

    ;(wrapper.vm as any).generateWebhookSecret()
    expect((wrapper.vm as any).webhookSecretInput).toMatch(/^[0-9a-f]{32}$/)

    await (wrapper.vm as any).saveWebhookSecret()
    expect(updateStack).toHaveBeenCalledWith('stack-1', expect.objectContaining({ webhook_secret: expect.any(String) }))
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Webhook secret saved', color: 'success' })

    ;(wrapper.vm as any).generateWebhookSecret()
    updateStack.mockRejectedValueOnce(new Error('nope'))
    await (wrapper.vm as any).saveWebhookSecret()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Failed to save webhook secret', description: 'nope', color: 'error' })
  })

  it('pauses and resumes a stack via togglePause/confirmPause', async () => {
    const { updateStack, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', status: 'active' }
    await flushPromises()

    await (wrapper.vm as any).togglePause()
    expect((wrapper.vm as any).showPauseModal).toBe(true)

    await (wrapper.vm as any).confirmPause()
    expect(updateStack).toHaveBeenCalledWith('stack-1', { status: 'paused' })
    expect((wrapper.vm as any).showPauseModal).toBe(false)

    asyncDataStore['stack_stack-1'].data.value.status = 'paused'
    await (wrapper.vm as any).togglePause()
    expect(updateStack).toHaveBeenLastCalledWith('stack-1', { status: 'active' })
  })

  it('force-redeploys, reporting failures without resetting form state', async () => {
    const { api, toastAdd } = setupGlobals()
    const wrapper = await mountPage()

    await (wrapper.vm as any).handleForceRedeploy()
    expect(api.forceRedeploy).toHaveBeenCalledWith('stack-1', expect.objectContaining({ pause_after_redeploy: true }))
    expect((wrapper.vm as any).activeTab).toBe('logs')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Force redeploy triggered', color: 'info' })

    api.forceRedeploy.mockRejectedValueOnce(new Error('busy'))
    await (wrapper.vm as any).handleForceRedeploy()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'busy', color: 'error' })
  })

  it('opens the overrides modal pre-filled from git state and containers_list', async () => {
    const { asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore['stack_stack-1'].data.value = {
      id: 'stack-1', name: 'my-stack', status: 'active',
      containers_list: [{ name: 'web', slug: 'web-1' }],
      render_overrides: { web: { image: 'nginx:latest', ports: ['80:80'], networks: ['edge'], scale: 2 } },
    }
    await flushPromises()

    ;(wrapper.vm as any).openOverridesModal()
    expect((wrapper.vm as any).showOverridesModal).toBe(true)
    expect((wrapper.vm as any).overridesForm.web).toEqual({ image: 'nginx:latest', ports: '80:80', networks: 'edge', scale: '2' })
    expect((wrapper.vm as any).getOverrideServiceSlug('web')).toBe('web-1')
  })

  it('warns when applying overrides with nothing filled in', async () => {
    const { api, toastAdd, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore['stack_stack-1'].data.value = {
      id: 'stack-1', name: 'my-stack', status: 'active', containers_list: [{ name: 'web' }],
    }
    await flushPromises()

    ;(wrapper.vm as any).overridesForm = { web: { image: '', ports: '', networks: '', scale: '' } }
    await (wrapper.vm as any).handleApplyOverrides()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'No overrides to apply', color: 'warning' })
    expect(api.setRenderOverrides).not.toHaveBeenCalled()
  })

  it('reports a failure toast when applying or clearing overrides fails', async () => {
    const { api, toastAdd, asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()
    asyncDataStore['stack_stack-1'].data.value = {
      id: 'stack-1', name: 'my-stack', status: 'active', containers_list: [{ name: 'web' }],
    }
    await flushPromises()

    api.setRenderOverrides.mockRejectedValueOnce(new Error('rejected'))
    ;(wrapper.vm as any).overridesForm = { web: { image: 'nginx', ports: '', networks: '', scale: '' } }
    await (wrapper.vm as any).handleApplyOverrides()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'rejected', color: 'error' })

    api.clearRenderOverrides.mockRejectedValueOnce(new Error('cannot clear'))
    await (wrapper.vm as any).handleClearOverrides()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'cannot clear', color: 'error' })
  })

  it('navigates away once a stack delete completes', async () => {
    const { navigateTo } = setupGlobals()
    const wrapper = await mountPage()

    await (wrapper.vm as any).onStackDeleted()
    expect((wrapper.vm as any).showDeleteModal).toBe(false)
    expect(navigateTo).toHaveBeenCalledWith('/stacks')
  })

  it('builds danger-zone actions, including migrate only for non-local stacks', async () => {
    const { asyncDataStore } = setupGlobals()
    const wrapper = await mountPage()

    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', status: 'active', source_type: 'git' }
    await flushPromises()
    let keys = (wrapper.vm as any).dangerZoneActions.map((a: any) => a.key)
    expect(keys).toEqual(['transfer', 'migrate', 'remove'])

    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', status: 'active', source_type: 'local' }
    await flushPromises()
    keys = (wrapper.vm as any).dangerZoneActions.map((a: any) => a.key)
    expect(keys).toEqual(['transfer', 'remove'])

    const actions = (wrapper.vm as any).dangerZoneActions
    actions.find((a: any) => a.key === 'transfer').onClick()
    expect((wrapper.vm as any).showTransferModal).toBe(true)
    actions.find((a: any) => a.key === 'remove').onClick()
    expect((wrapper.vm as any).showDeleteModal).toBe(true)
  })

  it('switches to the logs tab and schedules a refresh once transfer/migrate completes', async () => {
    setupGlobals()
    const wrapper = await mountPage()
    ;(wrapper.vm as any).activeTab = 'overview'

    ;(wrapper.vm as any).onTransferDone()
    expect((wrapper.vm as any).showTransferModal).toBe(false)
    expect((wrapper.vm as any).activeTab).toBe('logs')

    ;(wrapper.vm as any).activeTab = 'overview'
    ;(wrapper.vm as any).onMigrateDone()
    expect((wrapper.vm as any).showMigrateModal).toBe(false)
    expect((wrapper.vm as any).activeTab).toBe('logs')
  })

  it('reacts to realtime sync_logs and workers events, and the Cmd/Ctrl+S shortcut', async () => {
    const { api, asyncDataStore } = setupGlobals()
    api.getWorkers.mockResolvedValue([{ id: 'worker-1', status: 'ACTIVE' }])
    const subscribeHandlers: Record<string, (data?: any) => void> = {}
    ;(globalThis as any).useRealtime = () => ({
      subscribe: vi.fn((channel: string, handler: (data?: any) => void) => {
        subscribeHandlers[channel] = handler
      }),
    })
    const wrapper = await mountPage()
    asyncDataStore['stack_stack-1'].data.value = { id: 'stack-1', name: 'my-stack', worker: 'worker-1', status: 'active' }
    asyncDataStore.workers_for_stacks.data.value = [{ id: 'worker-1', status: 'ACTIVE' }]
    await flushPromises()

    const before = asyncDataStore['logs_stack-1'].refresh.mock.calls.length
    subscribeHandlers.sync_logs!({ record: { stack: 'stack-1' } })
    await flushPromises()
    expect(asyncDataStore['logs_stack-1'].refresh.mock.calls.length).toBeGreaterThan(before)

    subscribeHandlers.sync_logs!({ record: { stack: 'other-stack' } })
    await flushPromises()
    expect(asyncDataStore['logs_stack-1'].refresh.mock.calls.length).toBe(before + 1)

    const workersBefore = asyncDataStore.workers_for_stacks.refresh.mock.calls.length
    subscribeHandlers.workers!()
    await flushPromises()
    expect(asyncDataStore.workers_for_stacks.refresh.mock.calls.length).toBeGreaterThan(workersBefore)

    expect((wrapper.vm as any).canSyncDeploy).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 's', ctrlKey: true }))
    await flushPromises()
    expect((wrapper.vm as any).showSyncModal).toBe(true)
  })
})
