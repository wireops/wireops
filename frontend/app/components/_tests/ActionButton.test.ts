import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import ActionButton from '../ActionButton.vue'

const stubs = {
  UButton: {
    props: ['icon', 'label', 'color', 'variant', 'size', 'disabled', 'loading'],
    emits: ['click'],
    setup(props: Record<string, unknown>, { emit }: { emit: (e: string, ev: MouseEvent) => void }) {
      return () => h('button', {
        'data-icon': props.icon,
        'data-label': props.label,
        'data-color': props.color,
        'data-variant': props.variant,
        'data-size': props.size,
        disabled: props.disabled,
        'data-loading': props.loading,
        onClick: (ev: MouseEvent) => emit('click', ev),
      })
    },
  },
}

describe('ActionButton', () => {
  it('forwards props with default color/variant/size and glow class', () => {
    const wrapper = mount(ActionButton, {
      props: { icon: 'i-lucide-plus', label: 'Add Stack' },
      global: { stubs },
    })

    const button = wrapper.get('button')
    expect(button.attributes('data-icon')).toBe('i-lucide-plus')
    expect(button.attributes('data-label')).toBe('Add Stack')
    expect(button.attributes('data-color')).toBe('primary')
    expect(button.attributes('data-variant')).toBe('solid')
    expect(button.attributes('data-size')).toBe('md')
    expect(button.classes().join(' ')).toContain('shadow-[0_0_16px_rgba(255,198,0,0.35)]')
  })

  it('omits label when not provided', () => {
    const wrapper = mount(ActionButton, {
      props: { icon: 'i-lucide-plus' },
      global: { stubs },
    })

    expect(wrapper.get('button').attributes('data-label')).toBeUndefined()
  })

  it('overrides color/variant/size when passed', () => {
    const wrapper = mount(ActionButton, {
      props: { icon: 'i-lucide-plus', color: 'error', variant: 'outline', size: 'lg' },
      global: { stubs },
    })

    const button = wrapper.get('button')
    expect(button.attributes('data-color')).toBe('error')
    expect(button.attributes('data-variant')).toBe('outline')
    expect(button.attributes('data-size')).toBe('lg')
  })

  it('emits click', async () => {
    const wrapper = mount(ActionButton, {
      props: { icon: 'i-lucide-plus' },
      global: { stubs },
    })

    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
  })

  it('reflects disabled and loading state', () => {
    const wrapper = mount(ActionButton, {
      props: { icon: 'i-lucide-plus', disabled: true, loading: true },
      global: { stubs },
    })

    const button = wrapper.get('button')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('data-loading')).toBe('true')
  })
})
