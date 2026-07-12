import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'

describe('default layout accessibility', () => {
  beforeEach(() => {
    ;(globalThis as any).useAuth = () => ({
      isAuthenticated: true,
      logout: vi.fn(),
    })
    ;(globalThis as any).useRoute = () => ({
      path: '/stacks',
      fullPath: '/stacks',
    })
    ;(globalThis as any).useColorMode = () => ({
      value: 'dark',
      preference: 'dark',
    })
    ;(globalThis as any).useKeyboard = () => ({
      isShowingHelp: { value: false },
      shortcuts: [],
    })
    ;(globalThis as any).useA11yAnnouncer = () => ({
      announce: vi.fn(),
    })
    ;(globalThis as any).usePermissions = () => ({
      isViewer: ref(false),
    })
    ;(globalThis as any).useCookie = (_key: string, opts?: { default?: () => any }) => {
      const val = opts?.default ? opts.default() : undefined
      return { value: val }
    }
    ;(globalThis as any).useHead = vi.fn()
  })

  it('renders a single main landmark with the skip-link target id', async () => {
    const DefaultLayout = (await import('../default.vue')).default
    const wrapper = mount(DefaultLayout, {
      slots: {
        default: '<div>Page content</div>',
      },
      global: {
        stubs: {
          AppSidebar: { template: '<aside />' },
          AppCommandPalette: { template: '<div />' },
          UButton: {
            props: ['label', 'icon'],
            template: '<button v-bind="$attrs">{{ label }}<slot /></button>',
          },
          UIcon: { template: '<span />' },
          UModal: { template: '<div><slot name="content" /></div>' },
          UCard: { template: '<section><slot name="header" /><slot /></section>' },
          NuxtLink: { template: '<a><slot /></a>' },
        },
      },
    })

    const mains = wrapper.findAll('main')
    expect(mains).toHaveLength(1)
    expect(mains[0]?.attributes('id')).toBe('main-content')
    expect(mains[0]?.attributes('tabindex')).toBe('-1')
  })
})
