import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import EnvironmentVariablesPendingEditor from '../EnvironmentVariablesPendingEditor.vue'

type Slots = Record<string, (() => unknown) | undefined>

const stubs = {
  AppTextInput: {
    props: ['modelValue', 'placeholder', 'type', 'disabled'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string, placeholder?: string, disabled?: boolean }, { emit }: { emit: (e: string, v: string) => void }) {
      return () => h('input', {
        value: props.modelValue,
        placeholder: props.placeholder,
        disabled: props.disabled,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value),
      })
    },
  },
  AppSelectInput: {
    props: ['modelValue', 'items'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string, items?: { label: string, value: string }[] }, { emit }: { emit: (e: string, v: string) => void }) {
      return () => h('select', {
        class: 'provider-select',
        value: props.modelValue,
        onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLSelectElement).value),
      }, (props.items || []).map(item => h('option', { value: item.value }, item.label)))
    },
  },
  AppButtonInput: {
    props: ['icon', 'label'],
    emits: ['click'],
    setup(props: { label?: string }, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { type: 'button', onClick: () => emit('click') }, props.label)
    },
  },
  UButton: {
    props: ['icon', 'label', 'disabled', 'type', 'ariaLabel'],
    emits: ['click'],
    setup(props: { type?: string, label?: string, ariaLabel?: string, disabled?: boolean }, { emit, attrs }: { emit: (e: string) => void, attrs: Record<string, unknown> }) {
      return () => h('button', {
        type: props.type || 'button',
        disabled: props.disabled,
        'aria-label': props.ariaLabel ?? attrs['aria-label'],
        onClick: () => emit('click'),
      }, props.label)
    },
  },
  UTextarea: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string }, { emit }: { emit: (e: string, v: string) => void }) {
      return () => h('textarea', {
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLTextAreaElement).value),
      })
    },
  },
  CancelButton: {
    emits: ['click'],
    setup(_props: unknown, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { type: 'button', onClick: () => emit('click') }, 'Cancel')
    },
  },
  IntegrationsVaultReferencePicker: {
    props: ['modelValue'],
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', { class: 'vault-picker' }, slots.default?.())
    },
  },
  IntegrationsInfisicalReferencePicker: {
    props: ['modelValue'],
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', { class: 'infisical-picker' }, slots.default?.())
    },
  },
}

function mountEditor(modelValue: any[] = []) {
  return mount(EnvironmentVariablesPendingEditor, {
    props: { modelValue },
    global: { stubs },
  })
}

describe('EnvironmentVariablesPendingEditor', () => {
  beforeEach(() => {
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
    ;(globalThis as any).useSecretProviderOptions = () => ({
      load: vi.fn(),
      providerOptions: [{ label: 'Internal', value: 'internal' }, { label: 'Vault', value: 'vault' }],
      hasActiveBackends: { value: true },
      iconFor: () => undefined,
      avatarFor: () => undefined,
      labelFor: (provider: string) => provider,
    })
  })

  it('renders the empty state when no vars are pending', () => {
    const wrapper = mountEditor([])
    expect(wrapper.text()).toContain('No environment variables added')
  })

  it('adds a valid row and emits the updated list', async () => {
    const wrapper = mountEditor([])

    await wrapper.find('input[placeholder="KEY"]').setValue('FOO')
    await wrapper.find('input[placeholder="value"]').setValue('bar')
    await wrapper.find('form').trigger('submit.prevent')

    expect(wrapper.emitted('update:modelValue')![0]![0]).toEqual([
      { key: 'FOO', value: 'bar', secret: false, secret_provider: '' },
    ])
  })

  it('rejects an invalid key and disables adding it', async () => {
    const wrapper = mountEditor([])

    await wrapper.find('input[placeholder="KEY"]').setValue('123bad')

    expect(wrapper.text()).toContain('Invalid key format')
    const addButton = wrapper.findAll('button').find(b => b.attributes('aria-label') === 'Add environment variable')
    expect(addButton?.attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('update:modelValue')).toBeFalsy()
  })

  it('rejects a key that was already added and disables adding it', async () => {
    const wrapper = mountEditor([{ key: 'FOO', value: 'x', secret: false, secret_provider: '' }])

    await wrapper.find('input[placeholder="KEY"]').setValue('FOO')

    expect(wrapper.text()).toContain('Key already added')
    const addButton = wrapper.findAll('button').find(b => b.attributes('aria-label') === 'Add environment variable')
    expect(addButton?.attributes('disabled')).toBeDefined()
  })

  it('marks a row as secret with the internal provider when the lock toggle is used', async () => {
    const wrapper = mountEditor([])

    await wrapper.find('input[placeholder="KEY"]').setValue('TOKEN')
    await wrapper.find('input[placeholder="value"]').setValue('hunter2')
    const secretToggle = wrapper.findAll('button').find(b => b.attributes('aria-label') === 'Set as secret')
    await secretToggle!.trigger('click')
    await wrapper.find('form').trigger('submit.prevent')

    expect(wrapper.emitted('update:modelValue')![0]![0]).toEqual([
      { key: 'TOKEN', value: 'hunter2', secret: true, secret_provider: 'internal' },
    ])
  })

  it('removes a row and emits the list without it', async () => {
    const wrapper = mountEditor([
      { key: 'FOO', value: 'a', secret: false, secret_provider: '' },
      { key: 'BAR', value: 'b', secret: false, secret_provider: '' },
    ])

    const removeButtons = wrapper.findAll('button').filter(b => b.attributes('aria-label') === 'Remove environment variable')
    await removeButtons[0]!.trigger('click')

    expect(wrapper.emitted('update:modelValue')![0]![0]).toEqual([
      { key: 'BAR', value: 'b', secret: false, secret_provider: '' },
    ])
  })

  it('parses pasted KEY=VALUE lines into rows', async () => {
    const wrapper = mountEditor([])

    const pasteToggle = wrapper.findAll('button').find(b => b.text() === 'Paste .env')
    await pasteToggle!.trigger('click')
    await wrapper.find('textarea').setValue('FOO=bar\nBAZ=qux')
    const addVarsButton = wrapper.findAll('button').find(b => b.text() === 'Add variables')
    await addVarsButton!.trigger('click')

    expect(wrapper.emitted('update:modelValue')![0]![0]).toEqual([
      { key: 'FOO', value: 'bar', secret: false, secret_provider: '' },
      { key: 'BAZ', value: 'qux', secret: false, secret_provider: '' },
    ])
  })

  it('reports parse errors for malformed pasted lines without emitting', async () => {
    const wrapper = mountEditor([])

    const pasteToggle = wrapper.findAll('button').find(b => b.text() === 'Paste .env')
    await pasteToggle!.trigger('click')
    await wrapper.find('textarea').setValue('not-a-valid-line')
    const addVarsButton = wrapper.findAll('button').find(b => b.text() === 'Add variables')
    await addVarsButton!.trigger('click')

    expect(wrapper.text()).toContain('expected KEY=VALUE')
    expect(wrapper.emitted('update:modelValue')).toBeFalsy()
  })
})
