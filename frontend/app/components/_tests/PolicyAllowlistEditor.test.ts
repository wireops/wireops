import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import PolicyAllowlistEditor from '../PolicyAllowlistEditor.vue'

const stubs = {
  AppTextInput: {
    inheritAttrs: false,
    props: ['modelValue', 'placeholder', 'readonly'],
    emits: ['update:modelValue', 'focus', 'blur', 'keyup'],
    setup(
      props: { modelValue?: string, placeholder?: string, readonly?: boolean },
      { attrs, emit }: { attrs: Record<string, unknown>, emit: (event: string, ...args: unknown[]) => void }
    ) {
      return () => h('input', {
        ...attrs,
        value: props.modelValue,
        placeholder: props.placeholder,
        readonly: props.readonly,
        onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value),
        onFocus: (event: FocusEvent) => emit('focus', event),
        onBlur: (event: FocusEvent) => emit('blur', event),
        onKeyup: (event: KeyboardEvent) => emit('keyup', event),
      })
    },
  },
  UButton: {
    inheritAttrs: false,
    props: ['label'],
    emits: ['click'],
    template: '<button type="button" v-bind="$attrs" @click="$emit(\'click\', $event)">{{ label }}<slot /></button>',
  },
  UModal: {
    props: ['open'],
    template: '<div v-if="open"><slot name="content" /></div>',
  },
  DeleteRuleModal: {
    props: ['value'],
    emits: ['confirm', 'cancel'],
    template: `
      <div data-testid="delete-rule-modal">
        <span>{{ value }}</span>
        <button type="button" data-testid="confirm-delete" @click="$emit('confirm')">Remove Rule</button>
        <button type="button" data-testid="cancel-delete" @click="$emit('cancel')">Cancel</button>
      </div>
    `,
  },
}

const props = {
  placeholder: 'e.g. NET_ADMIN',
  emptyText: 'No restrictions — all entries permitted.',
  addLabel: 'Add Entry',
}

function mountEditor(modelValue: string[]) {
  return mount(PolicyAllowlistEditor, {
    props: { ...props, modelValue },
    global: { stubs },
  })
}

const wait = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

describe('PolicyAllowlistEditor', () => {
  it('removes a blank newly added entry on blur without emitting save', async () => {
    const wrapper = mountEditor([])

    await wrapper.get('button').trigger('click') // "Add Entry"
    expect(wrapper.findAll('input')).toHaveLength(1)

    const input = wrapper.get('input')
    await input.trigger('focus')
    expect(input.attributes('readonly')).toBeUndefined()

    await input.trigger('blur')
    await wait(160)

    expect(wrapper.findAll('input')).toHaveLength(0)
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('restores an edited existing value when cleared on blur', async () => {
    const wrapper = mountEditor(['NET_ADMIN'])

    const input = wrapper.get('input')
    await input.trigger('focus')
    expect(input.attributes('readonly')).toBeUndefined()

    await input.setValue('')
    await input.trigger('blur')

    // activeEditIndex resets synchronously, before the restore timeout fires.
    expect(wrapper.get('input').attributes('readonly')).toBeDefined()

    await wait(160)

    expect(wrapper.get('input').element.value).toBe('NET_ADMIN')
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('emits save when an existing entry is edited to a new non-empty value', async () => {
    const wrapper = mountEditor(['NET_ADMIN'])

    const input = wrapper.get('input')
    await input.trigger('focus')
    await input.setValue('SYS_ADMIN')
    await input.trigger('blur')
    await wait(160)

    expect(wrapper.get('input').element.value).toBe('SYS_ADMIN')
    expect(wrapper.emitted('save')).toHaveLength(1)
  })

  it('confirms deletion of a populated entry through the modal and emits save', async () => {
    const wrapper = mountEditor(['NET_ADMIN'])

    expect(wrapper.find('[data-testid="delete-rule-modal"]').exists()).toBe(false)

    await wrapper.get('[aria-label="Delete entry"]').trigger('click')

    const modal = wrapper.get('[data-testid="delete-rule-modal"]')
    expect(modal.text()).toContain('NET_ADMIN')

    await wrapper.get('[data-testid="confirm-delete"]').trigger('click')

    expect(wrapper.findAll('input')).toHaveLength(0)
    expect(wrapper.find('[data-testid="delete-rule-modal"]').exists()).toBe(false)
    expect(wrapper.emitted('save')).toHaveLength(1)
  })
})
