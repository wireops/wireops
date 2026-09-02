import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h, ref } from 'vue'
import JobsPanel from '../JobsPanel.vue'
import { useListDensity } from '../../composables/useListDensity'
import { GROUP_ALL, GROUP_UNGROUPED, encodeGroupValue } from '../../utils/job-filter'

const jobFixture = {
  id: 'job-1',
  name: 'Nightly Backup',
  status: 'active',
  enabled: true,
  description: 'Backs up the database every night',
  job_file: 'jobs/backup.yaml',
  last_run_at: new Date(Date.now() - 60_000).toISOString(),
  repository: { name: 'repo-a' },
  recent_runs: [
    { id: 'run-1', status: 'success', created: new Date().toISOString() },
  ],
  definition: {
    name: 'Nightly Backup',
    cron: '0 2 * * *',
    image: 'postgres:16',
    tags: ['db'],
    network: 'wireops',
    group: '',
  },
}

const stubs = {
  BadgeStatus: { props: ['status'], setup(props: { status?: string }) { return () => h('span', props.status) } },
  UButton: {
    props: ['label', 'icon', 'ariaLabel', 'disabled'],
    setup(props: { label?: string }, { attrs, slots }: { attrs: Record<string, unknown>, slots: Record<string, (() => unknown) | undefined> }) {
      return () => h('button', attrs, [props.label, slots.default?.()])
    },
  },
  AppTextInput: { setup() { return () => h('div', [h('input')]) } },
  AppSelectInput: { setup() { return () => h('select') } },
  UTooltip: { setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) { return () => h('div', slots.default?.()) } },
  UIcon: { setup() { return () => h('span') } },
  UBadge: { props: ['label'], setup(props: { label?: string }) { return () => h('span', { class: 'ubadge' }, props.label) } },
  NuxtLink: {
    props: ['to'],
    setup(props: { to?: string }, { attrs, slots }: { attrs: Record<string, unknown>, slots: Record<string, (() => unknown) | undefined> }) {
      return () => h('a', { href: props.to, ...attrs }, slots.default?.())
    },
  },
  USkeleton: { setup() { return () => h('div') } },
  UPagination: { setup() { return () => h('div') } },
  EmptyState: {
    props: ['description', 'ctaLabel'],
    setup(props: { description?: string, ctaLabel?: string }, { emit }: { emit: (e: string) => void }) {
      return () => h('div', [props.description, h('button', { onClick: () => emit('cta') }, props.ctaLabel)])
    },
  },
  JobCreateModal: true,
  JobBuilderModal: true,
  RepositoryCreateModal: true,
}

function stubGlobals(overrides: { listJobGroups?: { groups: string[], has_ungrouped: boolean } } = {}) {
  ;(globalThis as any).WORKER_STATUS = { PENDING: 'PENDING', REVOKED: 'REVOKED', ACTIVE: 'ACTIVE', DEGRADED: 'DEGRADED', OFFLINE: 'OFFLINE' }

  const scheduledJobsUpdate = vi.fn().mockResolvedValue({})
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: (name: string) => {
        if (name === 'scheduled_jobs') {
          return {
            getFullList: vi.fn().mockResolvedValue([jobFixture]),
            update: scheduledJobsUpdate,
          }
        }
        return { getFullList: vi.fn().mockResolvedValue([]) }
      },
    },
  })

  const listJobs = vi.fn().mockResolvedValue({ items: [jobFixture], total_items: 1 })
  const triggerJobRun = vi.fn().mockResolvedValue({})
  const getWorkers = vi.fn().mockResolvedValue([])
  const listJobGroups = vi.fn().mockResolvedValue(overrides.listJobGroups ?? { groups: [], has_ungrouped: false })
  ;(globalThis as any).useApi = () => ({ listJobs, triggerJobRun, getWorkers, listJobGroups })

  const subscribeHandlers: Record<string, (data?: any) => void> = {}
  const subscribe = vi.fn((channel: string, handler: (data?: any) => void) => {
    subscribeHandlers[channel] = handler
  })
  ;(globalThis as any).useRealtime = () => ({ subscribe })

  const toastAdd = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: toastAdd })
  ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
  ;(globalThis as any).useRoute = () => ({ query: {} })
  const navigateTo = vi.fn()
  ;(globalThis as any).navigateTo = navigateTo

  const asyncDataStore: Record<string, { data: any, refresh: ReturnType<typeof vi.fn> }> = {}
  ;(globalThis as any).useAsyncData = (key: string, fn: () => Promise<any>) => {
    const data = ref<any>([])
    const refresh = vi.fn(async () => { data.value = await fn() })
    const entry = { data, refresh }
    asyncDataStore[key] = entry
    refresh()
    return entry
  }

  return { listJobs, triggerJobRun, getWorkers, listJobGroups, scheduledJobsUpdate, subscribeHandlers, toastAdd, asyncDataStore, navigateTo }
}

describe('JobsPanel', () => {
  afterEach(() => {
    useListDensity().setDensity('comfortable')
  })

  it('renders full job metadata in comfortable (default) view', async () => {
    stubGlobals()

    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('Nightly Backup')
    expect(wrapper.text()).toContain('Backs up the database every night')
    expect(wrapper.text()).toContain('repo-a / jobs/backup.yaml')
    expect(wrapper.text()).toContain('postgres:16')
    expect(wrapper.text()).toContain('db')
    expect(wrapper.text()).toContain('net: wireops')
    expect(wrapper.text()).toContain('0 2 * * *')
  })

  it('hides secondary metadata but keeps core fields in compact view', async () => {
    useListDensity().setDensity('compact')
    stubGlobals()

    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('Nightly Backup')
    expect(wrapper.text()).toContain('active')
    expect(wrapper.text()).toContain('0 2 * * *')

    expect(wrapper.text()).not.toContain('Backs up the database every night')
    expect(wrapper.text()).not.toContain('repo-a / jobs/backup.yaml')
    expect(wrapper.text()).not.toContain('postgres:16')
    expect(wrapper.text()).not.toContain('net: wireops')
  })

  it('walks the empty-state step through repo, worker, then job creation', async () => {
    const { asyncDataStore, navigateTo } = stubGlobals()
    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()

    expect((wrapper.vm as any).emptyStateStep.ctaLabel).toBe('Add Repository')
    ;(wrapper.vm as any).emptyStateStep.action()
    expect((wrapper.vm as any).showCreateRepoFromEmpty).toBe(true)

    asyncDataStore.repos_for_jobs.data.value = [{ id: 'repo-1', name: 'repo-a' }]
    await flushPromises()
    expect((wrapper.vm as any).emptyStateStep.ctaLabel).toBe('Add Worker')
    ;(wrapper.vm as any).emptyStateStep.action()
    expect(navigateTo).toHaveBeenCalledWith('/workers')

    asyncDataStore.job_builder_workers.data.value = [{ id: 'worker-1', status: 'ACTIVE' }]
    await flushPromises()
    expect((wrapper.vm as any).emptyStateStep.ctaLabel).toBe('New Job')
    ;(wrapper.vm as any).emptyStateStep.action()
    await flushPromises()
    expect((wrapper.vm as any).showCreate).toBe(true)
  })

  it('builds group filter options including ungrouped', async () => {
    stubGlobals({ listJobGroups: { groups: ['infra', 'db'], has_ungrouped: true } })
    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()

    const options = (wrapper.vm as any).groupOptions
    expect(options.map((o: any) => o.label)).toEqual(['All groups', 'Ungrouped', 'infra', 'db'])
  })

  it('filters jobs by group and by ungrouped, client-side', async () => {
    const grouped = { ...jobFixture, id: 'job-2', definition: { ...jobFixture.definition, group: 'infra' } }
    const ungrouped = { ...jobFixture, id: 'job-3', definition: { ...jobFixture.definition, group: undefined } }
    const { listJobs } = stubGlobals()
    listJobs.mockResolvedValue({ items: [grouped, ungrouped], total_items: 2 })

    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()

    ;(wrapper.vm as any).groupFilter = encodeGroupValue('infra')
    await flushPromises()
    expect((wrapper.vm as any).jobs.map((j: any) => j.id)).toEqual(['job-2'])

    ;(wrapper.vm as any).groupFilter = GROUP_UNGROUPED
    await flushPromises()
    expect((wrapper.vm as any).jobs.map((j: any) => j.id)).toEqual(['job-3'])
  })

  it('reacts to realtime events for scheduled_jobs, job_runs, repositories, and workers', async () => {
    const { subscribeHandlers, listJobs, asyncDataStore, getWorkers } = stubGlobals()
    mount(JobsPanel, { global: { stubs } })
    await flushPromises()

    const listJobsCallsBefore = listJobs.mock.calls.length
    subscribeHandlers.scheduled_jobs!()
    await flushPromises()
    expect(listJobs.mock.calls.length).toBeGreaterThan(listJobsCallsBefore)
    expect(asyncDataStore.jobs_aggregate!.refresh).toHaveBeenCalled()
    expect(asyncDataStore.job_groups!.refresh).toHaveBeenCalled()

    const listJobsCallsMatched = listJobs.mock.calls.length
    subscribeHandlers.job_runs!({ record: { job: 'job-1' } })
    await flushPromises()
    expect(listJobs.mock.calls.length).toBeGreaterThan(listJobsCallsMatched)

    const listJobsCallsUnmatched = listJobs.mock.calls.length
    subscribeHandlers.job_runs!({ record: { job: 'no-such-job' } })
    await flushPromises()
    expect(listJobs.mock.calls.length).toBe(listJobsCallsUnmatched)

    subscribeHandlers.repositories!()
    await flushPromises()
    expect(asyncDataStore.repos_for_jobs!.refresh).toHaveBeenCalled()

    const getWorkersCallsBefore = getWorkers.mock.calls.length
    subscribeHandlers.workers!()
    await flushPromises()
    expect(getWorkers.mock.calls.length).toBeGreaterThan(getWorkersCallsBefore)
  })

  it('toggles a job enabled state and reports update failures', async () => {
    const { scheduledJobsUpdate, toastAdd } = stubGlobals()
    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()

    await (wrapper.vm as any).toggleEnabled(jobFixture)
    expect(scheduledJobsUpdate).toHaveBeenCalledWith('job-1', { enabled: false })
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Job disabled', color: 'success' })

    scheduledJobsUpdate.mockRejectedValueOnce(new Error('boom'))
    await (wrapper.vm as any).toggleEnabled({ ...jobFixture, enabled: false })
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Failed to update job', description: 'boom', color: 'error' })
  })

  it('triggers a manual job run and reports trigger failures', async () => {
    const { triggerJobRun, toastAdd } = stubGlobals()
    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()

    await (wrapper.vm as any).triggerRun(jobFixture)
    expect(triggerJobRun).toHaveBeenCalledWith('job-1')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Job triggered', description: 'A manual run has been dispatched.', color: 'success' })

    triggerJobRun.mockRejectedValueOnce(new Error('nope'))
    await (wrapper.vm as any).triggerRun(jobFixture)
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Failed to trigger job', description: 'nope', color: 'error' })
  })

  it('closes the create modal and refreshes the list on job creation', async () => {
    const { listJobs } = stubGlobals()
    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()
    ;(wrapper.vm as any).showCreate = true

    const before = listJobs.mock.calls.length
    ;(wrapper.vm as any).onCreated()
    await flushPromises()

    expect((wrapper.vm as any).showCreate).toBe(false)
    expect(listJobs.mock.calls.length).toBeGreaterThan(before)
  })

  it('formats relative last-run timestamps', async () => {
    stubGlobals()
    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()
    const fmt = (wrapper.vm as any).formatRelative

    expect(fmt('')).toBe('Never')
    expect(fmt('0001-01-01 00:00:00.000Z')).toBe('Never')
    expect(fmt(new Date(Date.now() - 5_000).toISOString())).toMatch(/s ago$/)
    expect(fmt(new Date(Date.now() - 5 * 60_000).toISOString())).toMatch(/m ago$/)
    expect(fmt(new Date(Date.now() - 5 * 3_600_000).toISOString())).toMatch(/h ago$/)
    expect(fmt(new Date(Date.now() - 5 * 86_400_000).toISOString())).toMatch(/d ago$/)
  })

  it('maps every job status to a border class, including the unknown fallback', async () => {
    stubGlobals()
    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()
    const fn = (wrapper.vm as any).statusBorderClass

    expect(fn('active')).toContain('emerald')
    expect(fn('stalled')).toContain('amber')
    expect(fn('error')).toContain('rose')
    expect(fn('paused')).toContain('carbon-600')
    expect(fn('something-else')).toContain('carbon-600')
  })

  it('normalizes route query group values', async () => {
    stubGlobals()
    const wrapper = mount(JobsPanel, { global: { stubs } })
    await flushPromises()
    const fn = (wrapper.vm as any).groupQueryToFilterValue

    expect(fn(undefined)).toBe(GROUP_ALL)
    expect(fn('')).toBe(GROUP_ALL)
    expect(fn(GROUP_UNGROUPED)).toBe(GROUP_UNGROUPED)
    expect(fn('infra')).toBe(encodeGroupValue('infra'))
  })
})
