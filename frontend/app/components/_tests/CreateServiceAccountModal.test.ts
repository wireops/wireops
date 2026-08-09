import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import CreateServiceAccountModal from '../CreateServiceAccountModal.vue'

const stubs = {
  AppPanelCard: { setup: (_p: unknown, { slots }: any) => () => h('div', [slots.header?.(), slots.default?.()]) },
  UIcon: { template: '<span />' },
  UFormField: {
    props: ['label', 'error'],
    setup(props: { error?: string }, { slots }: any) {
      return () => h('div', [h('div', { class: 'error' }, props.error), slots.default?.()])
    },
  },
  AppTextInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string }, { emit }: any) {
      return () => h('input', {
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value),
      })
    },
  },
  AppSelectInput: { props: ['modelValue', 'items'], template: '<select />' },
  CancelButton: {
    emits: ['click'],
    setup(_p: unknown, { emit }: any) { return () => h('button', { class: 'cancel', onClick: () => emit('click') }) },
  },
  ActionButton: { props: ['type', 'label', 'icon', 'glow'], template: '<button type="submit">{{ label }}</button>' },
}

describe('CreateServiceAccountModal', () => {
  it('blocks submit and shows field errors when name and description are empty', async () => {
    const wrapper = mount(CreateServiceAccountModal, { global: { stubs } })

    await wrapper.get('form').trigger('submit')

    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(wrapper.findAll('.error').map(e => e.text())).toEqual(
      expect.arrayContaining(['Name is required', 'Description is required'])
    )
  })

  it('emits submit with the form payload when valid', async () => {
    const wrapper = mount(CreateServiceAccountModal, { global: { stubs } })

    const inputs = wrapper.findAll('input')
    await inputs[0].setValue('CI deployer')
    await inputs[1].setValue('Used by CI')
    await wrapper.get('form').trigger('submit')

    expect(wrapper.emitted('submit')?.[0]?.[0]).toEqual({
      name: 'CI deployer',
      description: 'Used by CI',
      role: 'viewer',
    })
  })
})
