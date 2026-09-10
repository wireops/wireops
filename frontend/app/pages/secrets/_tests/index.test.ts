import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import { h, ref } from 'vue'
import SecretsPage from '../index.vue'

type Slots = Record<string, (() => unknown) | undefined>

function setupGlobals(opts: { isAdmin?: boolean, globals?: any[] } = {}) {
  for (const key of ['ref', 'computed', 'watch', 'onMounted', 'nextTick']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: (name: string) => ({
        getFullList: vi.fn().mockResolvedValue(
          name === 'global_env_vars' ? (opts.globals ?? []) : [],
        ),
      }),
    },
  })
  ;(globalThis as any).usePermissions = () => ({ canOperate: ref(true), isAdmin: ref(!!opts.isAdmin) })
  ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  ;(globalThis as any).useRoute = () => ({ query: {} })
  ;(globalThis as any).useRouter = () => ({ replace: vi.fn() })
  ;(globalThis as any).useSecretProviderOptions = () => ({
    load: vi.fn(),
    providerOptions: [{ label: 'Internal', value: 'internal' }],
    hasActiveBackends: { value: false },
    iconFor: () => undefined,
    avatarFor: () => undefined,
    labelFor: (provider: string) => provider,
  })
}

const stubs = {
  RefreshButton: { setup: () => () => h('button', { type: 'button' }, 'Refresh') },
  UTabs: { setup: () => () => null },
  AppPanelCard: {
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', [slots.header?.(), slots.default?.(), slots.footer?.()])
    },
  },
  UBadge: {
    props: ['label'],
    setup(props: { label?: string }) {
      return () => h('span', props.label)
    },
  },
  UButton: {
    props: ['icon', 'label', 'disabled', 'loading'],
    emits: ['click'],
    setup(props: { label?: string }, { emit, slots }: { emit: (e: string) => void, slots: Slots }) {
      return () => h('button', { type: 'button', onClick: () => emit('click') }, props.label ?? slots.default?.())
    },
  },
  AppTextInput: {
    props: ['modelValue', 'placeholder', 'type', 'disabled', 'icon', 'title'],
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
        onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLSelectElement).value),
      }, (props.items || []).map(item => h('option', { value: item.value }, item.label)))
    },
  },
  IntegrationsVaultReferencePicker: { setup: () => () => h('div', 'vault-picker') },
  IntegrationsInfisicalReferencePicker: { setup: () => () => h('div', 'infisical-picker') },
  CloseButton: {
    setup(_props: unknown, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { type: 'button', onClick: () => emit('click') })
    },
  },
  UIcon: { setup: () => () => h('span') },
  UModal: {
    props: ['open'],
    setup(props: { open?: boolean }, { slots }: { slots: Slots }) {
      return () => (props.open ? h('div', [slots.content?.(), slots.body?.()]) : null)
    },
  },
  UFormField: {
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', slots.default?.())
    },
  },
  CancelButton: {
    setup(_props: unknown, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { type: 'button', onClick: () => emit('click') }, 'Cancel')
    },
  },
  UTooltip: {
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', slots.default?.())
    },
  },
  RepositoryKeysPanel: { setup: () => () => null },
  RegistryCredentialsPanel: { setup: () => () => null },
  EncryptSopsSecretsModal: { setup: () => () => null },
  SecretRevealField: {
    props: ['collection', 'envVarId', 'icon', 'title'],
    setup(props: { collection: string, envVarId: string }) {
      return () => h('div', { class: 'secret-reveal-field', 'data-collection': props.collection, 'data-env-var-id': props.envVarId }, 'reveal-field')
    },
  },
}

describe('secrets/index.vue global variables reveal gating', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('creates a global multiline secret from pasted JSON', async () => {
    setupGlobals()
    const create = vi.fn().mockResolvedValue({})
    ;(globalThis as any).useRoute = () => ({ query: { create: 'true' } })
    ;(globalThis as any).useNuxtApp = () => ({ $pb: { collection: () => ({ getFullList: vi.fn().mockResolvedValue([]), create }) } })
    const wrapper = mount(SecretsPage, { global: { stubs } })
    await flushPromises()
    const form = wrapper.get('form')
    const inputs = form.findAll('input')
    await inputs[0]!.setValue('GCP_JSON')
    const value = '{\n"private_key":"FAKE\\nKEY"\n}'
    await inputs[1]!.trigger('paste', { clipboardData: { getData: () => value } })
    await form.trigger('submit')
    expect(create).toHaveBeenCalledWith({ key: 'GCP_JSON', value, secret: true, secret_provider: 'internal' }, expect.anything())
  })

  it('shows the SecretRevealField for an internal secret when the user is admin', async () => {
    setupGlobals({
      isAdmin: true,
      globals: [{ id: 'env-1', key: 'DB_PASS', value: '', secret: true, secret_provider: 'internal' }],
    })

    const wrapper = mount(SecretsPage, { global: { stubs } })
    await flushPromises()

    const reveal = wrapper.find('.secret-reveal-field')
    expect(reveal.exists()).toBe(true)
    expect(reveal.attributes('data-collection')).toBe('global_env_vars')
    expect(reveal.attributes('data-env-var-id')).toBe('env-1')
  })

  it('does not show the SecretRevealField for a non-admin, keeping the static mask', async () => {
    setupGlobals({
      isAdmin: false,
      globals: [{ id: 'env-1', key: 'DB_PASS', value: '', secret: true, secret_provider: 'internal' }],
    })

    const wrapper = mount(SecretsPage, { global: { stubs } })
    await flushPromises()

    expect(wrapper.find('.secret-reveal-field').exists()).toBe(false)
    const values = wrapper.findAll('input').map(i => (i.element as HTMLInputElement).value)
    expect(values).toContain('••••••••')
  })

  it('does not show the SecretRevealField for a vault-provider secret even as admin', async () => {
    setupGlobals({
      isAdmin: true,
      globals: [{ id: 'env-1', key: 'DB_PASS', value: 'secret/data/myapp#DB_PASS', secret: true, secret_provider: 'vault' }],
    })

    const wrapper = mount(SecretsPage, { global: { stubs } })
    await flushPromises()

    expect(wrapper.find('.secret-reveal-field').exists()).toBe(false)
    const values = wrapper.findAll('input').map(i => (i.element as HTMLInputElement).value)
    expect(values).toContain('secret/data/myapp#DB_PASS')
  })
})
