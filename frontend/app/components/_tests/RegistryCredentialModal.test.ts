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
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Entered values work', color: 'success' }))
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
      title: 'Entered values failed',
      description: 'registry unreachable.',
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

  it('shows a normalized duplicate-host error on Registry URL', async () => {
    const duplicateError = {
      response: {
        data: {
          data: {
            registry_url: { message: 'A registry credential already exists for registry.example.com.' },
          },
        },
      },
    }
    const { create, addToast } = setupGlobals({ create: vi.fn().mockRejectedValue(duplicateError) })
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    Object.assign((wrapper.vm as any).form, {
      name: 'Duplicate',
      registry_url: 'HTTPS://REGISTRY.EXAMPLE.COM/v2/',
      auth_type: 'basic',
      username: 'deploy',
      password: 'hunter2',
    })
    await (wrapper.vm as any).submit()
    await flushPromises()

    expect(create).toHaveBeenCalled()
    expect((wrapper.vm as any).registryUrlError).toContain('already exists')
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Failed to create credential', color: 'error' }))
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

  it('never mixes the saved record into the draft connection test', async () => {
    const { testRegistryCredential } = setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: {
        open: false,
        credential: { id: 'cred-1', name: 'GHCR', registry_url: 'ghcr.io', auth_type: 'basic', username: 'deploy', insecure: false },
      },
      shallow: true,
    })
    await wrapper.setProps({ open: true })
    ;(wrapper.vm as any).form.password = 'hunter2'
    await flushPromises()

    await (wrapper.vm as any).testConnection()
    await flushPromises()

    const body = testRegistryCredential.mock.calls[0][0]
    expect(body).not.toHaveProperty('credential_id')
    expect(body.registry_url).toBe('ghcr.io')
  })

  it('requires a password for a draft test in edit mode', async () => {
    const { addToast, testRegistryCredential } = setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: {
        open: false,
        credential: { id: 'cred-1', name: 'GHCR', registry_url: 'ghcr.io', auth_type: 'basic', username: 'deploy', insecure: false },
      },
      shallow: true,
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    await (wrapper.vm as any).testConnection()
    await flushPromises()

    expect(testRegistryCredential).not.toHaveBeenCalled()
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Password not entered',
      description: expect.stringContaining('Verify Saved Credential'),
      color: 'warning',
    }))
  })

  it('verifies the persisted credential by id alone', async () => {
    const { addToast, testRegistryCredential } = setupGlobals({
      testRegistryCredential: vi.fn().mockResolvedValue({ success: true, source: 'persisted' }),
    })
    const wrapper = mount(RegistryCredentialModal, {
      props: {
        open: false,
        credential: { id: 'cred-1', name: 'GHCR', registry_url: 'ghcr.io', auth_type: 'basic', username: 'deploy', insecure: false },
      },
      shallow: true,
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    await (wrapper.vm as any).verifySaved()
    await flushPromises()

    expect(testRegistryCredential).toHaveBeenCalledWith({ credential_id: 'cred-1' })
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Saved credential verified', color: 'success' }))
  })

  it('surfaces the diagnostic stage and hint when the saved credential fails', async () => {
    const { addToast, testRegistryCredential } = setupGlobals({
      testRegistryCredential: vi.fn().mockResolvedValue({
        success: false,
        source: 'persisted',
        error: 'authentication rejected (HTTP 401)',
        diagnostic: { stage: 'challenge', category: 'auth_rejected', http_status: 401, message: 'authentication rejected (HTTP 401)', hint: 'verify the saved username/password or token' },
      }),
    })
    const wrapper = mount(RegistryCredentialModal, {
      props: {
        open: false,
        credential: { id: 'cred-1', name: 'GHCR', registry_url: 'ghcr.io', auth_type: 'basic', username: 'deploy', insecure: false },
      },
      shallow: true,
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    await (wrapper.vm as any).verifySaved()
    await flushPromises()

    expect(testRegistryCredential).toHaveBeenCalledWith({ credential_id: 'cred-1' })
    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Saved credential failed',
      color: 'error',
      description: expect.stringContaining('stage: challenge'),
    }))
    expect(addToast.mock.calls.at(-1)![0].description).toContain('verify the saved username/password or token')
  })

  it('surfaces diagnostics from a rejected draft probe response', async () => {
    const probeError = Object.assign(new Error('authentication rejected (HTTP 401)'), {
      data: {
        error: 'authentication rejected (HTTP 401)',
        diagnostic: { stage: 'challenge', hint: 'verify the entered username/password or token' },
      },
    })
    const { addToast } = setupGlobals({ testRegistryCredential: vi.fn().mockRejectedValue(probeError) })
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
      description: 'authentication rejected (HTTP 401) (stage: challenge). verify the entered username/password or token',
      color: 'error',
    }))
  })

  it('surfaces diagnostics from a rejected saved-credential probe response', async () => {
    const probeError = Object.assign(new Error('authentication rejected (HTTP 401)'), {
      data: {
        error: 'authentication rejected (HTTP 401)',
        diagnostic: { stage: 'challenge', hint: 'verify the saved username/password or token' },
      },
    })
    const { addToast } = setupGlobals({ testRegistryCredential: vi.fn().mockRejectedValue(probeError) })
    const wrapper = mount(RegistryCredentialModal, {
      props: {
        open: false,
        credential: { id: 'cred-1', name: 'GHCR', registry_url: 'ghcr.io', auth_type: 'basic', username: 'deploy', insecure: false },
      },
      shallow: true,
    })
    await wrapper.setProps({ open: true })
    await flushPromises()

    await (wrapper.vm as any).verifySaved()
    await flushPromises()

    expect(addToast).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Verification failed',
      description: 'authentication rejected (HTTP 401) (stage: challenge). verify the saved username/password or token',
      color: 'error',
    }))
  })

  it('does not verify without a saved credential', async () => {
    const { testRegistryCredential } = setupGlobals()
    const wrapper = mount(RegistryCredentialModal, {
      props: { open: true },
      shallow: true,
    })
    await flushPromises()

    await (wrapper.vm as any).verifySaved()
    await flushPromises()

    expect(testRegistryCredential).not.toHaveBeenCalled()
  })
})
