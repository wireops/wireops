import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import CancelButton from '../CancelButton.vue'

const stubs = {
  UButton: {
    props: ['type', 'label', 'size', 'disabled', 'block', 'variant', 'color'],
    emits: ['click'],
    setup(props: Record<string, unknown>, { emit }: { emit: (e: string, ev: MouseEvent) => void }) {
      return () => h('button', {
        type: props.type,
        'data-label': props.label,
        'data-size': props.size,
        disabled: props.disabled,
        'data-block': props.block,
        'data-variant': props.variant,
        'data-color': props.color,
        onClick: (ev: MouseEvent) => { if (!props.disabled) emit('click', ev) },
      })
    },
  },
}

describe('CancelButton', () => {
  it('defaults to a neutral outline "Cancel" button', () => {
    const wrapper = mount(CancelButton, { global: { stubs } })

    const button = wrapper.get('button')
    expect(button.attributes('data-label')).toBe('Cancel')
    expect(button.attributes('data-variant')).toBe('outline')
    expect(button.attributes('data-color')).toBe('neutral')
    expect(button.attributes('type')).toBe('button')
    expect(button.attributes('data-size')).toBe('md')
    expect(button.attributes('data-block')).toBe('false')
    expect(button.attributes('disabled')).toBeUndefined()
  })

  it('overrides label, size, block, type and disabled', () => {
    const wrapper = mount(CancelButton, {
      props: { label: 'Close', size: 'sm', block: true, type: 'submit', disabled: true },
      global: { stubs },
    })

    const button = wrapper.get('button')
    expect(button.attributes('data-label')).toBe('Close')
    expect(button.attributes('data-size')).toBe('sm')
    expect(button.attributes('data-block')).toBe('true')
    expect(button.attributes('type')).toBe('submit')
    expect(button.attributes('disabled')).toBeDefined()
  })

  it('emits click', async () => {
    const wrapper = mount(CancelButton, { global: { stubs } })

    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
  })

  it('does not emit click when disabled', async () => {
    const wrapper = mount(CancelButton, {
      props: { disabled: true },
      global: { stubs },
    })

    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('click')).toBeUndefined()
  })
})
