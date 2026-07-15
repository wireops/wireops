import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import EnvironmentVariablesCard from '../EnvironmentVariablesCard.vue'

type Slots = Record<string, (() => unknown) | undefined>

describe('EnvironmentVariablesCard', () => {
  const createFn = vi.fn().mockResolvedValue({})

  beforeEach(() => {
    createFn.mockClear()
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        collection: () => ({
          getFullList: vi.fn().mockResolvedValue([]),
          create: createFn,
          update: vi.fn().mockResolvedValue({}),
          delete: vi.fn().mockResolvedValue({}),
        }),
      },
    })
    ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
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

  const stubs = {
    UCard: {
      setup(_props: unknown, { slots }: { slots: Slots }) {
        return () => h('div', [slots.header?.(), slots.default?.()])
      },
    },
    UButton: {
      props: ['icon', 'label', 'variant', 'color', 'size', 'loading', 'disabled', 'type', 'ariaLabel'],
      emits: ['click'],
      setup(props: { type?: string, label?: string, ariaLabel?: string }, { emit, slots, attrs }: { emit: (e: string, ...a: unknown[]) => void, slots: Slots, attrs: Record<string, unknown> }) {
        return () => h('button', {
          type: props.type || 'button',
          'aria-label': props.ariaLabel ?? attrs['aria-label'],
          onClick: () => emit('click'),
        }, props.label ?? slots.default?.())
      },
    },
    AppTextInput: {
      props: ['modelValue', 'placeholder', 'type', 'disabled'],
      emits: ['update:modelValue'],
      setup(props: { modelValue?: string, disabled?: boolean }, { emit }: { emit: (e: string, v: string) => void }) {
        return () => h('input', {
          value: props.modelValue,
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
          value: props.modelValue,
          class: 'u-select',
          onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLSelectElement).value),
        }, (props.items || []).map(item => h('option', { value: item.value }, item.label)))
      },
    },
    UIcon: { setup: () => () => h('span') },
    UBadge: {
      props: ['label'],
      setup(props: { label?: string }) {
        return () => h('span', props.label)
      },
    },
    UModal: {
      props: ['open'],
      setup(props: { open?: boolean }, { slots }: { slots: Slots }) {
        return () => (props.open ? h('div', slots.content?.()) : null)
      },
    },
    UFormField: {
      props: ['label'],
      setup(_props: unknown, { slots }: { slots: Slots }) {
        return () => h('div', slots.default?.())
      },
    },
    IntegrationsVaultReferencePicker: {
      props: ['modelValue'],
      emits: ['update:modelValue'],
      setup(_props: unknown, { emit }: { emit: (e: string, v: string) => void }) {
        return () => h('div', { class: 'vault-picker', onClick: () => emit('update:modelValue', 'secret/data/myapp#DB_PASS') }, 'vault-picker')
      },
    },
  }

  it('defaults new secret env vars to the internal provider and shows a provider select', async () => {
    const wrapper = mount(EnvironmentVariablesCard, {
      props: { targetType: 'stack', targetId: 'stack-1' },
      global: { stubs },
    })
    await Promise.resolve()
    await Promise.resolve()

    const addButtons = wrapper.findAll('button').filter(b => b.text() === 'Add')
    await addButtons[1]!.trigger('click')

    expect(wrapper.find('.u-select').exists()).toBe(true)
  })

  it('swaps in the Vault picker when the vault provider is selected, and includes secret_provider on create', async () => {
    const wrapper = mount(EnvironmentVariablesCard, {
      props: { targetType: 'stack', targetId: 'stack-1' },
      global: { stubs },
    })
    await Promise.resolve()
    await Promise.resolve()

    const addButtons = wrapper.findAll('button').filter(b => b.text() === 'Add')
    await addButtons[1]!.trigger('click')

    await wrapper.find('.u-select').setValue('vault')
    expect(wrapper.find('.vault-picker').exists()).toBe(true)

    await wrapper.find('.vault-picker').trigger('click')

    const keyInput = wrapper.find('input')
    await keyInput.setValue('DB_PASS')

    await wrapper.find('form').trigger('submit')
    await Promise.resolve()

    expect(createFn).toHaveBeenCalledWith(
      expect.objectContaining({
        key: 'DB_PASS',
        secret: true,
        secret_provider: 'vault',
        value: 'secret/data/myapp#DB_PASS',
      }),
      expect.anything(),
    )
  })

  it('shows the raw reference for vault/infisical secrets but masks internal secrets', async () => {
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        collection: () => ({
          getFullList: vi.fn().mockResolvedValue([
            { id: 'env-internal', key: 'INTERNAL_TOKEN', value: '', secret: true, secret_provider: 'internal' },
            { id: 'env-vault', key: 'DB_PASS', value: 'secret/data/myapp#DB_PASS', secret: true, secret_provider: 'vault' },
          ]),
          create: createFn,
          update: vi.fn().mockResolvedValue({}),
          delete: vi.fn().mockResolvedValue({}),
        }),
      },
    })

    const wrapper = mount(EnvironmentVariablesCard, {
      props: { targetType: 'stack', targetId: 'stack-1' },
      global: { stubs },
    })
    await Promise.resolve()
    await Promise.resolve()

    const rowInputs = wrapper.findAll('input')
    const values = rowInputs.map(i => (i.element as HTMLInputElement).value)

    expect(values).toContain('••••••••')
    expect(values).toContain('secret/data/myapp#DB_PASS')
  })

  it('does not send a provider when the value is plain text', async () => {
    const wrapper = mount(EnvironmentVariablesCard, {
      props: { targetType: 'stack', targetId: 'stack-1' },
      global: { stubs },
    })
    await Promise.resolve()
    await Promise.resolve()

    const addButtons = wrapper.findAll('button').filter(b => b.text() === 'Add')
    await addButtons[1]!.trigger('click')

    const secretToggle = wrapper.find('button[aria-label="Set as plain text"]')
    await secretToggle.trigger('click')

    expect(wrapper.find('.u-select').exists()).toBe(false)

    const keyInput = wrapper.find('input')
    await keyInput.setValue('PLAIN_KEY')

    await wrapper.find('form').trigger('submit')
    await Promise.resolve()

    expect(createFn).toHaveBeenCalledWith(
      expect.objectContaining({
        key: 'PLAIN_KEY',
        secret: false,
        secret_provider: '',
      }),
      expect.anything(),
    )
  })
})
