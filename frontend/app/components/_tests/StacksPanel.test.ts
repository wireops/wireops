import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h, ref } from 'vue'
import StacksPanel from '../StacksPanel.vue'
import StackCard from '../StackCard.vue'
import StackRow from '../StackRow.vue'
import GitProviderBadge from '../GitProviderBadge.vue'
import RepositoryIcon from '../RepositoryIcon.vue'
import GithubIcon from '../GithubIcon.vue'
import GenericIcon from '../GenericIcon.vue'
import { useListDensity } from '../../composables/useListDensity'

const stackFixture = {
  id: 'stack-1',
  name: 'Payments',
  worker: 'worker-1',
  status: 'active',
  group: '',
  deployed_at: '2024-01-01 00:00:00.000Z',
  expand: {
    repository: { name: 'repo-a', platform: 'github', status: 'connected' },
    worker: { id: 'worker-1', hostname: 'worker-a', status: 'ACTIVE' },
  },
  containers_list: [],
}

describe('StacksPanel', () => {
  afterEach(() => {
    useListDensity().setDensity('comfortable')
  })

  it('renders a keyboard-focusable link for each stack with a status badge', async () => {
    const refresh = vi.fn()
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        filter: (raw: string) => raw,
        collection: () => ({
          getFullList: vi.fn().mockResolvedValue([stackFixture]),
          getList: vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 }),
        }),
      },
    })
    ;(globalThis as any).useApi = () => ({
      getWorkers: vi.fn(),
      listOrphans: vi.fn(),
      purgeOrphan: vi.fn(),
    })
    ;(globalThis as any).useRealtime = () => ({
      subscribe: vi.fn(),
    })
    ;(globalThis as any).useToast = () => ({
      add: vi.fn(),
    })
    ;(globalThis as any).useA11yAnnouncer = () => ({
      announce: vi.fn(),
    })
    ;(globalThis as any).usePermissions = () => ({
      isViewer: ref(false),
    })
    ;(globalThis as any).useRepositoryPlatform = () => ({
      platformIconUrl: vi.fn(),
    })
    ;(globalThis as any).useRoute = () => ({ query: {} })
    ;(globalThis as any).useAsyncData = (key: string) => {
      if (key === 'stack_card_workers') {
        return {
          data: ref([
            { id: 'worker-1', hostname: 'worker-a', status: 'OFFLINE' },
          ]),
          refresh,
        }
      }

      return {
        data: ref([stackFixture]),
        refresh,
      }
    }

    const wrapper = mount(StacksPanel, {
      global: {
        components: {
          StackCard,
          StackRow,
          GitProviderBadge,
          RepositoryIcon,
          GithubIcon,
          GenericIcon,
        },
        stubs: {
          BadgeStatus: {
            props: ['status'],
            setup(props) {
              return () => h('span', { class: 'badge-status' }, props.status)
            },
          },
          UCard: { template: '<section><slot name="header" /><slot /></section>' },
          UButton: {
            props: ['label', 'icon', 'ariaLabel'],
            template: '<button v-bind="$attrs">{{ label }}<slot /></button>',
          },
          AppTextInput: {
            setup() {
              return () => h('div', [h('input')])
            },
          },
          AppSelectInput: {
            setup() {
              return () => h('select')
            },
          },
          UTooltip: { template: '<div><slot /></div>' },
          UIcon: { template: '<span />' },
          NuxtLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
          CreateStackModal: true,
          BadgeLabel: true,
          DeleteStackModal: true,
          ImportStackModal: true,
          StackContainersList: true,
          UModal: { template: '<div><slot name="body" /></div>' },
          UPagination: {
            setup() {
              return () => h('div')
            },
          },
        },
      },
    })

    await flushPromises()

    const stackLinks = wrapper.findAll('a[aria-label="Open stack Payments"]')
    expect(stackLinks).toHaveLength(1)
    expect(stackLinks[0]?.attributes('href')).toBe('/stacks/stack-1')

    expect(wrapper.text()).toContain('repo-a')
    expect(wrapper.text()).toContain('Deploy')
    expect(wrapper.text()).toContain('Unknown')
    expect(wrapper.text()).toContain('Worker')
    expect(wrapper.text()).toContain('Offline')
    expect(wrapper.text()).not.toContain('Deployed')
    expect(wrapper.text()).not.toContain('Synced')
    expect(wrapper.find('[title="Git: Up to date"]').exists()).toBe(true)
    expect(wrapper.find('.badge-status').text()).toBe('active')
  })

  const dropdownStubs = {
    BadgeStatus: { props: ['status'], setup(props: { status?: string }) { return () => h('span', props.status) } },
    UCard: { setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) { return () => h('section', [slots.header?.(), slots.default?.()]) } },
    UButton: {
      props: ['label', 'icon', 'ariaLabel'],
      setup(props: { label?: string }, { attrs, slots }: { attrs: Record<string, unknown>, slots: Record<string, (() => unknown) | undefined> }) {
        return () => h('button', attrs, [props.label, slots.default?.()])
      },
    },
    AppTextInput: { setup() { return () => h('div', [h('input')]) } },
    AppSelectInput: { setup() { return () => h('select') } },
    UTooltip: { setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) { return () => h('div', slots.default?.()) } },
    UIcon: { setup() { return () => h('span') } },
    NuxtLink: {
      props: ['to'],
      setup(props: { to?: string }, { attrs, slots }: { attrs: Record<string, unknown>, slots: Record<string, (() => unknown) | undefined> }) {
        return () => h('a', { href: props.to, ...attrs }, slots.default?.())
      },
    },
    CreateStackModal: true,
    BadgeLabel: true,
    DeleteStackModal: true,
    ImportStackModal: true,
    // Respects the `open` prop, same reasoning as the UModal stub below -
    // StackBuilderModal owns its own open/close state rather than being
    // wrapped in a parent UModal.
    StackBuilderModal: {
      props: ['open'],
      setup(props: { open?: boolean }) {
        return () => (props.open ? h('div', { class: 'stack-builder-modal-stub' }) : null)
      },
    },
    StackContainersList: true,
    // Respects the `open` prop so tests can assert whether a modal was
    // actually triggered by a dropdown action, not just that it exists.
    UModal: {
      props: ['open'],
      setup(props: { open?: boolean }, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
        return () => (props.open ? h('div', slots.body?.()) : null)
      },
    },
    UPagination: { setup() { return () => h('div') } },
    // Renders each dropdown item as a clickable button so tests can invoke
    // its onSelect handler directly, instead of only asserting the trigger renders.
    UDropdownMenu: {
      props: ['items'],
      setup(props: { items?: { label: string, onSelect?: () => void }[][] }, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
        return () => h('div', [
          slots.default?.(),
          h('div', (props.items ?? []).flat().map(item =>
            h('button', { type: 'button', onClick: () => item.onSelect?.() }, item.label)
          )),
        ])
      },
    },
  }

  it('defaults to sorting stacks alphabetically by name', async () => {
    const getList = vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 })
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        filter: (raw: string) => raw,
        collection: () => ({
          getFullList: vi.fn().mockResolvedValue([stackFixture]),
          getList,
        }),
      },
    })
    ;(globalThis as any).useApi = () => ({
      getWorkers: vi.fn(),
      listOrphans: vi.fn(),
      purgeOrphan: vi.fn(),
    })
    ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
    ;(globalThis as any).useA11yAnnouncer = () => ({ announce: vi.fn() })
    ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
    ;(globalThis as any).useRepositoryPlatform = () => ({ platformIconUrl: vi.fn() })
    ;(globalThis as any).useRoute = () => ({ query: {} })
    ;(globalThis as any).useAsyncData = (key: string) => ({
      data: ref(key === 'stack_card_workers' ? [] : [stackFixture]),
      refresh: vi.fn(),
    })

    mount(StacksPanel, {
      global: {
        components: { StackCard, StackRow, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
        stubs: dropdownStubs,
      },
    })

    await flushPromises()

    expect(getList).toHaveBeenCalled()
    expect(getList.mock.calls[0]![2]).toMatchObject({ sort: 'name' })
  })

  // The "degraded" bucket is filtered server-side on worker.docker_online, so
  // a worker flipping online/offline can change page membership without any
  // stack record changing - the 'workers' realtime subscription has to force
  // its own reload in that case instead of relying on the 'stacks' channel.
  async function mountAndCaptureWorkersHandler(getList: ReturnType<typeof vi.fn>) {
    let workersHandler: (() => void) | undefined
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        filter: (raw: string) => raw,
        collection: () => ({
          getFullList: vi.fn().mockResolvedValue([stackFixture]),
          getList,
        }),
      },
    })
    ;(globalThis as any).useApi = () => ({
      getWorkers: vi.fn(),
      listOrphans: vi.fn(),
      purgeOrphan: vi.fn(),
    })
    ;(globalThis as any).useRealtime = () => ({
      subscribe: vi.fn((channel: string, handler: () => void) => {
        if (channel === 'workers') workersHandler = handler
      }),
    })
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
    ;(globalThis as any).useA11yAnnouncer = () => ({ announce: vi.fn() })
    ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
    ;(globalThis as any).useRepositoryPlatform = () => ({ platformIconUrl: vi.fn() })
    ;(globalThis as any).useRoute = () => ({ query: {} })
    ;(globalThis as any).useAsyncData = (key: string) => ({
      data: ref(key === 'stack_card_workers' ? [] : [stackFixture]),
      refresh: vi.fn(),
    })

    const wrapper = mount(StacksPanel, {
      global: {
        components: { StackCard, StackRow, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
        stubs: dropdownStubs,
      },
    })
    await flushPromises()

    return { wrapper, workersHandler: workersHandler! }
  }

  it('reloads the paginated stack list on a worker event when the status filter is degraded', async () => {
    const getList = vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 })
    const { wrapper, workersHandler } = await mountAndCaptureWorkersHandler(getList)
    ;(wrapper.vm as any).statusFilter = 'degraded'
    await flushPromises()

    const callsBeforeEvent = getList.mock.calls.length
    workersHandler()
    await flushPromises()

    expect(getList.mock.calls.length).toBeGreaterThan(callsBeforeEvent)
  })

  it('does not reload the paginated stack list on a worker event for a non-degraded status filter', async () => {
    const getList = vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 })
    const { wrapper, workersHandler } = await mountAndCaptureWorkersHandler(getList)
    ;(wrapper.vm as any).statusFilter = 'active'
    await flushPromises()

    const callsBeforeEvent = getList.mock.calls.length
    workersHandler()
    await flushPromises()

    expect(getList.mock.calls.length).toBe(callsBeforeEvent)
  })

  async function mountWithWorkers(getList: ReturnType<typeof vi.fn>, workersData: any[]) {
    const workersRef = ref(workersData)
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        filter: (raw: string) => raw,
        collection: () => ({
          getFullList: vi.fn().mockResolvedValue([stackFixture]),
          getList,
        }),
      },
    })
    ;(globalThis as any).useApi = () => ({
      getWorkers: vi.fn(),
      listOrphans: vi.fn(),
      purgeOrphan: vi.fn(),
    })
    ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
    ;(globalThis as any).useA11yAnnouncer = () => ({ announce: vi.fn() })
    ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
    ;(globalThis as any).useRepositoryPlatform = () => ({ platformIconUrl: vi.fn() })
    ;(globalThis as any).useRoute = () => ({ query: {} })
    ;(globalThis as any).useAsyncData = (key: string) => ({
      data: key === 'stack_card_workers' ? workersRef : ref([stackFixture]),
      refresh: vi.fn(),
    })

    const wrapper = mount(StacksPanel, {
      global: {
        components: { StackCard, StackRow, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
        stubs: dropdownStubs,
      },
    })
    await flushPromises()

    return { wrapper, workersRef }
  }

  // buildStacksFilter/statusFilter/workerFilter/groupFilter changes go
  // through usePaginatedList's watchDebounced (see usePaginatedList.ts),
  // which waits 400ms before refetching - flushPromises alone doesn't
  // advance that real timer.
  async function waitForDebouncedReload() {
    await new Promise(resolve => setTimeout(resolve, 450))
    await flushPromises()
  }

  it('omits the worker filter clause when set to "All workers"', async () => {
    const getList = vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 })
    await mountWithWorkers(getList, [{ id: 'worker-1', hostname: 'worker-a' }])

    const lastFilter = getList.mock.calls.at(-1)![2].filter
    expect(lastFilter).not.toContain('worker = {:w}')
  })

  it('adds the worker filter clause once a worker is selected, and reloads on change', async () => {
    const getList = vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 })
    const { wrapper } = await mountWithWorkers(getList, [
      { id: 'worker-1', hostname: 'worker-a' },
      { id: 'worker-2', hostname: 'worker-b' },
    ])

    const callsBeforeSelect = getList.mock.calls.length
    ;(wrapper.vm as any).workerFilter = 'worker-1'
    await waitForDebouncedReload()

    expect(getList.mock.calls.length).toBeGreaterThan(callsBeforeSelect)
    expect(getList.mock.calls.at(-1)![2].filter).toContain('worker = {:w}')

    const callsBeforeChange = getList.mock.calls.length
    ;(wrapper.vm as any).workerFilter = 'worker-2'
    await waitForDebouncedReload()

    expect(getList.mock.calls.length).toBeGreaterThan(callsBeforeChange)
    expect(getList.mock.calls.at(-1)![2].filter).toContain('worker = {:w}')
  })

  it('resets the worker filter to "all" when the selected worker disappears from the options', async () => {
    const getList = vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 })
    const { wrapper, workersRef } = await mountWithWorkers(getList, [
      { id: 'worker-1', hostname: 'worker-a' },
    ])

    ;(wrapper.vm as any).workerFilter = 'worker-1'
    await flushPromises()
    expect((wrapper.vm as any).workerFilter).toBe('worker-1')

    workersRef.value = []
    await waitForDebouncedReload()

    expect((wrapper.vm as any).workerFilter).toBe('all')
    expect(getList.mock.calls.at(-1)![2].filter).not.toContain('worker = {:w}')
  })

  it('selects Stack Builder, Import and Manage Orphans from the actions dropdown', async () => {
    const listOrphans = vi.fn().mockResolvedValue([
      { dir_name: 'orphan-1', compose_file: 'docker-compose.yml', has_compose: true },
    ])
    const purgeOrphan = vi.fn().mockResolvedValue({})
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        filter: (raw: string) => raw,
        collection: () => ({
          getFullList: vi.fn().mockResolvedValue([stackFixture]),
          getList: vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 }),
        }),
      },
    })
    ;(globalThis as any).useApi = () => ({
      getWorkers: vi.fn(),
      listOrphans,
      purgeOrphan,
    })
    ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
    ;(globalThis as any).useA11yAnnouncer = () => ({ announce: vi.fn() })
    ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
    ;(globalThis as any).useRepositoryPlatform = () => ({ platformIconUrl: vi.fn() })
    ;(globalThis as any).useRoute = () => ({ query: {} })
    ;(globalThis as any).useAsyncData = (key: string) => ({
      data: ref(key === 'stack_card_workers' ? [] : [stackFixture]),
      refresh: vi.fn(),
    })

    const wrapper = mount(StacksPanel, {
      global: {
        components: { StackCard, StackRow, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
        stubs: dropdownStubs,
      },
    })

    await flushPromises()

    expect(wrapper.find('.stack-builder-modal-stub').exists()).toBe(false)
    const builderItem = wrapper.findAll('button').find(b => b.text() === 'Stack Builder')
    await builderItem!.trigger('click')
    await flushPromises()
    expect(wrapper.find('.stack-builder-modal-stub').exists()).toBe(true)

    expect(wrapper.findComponent({ name: 'ImportStackModal' }).exists()).toBe(false)
    const importItem = wrapper.findAll('button').find(b => b.text() === 'Import')
    await importItem!.trigger('click')
    await flushPromises()

    expect(wrapper.findComponent({ name: 'ImportStackModal' }).exists()).toBe(true)

    const orphansItem = wrapper.findAll('button').find(b => b.text() === 'Manage Orphans')
    await orphansItem!.trigger('click')
    await flushPromises()

    expect(listOrphans).toHaveBeenCalled()
    expect(wrapper.text()).toContain('orphan-1')

    const purgeButton = wrapper.findAll('button').find(b => b.text() === 'Purge')
    await purgeButton!.trigger('click')
    await flushPromises()

    expect(purgeOrphan).toHaveBeenCalledWith('orphan-1')
    expect(wrapper.text()).not.toContain('orphan-1')
  })

  it('renders dense StackRow items instead of StackCard when compact view is active', async () => {
    useListDensity().setDensity('compact')

    const getList = vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 })
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        filter: (raw: string) => raw,
        collection: () => ({
          getFullList: vi.fn().mockResolvedValue([stackFixture]),
          getList,
        }),
      },
    })
    ;(globalThis as any).useApi = () => ({
      getWorkers: vi.fn(),
      listOrphans: vi.fn(),
      purgeOrphan: vi.fn(),
    })
    ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
    ;(globalThis as any).useA11yAnnouncer = () => ({ announce: vi.fn() })
    ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
    ;(globalThis as any).useRepositoryPlatform = () => ({ platformIconUrl: vi.fn() })
    ;(globalThis as any).useRoute = () => ({ query: {} })
    ;(globalThis as any).useAsyncData = (key: string) => ({
      data: ref(key === 'stack_card_workers' ? [] : [stackFixture]),
      refresh: vi.fn(),
    })

    const wrapper = mount(StacksPanel, {
      global: {
        components: { StackCard, StackRow, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
        stubs: dropdownStubs,
      },
    })

    await flushPromises()

    expect(wrapper.findComponent(StackRow).exists()).toBe(true)
    expect(wrapper.findComponent(StackCard).exists()).toBe(false)
  })

  function mountStacksPanel(opts: {
    asyncData?: Record<string, any[]>
    listOrphans?: ReturnType<typeof vi.fn>
    purgeOrphan?: ReturnType<typeof vi.fn>
  } = {}) {
    ;(globalThis as any).WORKER_STATUS = { PENDING: 'PENDING', REVOKED: 'REVOKED', ACTIVE: 'ACTIVE', DEGRADED: 'DEGRADED', OFFLINE: 'OFFLINE' }
    const getList = vi.fn().mockResolvedValue({ items: [stackFixture], totalItems: 1 })
    const getFullList = vi.fn().mockResolvedValue([stackFixture])
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: { filter: (raw: string) => raw, collection: () => ({ getFullList, getList }) },
    })
    ;(globalThis as any).useApi = () => ({
      getWorkers: vi.fn(),
      listOrphans: opts.listOrphans ?? vi.fn(),
      purgeOrphan: opts.purgeOrphan ?? vi.fn(),
    })
    const subscribeHandlers: Record<string, (data?: any) => void> = {}
    const subscribe = vi.fn((channel: string, handler: (data?: any) => void) => {
      subscribeHandlers[channel] = handler
    })
    ;(globalThis as any).useRealtime = () => ({ subscribe })
    const toastAdd = vi.fn()
    ;(globalThis as any).useToast = () => ({ add: toastAdd })
    const announce = vi.fn()
    ;(globalThis as any).useA11yAnnouncer = () => ({ announce })
    ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
    ;(globalThis as any).useRepositoryPlatform = () => ({ platformIconUrl: vi.fn() })
    ;(globalThis as any).useRoute = () => ({ query: {} })
    const navigateTo = vi.fn()
    ;(globalThis as any).navigateTo = navigateTo

    const defaults: Record<string, any[]> = {
      stacks_aggregate: [stackFixture],
      stack_card_workers: [],
      repos_for_stacks_empty: [{ id: 'repo-1' }],
    }
    const asyncDataRefs: Record<string, { data: any, refresh: ReturnType<typeof vi.fn> }> = {}
    ;(globalThis as any).useAsyncData = (key: string, fn?: () => Promise<any>) => {
      const data = ref(opts.asyncData?.[key] ?? defaults[key] ?? [])
      const refresh = vi.fn(async () => {
        if (fn) data.value = await fn()
      })
      asyncDataRefs[key] = { data, refresh }
      return { data, refresh }
    }

    const wrapper = mount(StacksPanel, {
      global: {
        components: { StackCard, StackRow, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
        stubs: dropdownStubs,
      },
    })

    return { wrapper, getList, getFullList, subscribeHandlers, toastAdd, announce, navigateTo, asyncDataRefs }
  }

  it('reports empty-state steps from the underlying repos/workers data', async () => {
    const noRepos = mountStacksPanel({
      asyncData: { stacks_aggregate: [], stack_card_workers: [], repos_for_stacks_empty: [] },
    })
    await flushPromises()
    expect((noRepos.wrapper.vm as any).emptyStateStep.ctaLabel).toBe('Add Repository')

    const noWorkers = mountStacksPanel({
      asyncData: { stacks_aggregate: [], stack_card_workers: [], repos_for_stacks_empty: [{ id: 'repo-1' }] },
    })
    await flushPromises()
    expect((noWorkers.wrapper.vm as any).emptyStateStep.ctaLabel).toBe('Add Worker')
    ;(noWorkers.wrapper.vm as any).emptyStateStep.action()
    expect(noWorkers.navigateTo).toHaveBeenCalledWith('/workers')

    const ready = mountStacksPanel({
      asyncData: {
        stacks_aggregate: [],
        stack_card_workers: [{ id: 'worker-1', status: 'ACTIVE' }],
        repos_for_stacks_empty: [{ id: 'repo-1' }],
      },
    })
    await flushPromises()
    expect((ready.wrapper.vm as any).emptyStateStep.ctaLabel).toBe('Add Stack')
    ;(ready.wrapper.vm as any).emptyStateStep.action()
    expect((ready.wrapper.vm as any).showCreate).toBe(true)
  })

  it('reacts to realtime stacks and repositories events', async () => {
    const { subscribeHandlers, getList, getFullList, announce } = mountStacksPanel()
    await flushPromises()

    const getListCallsBefore = getList.mock.calls.length
    subscribeHandlers.stacks!()
    await flushPromises()

    expect(getList.mock.calls.length).toBeGreaterThan(getListCallsBefore)
    expect(announce).toHaveBeenCalledWith('Stacks list updating')

    // debouncedRefreshStacksAggregate (300ms) + the isUpdating settle timer (500ms).
    await new Promise(resolve => setTimeout(resolve, 600))
    expect(announce).toHaveBeenCalledWith('Stacks list updated')

    const getFullListCallsBefore = getFullList.mock.calls.length
    const getListCallsBeforeRepo = getList.mock.calls.length
    subscribeHandlers.repositories!()
    await new Promise(resolve => setTimeout(resolve, 350))
    await flushPromises()

    expect(getList.mock.calls.length).toBeGreaterThan(getListCallsBeforeRepo)
    expect(getFullList.mock.calls.length).toBeGreaterThan(getFullListCallsBefore)
  })

  it('opens and closes the delete-stack modal', async () => {
    const { wrapper } = mountStacksPanel()
    await flushPromises()

    ;(wrapper.vm as any).openDelete(stackFixture)
    expect((wrapper.vm as any).showDelete).toBe(true)
    expect((wrapper.vm as any).deleteTarget).toEqual(stackFixture)

    ;(wrapper.vm as any).onDeleted()
    expect((wrapper.vm as any).showDelete).toBe(false)
    expect((wrapper.vm as any).deleteTarget).toBe(null)
  })

  it('closes the import modal and refreshes the list once a stack is imported', async () => {
    const { wrapper, getList } = mountStacksPanel()
    await flushPromises()
    ;(wrapper.vm as any).showImport = true

    const before = getList.mock.calls.length
    ;(wrapper.vm as any).onImported('new-stack-id')
    await flushPromises()

    expect((wrapper.vm as any).showImport).toBe(false)
    expect(getList.mock.calls.length).toBeGreaterThan(before)
  })

  it('builds group filter options from the fleet-wide aggregate, including ungrouped', async () => {
    const { wrapper } = mountStacksPanel({
      asyncData: {
        stacks_aggregate: [
          { ...stackFixture, group: 'infra' },
          { ...stackFixture, id: 'stack-2', group: '' },
        ],
      },
    })
    await flushPromises()

    const labels = (wrapper.vm as any).groupOptions.map((o: any) => o.label)
    expect(labels).toEqual(['All groups', 'Ungrouped', 'infra'])
  })

  it('reports a toast and announcement when purging an orphan fails', async () => {
    const purgeOrphan = vi.fn().mockRejectedValue(new Error('boom'))
    const listOrphans = vi.fn().mockResolvedValue([
      { dir_name: 'orphan-1', compose_file: 'docker-compose.yml', has_compose: true },
    ])
    const { wrapper, toastAdd, announce } = mountStacksPanel({ listOrphans, purgeOrphan })
    await flushPromises()

    await (wrapper.vm as any).openOrphans()
    await (wrapper.vm as any).handlePurge('orphan-1')

    expect(purgeOrphan).toHaveBeenCalledWith('orphan-1')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Failed to purge orphan-1', color: 'error' })
    expect(announce).toHaveBeenCalledWith('Failed to remove orphan directory orphan-1', 'assertive')
    expect((wrapper.vm as any).purgingDir).toBe('')
  })

  it('focuses the search input on "/" unless a typing target is already active', async () => {
    const { wrapper, announce } = mountStacksPanel()
    await flushPromises()

    const input = wrapper.find('input')
    const focusSpy = vi.spyOn(input.element as HTMLInputElement, 'focus')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: '/' }))
    await flushPromises()

    expect(focusSpy).toHaveBeenCalled()
    expect(announce).toHaveBeenCalledWith('Stack search focused')

    focusSpy.mockClear()
    const textarea = document.createElement('textarea')
    document.body.appendChild(textarea)
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    await flushPromises()

    expect(focusSpy).not.toHaveBeenCalled()
    textarea.remove()
  })
})
