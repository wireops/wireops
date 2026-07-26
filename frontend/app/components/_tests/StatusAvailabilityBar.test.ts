import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import StatusAvailabilityBar from '../StatusAvailabilityBar.vue'

const stubs = {
  UTooltip: {
    setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
      return () => h('div', [slots.default?.()])
    },
  },
}

const segments = [
  { key: 'active', label: 'Active', barClass: 'bg-emerald-400', dotClass: 'bg-emerald-400', statuses: ['active'] },
  { key: 'error', label: 'Error', barClass: 'bg-rose-400', dotClass: 'bg-rose-400', statuses: ['error'] },
]

function mountBar(items: any[], modelValue = 'all') {
  return mount(StatusAvailabilityBar, {
    props: { items, segments, modelValue, ariaLabel: 'Status breakdown' },
    global: { stubs },
  })
}

describe('StatusAvailabilityBar', () => {
  it('renders segment counts and percentages, and marks the active filter', () => {
    const wrapper = mountBar([
      { status: 'active' },
      { status: 'active' },
      { status: 'error' },
      { status: 'active' },
    ], 'active')

    const buttons = wrapper.findAll('button')
    expect(buttons.length).toBeGreaterThanOrEqual(2)
    expect(wrapper.text()).toContain('Active (3)')
    expect(wrapper.text()).toContain('Error (1)')

    const activeLegendButton = buttons.find(b => b.text().includes('Active (3)'))
    expect(activeLegendButton?.classes()).toContain('font-semibold')
  })

  it('emits update:modelValue with the segment key on click, and toggles back to all', async () => {
    const wrapper = mountBar([{ status: 'active' }, { status: 'error' }])

    const legendButtons = wrapper.findAll('button').filter(b => !b.attributes('style'))
    const errorButton = legendButtons.find(b => b.text().includes('Error'))
    await errorButton!.trigger('click')

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['error'])
  })

  it('groups unrecognized statuses into a disabled Other segment', async () => {
    const wrapper = mountBar([{ status: 'active' }, { status: 'weird' }])

    expect(wrapper.text()).toContain('Other (1)')

    const legendButtons = wrapper.findAll('button')
    const otherButton = legendButtons.find(b => b.text().includes('Other'))
    expect(otherButton?.attributes('disabled')).toBeDefined()

    await otherButton!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
