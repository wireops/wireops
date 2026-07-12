import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import WorkerEnvBadges from '../WorkerEnvBadges.vue'

describe('WorkerEnvBadges', () => {
  const stubs = {
    UBadge: {
      props: ['label', 'color', 'variant', 'size', 'icon'],
      setup(props: { color?: string, label?: string }, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
        return () => h('span', { class: 'u-badge', 'data-color': props.color }, [slots.default?.(), props.label])
      },
    },
    UTooltip: {
      setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
        return () => h('div', slots.default?.())
      },
    },
  }

  it('renders badges for a fully reporting worker', () => {
    const worker = { os: 'linux', arch: 'amd64', docker_version: '27.0.0', docker_online: true, version: '1.2.3', compose_version: '2.27.0' }
    const wrapper = mount(WorkerEnvBadges, { props: { worker }, global: { stubs } })

    expect(wrapper.text()).toContain('linux')
    expect(wrapper.text()).toContain('amd64')
    expect(wrapper.text()).toContain('docker 27.0.0')
    expect(wrapper.text()).toContain('v1.2.3')
    expect(wrapper.text()).toContain('compose 2.27.0')
    expect(wrapper.findAll('.u-badge[data-color="warning"]').length).toBe(0)
  })

  it('falls back to the unknown label when os is unreported', () => {
    const worker = { docker_version: '27.0.0' }
    const wrapper = mount(WorkerEnvBadges, { props: { worker }, global: { stubs } })
    expect(wrapper.text()).toContain('unknown')
  })

  it('renders nothing for a legacy worker with no reported info', () => {
    const wrapper = mount(WorkerEnvBadges, { props: { worker: {} }, global: { stubs } })
    expect(wrapper.findAll('.u-badge').length).toBe(0)
  })

  it('shows a docker offline badge when the daemon is down', () => {
    const worker = { os: 'linux', docker_version: '27.0.0', docker_online: false }
    const wrapper = mount(WorkerEnvBadges, { props: { worker }, global: { stubs } })

    expect(wrapper.text()).toContain('docker offline')
    expect(wrapper.find('.u-badge[data-color="error"]').exists()).toBe(true)
  })

  it('shows a no-compose warning badge when compose_version is missing', () => {
    const worker = { os: 'linux', docker_version: '27.0.0', compose_version: '' }
    const wrapper = mount(WorkerEnvBadges, { props: { worker }, global: { stubs } })

    expect(wrapper.text()).toContain('no compose')
  })
})
