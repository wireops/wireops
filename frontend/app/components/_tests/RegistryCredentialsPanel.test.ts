import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import { h, ref } from 'vue'
import RegistryCredentialsPanel from '../RegistryCredentialsPanel.vue'

for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive']) {
  (globalThis as any)[key] = (vue as any)[key]
}

const credentialFixture = {
  id: 'cred-1',
  name: 'GHCR',
  registry_url: 'ghcr.io',
  auth_type: 'basic',
  insecure: false,
}

const stubs = {
  AppPanelCard: {
    setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
      return () => h('section', [slots.header?.(), slots.default?.()])
    },
  },
  UButton: {
    props: ['label', 'icon', 'ariaLabel', 'disabled'],
    emits: ['click'],
    setup(props: { label?: string, ariaLabel?: string, disabled?: boolean }, { emit, attrs }: { emit: (e: string) => void, attrs: Record<string, unknown> }) {
      return () => h('button', {
        disabled: props.disabled,
        'aria-label': props.ariaLabel ?? attrs['aria-label'],
        onClick: () => emit('click'),
      }, props.label)
    },
  },
  AppTextInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string }, { emit }: { emit: (e: string, v: string) => void }) {
      return () => h('input', {
        value: props.modelValue,
        onInput: (ev: Event) => emit('update:modelValue', (ev.target as HTMLInputElement).value),
      })
    },
  },
  UIcon: { setup() { return () => h('span') } },
  UBadge: { setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) { return () => h('span', slots.default?.()) } },
  UTooltip: { props: ['text'], setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) { return () => h('div', slots.default?.()) } },
  RegistryCredentialModal: {
    props: ['open', 'credential'],
    emits: ['update:open', 'saved'],
    setup(props: { open?: boolean }) {
      return () => (props.open ? h('div', { class: 'registry-modal-stub' }) : null)
    },
  },
  ConfirmModal: {
    props: ['open', 'title', 'description', 'loading'],
    emits: ['update:open', 'confirm'],
    setup(props: { open?: boolean, description?: string }, { emit }: { emit: (e: string) => void }) {
      return () => (props.open
        ? h('div', { class: 'confirm-modal-stub' }, [
            h('span', props.description),
            h('button', { class: 'confirm-delete', onClick: () => emit('confirm') }, 'Confirm'),
          ])
        : null)
    },
  },
}

function setupGlobals(overrides: { credentials?: any[], stacks?: any[] } = {}) {
  const deleteFn = vi.fn().mockResolvedValue({})
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: (name: string) => ({
        getFullList: vi.fn().mockResolvedValue(name === 'registry_credentials' ? (overrides.credentials ?? [credentialFixture]) : (overrides.stacks ?? [])),
        delete: deleteFn,
      }),
    },
  })
  ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
  ;(globalThis as any).usePermissions = () => ({ canManageRepos: ref(true) })
  const addToast = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: addToast })
  ;(globalThis as any).useAsyncData = () => ({
    data: ref({ credentials: overrides.credentials ?? [credentialFixture], stacks: overrides.stacks ?? [] }),
    refresh: vi.fn(),
  })

  return { deleteFn, addToast }
}

describe('RegistryCredentialsPanel', () => {
  it('renders each credential with its auth type and registry url', async () => {
    setupGlobals()
    const wrapper = mount(RegistryCredentialsPanel, { global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('GHCR')
    expect(wrapper.text()).toContain('ghcr.io')
    expect(wrapper.text()).toContain('Username / Password')
  })

  it('shows an insecure badge when the credential is marked insecure', async () => {
    setupGlobals({ credentials: [{ ...credentialFixture, insecure: true }] })
    const wrapper = mount(RegistryCredentialsPanel, { global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('Insecure')
  })

  it('filters the list by search query', async () => {
    setupGlobals({
      credentials: [
        credentialFixture,
        { id: 'cred-2', name: 'Docker Hub', registry_url: 'docker.io', auth_type: 'token', insecure: false },
      ],
    })
    const wrapper = mount(RegistryCredentialsPanel, { global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('GHCR')
    expect(wrapper.text()).toContain('Docker Hub')

    await wrapper.find('input').setValue('docker.io')
    await flushPromises()

    expect(wrapper.text()).not.toContain('GHCR')
    expect(wrapper.text()).toContain('Docker Hub')
  })

  it('opens the add-credential modal', async () => {
    setupGlobals()
    const wrapper = mount(RegistryCredentialsPanel, { global: { stubs } })
    await flushPromises()

    expect(wrapper.find('.registry-modal-stub').exists()).toBe(false)
    await wrapper.find('button[aria-label="Add Credential"], button').trigger('click')
    // "Add Credential" is the header button; find it explicitly by label text.
    const addButton = wrapper.findAll('button').find(b => b.text() === 'Add Credential')!
    await addButton.trigger('click')
    await flushPromises()

    expect(wrapper.find('.registry-modal-stub').exists()).toBe(true)
  })

  it('disables delete and shows a link icon for a credential still assigned to a stack', async () => {
    setupGlobals({
      credentials: [credentialFixture],
      stacks: [{ id: 'stack-1', registry_credential: 'cred-1' }],
    })
    const wrapper = mount(RegistryCredentialsPanel, { global: { stubs } })
    await flushPromises()

    const deleteButton = wrapper.find('button[aria-label="Delete credential"]')
    expect(deleteButton.attributes('disabled')).toBeDefined()
  })

  it('deletes an unused credential after confirmation', async () => {
    const { deleteFn, addToast } = setupGlobals({ credentials: [credentialFixture], stacks: [] })
    const wrapper = mount(RegistryCredentialsPanel, { global: { stubs } })
    await flushPromises()

    const deleteButton = wrapper.find('button[aria-label="Delete credential"]')
    expect(deleteButton.attributes('disabled')).toBeUndefined()
    await deleteButton.trigger('click')
    await flushPromises()

    const confirmButton = wrapper.find('.confirm-delete')
    expect(confirmButton.exists()).toBe(true)
    await confirmButton.trigger('click')
    await flushPromises()

    expect(deleteFn).toHaveBeenCalledWith('cred-1')
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Credential deleted', color: 'success' }))
  })

  it('shows the empty state when there are no credentials', async () => {
    setupGlobals({ credentials: [], stacks: [] })
    const wrapper = mount(RegistryCredentialsPanel, { global: { stubs } })
    await flushPromises()

    expect(wrapper.text()).toContain('No registry credentials yet')
  })
})
