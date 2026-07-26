import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AppTextArea from '../AppTextArea.vue'

describe('AppTextArea', () => {
  it('emits update:modelValue on input', async () => {
    const wrapper = mount(AppTextArea, { props: { modelValue: '' } })
    const textarea = wrapper.get('textarea')

    await textarea.setValue('hello')

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['hello'])
  })

  it('applies disabled, readonly, and rows props', () => {
    const wrapper = mount(AppTextArea, {
      props: { modelValue: '', disabled: true, readonly: true, rows: 8 },
    })
    const textarea = wrapper.get('textarea')

    expect(textarea.attributes('disabled')).toBeDefined()
    expect(textarea.attributes('readonly')).toBeDefined()
    expect(textarea.attributes('rows')).toBe('8')
  })

  it('defaults rows to 4 when not provided', () => {
    const wrapper = mount(AppTextArea, { props: { modelValue: '' } })
    expect(wrapper.get('textarea').attributes('rows')).toBe('4')
  })
})
