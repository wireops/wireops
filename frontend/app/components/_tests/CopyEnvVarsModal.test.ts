import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h } from 'vue'
import CopyEnvVarsModal from '../CopyEnvVarsModal.vue'

type Slots = Record<string, (() => unknown) | undefined>

const stubs = {
  UCard: {
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', [slots.header?.(), slots.default?.(), slots.footer?.()])
    },
  },
  UButton: {
    props: ['label', 'loading', 'disabled'],
    emits: ['click'],
    setup(props: { label?: string, disabled?: boolean }, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { disabled: props.disabled, onClick: () => emit('click') }, props.label)
    },
  },
  CancelButton: {
    props: ['disabled'],
    emits: ['click'],
    setup(props: { disabled?: boolean }, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { disabled: props.disabled, onClick: () => emit('click') }, 'Cancel')
    },
  },
  UFormField: {
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', slots.default?.())
    },
  },
  AppSelectInput: {
    props: ['modelValue', 'items'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string, items?: { label: string, value: string }[] }, { emit }: { emit: (e: string, v: string) => void }) {
      return () => h('select', {
        value: props.modelValue,
        class: 'source-select',
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
}

function setupApiMocks({ sopsAvailable = false }: { sopsAvailable?: boolean } = {}) {
  const customGet = vi.fn().mockResolvedValue({ available: sopsAvailable })
  const customPost = vi.fn().mockResolvedValue({ copied: 1, skipped: [] })
  ;(globalThis as any).useApi = () => ({ customGet, customPost })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  return { customGet, customPost }
}

function setupPbMocks(stacks: any[], envVars: any[]) {
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: (name: string) => {
        if (name === 'stacks') {
          return { getFullList: vi.fn().mockResolvedValue(stacks) }
        }
        return { getFullList: vi.fn().mockResolvedValue(envVars) }
      },
    },
  })
}

describe('CopyEnvVarsModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists stacks excluding the target and lets the user pick one', async () => {
    setupApiMocks()
    setupPbMocks(
      [{ id: 'source-1', name: 'source-stack', repository: 'repo-a', expand: { repository: { name: 'repo-a' } } }],
      [{ id: 'env-1', key: 'FOO', secret: false, secret_provider: '' }],
    )

    const wrapper = mount(CopyEnvVarsModal, {
      props: { targetId: 'target-1', targetRepository: 'repo-a' },
      global: { stubs },
    })
    await flushPromises()

    const options = wrapper.findAll('option')
    expect(options.map(o => o.text())).toEqual(['source-stack (repo-a)'])
  })

  it('blocks proceeding to step 2 when the source is cross-repository and has SOPS secrets', async () => {
    const { customGet } = setupApiMocks({ sopsAvailable: true })
    setupPbMocks(
      [{ id: 'source-1', name: 'source-stack', repository: 'repo-b', expand: { repository: { name: 'repo-b' } } }],
      [],
    )

    const wrapper = mount(CopyEnvVarsModal, {
      props: { targetId: 'target-1', targetRepository: 'repo-a' },
      global: { stubs },
    })
    await flushPromises()

    await wrapper.find('.source-select').setValue('source-1')
    await flushPromises()

    expect(customGet).toHaveBeenCalledWith('/api/custom/stacks/source-1/sops-env-vars')
    expect(wrapper.text()).toContain('can\'t be copied across repositories')

    const continueButton = wrapper.findAll('button').find(b => b.text() === 'Continue')
    expect(continueButton!.attributes('disabled')).toBeDefined()
  })

  it('allows a cross-repository copy when the source has no SOPS secrets', async () => {
    setupApiMocks({ sopsAvailable: false })
    setupPbMocks(
      [{ id: 'source-1', name: 'source-stack', repository: 'repo-b', expand: { repository: { name: 'repo-b' } } }],
      [{ id: 'env-1', key: 'FOO', secret: false, secret_provider: '' }],
    )

    const wrapper = mount(CopyEnvVarsModal, {
      props: { targetId: 'target-1', targetRepository: 'repo-a' },
      global: { stubs },
    })
    await flushPromises()

    await wrapper.find('.source-select').setValue('source-1')
    await flushPromises()

    const continueButton = wrapper.findAll('button').find(b => b.text() === 'Continue')
    expect(continueButton!.attributes('disabled')).toBeUndefined()
  })

  it('submits the copy request with selected keys and overwrite flag', async () => {
    const { customPost } = setupApiMocks({ sopsAvailable: false })
    setupPbMocks(
      [{ id: 'source-1', name: 'source-stack', repository: 'repo-a', expand: { repository: { name: 'repo-a' } } }],
      [{ id: 'env-1', key: 'FOO', secret: false, secret_provider: '' }, { id: 'env-2', key: 'BAR', secret: true, secret_provider: 'internal' }],
    )

    const wrapper = mount(CopyEnvVarsModal, {
      props: { targetId: 'target-1', targetRepository: 'repo-a' },
      global: { stubs },
    })
    await flushPromises()

    await wrapper.find('.source-select').setValue('source-1')
    await flushPromises()

    await wrapper.findAll('button').find(b => b.text() === 'Continue')!.trigger('click')
    await flushPromises()

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    await checkboxes[0]!.setValue(true)
    const overwriteCheckbox = checkboxes[checkboxes.length - 1]!
    await overwriteCheckbox.setValue(true)

    await wrapper.findAll('button').find(b => b.text() === 'Copy Variables')!.trigger('click')
    await flushPromises()

    expect(customPost).toHaveBeenCalledWith('/api/custom/stacks/target-1/env-vars/copy-from', {
      source_stack: 'source-1',
      keys: ['FOO'],
      overwrite: true,
    })
    expect(wrapper.emitted('copied')).toBeTruthy()
  })

  it('emits cancel when Cancel is clicked', async () => {
    setupApiMocks()
    setupPbMocks([], [])

    const wrapper = mount(CopyEnvVarsModal, {
      props: { targetId: 'target-1', targetRepository: 'repo-a' },
      global: { stubs },
    })
    await flushPromises()

    await wrapper.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(wrapper.emitted('cancel')).toBeTruthy()
  })
})
