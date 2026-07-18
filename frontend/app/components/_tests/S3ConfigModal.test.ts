import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import S3ConfigModal from '../integrations/S3ConfigModal.vue'

function setupGlobals(saveIntegration = vi.fn().mockResolvedValue({ slug: 's3', enabled: true, config: {} })) {
  for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  const addToast = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: addToast })
  ;(globalThis as any).useIntegrations = () => ({ saveIntegration })

  return { addToast, saveIntegration }
}

const validForm = {
  bucket: 'my-bucket',
  region: 'us-east-1',
  accessKey: 'AKIA...',
  secret: 's3cr3t',
}

describe('S3ConfigModal handleSave', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('saves valid settings, shows a success toast, emits saved and closes', async () => {
    const { addToast, saveIntegration } = setupGlobals()

    const wrapper = mount(S3ConfigModal, {
      props: { integration: { enabled: true, config: {} }, open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, validForm)
    await (wrapper.vm as any).handleSave()
    await flushPromises()

    expect(saveIntegration).toHaveBeenCalledWith('s3', true, expect.objectContaining({
      bucket: 'my-bucket',
      region: 'us-east-1',
      access_key: 'AKIA...',
      secret: 's3cr3t',
    }))
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'S3 storage settings saved', color: 'success' }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
  })

  it('rejects a bucket that includes a path/prefix without calling saveIntegration', async () => {
    const { addToast, saveIntegration } = setupGlobals()

    const wrapper = mount(S3ConfigModal, {
      props: { integration: { enabled: true, config: {} }, open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, { ...validForm, bucket: 'my-bucket/sub-path' })
    await (wrapper.vm as any).handleSave()
    await flushPromises()

    expect(saveIntegration).not.toHaveBeenCalled()
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Bucket must not include a path/prefix', color: 'error' }))
    expect(wrapper.emitted('saved')).toBeUndefined()
  })

  it('shows an error toast and stays open when the save reports failure', async () => {
    const { addToast, saveIntegration } = setupGlobals(vi.fn().mockResolvedValue(false))

    const wrapper = mount(S3ConfigModal, {
      props: { integration: { enabled: true, config: {} }, open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, validForm)
    await (wrapper.vm as any).handleSave()
    await flushPromises()

    expect(saveIntegration).toHaveBeenCalled()
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Failed to save settings', color: 'error' }))
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.emitted('update:open')).toBeUndefined()
  })
})
