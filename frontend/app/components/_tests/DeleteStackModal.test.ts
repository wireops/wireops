import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import DeleteStackModal from '../DeleteStackModal.vue'

const stubs = {
  UCard: { template: '<div><slot name="header" /><slot /><slot name="footer" /></div>' },
  UButton: {
    props: ['label', 'loading', 'disabled'],
    template: '<button :disabled="disabled" v-bind="$attrs"><slot>{{ label }}</slot></button>',
  },
  UCheckbox: {
    props: ['modelValue', 'label'],
    emits: ['update:modelValue'],
    template: '<label><input type="checkbox" :checked="modelValue" @change="$emit(\'update:modelValue\', $event.target.checked)" />{{ label }}</label>',
  },
  UIcon: { template: '<span />' },
  WorkerNameLabel: { props: ['name'], template: '<span>{{ name }}</span>' },
  TerminalOutput: { props: ['lines'], template: '<pre />' },
}

function setupGlobals() {
  ;(globalThis as any).WORKER_STATUS = { ACTIVE: 'ACTIVE', OFFLINE: 'OFFLINE', REVOKED: 'REVOKED', PENDING: 'PENDING' }

  const addToast = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: addToast })

  const announce = vi.fn()
  ;(globalThis as any).useA11yAnnouncer = () => ({ announce })

  ;(globalThis as any).useNuxtApp = () => ({
    $pb: { baseURL: 'http://test', authStore: { token: 'tok' } },
  })

  // No fetch call is expected unless deleting/deleted flips the stream on —
  // stub it so any such call resolves harmlessly instead of hitting the network.
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, body: null }))

  const deleteStack = vi.fn()
  ;(globalThis as any).useApi = () => ({ deleteStack })

  return { addToast, announce, deleteStack }
}

const onlineWorker = { id: 'worker-1', hostname: 'worker-a', status: 'ACTIVE' }

function baseStack(overrides: Record<string, any> = {}) {
  return {
    id: 'stack-1',
    name: 'my-stack',
    expand: { worker: onlineWorker },
    ...overrides,
  }
}

describe('DeleteStackModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('retains the stack snapshot after the parent nulls the stack prop', async () => {
    setupGlobals()
    const wrapper = mount(DeleteStackModal, {
      props: { stack: baseStack() },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('my-stack')

    await wrapper.setProps({ stack: null })

    // Background refetch nulling the prop must not blank the modal mid-flow.
    expect(wrapper.text()).toContain('my-stack')
  })

  it('deletes successfully and shows the success state', async () => {
    const { deleteStack, addToast, announce } = setupGlobals()
    deleteStack.mockResolvedValue({})

    const wrapper = mount(DeleteStackModal, {
      props: { stack: baseStack() },
      global: { stubs },
    })

    const deleteButton = wrapper.findAll('button').find(b => b.text() === 'Delete Stack')!
    await deleteButton.trigger('click')
    await flushPromises()

    expect(deleteStack).toHaveBeenCalledWith('stack-1', false)
    expect(wrapper.text()).toContain('deleted successfully')
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: expect.stringContaining('deleted') }))
    expect(announce).toHaveBeenCalledWith(expect.stringContaining('deleted'))
  })

  it('shows the API error and offers force-delete when the API reports the worker offline', async () => {
    const { deleteStack } = setupGlobals()
    deleteStack.mockResolvedValue({ error: 'worker is offline' })

    const wrapper = mount(DeleteStackModal, {
      props: { stack: baseStack() },
      global: { stubs },
    })

    const deleteButton = wrapper.findAll('button').find(b => b.text() === 'Delete Stack')!
    await deleteButton.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('worker is offline')
    expect(wrapper.text()).toContain('Force delete database records only')
  })

  it('shows the API error and offers force-delete when the request throws (offline network)', async () => {
    const { deleteStack, announce } = setupGlobals()
    deleteStack.mockRejectedValue(new Error('Failed to fetch: offline'))

    const wrapper = mount(DeleteStackModal, {
      props: { stack: baseStack() },
      global: { stubs },
    })

    const deleteButton = wrapper.findAll('button').find(b => b.text() === 'Delete Stack')!
    await deleteButton.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to fetch: offline')
    expect(wrapper.text()).toContain('Force delete database records only')
    expect(announce).toHaveBeenCalledWith(expect.stringContaining('Failed to delete'), 'assertive')
  })

  // A real parent — not just wrapper.emitted() — is needed here: the parent
  // reacts to the first 'deleted' emit by unmounting the modal via v-if,
  // which is exactly the trigger that previously caused onUnmounted to fire
  // a second 'deleted' emit. wrapper.emitted() alone doesn't observe emits
  // issued during that teardown, so it can't catch the regression.
  function mountWithRealParent(stack: any) {
    const onDeleted = vi.fn()
    const Parent = defineComponent({
      setup() {
        const show = ref(true)
        function handleDeleted() {
          onDeleted()
          show.value = false
        }
        return () => (show.value
          ? h(DeleteStackModal, { stack, onDeleted: handleDeleted })
          : h('div', 'gone'))
      },
    })
    const wrapper = mount(Parent, { global: { stubs } })
    return { wrapper, onDeleted }
  }

  it('calls the parent deleted handler exactly once when the user clicks Close after a successful delete', async () => {
    const { deleteStack } = setupGlobals()
    deleteStack.mockResolvedValue({})

    const { wrapper, onDeleted } = mountWithRealParent(baseStack())

    const deleteButton = wrapper.findAll('button').find(b => b.text() === 'Delete Stack')!
    await deleteButton.trigger('click')
    await flushPromises()

    const closeButton = wrapper.findAll('button').find(b => b.text() === 'Close')!
    await closeButton.trigger('click')
    await flushPromises()

    expect(onDeleted).toHaveBeenCalledTimes(1)
  })

  it('calls the parent deleted handler exactly once when dismissed via backdrop/ESC after a successful delete', async () => {
    const { deleteStack } = setupGlobals()
    deleteStack.mockResolvedValue({})

    const { wrapper, onDeleted } = mountWithRealParent(baseStack())

    const deleteButton = wrapper.findAll('button').find(b => b.text() === 'Delete Stack')!
    await deleteButton.trigger('click')
    await flushPromises()

    // Backdrop/ESC dismissal never clicks "Close" — only onUnmounted's
    // fallback should fire the parent's handler, and only once.
    wrapper.unmount()

    expect(onDeleted).toHaveBeenCalledTimes(1)
  })

  it('does not call the parent deleted handler on dismissal if the delete never completed (e.g. plain cancel)', async () => {
    setupGlobals()
    const { wrapper, onDeleted } = mountWithRealParent(baseStack())

    const cancelButton = wrapper.findAll('button').find(b => b.text() === 'Cancel')!
    await cancelButton.trigger('click')

    wrapper.unmount()
    expect(onDeleted).not.toHaveBeenCalled()
  })
})
