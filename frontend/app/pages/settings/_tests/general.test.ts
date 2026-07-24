import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import GeneralPage from '../general.vue'

function setupGlobals() {
  const keyscan = vi.fn().mockResolvedValue({ success: 'true', result: 'github.com ssh-ed25519 AAAA' })
  const toastAdd = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: toastAdd })
  ;(globalThis as any).useApi = () => ({
    getAppSettings: vi.fn().mockResolvedValue(null),
    saveAppSettings: vi.fn(),
    keyscan,
  })
  return { keyscan, toastAdd }
}

describe('settings/general.vue keyscan validation', () => {
  it('rejects unusable SSH ports before running keyscan', async () => {
    const { keyscan, toastAdd } = setupGlobals()
    const wrapper = mount(GeneralPage, { shallow: true })
    await flushPromises()

    ;(wrapper.vm as any).keyscanHost = 'github.com'

    for (const value of ['', 'abc', '22.5', '0', '65536']) {
      ;(wrapper.vm as any).keyscanPort = value
      await (wrapper.vm as any).runKeyscan()
    }

    expect(keyscan).not.toHaveBeenCalled()
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Invalid SSH port' }))
  })

  it('passes a trimmed host and valid integer port to keyscan', async () => {
    const { keyscan } = setupGlobals()
    const wrapper = mount(GeneralPage, { shallow: true })
    await flushPromises()

    ;(wrapper.vm as any).keyscanHost = ' github.com '
    ;(wrapper.vm as any).keyscanPort = '2222'
    await (wrapper.vm as any).runKeyscan()

    expect(keyscan).toHaveBeenCalledWith('github.com', 2222)
  })
})
