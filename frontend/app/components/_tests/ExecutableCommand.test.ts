import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import ExecutableCommand from '../ExecutableCommand.vue'

const stubs = {
  UFormField: { props: ['label'], setup: (_p: unknown, { slots }: any) => () => h('div', slots.default?.()) },
  AppButtonInput: {
    props: ['icon', 'ariaLabel'],
    emits: ['click'],
    setup(props: { ariaLabel?: string }, { emit }: any) {
      return () => h('button', { 'aria-label': props.ariaLabel, onClick: () => emit('click') })
    },
  },
}

describe('ExecutableCommand', () => {
  beforeEach(() => {
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  })

  it('re-masks and requires another reveal when masked content changes', async () => {
    const wrapper = mount(ExecutableCommand, {
      props: { label: 'Token', content: 'secret-a', masked: true },
      global: { stubs },
    })

    await wrapper.get('[aria-label="Reveal"]').trigger('click')
    expect(wrapper.get('code').text()).toBe('secret-a')

    await wrapper.setProps({ content: 'secret-b' })
    expect(wrapper.get('code').text()).not.toBe('secret-b')
    expect(wrapper.get('[aria-label="Reveal"]')).toBeTruthy()

    await wrapper.get('[aria-label="Reveal"]').trigger('click')
    expect(wrapper.get('code').text()).toBe('secret-b')
  })
})
