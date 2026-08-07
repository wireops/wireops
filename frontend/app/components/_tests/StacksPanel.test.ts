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
        stubs: {
          BadgeStatus: { props: ['status'], setup(props) { return () => h('span', props.status) } },
          UCard: { setup(_props, { slots }) { return () => h('section', [slots.header?.(), slots.default?.()]) } },
          UButton: {
            props: ['label', 'icon', 'ariaLabel'],
            setup(props, { attrs, slots }) {
              return () => h('button', attrs, [props.label, slots.default?.()])
            },
          },
          AppTextInput: { setup() { return () => h('div', [h('input')]) } },
          AppSelectInput: { setup() { return () => h('select') } },
          UTooltip: { setup(_props, { slots }) { return () => h('div', slots.default?.()) } },
          UIcon: { setup() { return () => h('span') } },
          NuxtLink: {
            props: ['to'],
            setup(props, { attrs, slots }) {
              return () => h('a', { href: props.to, ...attrs }, slots.default?.())
            },
          },
          CreateStackModal: true,
          BadgeLabel: true,
          DeleteStackModal: true,
          ImportStackModal: true,
          StackContainersList: true,
          UModal: { setup(_props, { slots }) { return () => h('div', slots.body?.()) } },
          UPagination: { setup() { return () => h('div') } },
          UDropdownMenu: { setup(_props, { slots }) { return () => h('div', slots.default?.()) } },
        },
      },
    })

    await flushPromises()

    expect(getList).toHaveBeenCalled()
    expect(getList.mock.calls[0]![2]).toMatchObject({ sort: 'name' })
  })
})
