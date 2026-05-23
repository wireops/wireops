import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AppSidebar from '../AppSidebar.vue'

describe('AppSidebar', () => {
  const navItems = [
    { label: 'Dashboard', icon: 'i-home', to: '/' },
    {
      label: 'Workloads',
      icon: 'i-box',
      to: '/workloads',
      children: [
        { label: 'Stacks', icon: 'i-stack', to: '/stacks' },
      ],
    },
  ]

  it('exposes submenu state and current page semantics', () => {
    const wrapper = mount(AppSidebar, {
      props: {
        navItems,
        currentPath: '/stacks',
        colorModeValue: 'dark',
      },
      global: {
        stubs: {
          NuxtLink: { template: '<a v-bind="$attrs"><slot /></a>' },
          UIcon: { template: '<span />' },
          UButton: {
            inheritAttrs: false,
            props: ['label', 'icon', 'to'],
            template: `
              <button v-if="!to" v-bind="$attrs">
                {{ label }}
                <slot />
              </button>
              <a v-else v-bind="$attrs">
                {{ label }}
                <slot />
              </a>
            `,
          },
        },
      },
    })

    const buttons = wrapper.findAll('button')
    const submenuToggle = buttons.find(button => button.attributes('aria-controls')?.includes('nav-section-workloads'))
    expect(submenuToggle?.attributes('aria-expanded')).toBe('true')

    const currentPageLink = wrapper.find('[aria-current="page"]')
    expect(currentPageLink.exists()).toBe(true)
    expect(currentPageLink.text()).toContain('Stacks')
  })
})
