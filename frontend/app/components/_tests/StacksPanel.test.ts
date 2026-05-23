import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import StacksPanel from '../StacksPanel.vue'

describe('StacksPanel', () => {
  it('renders a keyboard-focusable link for each stack and keeps sync separate', () => {
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        collection: () => ({
          getFullList: vi.fn(),
        }),
      },
    })
    ;(globalThis as any).useApi = () => ({
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
    ;(globalThis as any).useAsyncData = () => ({
      data: ref([
        {
          id: 'stack-1',
          name: 'Payments',
          status: 'active',
          expand: {
            repository: { name: 'repo-a', platform: 'github' },
            worker: { hostname: 'worker-a' },
          },
          containers_list: [],
        },
      ]),
      refresh: vi.fn(),
    })

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

    const syncButtons = wrapper.findAll('button')
    expect(syncButtons.some(button =>
      button.attributes('aria-label') === 'Sync stack Payments' || button.text().includes('Sync')
    )).toBe(true)
  })
})
