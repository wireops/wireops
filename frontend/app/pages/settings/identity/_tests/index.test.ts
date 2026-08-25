import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import IdentityPage from '../index.vue'

vi.mock('../../../../composables/usePaginatedList', async () => {
  const { ref } = await import('vue')
  return {
    usePaginatedList: () => ({
      page: ref(1),
      perPage: ref(24),
      items: ref([]),
      totalItems: ref(0),
      totalPages: ref(1),
      loading: ref(false),
      reload: vi.fn(),
    }),
  }
})

function setupGlobals() {
  const toastAdd = vi.fn()
  const fetchMock = vi.fn()
  const clipboardWrite = vi.fn()
  const isAdmin = ref(false)

  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('navigator', { clipboard: { writeText: clipboardWrite } })
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      baseURL: 'http://wireops.test',
      authStore: { token: 'token' },
      collection: () => ({ getList: vi.fn() }),
      filter: vi.fn(),
    },
  })
  ;(globalThis as any).useToast = () => ({ add: toastAdd })
  ;(globalThis as any).usePermissions = () => ({ isAdmin })
  ;(globalThis as any).useRoute = () => ({ query: {} })
  ;(globalThis as any).useRouter = () => ({ replace: vi.fn() })

  return { clipboardWrite, fetchMock, isAdmin, toastAdd }
}

function mountPage() {
  return mount(IdentityPage, {
    global: { stubs: { transition: false } },
    shallow: true,
  })
}

describe('settings/identity invite actions', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends an email invite and clears the form after success', async () => {
    const { clipboardWrite, fetchMock, toastAdd } = setupGlobals()
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve({ status: 'invited' }) })
    const wrapper = mountPage()
    await flushPromises()

    ;(wrapper.vm as any).inviteEmail = 'viewer@example.com'
    ;(wrapper.vm as any).inviteRole = 'operator'
    await (wrapper.vm as any).createInvite('email')

    expect(fetchMock).toHaveBeenCalledWith('http://wireops.test/api/custom/users/invite', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ email: 'viewer@example.com', role: 'operator', delivery: 'email' }),
    }))
    expect((wrapper.vm as any).inviteEmail).toBe('')
    expect((wrapper.vm as any).inviteRole).toBe('viewer')
    expect(clipboardWrite).not.toHaveBeenCalled()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Invitation sent', color: 'success' })
  })

  it('creates and copies a shareable invite link', async () => {
    const { clipboardWrite, fetchMock, toastAdd } = setupGlobals()
    const inviteURL = 'http://wireops.test/invite?token=abc'
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve({ status: 'invited', invite_url: inviteURL }) })
    const wrapper = mountPage()
    await flushPromises()

    ;(wrapper.vm as any).inviteEmail = 'viewer@example.com'
    await (wrapper.vm as any).createInvite('link')

    expect(JSON.parse((fetchMock.mock.calls[0]![1] as RequestInit).body as string)).toEqual({
      email: 'viewer@example.com', role: 'viewer', delivery: 'link',
    })
    expect((wrapper.vm as any).inviteLink).toBe(inviteURL)
    expect(clipboardWrite).toHaveBeenCalledWith(inviteURL)
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Invitation link copied', color: 'success' })
  })

  it('keeps the link available when copying it fails', async () => {
    const { clipboardWrite, fetchMock, toastAdd } = setupGlobals()
    const inviteURL = 'http://wireops.test/invite?token=abc'
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve({ status: 'invited', invite_url: inviteURL }) })
    clipboardWrite.mockRejectedValue(new Error('clipboard denied'))
    const wrapper = mountPage()
    await flushPromises()

    ;(wrapper.vm as any).inviteEmail = 'viewer@example.com'
    await (wrapper.vm as any).createInvite('link')

    expect((wrapper.vm as any).inviteLink).toBe(inviteURL)
    expect(toastAdd).toHaveBeenCalledWith({
      title: 'Invitation link created',
      description: 'Copy the link below to share it.',
      color: 'warning',
    })
  })

  it('reports failed invite requests without clearing the form', async () => {
    const { fetchMock, toastAdd } = setupGlobals()
    fetchMock.mockResolvedValue({ ok: false, json: () => Promise.resolve({ error: 'invite failed' }) })
    const wrapper = mountPage()
    await flushPromises()

    ;(wrapper.vm as any).inviteEmail = 'viewer@example.com'
    await (wrapper.vm as any).createInvite('email')

    expect((wrapper.vm as any).inviteEmail).toBe('viewer@example.com')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Failed to create invite', description: 'invite failed', color: 'error' })
  })

  it('copies an existing link and reports a manual-copy fallback on failure', async () => {
    const { clipboardWrite, toastAdd } = setupGlobals()
    const wrapper = mountPage()
    await flushPromises()
    ;(wrapper.vm as any).inviteLink = 'http://wireops.test/invite?token=abc'

    await (wrapper.vm as any).copyInviteLink()
    expect(clipboardWrite).toHaveBeenCalledWith('http://wireops.test/invite?token=abc')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Invitation link copied', color: 'success' })

    clipboardWrite.mockRejectedValue(new Error('clipboard denied'))
    await (wrapper.vm as any).copyInviteLink()
    expect(toastAdd).toHaveBeenCalledWith({
      title: 'Could not copy invitation link',
      description: 'Copy it manually from the field.',
      color: 'error',
    })
  })

  it('updates user status and roles, while preserving failures for retry', async () => {
    const { fetchMock, toastAdd } = setupGlobals()
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))
    fetchMock.mockResolvedValue({ ok: true, json: () => Promise.resolve({}) })
    const wrapper = mountPage()
    await flushPromises()
    const user = { id: 'user-1', email: 'viewer@example.com', disabled: false, role: 'viewer' }

    await (wrapper.vm as any).toggleUserDisabled(user)
    expect(user.disabled).toBe(true)
    expect(JSON.parse((fetchMock.mock.calls[0]![1] as RequestInit).body as string)).toEqual({ disabled: true })
    expect(toastAdd).toHaveBeenCalledWith({ title: 'User disabled', color: 'success' })

    await (wrapper.vm as any).updateUserRole(user, 'operator')
    expect(user.role).toBe('operator')
    expect(JSON.parse((fetchMock.mock.calls[1]![1] as RequestInit).body as string)).toEqual({ role: 'operator' })
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Role updated', color: 'success' })

    fetchMock.mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'forbidden' }) })
    await (wrapper.vm as any).updateUserRole(user, 'admin')
    expect(user.role).toBe('operator')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Failed to update role', description: 'forbidden', color: 'error' })
  })

  it('loads, filters, and creates service accounts', async () => {
    const { fetchMock, isAdmin, toastAdd } = setupGlobals()
    const wrapper = mountPage()
    await flushPromises()
    isAdmin.value = true

    fetchMock.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve([
        { id: 'disabled', name: 'Archive', description: 'old', enabled: false },
        { id: 'active', name: 'Deploy', description: 'production', enabled: true },
      ]),
    })
    await (wrapper.vm as any).loadServiceAccounts()
    expect((wrapper.vm as any).filteredServiceAccounts.map((account: any) => account.id)).toEqual(['active'])

    ;(wrapper.vm as any).showDisabledSAs = true
    ;(wrapper.vm as any).saSearchQuery = 'archive'
    expect((wrapper.vm as any).filteredServiceAccounts.map((account: any) => account.id)).toEqual(['disabled'])

    fetchMock
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ api_key: 'key-1', name: 'Automation' }) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([]) })
    await (wrapper.vm as any).createServiceAccount({ name: 'Automation', description: 'CI', role: 'operator' })

    expect((wrapper.vm as any).createdApiKey).toBe('key-1')
    expect((wrapper.vm as any).targetAccountName).toBe('Automation')
    expect((wrapper.vm as any).showApiKeyModal).toBe(true)
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Service account created', color: 'success' })
  })

  it('issues, revokes, and disables service-account keys', async () => {
    const { fetchMock, isAdmin, toastAdd } = setupGlobals()
    const wrapper = mountPage()
    await flushPromises()
    isAdmin.value = true
    const account = { id: 'service-1', name: 'Automation', enabled: true }

    fetchMock
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ api_key: 'issued-key' }) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([]) })
    await (wrapper.vm as any).issueApiKey(account)
    expect((wrapper.vm as any).createdApiKey).toBe('issued-key')
    expect(toastAdd).toHaveBeenCalledWith({ title: 'API key issued', color: 'success' })

    ;(wrapper.vm as any).confirmRevokeApiKey(account)
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([]) })
    await (wrapper.vm as any).executeRevokeApiKey()
    expect((wrapper.vm as any).showRevokeKeyModal).toBe(false)
    expect(toastAdd).toHaveBeenCalledWith({ title: 'API key revoked', color: 'success' })

    ;(wrapper.vm as any).openApiKeys = { 'service-1': true }
    ;(wrapper.vm as any).confirmToggleSAEnabled(account)
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve([]) })
    await (wrapper.vm as any).executeToggleSAEnabled()
    expect((wrapper.vm as any).showDisableSAModal).toBe(false)
    expect((wrapper.vm as any).openApiKeys['service-1']).toBe(false)
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Service account disabled', color: 'success' })
  })
})
