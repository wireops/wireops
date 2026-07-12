import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import WorkerSystemInfoCard from '../WorkerSystemInfoCard.vue'

type Slots = Record<string, (() => unknown) | undefined>

describe('WorkerSystemInfoCard', () => {
  const stubs = {
    UCard: {
      setup(_props: unknown, { slots }: { slots: Slots }) {
        return () => h('div', [slots.header?.(), slots.default?.()])
      },
    },
    UAlert: {
      props: ['color', 'variant', 'icon', 'title', 'description'],
      setup(props: { color?: string, title?: string }) {
        return () => h('div', { class: 'u-alert', 'data-color': props.color }, [h('span', { class: 'alert-title' }, props.title)])
      },
    },
    UBadge: {
      props: ['label', 'color', 'variant', 'size', 'icon'],
      setup(props: { label?: string }, { slots }: { slots: Slots }) {
        return () => h('span', { class: 'u-badge' }, [props.label, slots.default?.()])
      },
    },
    UProgress: {
      props: ['modelValue', 'size', 'color'],
      setup(props: { modelValue?: number, color?: string }) {
        return () => h('div', { class: 'u-progress', 'data-value': props.modelValue, 'data-color': props.color })
      },
    },
    UIcon: {
      props: ['name'],
      setup() {
        return () => h('span', { class: 'u-icon' })
      },
    },
    UTooltip: {
      setup(_props: unknown, { slots }: { slots: Slots }) {
        return () => h('div', slots.default?.())
      },
    },
  }

  it('renders complete worker data without warnings', () => {
    const worker = {
      status: 'ACTIVE',
      version: '1.2.3',
      os: 'linux',
      arch: 'amd64',
      docker_version: '27.0.0',
      compose_version: '2.27.0',
      docker_online: true,
      cpu_usage: 42,
      memory_usage: 63,
      disk_usage: 91,
    }
    const wrapper = mount(WorkerSystemInfoCard, { props: { worker }, global: { stubs } })

    expect(wrapper.text()).not.toContain('v1.2.3')
    expect(wrapper.text()).toContain('linux')
    expect(wrapper.text()).toContain('amd64')
    expect(wrapper.text()).toContain('docker 27.0.0')
    expect(wrapper.findAll('.u-alert').length).toBe(0)

    const progress = wrapper.findAll('.u-progress')
    expect(progress[0]?.attributes('data-value')).toBe('42')
    expect(progress[1]?.attributes('data-value')).toBe('63')
    expect(progress[2]?.attributes('data-value')).toBe('91')
    expect(progress[2]?.attributes('data-color')).toBe('error')
  })

  it('falls back cleanly for legacy worker with no reported data', () => {
    const worker = {}
    const wrapper = mount(WorkerSystemInfoCard, { props: { worker }, global: { stubs } })

    expect(wrapper.text()).toContain('No telemetry reported yet.')

    const alerts = wrapper.findAll('.u-alert')
    expect(alerts.length).toBe(1)
    expect(alerts[0]?.text()).toContain('Outdated agent')
  })

  it('shows docker offline warning when worker has reported info but docker is down', () => {
    const worker = { status: 'ACTIVE', os: 'linux', docker_version: '27.0.0', docker_online: false }
    const wrapper = mount(WorkerSystemInfoCard, { props: { worker }, global: { stubs } })

    const alerts = wrapper.findAll('.u-alert')
    const titles = alerts.map(a => a.text())
    expect(titles.some(t => t.includes('Docker offline'))).toBe(true)
  })

  it('shows compose missing warning when compose_version is absent', () => {
    const worker = { os: 'linux', docker_version: '27.0.0', compose_version: '' }
    const wrapper = mount(WorkerSystemInfoCard, { props: { worker }, global: { stubs } })

    const alerts = wrapper.findAll('.u-alert')
    expect(alerts.some(a => a.text().includes('Compose not found'))).toBe(true)
  })

  it('shows real zero telemetry percentages when info has been reported', () => {
    const worker = { os: 'darwin', docker_version: '27.0.0', cpu_usage: 0, memory_usage: 0, disk_usage: 0 }
    const wrapper = mount(WorkerSystemInfoCard, { props: { worker }, global: { stubs } })

    expect(wrapper.text()).not.toContain('No telemetry reported yet.')
    const progress = wrapper.findAll('.u-progress')
    expect(progress.length).toBe(3)
    expect(progress[0]?.attributes('data-value')).toBe('0')
  })

  it('rounds usage percentages to two decimal places', () => {
    const worker = { os: 'linux', docker_version: '27.0.0', disk_usage: 59.42506116413342 }
    const wrapper = mount(WorkerSystemInfoCard, { props: { worker }, global: { stubs } })

    const progress = wrapper.findAll('.u-progress')
    expect(progress[2]?.attributes('data-value')).toBe('59.43')
    expect(wrapper.text()).toContain('59.43%')
    expect(wrapper.text()).not.toContain('59.42506116413342')
  })
})
