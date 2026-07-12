import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import DeployTimeline from '../DeployTimeline.vue'

const stubs = {
  UIcon: { props: ['name'], template: '<i :class="name" />' },
}

function stubGlobals(items: any[]) {
  ;(globalThis as any).useNuxtApp = () => ({ $pb: { collection: () => ({}) } })
  ;(globalThis as any).useRealtime = () => ({ subscribe: () => {} })
  ;(globalThis as any).useAsyncData = () => ({
    data: ref({ items }),
    refresh: () => {},
  })
}

describe('DeployTimeline', () => {
  it('renders all 8 canonical phases in order when data is present', () => {
    stubGlobals([
      { phase: 'git_fetch', status: 'success', duration_ms: 120, seq: 0 },
      { phase: 'render', status: 'success', duration_ms: 45, seq: 1 },
      { phase: 'policy_check', status: 'skipped', duration_ms: 0, seq: 2 },
      { phase: 'dispatch', status: 'success', duration_ms: 900, seq: 3 },
      { phase: 'worker_ack', status: 'success', duration_ms: 30, seq: 4 },
      { phase: 'compose_up', status: 'success', duration_ms: 850, seq: 5 },
      { phase: 'post_check', status: 'success', duration_ms: 200, seq: 6 },
      { phase: 'notify', status: 'success', duration_ms: 5, seq: 7 },
    ])

    const wrapper = mount(DeployTimeline, {
      props: { syncLogId: 'log-1' },
      global: { stubs },
    })

    const text = wrapper.text()
    expect(text).toContain('Git Fetch')
    expect(text).toContain('Render')
    expect(text).toContain('Policy Check')
    expect(text).toContain('Dispatch')
    expect(text).toContain('Worker Received')
    expect(text).toContain('Compose Up')
    expect(text).toContain('Post-Check')
    expect(text).toContain('Notify')

    // Order in the DOM should follow the canonical order, not insertion order.
    const rows = wrapper.findAll('tbody > tr').map((r) => r.find('td').text())
    expect(rows).toEqual([
      'Git Fetch', 'Render', 'Policy Check', 'Dispatch',
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

  it('shows error detail for a failed phase', () => {
    stubGlobals([
      { phase: 'git_fetch', status: 'error', duration_ms: 300, detail: 'connection refused', seq: 0 },
    ])

    const wrapper = mount(DeployTimeline, {
      props: { syncLogId: 'log-2' },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('connection refused')
    expect(wrapper.find('.text-red-500').exists()).toBe(true)
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
