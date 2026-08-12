import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import ExecutableCommand from '../ExecutableCommand.vue'

const stubs = {
  UFormField: { props: ['label'], setup: (_p: unknown, { slots }: any) => () => h('div', slots.default?.()) },
  AppButtonInput: {
    props: ['icon', 'ariaLabel'],
    emits: ['click'],
    setup(props: { ariaLabel?: string }, { emit }: any) {
      return () => h('button', { 'aria-label': props.ariaLabel, onClick: () => emit('click') })
    },
  },
}

describe('ExecutableCommand', () => {
  beforeEach(() => {
    ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    delete (document as any).execCommand
  })

  it('re-masks and requires another reveal when masked content changes', async () => {
    const wrapper = mount(ExecutableCommand, {
      props: { label: 'Token', content: 'secret-a', masked: true },
      global: { stubs },
    })

    await wrapper.get('[aria-label="Reveal"]').trigger('click')
    expect(wrapper.get('code').text()).toBe('secret-a')

    await wrapper.setProps({ content: 'secret-b' })
    expect(wrapper.get('code').text()).not.toBe('secret-b')
    expect(wrapper.get('[aria-label="Reveal"]')).toBeTruthy()

    await wrapper.get('[aria-label="Reveal"]').trigger('click')
    expect(wrapper.get('code').text()).toBe('secret-b')
  })

  it('copies to the clipboard and falls back to the legacy copy command when needed', async () => {
    const toastAdd = vi.fn()
    ;(globalThis as any).useToast = () => ({ add: toastAdd })
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const wrapper = mount(ExecutableCommand, { props: { label: 'Command', content: 'docker compose up' }, global: { stubs } })

    await wrapper.get('[aria-label="Copy"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith('docker compose up')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Copied!', color: 'success' })

    writeText.mockRejectedValueOnce(new Error('denied'))
    Object.assign(document, { execCommand: vi.fn().mockReturnValue(true) })
    await wrapper.get('[aria-label="Copy"]').trigger('click')
    expect(document.querySelector('textarea')).toBeNull()
    expect(toastAdd).toHaveBeenLastCalledWith({ title: 'Copied!', color: 'success' })
  })

  it('reports a failed copy and downloads the supplied content with its configured filename', async () => {
    const toastAdd = vi.fn()
    ;(globalThis as any).useToast = () => ({ add: toastAdd })
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    Object.assign(document, { execCommand: vi.fn().mockReturnValue(false) })
    const createObjectURL = vi.fn().mockReturnValue('blob:command')
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const wrapper = mount(ExecutableCommand, {
      props: { label: 'Command', content: 'echo hello', multiline: true, downloadFilename: 'command.sh' },
      global: { stubs },
    })

    await wrapper.get('[aria-label="Copy"]').trigger('click')
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Copy failed', color: 'error' }))

    await wrapper.get('[aria-label="Download"]').trigger('click')
    const blob = createObjectURL.mock.calls[0]?.[0] as Blob
    expect(await blob.text()).toBe('echo hello')
    expect(click).toHaveBeenCalledOnce()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:command')
  })
})
