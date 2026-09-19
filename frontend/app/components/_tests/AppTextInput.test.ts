import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { computed, defineComponent, h } from 'vue'
import { formFieldInjectionKey } from '@nuxt/ui/composables/useFormField'
import AppTextInput from '../AppTextInput.vue'

// Exercises the real useFormField composable (not stubbed) by providing the
// same injection key a wrapping UFormField would, rather than mounting
// UFormField itself - consistent with how the rest of the suite avoids
// mounting real Nuxt UI components.
function mountWithFormField(formFieldState: Record<string, any>, props: Record<string, any> = {}) {
  const Host = defineComponent({
    setup() {
      return () => h(AppTextInput, { modelValue: '', ...props })
    },
  })
  return mount(Host, {
    global: {
      provide: {
        [formFieldInjectionKey as unknown as string]: computed(() => formFieldState),
      },
    },
  })
}

describe('AppTextInput', () => {
  it('falls back to the wrapping UFormField\'s generated aria attributes when no explicit override is given', () => {
    const wrapper = mountWithFormField({ error: 'Required', ariaId: 'field-1' })
    const input = wrapper.find('input')

    expect(input.attributes('aria-invalid')).toBe('true')
    expect(input.attributes('aria-describedby')).toBe('field-1-error')
  })

  it('lets an explicit ariaInvalid/ariaDescribedby override the generated attributes', () => {
    const wrapper = mountWithFormField(
      { error: 'Required', ariaId: 'field-1' },
      { ariaInvalid: false, ariaDescribedby: 'custom-hint' }
    )
    const input = wrapper.find('input')

    expect(input.attributes('aria-invalid')).toBe('false')
    expect(input.attributes('aria-describedby')).toBe('custom-hint')
  })

  it('has no aria-invalid/aria-describedby when not wrapped in a UFormField', () => {
    const wrapper = mount(AppTextInput, { props: { modelValue: '' } })
    const input = wrapper.find('input')

    expect(input.attributes('aria-invalid')).toBeUndefined()
    expect(input.attributes('aria-describedby')).toBeUndefined()
  })
})
