import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import StackRow from '../StackRow.vue'

const stackFixture = {
  id: 'stack-1',
  name: 'Payments',
  worker: 'worker-1',
  status: 'active',
  group: 'billing',
  deployed_at: '2024-01-01 00:00:00.000Z',
  last_synced_at: new Date(Date.now() - 5 * 60_000).toISOString(),
  expand: {
    repository: { name: 'repo-a', platform: 'github', status: 'connected' },
    worker: { id: 'worker-1', hostname: 'worker-a', status: 'ACTIVE' },
  },
}

const workersById = {
  'worker-1': { id: 'worker-1', hostname: 'worker-a', status: 'ACTIVE' },
}

function mountRow() {
  return mount(StackRow, {
    props: { stack: stackFixture, workersById },
    global: {
      stubs: {
        BadgeStatus: {
          props: ['status'],
          setup(props) {
            return () => h('span', { class: 'badge-status' }, props.status)
          },
        },
        UBadge: {
          props: ['label'],
          setup(props) {
            return () => h('span', { class: 'badge' }, props.label)
          },
        },
        UTooltip: { setup(_props, { slots }) { return () => h('div', slots.default?.()) } },
        UIcon: { setup() { return () => h('span') } },
        NuxtLink: {
          props: ['to'],
          setup(props, { attrs, slots }) {
            return () => h('a', { href: props.to, ...attrs }, slots.default?.())
          },
        },
      },
    },
  })
}

describe('StackRow', () => {
  it('renders a single accessible link with name, status, sync, deploy and worker info', () => {
    const wrapper = mountRow()

    const link = wrapper.find('a[aria-label="Open stack Payments"]')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toBe('/stacks/stack-1')

    expect(wrapper.text()).toContain('Payments')
    expect(wrapper.text()).toContain('billing')
    expect(wrapper.find('.badge-status').text()).toBe('active')
    expect(wrapper.text()).toContain('Up to date')
    expect(wrapper.text()).toContain('Online')
  })
})
