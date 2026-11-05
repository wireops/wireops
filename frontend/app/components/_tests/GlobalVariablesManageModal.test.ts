import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, nextTick, ref } from 'vue'
import GlobalVariablesManageModal from '../GlobalVariablesManageModal.vue'

type Slots = Record<string, (() => unknown) | undefined>

describe('GlobalVariablesManageModal', () => {
  const createFn = vi.fn()
  const deleteFn = vi.fn()
  const toastAdd = vi.fn()
  const collections: string[] = []

  const globals = [
    { id: 'g-b', key: 'B_KEY', secret: false },
    { id: 'g-a', key: 'A_KEY', secret: true },
    { id: 'g-c', key: 'C_KEY', secret: false },
  ]
  const bindings = [{ id: 'bind-c', global_env_var: 'g-c', stack: 'stack-1' }]

  beforeEach(() => {
    createFn.mockReset().mockResolvedValue({ id: 'new-global' })
    deleteFn.mockReset().mockResolvedValue({})
    toastAdd.mockReset()
    collections.length = 0
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        collection: (name: string) => {
          collections.push(name)
          return {
            create: (data: unknown, opts: unknown) => createFn(name, data, opts),
            delete: deleteFn,
          }
        },
      },
    })
    ;(globalThis as any).usePermissions = () => ({ canOperate: ref(true) })
    ;(globalThis as any).useToast = () => ({ add: toastAdd })
    ;(globalThis as any).useSecretProviderOptions = () => ({
      load: vi.fn(),
      providerOptions: [{ label: 'Internal', value: 'internal' }],
      hasActiveBackends: ref(false),
      iconFor: () => undefined,
      avatarFor: () => undefined,
    })
  })

  const stubs = {
    AppPanelCard: {
      setup(_props: unknown, { slots }: { slots: Slots }) {
        return () => h('div', [slots.header?.(), slots.default?.(), slots.footer?.()])
      },
    },
    UModal: {
      props: ['open'],
      setup(props: { open?: boolean }, { slots }: { slots: Slots }) {
        return () => (props.open ? h('div', slots.content?.()) : null)
      },
    },
    UButton: {
      props: ['label', 'type', 'disabled', 'loading'],
      emits: ['click'],
      setup(props: { label?: string, type?: string, disabled?: boolean }, { emit, slots, attrs }: { emit: (e: string) => void, slots: Slots, attrs: Record<string, unknown> }) {
        return () => h('button', {
          type: props.type || 'button',
          disabled: props.disabled,
          'aria-label': attrs['aria-label'],
          onClick: () => emit('click'),
        }, props.label ?? slots.default?.())
      },
    },
    CloseButton: {
      setup(_props: unknown, { emit }: { emit: (e: string) => void }) {
        return () => h('button', { type: 'button', 'aria-label': 'Close', onClick: () => emit('click') })
      },
    },
    CancelButton: {
      setup(_props: unknown, { emit }: { emit: (e: string) => void }) {
        return () => h('button', { type: 'button', onClick: () => emit('click') }, 'Cancel')
      },
    },
    AppTextInput: {
      props: ['modelValue', 'disabled', 'placeholder'],
      emits: ['update:modelValue'],
      setup(props: { modelValue?: string, disabled?: boolean, placeholder?: string }, { emit }: { emit: (e: string, v: string) => void }) {
        return () => h('input', {
          value: props.modelValue,
          disabled: props.disabled,
          placeholder: props.placeholder,
          onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value),
        })
      },
    },
    EnvValueInput: {
      props: ['modelValue'],
      emits: ['update:modelValue'],
      setup(props: { modelValue?: string }, { emit }: { emit: (e: string, v: string) => void }) {
        return () => h('input', {
          class: 'value-input',
          value: props.modelValue,
          onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value),
        })
      },
    },
    UFormField: {
      setup(_props: unknown, { slots }: { slots: Slots }) {
        return () => h('div', slots.default?.())
      },
    },
    UIcon: { setup: () => () => h('span') },
    AppSelectInput: { setup: () => () => h('select') },
    IntegrationsVaultReferencePicker: { setup: () => () => h('div') },
    IntegrationsInfisicalReferencePicker: { setup: () => () => h('div') },
  }

  function mountModal(extra: Record<string, unknown> = {}) {
    return mount(GlobalVariablesManageModal, {
      props: { open: true, targetType: 'stack', targetId: 'stack-1', globals, bindings, ...extra },
      global: { stubs },
    })
  }

  const keysInOrder = (wrapper: ReturnType<typeof mountModal>) =>
    wrapper.findAll('input').map(i => (i.element as HTMLInputElement).value).filter(v => v.endsWith('_KEY'))

  it('lists every global with attached first, then alphabetical', () => {
    expect(keysInOrder(mountModal())).toEqual(['C_KEY', 'A_KEY', 'B_KEY'])
  })

  it('filters the list by search query', async () => {
    const wrapper = mountModal()
    await wrapper.get('input[placeholder="Search global variables..."]').setValue('b_')
    expect(keysInOrder(wrapper)).toEqual(['B_KEY'])
  })

  it('shows the empty-catalog message when no globals exist', () => {
    const wrapper = mountModal({ globals: [], bindings: [] })
    expect(wrapper.text()).toContain('No global variables yet')
  })

  it('shows the no-match message when the search has no hits', async () => {
    const wrapper = mountModal()
    await wrapper.get('input[placeholder="Search global variables..."]').setValue('zzz')
    expect(wrapper.text()).toContain('No variables match your search')
    expect(keysInOrder(wrapper)).toEqual([])
  })

  it('does not start an attach while the parent is still reloading', async () => {
    const wrapper = mountModal({ loading: true })
    await wrapper.get('button[aria-label="Attach A_KEY"]').trigger('click')
    expect(wrapper.find('button[aria-label="Confirm attach A_KEY"]').exists()).toBe(false)
    expect(createFn).not.toHaveBeenCalled()
  })

  it('attaches without closing the modal and emits changed', async () => {
    const wrapper = mountModal()
    await wrapper.get('button[aria-label="Attach A_KEY"]').trigger('click')
    expect(createFn).not.toHaveBeenCalled()
    await wrapper.get('button[aria-label="Confirm attach A_KEY"]').trigger('click')
    await nextTick()

    expect(createFn).toHaveBeenCalledWith('stack_global_env_vars', { stack: 'stack-1', global_env_var: 'g-a' }, expect.anything())
    expect(toastAdd).toHaveBeenCalledWith({ title: 'A_KEY attached', color: 'success' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.emitted('update:open')).toBeUndefined()
  })

  it('detaches an attached global by binding id', async () => {
    const wrapper = mountModal()
    await wrapper.get('button[aria-label="Detach C_KEY"]').trigger('click')
    expect(deleteFn).not.toHaveBeenCalled()
    await wrapper.get('button[aria-label="Confirm detach C_KEY"]').trigger('click')
    await nextTick()

    expect(deleteFn).toHaveBeenCalledWith('bind-c', expect.anything())
    expect(toastAdd).toHaveBeenCalledWith({ title: 'C_KEY detached', color: 'success' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('does nothing when the confirmation is cancelled', async () => {
    const wrapper = mountModal()
    await wrapper.get('button[aria-label="Attach A_KEY"]').trigger('click')
    await wrapper.findAll('button').filter(b => b.text() === 'Cancel').at(-1)!.trigger('click')

    expect(wrapper.find('button[aria-label="Confirm attach A_KEY"]').exists()).toBe(false)
    expect(createFn).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:open')).toBeUndefined()
  })

  it('warns in the attach confirmation when a local key overrides the global', async () => {
    const wrapper = mountModal({ localKeys: ['A_KEY'] })
    await wrapper.get('button[aria-label="Attach A_KEY"]').trigger('click')
    expect(wrapper.text()).toContain('will override it')
  })

  it('uses job collections and field for job targets', async () => {
    const wrapper = mountModal({ targetType: 'job', targetId: 'job-1', bindings: [] })
    await wrapper.get('button[aria-label="Attach A_KEY"]').trigger('click')
    await wrapper.get('button[aria-label="Confirm attach A_KEY"]').trigger('click')
    await nextTick()

    expect(createFn).toHaveBeenCalledWith('job_global_env_vars', { job: 'job-1', global_env_var: 'g-a' }, expect.anything())
  })

  it('toasts and does not emit changed when attach fails', async () => {
    createFn.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mountModal()
    await wrapper.get('button[aria-label="Attach A_KEY"]').trigger('click')
    await wrapper.get('button[aria-label="Confirm attach A_KEY"]').trigger('click')
    await nextTick()
    await nextTick()

    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Failed to attach global variable', color: 'error' }))
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('shows an override hint for attached globals shadowed by a local key', () => {
    const wrapper = mountModal({ localKeys: ['C_KEY'] })
    expect(wrapper.text()).toContain('Overridden by a local variable')
    expect(mountModal({ localKeys: ['A_KEY'] }).text()).not.toContain('Overridden by a local variable')
  })

  describe('create and attach', () => {
    async function openForm(wrapper: ReturnType<typeof mountModal>) {
      await wrapper.findAll('button').find(b => b.text() === 'New global variable')!.trigger('click')
    }

    it('creates the global then attaches it, keeping the modal open', async () => {
      const wrapper = mountModal()
      await openForm(wrapper)
      await wrapper.get('input[placeholder="KEY"]').setValue('  NEW_KEY ')
      await wrapper.get('.value-input').setValue('s3cret')
      await wrapper.get('form').trigger('submit')
      await nextTick()
      await nextTick()

      expect(createFn).toHaveBeenNthCalledWith(1, 'global_env_vars', { key: 'NEW_KEY', value: 's3cret', secret: true, secret_provider: 'internal' }, expect.anything())
      expect(createFn).toHaveBeenNthCalledWith(2, 'stack_global_env_vars', { stack: 'stack-1', global_env_var: 'new-global' }, expect.anything())
      expect(wrapper.emitted('changed')).toHaveLength(1)
      expect(wrapper.emitted('update:open')).toBeUndefined()
    })

    it('toasts and skips the attach when creating the global fails', async () => {
      createFn.mockRejectedValueOnce(new Error('duplicate key'))
      const wrapper = mountModal()
      await openForm(wrapper)
      await wrapper.get('input[placeholder="KEY"]').setValue('A_KEY')
      await wrapper.get('form').trigger('submit')
      await nextTick()
      await nextTick()

      expect(createFn).toHaveBeenCalledTimes(1)
      expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Failed to create global variable', color: 'error' }))
      expect(wrapper.emitted('changed')).toBeUndefined()
      // Form stays open so the draft isn't lost.
      expect(wrapper.find('form').exists()).toBe(true)
    })

    it('keeps the form closed and unlocks again when a create finishes after the modal was reopened', async () => {
      let resolveCreate: (v: unknown) => void = () => {}
      createFn.mockImplementationOnce(() => new Promise((resolve) => { resolveCreate = resolve }))
      const wrapper = mountModal()
      await openForm(wrapper)
      await wrapper.get('input[placeholder="KEY"]').setValue('FIRST_KEY')
      await wrapper.get('form').trigger('submit')

      await wrapper.setProps({ open: false })
      await wrapper.setProps({ open: true })

      // A new draft can't be started while the earlier request is still running.
      const newButton = () => wrapper.findAll('button').find(b => b.text() === 'New global variable')!
      expect(newButton().attributes('disabled')).toBeDefined()

      resolveCreate({ id: 'first-global' })
      await nextTick()
      await nextTick()
      await nextTick()

      expect(wrapper.find('form').exists()).toBe(false)
      expect(newButton().attributes('disabled')).toBeUndefined()
    })

    it('blocks submit while the key is empty', async () => {
      const wrapper = mountModal()
      await openForm(wrapper)
      const submit = wrapper.findAll('button').find(b => b.text() === 'Create & attach')!
      expect(submit.attributes('disabled')).toBeDefined()
      await wrapper.get('form').trigger('submit')
      expect(createFn).not.toHaveBeenCalled()
    })

    it('still emits changed and warns when attaching the new global fails', async () => {
      createFn.mockResolvedValueOnce({ id: 'new-global' }).mockRejectedValueOnce(new Error('nope'))
      const wrapper = mountModal()
      await openForm(wrapper)
      await wrapper.get('input[placeholder="KEY"]').setValue('NEW_KEY')
      await wrapper.get('form').trigger('submit')
      await nextTick()
      await nextTick()
      await nextTick()

      expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Created, but failed to attach' }))
      expect(wrapper.emitted('changed')).toHaveLength(1)
    })
  })
})
