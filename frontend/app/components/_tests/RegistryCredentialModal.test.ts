import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import RegistryCredentialModal from '../RegistryCredentialModal.vue'

function setupGlobals(overrides: {
  testRegistryCredential?: ReturnType<typeof vi.fn>
  create?: ReturnType<typeof vi.fn>
  update?: ReturnType<typeof vi.fn>
} = {}) {
  for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  const addToast = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: addToast })

  const announce = vi.fn()
  ;(globalThis as any).useA11yAnnouncer = () => ({ announce })

  const testRegistryCredential = overrides.testRegistryCredential ?? vi.fn().mockResolvedValue({ success: true })
  ;(globalThis as any).useApi = () => ({ testRegistryCredential })

  const create = overrides.create ?? vi.fn().mockResolvedValue({ id: 'cred-1' })
  const update = overrides.update ?? vi.fn().mockResolvedValue({ id: 'cred-1' })
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: { collection: () => ({ create, update }) },
  })

  return { addToast, announce, testRegistryCredential, create, update }
}

describe('RegistryCredentialModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('switching to GCP service account auto-fills username and disables it', async () => {
    setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).form.insecure = true
    ;(wrapper.vm as any).form.auth_type = 'gcp_service_account'
    await flushPromises()

    expect((wrapper.vm as any).form.username).toBe('_json_key')
    expect((wrapper.vm as any).form.insecure).toBe(false)
  })

  it('switching away from GCP service account clears the auto-filled username', async () => {
    setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).form.auth_type = 'gcp_service_account'
    await flushPromises()
    ;(wrapper.vm as any).form.auth_type = 'basic'
    await flushPromises()

    expect((wrapper.vm as any).form.username).toBe('')
  })

  it('flags invalid JSON in the service account key without blocking edits', async () => {
    setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).form.auth_type = 'gcp_service_account'
    ;(wrapper.vm as any).form.password = 'not-json'
    await flushPromises()

    expect((wrapper.vm as any).jsonHint).toBe('Invalid JSON')

    ;(wrapper.vm as any).form.password = '{"type":"service_account"}'
    await flushPromises()

    expect((wrapper.vm as any).jsonHint).toBe('')
  })

  it('tests the connection and shows a success toast', async () => {
    const { addToast, testRegistryCredential } = setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, {
      name: 'GHCR',
      registry_url: 'ghcr.io',
      auth_type: 'basic',
      username: 'deploy',
      password: 'hunter2',
    })
    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(testRegistryCredential).toHaveBeenCalledWith(expect.objectContaining({
      registry_url: 'ghcr.io',
      username: 'deploy',
      password: 'hunter2',
    }))
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Connection successful', color: 'success' }))
  })

  it('shows a failure toast when the connection test reports an error', async () => {
    const { addToast } = setupGlobals({
      testRegistryCredential: vi.fn().mockRejectedValue(new Error('authentication rejected (HTTP 401)')),
    })
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, {
      name: 'GHCR',
      registry_url: 'ghcr.io',
      auth_type: 'basic',
      username: 'deploy',
      password: 'wrong',
    })
    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Connection failed',
      description: 'authentication rejected (HTTP 401)',
      color: 'error',
    }))
  })

  it('shows a failure toast when the connection test resolves with success: false', async () => {
    const { addToast } = setupGlobals({
      testRegistryCredential: vi.fn().mockResolvedValue({ success: false, error: 'registry unreachable' }),
    })
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, {
      name: 'GHCR',
      registry_url: 'ghcr.io',
      auth_type: 'basic',
      username: 'deploy',
      password: 'hunter2',
    })
    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Connection failed',
      description: 'registry unreachable',
      color: 'error',
    }))
  })

  it('does not test the connection when required fields are missing', async () => {
    const { addToast, testRegistryCredential } = setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(testRegistryCredential).not.toHaveBeenCalled()
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Invalid credential', color: 'error' }))
  })

  it('does not test the connection when the registry url is not valid', async () => {
    const { addToast, testRegistryCredential } = setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, {
      name: 'GHCR',
      registry_url: 'not a valid url',
      auth_type: 'basic',
      username: 'deploy',
      password: 'hunter2',
    })
    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(testRegistryCredential).not.toHaveBeenCalled()
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Invalid credential',
      description: 'Registry URL is not valid',
      color: 'error',
    }))
  })

  it('creates a new credential and emits saved', async () => {
    const { create, announce } = setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, {
      name: 'GHCR',
      registry_url: 'ghcr.io',
      auth_type: 'basic',
      username: 'deploy',
      password: 'hunter2',
    })
    await (wrapper.vm as any).submit()
    await flushPromises()

    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      name: 'GHCR',
      registry_url: 'ghcr.io',
      username: 'deploy',
      password: 'hunter2',
      insecure: false,
    }))
    expect(announce).toHaveBeenCalledWith(expect.stringContaining('created'))
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
  })

  it('updates an existing credential without resending an untouched password', async () => {
    const { update } = setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: {
        open: false,
        credential: { id: 'cred-1', name: 'GHCR', registry_url: 'ghcr.io', auth_type: 'basic', username: 'deploy', insecure: false },
      },
      shallow: true,
    })
    // The form only syncs from the `credential` prop when `open` transitions
    // false -> true (see the `watch(isOpen, ...)` in the component) — this
    // mirrors how RegistryCredentialsPanel actually opens the modal.
    await wrapper.setProps({ open: true })
    await flushPromises()

    await (wrapper.vm as any).submit()
    await flushPromises()

    expect(update).toHaveBeenCalledWith('cred-1', expect.not.objectContaining({ password: expect.anything() }))
  })
})
