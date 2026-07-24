import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import AppMultiSelectInput from '../AppMultiSelectInput.vue'

type Slots = Record<string, (() => unknown) | undefined>

const items = [
  { label: 'Alpha', value: 'alpha' },
  { label: 'Beta', value: 'beta' },
  { label: 'Gamma', value: 'gamma' },
]

const stubs = {
  PopoverRoot: {
    props: ['open'],
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', slots.default?.())
    },
  },
  PopoverTrigger: {
    props: ['asChild'],
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', slots.default?.())
    },
  },
  PopoverPortal: {
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', slots.default?.())
    },
  },
  PopoverContent: {
    setup(_props: unknown, { attrs, slots }: { attrs: Record<string, unknown>, slots: Slots }) {
      return () => h('div', attrs, slots.default?.())
    },
  },
  UIcon: {
    props: ['name'],
    setup() {
      return () => h('span')
    },
  },
}

function mountInput(props: Partial<InstanceType<typeof AppMultiSelectInput>['$props']> = {}) {
  return mount(AppMultiSelectInput, {
    props: {
      modelValue: [],
      items,
      id: 'worker-tags',
      ...props,
    },
    global: { stubs },
  })
}

describe('AppMultiSelectInput', () => {
  it('filters options and emits updated selections from keyboard and click toggles', async () => {
    const wrapper = mountInput({ modelValue: ['beta'] })

    const listbox = wrapper.get('[role="listbox"]')
    expect(listbox.attributes('aria-multiselectable')).toBe('true')
    expect(wrapper.get('[role="option"][aria-selected="true"]').text()).toContain('Beta')

    const search = wrapper.get('input[type="text"]')
    await search.setValue('alp')

    const filteredOptions = wrapper.findAll('[role="option"]')
    expect(filteredOptions).toHaveLength(1)
    expect(filteredOptions[0]?.text()).toContain('Alpha')
    expect(search.attributes('aria-activedescendant')).toBe('worker-tags-listbox-option-alpha')

    await search.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([['beta', 'alpha']])

    await wrapper.setProps({ modelValue: ['beta', 'alpha'] })
    await wrapper.get('[role="option"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([['beta']])
  })

  it('shows an empty state for searches with no results and does not emit on Enter', async () => {
    const wrapper = mountInput()

    const search = wrapper.get('input[type="text"]')
    await search.setValue('zzz')
    await search.trigger('keydown', { key: 'Enter' })

    expect(wrapper.findAll('[role="option"]')).toHaveLength(0)
    expect(wrapper.text()).toContain('No results found.')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('disables the trigger while disabled or loading', () => {
    expect(mountInput({ disabled: true }).get('button').attributes('disabled')).toBeDefined()

    const loading = mountInput({ loading: true })
    expect(loading.get('button').attributes('disabled')).toBeDefined()
    expect(loading.text()).toContain('Loading...')
  })
})
