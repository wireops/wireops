import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import CloseButton from '../CloseButton.vue'

const stubs = {
  UButton: {
    props: ['color', 'variant', 'icon', 'size', 'ariaLabel'],
    emits: ['click'],
    setup(props: Record<string, unknown>, { emit }: { emit: (e: string, ev: MouseEvent) => void }) {
      return () => h('button', {
        'data-color': props.color,
        'data-variant': props.variant,
        'data-icon': props.icon,
        'data-size': props.size,
        'aria-label': props.ariaLabel,
        onClick: (ev: MouseEvent) => emit('click', ev),
      })
    },
  },
}

describe('CloseButton', () => {
  it('defaults to a ghost neutral x-icon button labeled "Close"', () => {
    const wrapper = mount(CloseButton, { global: { stubs } })

    const button = wrapper.get('button')
    expect(button.attributes('data-color')).toBe('neutral')
    expect(button.attributes('data-variant')).toBe('ghost')
    expect(button.attributes('data-icon')).toBe('i-lucide-x')
    expect(button.attributes('data-size')).toBe('md')
    expect(button.attributes('aria-label')).toBe('Close')
  })

  it('forwards ariaLabel, size and class', () => {
    const wrapper = mount(CloseButton, {
      props: { ariaLabel: 'Close modal', size: 'xs' },
      attrs: { class: '-my-1' },
      global: { stubs },
    })

    const button = wrapper.get('button')
    expect(button.attributes('aria-label')).toBe('Close modal')
    expect(button.attributes('data-size')).toBe('xs')
    expect(button.classes()).toContain('-my-1')
  })

  it('emits click', async () => {
    const wrapper = mount(CloseButton, { global: { stubs } })

    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
  })
})
