import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import EnvValueInput from '../EnvValueInput.vue'

const stubs = {
  UIcon: true,
  AppTextInput: {
    props: ['modelValue', 'type', 'disabled', 'readonly'],
    emits: ['update:modelValue', 'focus'],
    setup(props: any, { emit, slots }: any) {
      return () => h('div', [h('input', {
        value: props.modelValue, type: props.type, disabled: props.disabled, readonly: props.readonly,
        onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value),
        onFocus: (event: FocusEvent) => emit('focus', event),
      }), slots.trailing?.()])
    },
  },
}

function editor(props: Record<string, unknown> = {}) {
  const wrapper = mount(EnvValueInput, {
    props: { modelValue: '', ...props, 'onUpdate:modelValue': (value: string) => wrapper.setProps({ modelValue: value }) },
    global: { stubs },
  })
  return wrapper
}

describe('EnvValueInput', () => {
  it('opens an existing multiline value on keyboard focus and restores it on Escape', async () => {
    const value = 'first\nlast\n'
    const wrapper = editor({ modelValue: value })
    await wrapper.get('input').trigger('focus')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe(value)
    await wrapper.get('textarea').setValue('replacement\nvalue')
    await wrapper.get('textarea').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.props('modelValue')).toBe(value)
  })

  it('keeps single-line editing compact and read-only inspection explicit', async () => {
    const wrapper = editor({ modelValue: 'single line' })
    await wrapper.get('input').trigger('focus')
    expect(wrapper.find('textarea').exists()).toBe(false)
    await wrapper.setProps({ modelValue: 'first\nlast', readonly: true })
    await wrapper.get('input').trigger('focus')
    expect(wrapper.find('textarea').exists()).toBe(false)
    await wrapper.get('[aria-label="Expand value"]').trigger('click')
    await wrapper.get('textarea').trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('lets the expanded textarea handle paste without altering the stored value twice', async () => {
    const wrapper = editor({ modelValue: 'first\nlast' })
    await wrapper.get('[aria-label="Expand value"]').trigger('click')
    await wrapper.get('textarea').trigger('paste', { clipboardData: { getData: () => 'pasted\ntext' } })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('textarea').setValue('pasted\ntext')
    expect(wrapper.props('modelValue')).toBe('pasted\ntext')
  })

  it('expands, preserves Enter, and keeps changes on Done', async () => {
    const wrapper = editor({ modelValue: 'old' })
    await wrapper.get('[aria-label="Expand value"]').trigger('click')
    await wrapper.get('textarea').setValue('first\nlast\n')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter' })
    expect(wrapper.find('textarea').exists()).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === 'Done')!.trigger('click')
    expect(wrapper.props('modelValue')).toBe('first\nlast\n')
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.get('input').attributes('readonly')).toBeDefined()
  })

  it('cancels back to the value from before expansion', async () => {
    const wrapper = editor({ modelValue: 'original\nvalue' })
    await wrapper.get('[aria-label="Expand value"]').trigger('click')
    await wrapper.get('textarea').setValue('changed\nvalue')
    await wrapper.findAll('button').find(button => button.text() === 'Cancel')!.trigger('click')
    expect(wrapper.props('modelValue')).toBe('original\nvalue')
  })

  it('captures multiline paste into a password field before browser sanitization', async () => {
    const wrapper = editor({ modelValue: 'prefix-suffix', type: 'password' })
    const input = wrapper.get('input')
    ;(input.element as HTMLInputElement).setSelectionRange(7, 7)
    const value = '{\n"private_key":"FAKE\\nKEY"\n}'
    await input.trigger('paste', { clipboardData: { getData: () => value } })
    expect(wrapper.props('modelValue')).toBe(`prefix-${value}suffix`)
    expect(wrapper.find('textarea').exists()).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === 'Cancel')!.trigger('click')
    expect(wrapper.props('modelValue')).toBe('prefix-suffix')
  })

  it('allows read-only multiline inspection and drops it when hidden', async () => {
    const wrapper = editor({ modelValue: 'secret\nvalue', disabled: true })
    await wrapper.get('[aria-label="Expand value"]').trigger('click')
    expect(wrapper.get('textarea').attributes('readonly')).toBeDefined()
    expect(wrapper.text()).not.toContain('Cancel')
    await wrapper.setProps({ modelValue: '••••••••', type: 'password' })
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.find('[aria-label="Expand value"]').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('secret')
  })

  it('leaves single-line paste and locked values untouched', async () => {
    const wrapper = editor()
    await wrapper.get('input').trigger('paste', { clipboardData: { getData: () => 'plain' } })
    expect(wrapper.find('textarea').exists()).toBe(false)
    await wrapper.setProps({ disabled: true })
    await wrapper.get('input').trigger('paste', { clipboardData: { getData: () => 'first\nlast' } })
    expect(wrapper.props('modelValue')).toBe('')
  })
})
