import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import StacksPanel from '../StacksPanel.vue'

describe('StacksPanel', () => {
  it('renders a keyboard-focusable link for each stack and keeps sync separate', () => {
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
        stubs: {
          UCard: { template: '<section><slot name="header" /><slot /></section>' },
          UButton: {
            props: ['label', 'icon', 'ariaLabel'],
            template: '<button v-bind="$attrs">{{ label }}<slot /></button>',
          },
          UInput: { template: '<div><input /></div>' },
          USelect: { template: '<select />' },
          UTooltip: { template: '<div><slot /></div>' },
          UIcon: { template: '<span />' },
          NuxtLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
          CreateStackModal: true,
          BadgeLabel: true,
          DeleteStackModal: true,
          StackSyncModal: true,
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
    expect(wrapper.find('[aria-label="Git: Connected"]').classes()).toContain('text-cyan-500')

    const syncButtons = wrapper.findAll('button')
    expect(syncButtons.some(button =>
      button.attributes('aria-label') === 'Sync stack Payments' || button.text().includes('Sync')
    )).toBe(true)
  })
})
