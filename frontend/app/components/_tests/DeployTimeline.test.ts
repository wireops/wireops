import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, ref } from 'vue'
import DeployTimeline from '../DeployTimeline.vue'

interface PhaseItem {
  phase: string
  status: string
  duration_ms: number
  seq: number
  detail?: string
}

const stubs = {
  UIcon: {
    props: ['name'],
    setup(props: { name: string }) {
      return () => h('i', { class: props.name })
    },
  },
}

function stubGlobals(items: PhaseItem[]) {
  const globals = globalThis as unknown as {
    useNuxtApp: () => unknown
    useRealtime: () => unknown
    useAsyncData: () => unknown
  }
  globals.useNuxtApp = () => ({ $pb: { collection: () => ({}) } })
  globals.useRealtime = () => ({ subscribe: () => {} })
  globals.useAsyncData = () => ({
    data: ref({ items }),
    refresh: () => {},
  })
}

describe('DeployTimeline', () => {
  it('renders all 9 canonical phases in order when data is present', () => {
    stubGlobals([
      { phase: 'git_fetch', status: 'success', duration_ms: 120, seq: 0 },
      { phase: 'render', status: 'success', duration_ms: 45, seq: 1 },
      { phase: 'secrets_fetch', status: 'success', duration_ms: 20, seq: 2 },
      { phase: 'policy_check', status: 'skipped', duration_ms: 0, seq: 3 },
      { phase: 'dispatch', status: 'success', duration_ms: 900, seq: 4 },
      { phase: 'worker_ack', status: 'success', duration_ms: 30, seq: 5 },
      { phase: 'compose_up', status: 'success', duration_ms: 850, seq: 6 },
      { phase: 'post_check', status: 'success', duration_ms: 200, seq: 7 },
      { phase: 'notify', status: 'success', duration_ms: 5, seq: 8 },
    ])

    const wrapper = mount(DeployTimeline, {
      props: { syncLogId: 'log-1' },
      global: { stubs },
    })

    const text = wrapper.text()
    expect(text).toContain('Git Fetch')
    expect(text).toContain('Render')
    expect(text).toContain('Fetch Secrets')
    expect(text).toContain('Policy Check')
    expect(text).toContain('Dispatch')
    expect(text).toContain('Worker Received')
    expect(text).toContain('Compose Up')
    expect(text).toContain('Post-Check')
    expect(text).toContain('Notify')

    // Order in the DOM should follow the canonical order, not insertion order.
    const rows = wrapper.findAll('tbody > tr').map((r) => r.find('td').text())
    expect(rows).toEqual([
      'Git Fetch', 'Lint', 'Render', 'Fetch Secrets', 'Policy Check', 'Dispatch',
      'Worker Received', 'Compose Up', 'Post-Check', 'Notify',
    ])
  })

  it('shows a fallback message when the phases list is empty (old log, pre-migration)', () => {
    stubGlobals([])

    const wrapper = mount(DeployTimeline, {
      props: { syncLogId: 'old-log' },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('No timeline data for this deploy.')
    expect(wrapper.find('table').exists()).toBe(false)
  })

  it('marks a failed phase red but leaves its error detail for the alert, not the row', () => {
    stubGlobals([
      { phase: 'git_fetch', status: 'error', duration_ms: 300, detail: 'connection refused', seq: 0 },
    ])

    const wrapper = mount(DeployTimeline, {
      props: { syncLogId: 'log-2' },
      global: { stubs },
    })

    expect(wrapper.text()).not.toContain('connection refused')
    expect(wrapper.find('.text-red-500').exists()).toBe(true)
  })

  it('collapses every phase after the failure into a single Skipped placeholder', () => {
    stubGlobals([
      { phase: 'git_fetch', status: 'success', duration_ms: 120, seq: 0 },
      { phase: 'render', status: 'error', duration_ms: 45, detail: 'boom', seq: 1 },
    ])

    const wrapper = mount(DeployTimeline, {
      props: { syncLogId: 'log-4' },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('Skipped (7)')
    expect(wrapper.text()).not.toContain('Fetch Secrets')
  })

  it('formats duration in seconds when over 1000ms', () => {
    stubGlobals([
      { phase: 'compose_up', status: 'success', duration_ms: 2500, seq: 5 },
    ])

    const wrapper = mount(DeployTimeline, {
      props: { syncLogId: 'log-3' },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('2.5s')
  })
})
