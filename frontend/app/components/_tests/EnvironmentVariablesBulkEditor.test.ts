import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h, ref } from 'vue'
import EnvironmentVariablesBulkEditor from '../EnvironmentVariablesBulkEditor.vue'

type Slots = Record<string, (() => unknown) | undefined>

const stubs = {
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
  UButton: {
    props: ['label', 'loading', 'disabled'],
    emits: ['click'],
    setup(props: { label?: string, disabled?: boolean }, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { disabled: props.disabled, onClick: () => emit('click') }, props.label)
    },
  },
  CancelButton: {
    emits: ['click'],
    setup(_props: unknown, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { onClick: () => emit('click') }, 'Cancel')
    },
  },
  UFormField: {
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', slots.default?.())
    },
  },
  UIcon: { setup: () => () => h('span') },
}

describe('EnvironmentVariablesBulkEditor', () => {
  const customPost = vi.fn().mockResolvedValue({})
  const revealStackEnvVars = vi.fn().mockResolvedValue({ values: {} })

  beforeEach(() => {
    customPost.mockClear()
    revealStackEnvVars.mockClear()
    revealStackEnvVars.mockResolvedValue({ values: {} })
    ;(globalThis as any).useApi = () => ({ customPost, revealStackEnvVars })
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
    ;(globalThis as any).usePermissions = () => ({ isAdmin: ref(false) })
  })

  it('imports a multiline secret without changing its JSON escapes', async () => {
    const value = '{\n"private_key":"FAKE\\nKEY"\n}\n'
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: { targetType: 'stack', targetId: 'stack-1', envVars: [], importContent: `GCP='${value}'`, defaultSecretKeys: new Set(['GCP']) },
      global: { stubs },
    })
    await wrapper.findAll('button').find(b => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(customPost).toHaveBeenCalledWith('/api/custom/stacks/stack-1/env-vars/bulk', {
      mode: 'append', vars: [{ key: 'GCP', value, secret: true, secret_provider: 'internal' }],
    })
  })

  it('prefills the textarea from envVars, masking internal secrets', () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: {
        targetType: 'stack',
        targetId: 'stack-1',
        envVars: [
          { key: 'PLAIN', value: 'hello', secret: false, secret_provider: '' },
          { key: 'TOKEN', value: 'ignored-ciphertext', secret: true, secret_provider: 'internal' },
        ],
      },
      global: { stubs },
    })

    const text = (wrapper.find('textarea').element as HTMLTextAreaElement).value
    expect(text).toContain('PLAIN=hello')
    expect(text).toContain('TOKEN=')
    expect(text).not.toContain('ignored-ciphertext')
  })

  it('parses KEY=VALUE lines and ignores blank/comment lines', async () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: { targetType: 'stack', targetId: 'stack-1', envVars: [] },
      global: { stubs },
    })

    await wrapper.find('textarea').setValue('# comment\n\nFOO=bar\nBAZ=qux')
    await wrapper.findAll('button').find(b => b.text() === 'Save')!.trigger('click')
    await Promise.resolve()

    expect(customPost).toHaveBeenCalledWith('/api/custom/stacks/stack-1/env-vars/bulk', {
      mode: 'replace',
      vars: [
        { key: 'FOO', value: 'bar', secret: false, secret_provider: '' },
        { key: 'BAZ', value: 'qux', secret: false, secret_provider: '' },
      ],
    })
  })

  it('rejects duplicate keys without calling customPost', async () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: { targetType: 'stack', targetId: 'stack-1', envVars: [] },
      global: { stubs },
    })

    await wrapper.find('textarea').setValue('FOO=1\nFOO=2')

    const saveButtons = wrapper.findAll('button').filter(b => b.text() === 'Save')
    expect(saveButtons[0]!.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('duplicate key')
    expect(customPost).not.toHaveBeenCalled()
  })

  it('preserves an unchanged secret by sending a blank value for it', async () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: {
        targetType: 'stack',
        targetId: 'stack-1',
        envVars: [{ key: 'TOKEN', value: 'ciphertext', secret: true, secret_provider: 'internal' }],
      },
      global: { stubs },
    })

    // Textarea is prefilled with "TOKEN=" already (masked); submit unchanged.
    await wrapper.findAll('button').find(b => b.text() === 'Save')!.trigger('click')
    await Promise.resolve()

    expect(customPost).toHaveBeenCalledWith('/api/custom/stacks/stack-1/env-vars/bulk', {
      mode: 'replace',
      vars: [{ key: 'TOKEN', value: '', secret: true, secret_provider: 'internal' }],
    })
  })

  it('emits saved after a successful submit', async () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: { targetType: 'stack', targetId: 'stack-1', envVars: [] },
      global: { stubs },
    })

    await wrapper.find('textarea').setValue('FOO=bar')
    await wrapper.findAll('button').find(b => b.text() === 'Save')!.trigger('click')
    await Promise.resolve()

    expect(wrapper.emitted('saved')).toBeTruthy()
  })

  it('emits cancel when Cancel is clicked', async () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: { targetType: 'stack', targetId: 'stack-1', envVars: [] },
      global: { stubs },
    })

    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('cancel')).toBeTruthy()
  })

  it('defaults to append mode with a Replace all toggle when opened from import', async () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: { targetType: 'stack', targetId: 'stack-1', envVars: [], importContent: 'FOO=bar' },
      global: { stubs },
    })

    expect((wrapper.find('textarea').element as HTMLTextAreaElement).value).toBe('FOO=bar')

    const saveButton = wrapper.findAll('button').find(b => b.text() === 'Save')
    await saveButton!.trigger('click')
    await Promise.resolve()

    expect(customPost).toHaveBeenCalledWith('/api/custom/stacks/stack-1/env-vars/bulk', {
      mode: 'append',
      vars: [{ key: 'FOO', value: 'bar', secret: false, secret_provider: '' }],
    })
  })

  it('marks a defaultSecretKeys key as secret/internal on save', async () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: {
        targetType: 'stack',
        targetId: 'stack-1',
        envVars: [],
        importContent: 'POSTGRES_PASSWORD=hunter2',
        defaultSecretKeys: new Set(['POSTGRES_PASSWORD']),
      },
      global: { stubs },
    })

    await wrapper.findAll('button').find(b => b.text() === 'Save')!.trigger('click')
    await Promise.resolve()

    expect(customPost).toHaveBeenCalledWith('/api/custom/stacks/stack-1/env-vars/bulk', {
      mode: 'append',
      vars: [{ key: 'POSTGRES_PASSWORD', value: 'hunter2', secret: true, secret_provider: 'internal' }],
    })
  })

  it('prefills decrypted secret values for admins via reveal-all', async () => {
    ;(globalThis as any).usePermissions = () => ({ isAdmin: ref(true) })
    revealStackEnvVars.mockResolvedValue({ values: { TOKEN: 's3cr3t' } })

    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: {
        targetType: 'stack',
        targetId: 'stack-1',
        envVars: [
          { key: 'PLAIN', value: 'hello', secret: false, secret_provider: '' },
          { key: 'TOKEN', value: 'ignored-ciphertext', secret: true, secret_provider: 'internal' },
        ],
      },
      global: { stubs },
    })
    await flushPromises()

    expect(revealStackEnvVars).toHaveBeenCalledWith('stack-1')
    const text = (wrapper.find('textarea').element as HTMLTextAreaElement).value
    expect(text).toContain('PLAIN=hello')
    expect(text).toContain('TOKEN=s3cr3t')
  })

  it('falls back to a blank secret value and toasts when reveal-all fails', async () => {
    ;(globalThis as any).usePermissions = () => ({ isAdmin: ref(true) })
    const addToast = vi.fn()
    ;(globalThis as any).useToast = () => ({ add: addToast })
    revealStackEnvVars.mockRejectedValue(new Error('network error'))

    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: {
        targetType: 'stack',
        targetId: 'stack-1',
        envVars: [{ key: 'TOKEN', value: 'ignored-ciphertext', secret: true, secret_provider: 'internal' }],
      },
      global: { stubs },
    })
    await flushPromises()

    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Failed to load secret values', color: 'error' }))
    const text = (wrapper.find('textarea').element as HTMLTextAreaElement).value
    expect(text).toContain('TOKEN=')
    expect(text).not.toContain('ignored-ciphertext')
  })

  it('does not fetch decrypted values for non-admins (blank secret prefill unchanged)', async () => {
    ;(globalThis as any).usePermissions = () => ({ isAdmin: ref(false) })

    mount(EnvironmentVariablesBulkEditor, {
      props: {
        targetType: 'stack',
        targetId: 'stack-1',
        envVars: [{ key: 'TOKEN', value: 'ignored-ciphertext', secret: true, secret_provider: 'internal' }],
      },
      global: { stubs },
    })
    await flushPromises()

    expect(revealStackEnvVars).not.toHaveBeenCalled()
  })

  it('resets to append mode when importContent changes while mounted', async () => {
    const wrapper = mount(EnvironmentVariablesBulkEditor, {
      props: { targetType: 'stack', targetId: 'stack-1', envVars: [], importContent: undefined },
      global: { stubs },
    })

    // Plain bulk-edit (no importContent) defaults replaceAll to true and
    // hides the toggle; switching to import content mid-mount (e.g. a
    // template picked while already in bulk view) must reset the mode to
    // append/update rather than keeping the previous "replace all" default.
    await wrapper.setProps({ importContent: 'FOO=bar' })
    await Promise.resolve()

    await wrapper.findAll('button').find(b => b.text() === 'Save')!.trigger('click')
    await Promise.resolve()

    expect(customPost).toHaveBeenCalledWith('/api/custom/stacks/stack-1/env-vars/bulk', {
      mode: 'append',
      vars: [{ key: 'FOO', value: 'bar', secret: false, secret_provider: '' }],
    })
  })
})
