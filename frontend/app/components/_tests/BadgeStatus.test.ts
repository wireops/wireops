import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import BadgeStatus from '../BadgeStatus.vue'

const stubs = {
  UBadge: {
    props: ['label', 'color', 'variant', 'size', 'icon'],
    setup(props: { color?: string, label?: string, icon?: string }, { attrs }: { attrs: Record<string, unknown> }) {
      return () => h('span', {
        class: 'u-badge',
        'data-color': props.color,
        'data-icon': props.icon,
        'data-aria-label': attrs['aria-label'],
        title: attrs.title,
      }, props.label)
    },
  },
}

describe('BadgeStatus', () => {
  it.each([
    ['active', 'success'],
    ['success', 'success'],
    ['running', 'success'],
    ['error', 'error'],
    ['exited', 'error'],
    ['paused', 'warning'],
    ['pending', 'warning'],
    ['queued', 'warning'],
    ['stalled', 'warning'],
    ['degraded', 'warning'],
    ['noop', 'neutral'],
    ['syncing', 'primary'],
    ['something-unknown', 'neutral'],
  ])('maps status %s to color %s', (status, color) => {
    const wrapper = mount(BadgeStatus, { props: { status }, global: { stubs } })
    expect(wrapper.find('.u-badge').attributes('data-color')).toBe(color)
    // Uppercase display is a CSS text-transform (class="uppercase"), not a
    // JS transform - the actual label text stays whatever the case was.
    expect(wrapper.text()).toBe(status)
  })

  it('relabels "connected" as "Up to date" without changing its color', () => {
    const wrapper = mount(BadgeStatus, { props: { status: 'connected' }, global: { stubs } })
    expect(wrapper.find('.u-badge').attributes('data-color')).toBe('success')
    expect(wrapper.text()).toBe('Up to date')
  })

  it('renders a single label badge by default (mobileIconOnly off)', () => {
    const wrapper = mount(BadgeStatus, { props: { status: 'running' }, global: { stubs } })
    expect(wrapper.findAll('.u-badge').length).toBe(1)
  })

  it.each([
    ['running', 'i-lucide-check-circle-2'],
    ['error', 'i-lucide-x-circle'],
    ['paused', 'i-lucide-pause-circle'],
    ['pending', 'i-lucide-clock'],
    ['stalled', 'i-lucide-alert-circle'],
    ['noop', 'i-lucide-minus-circle'],
    ['syncing', 'i-lucide-refresh-cw'],
    ['something-unknown', 'i-lucide-circle'],
  ])('renders an icon-only badge for status %s when mobileIconOnly is set', (status, icon) => {
    const wrapper = mount(BadgeStatus, { props: { status, mobileIconOnly: true }, global: { stubs } })
    const badges = wrapper.findAll('.u-badge')
    expect(badges.length).toBe(2)
    expect(badges[0]?.attributes('data-icon')).toBe(icon)
    expect(badges[0]?.attributes('data-aria-label')).toBe(status)
    expect(badges[1]?.text()).toBe(status)
  })

  it('forwards extra attributes (e.g. class) to both badges when mobileIconOnly is set', () => {
    const wrapper = mount(BadgeStatus, {
      props: { status: 'running', mobileIconOnly: true },
      attrs: { class: 'shrink-0' },
      global: { stubs },
    })
    const badges = wrapper.findAll('.u-badge')
    for (const badge of badges) {
      expect(badge.classes()).toContain('shrink-0')
    }
  })
})
