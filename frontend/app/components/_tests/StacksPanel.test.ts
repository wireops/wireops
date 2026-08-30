import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h, ref } from 'vue'
import StacksPanel from '../StacksPanel.vue'
import StackCard from '../StackCard.vue'
import GitProviderBadge from '../GitProviderBadge.vue'
import RepositoryIcon from '../RepositoryIcon.vue'
import GithubIcon from '../GithubIcon.vue'
import GenericIcon from '../GenericIcon.vue'

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
        components: { StackCard, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
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
        components: { StackCard, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
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
        components: { StackCard, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
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

  it('selects Import and Manage Orphans from the actions dropdown', async () => {
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
        components: { StackCard, GitProviderBadge, RepositoryIcon, GithubIcon, GenericIcon },
        stubs: dropdownStubs,
      },
    })

    await flushPromises()

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
})
