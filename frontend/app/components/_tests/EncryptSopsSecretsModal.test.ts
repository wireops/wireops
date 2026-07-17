import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import { ref } from 'vue'
import EncryptSopsSecretsModal from '../EncryptSopsSecretsModal.vue'

function setupGlobals(customPost = vi.fn().mockResolvedValue({ content: 'encrypted-yaml', filename: 'secrets.yaml' })) {
  for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: () => ({
        getFullList: vi.fn().mockResolvedValue([{ id: 'repo-1', name: 'repo', sops_age_public_key: 'age1abc' }]),
      }),
    },
  })
  ;(globalThis as any).useCopy = () => ({ copy: vi.fn() })
  const addToast = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: addToast })
  ;(globalThis as any).useApi = () => ({ customPost })
  ;(globalThis as any).useAsyncData = (_key: string, fn: () => Promise<any>) => {
    const data = ref<any[]>([])
    const refresh = async () => { data.value = await fn() }
    refresh()
    return { data, refresh }
  }

  return { addToast, customPost }
}

describe('EncryptSopsSecretsModal encrypt flow', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('encrypts values, stores the result and switches to the Result tab', async () => {
    const { customPost } = setupGlobals()

    const wrapper = mount(EncryptSopsSecretsModal, { props: { open: true }, shallow: true })
    await flushPromises()

    ;(wrapper.vm as any).repositoryId = 'repo-1'
    ;(wrapper.vm as any).rows = [{ key: 'DB_PASS', value: 'hunter2' }]
    await (wrapper.vm as any).encrypt()
    await flushPromises()

    expect(customPost).toHaveBeenCalledWith(
      '/api/custom/repositories/repo-1/sops-encrypt',
      { values: { DB_PASS: 'hunter2' } },
    )
    expect((wrapper.vm as any).result).toBe('encrypted-yaml')
    expect((wrapper.vm as any).activeTab).toBe('result')
    expect((wrapper.vm as any).encrypting).toBe(false)
  })

  it('shows an error toast and leaves the Values tab active on failure', async () => {
    const failingPost = vi.fn().mockRejectedValue({ data: { error: 'bad age key' } })
    const { addToast } = setupGlobals(failingPost)

    const wrapper = mount(EncryptSopsSecretsModal, { props: { open: true }, shallow: true })
    await flushPromises()

    ;(wrapper.vm as any).repositoryId = 'repo-1'
    ;(wrapper.vm as any).rows = [{ key: 'DB_PASS', value: 'hunter2' }]
    await (wrapper.vm as any).encrypt()
    await flushPromises()

    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Encryption failed', description: 'bad age key', color: 'error' }))
    expect((wrapper.vm as any).result).toBe('')
    expect((wrapper.vm as any).activeTab).toBe('values')
    expect((wrapper.vm as any).encrypting).toBe(false)
  })
})
