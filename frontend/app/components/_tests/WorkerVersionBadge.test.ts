import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { computed, h } from 'vue'
import WorkerVersionBadge from '../WorkerVersionBadge.vue'

const stubs = {
  UBadge: {
    props: ['label', 'color', 'variant', 'size', 'icon'],
    setup(props: { color?: string, label?: string, icon?: string }, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
      return () => h('span', { class: 'u-badge', 'data-color': props.color, 'data-icon': props.icon }, [slots.default?.(), props.label])
    },
  },
  UTooltip: {
    props: ['text'],
    setup(props: { text?: string }, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
      return () => h('div', { 'data-tooltip': props.text }, slots.default?.())
    },
  },
}

let serverVersion: string | null = null
vi.stubGlobal('useServerVersion', () => ({ serverVersion: computed(() => serverVersion) }))

describe('WorkerVersionBadge', () => {
  it('renders a plain neutral badge when the worker matches the server version', () => {
    serverVersion = '1.4.0'
    const wrapper = mount(WorkerVersionBadge, { props: { version: '1.4.0' }, global: { stubs } })

    expect(wrapper.text()).toContain('v1.4.0')
    expect(wrapper.find('.u-badge[data-color="warning"]').exists()).toBe(false)
  })

  it('renders an orange alert badge when the worker version is behind the server', () => {
    serverVersion = '1.5.0'
    const wrapper = mount(WorkerVersionBadge, { props: { version: '1.4.0' }, global: { stubs } })

    const badge = wrapper.find('.u-badge[data-color="warning"]')
    expect(badge.exists()).toBe(true)
    expect(badge.attributes('data-icon')).toBe('i-lucide-alert-triangle')
    expect(wrapper.text()).toContain('v1.4.0')
  })

  it('does not flag a non-numeric server version (e.g. "dev") as outdated', () => {
    serverVersion = 'dev'
    const wrapper = mount(WorkerVersionBadge, { props: { version: '1.4.0' }, global: { stubs } })

    expect(wrapper.find('.u-badge[data-color="warning"]').exists()).toBe(false)
  })

  it('still shows the missing-version fallback when the worker reports no version', () => {
    serverVersion = '1.5.0'
    const wrapper = mount(WorkerVersionBadge, { props: { version: '' }, global: { stubs } })

    expect(wrapper.text()).toContain('outdated agent')
  })
})
