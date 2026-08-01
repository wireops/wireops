import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, ref } from 'vue'
import StacksPanel from '../StacksPanel.vue'
import StackCard from '../StackCard.vue'
import GitProviderBadge from '../GitProviderBadge.vue'
import RepositoryIcon from '../RepositoryIcon.vue'
import GithubIcon from '../GithubIcon.vue'
import GenericIcon from '../GenericIcon.vue'

describe('StacksPanel', () => {
  it('renders a keyboard-focusable link for each stack with a status badge', () => {
    const refresh = vi.fn()
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        collection: () => ({
          getFullList: vi.fn(),
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
        data: ref([
          {
            id: 'stack-1',
            name: 'Payments',
            worker: 'worker-1',
            status: 'active',
            expand: {
              repository: { name: 'repo-a', platform: 'github', status: 'connected' },
              worker: { id: 'worker-1', hostname: 'worker-a', status: 'ACTIVE' },
            },
            containers_list: [],
          },
        ]),
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
        },
      },
    })

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
})
