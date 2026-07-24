import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import SecurityPage from '../security.vue'

function setupGlobals() {
  const updateUser = vi.fn().mockResolvedValue({})
  const toastAdd = vi.fn()
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([]),
  }))

  const queryState = reactive<{ query: Record<string, any> }>({ query: {} })
  ;(globalThis as any).useRoute = () => queryState
  ;(globalThis as any).useRouter = () => ({ push: vi.fn(), replace: vi.fn() })

  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      baseURL: 'http://localhost',
      authStore: { token: 'token', record: { id: 'user-1' } },
      collection: (name: string) => {
        if (name === 'users') return { update: updateUser }
        return { update: vi.fn() }
      },
    },
  })
  ;(globalThis as any).useToast = () => ({ add: toastAdd })
  ;(globalThis as any).usePermissions = () => ({ isAdmin: ref(true) })

  const saveAppSettings = vi.fn().mockResolvedValue({})
  const saveGlobalWorkerPolicy = vi.fn().mockResolvedValue({})
  const getGlobalWorkerPolicy = vi.fn().mockResolvedValue({
    enabled: false,
    allowed_volumes: [],
    allowed_networks: [],
    allowed_images: [],
    allowed_cap_add: [],
    allowed_devices: [],
    allowed_security_opt: [],
    prevent_latest_images: false,
    block_host_volumes: false,
    block_privileged: false,
    block_host_network: false,
    block_host_pid: false,
    block_host_ipc: false,
    block_docker_socket: false,
  })
  ;(globalThis as any).useApi = () => ({
    getAppSettings: vi.fn().mockResolvedValue(null),
    saveAppSettings,
    getGlobalWorkerPolicy,
    saveGlobalWorkerPolicy,
    listAuditLogs: vi.fn().mockResolvedValue({ items: [], totalItems: 0 }),
  })

  return { saveAppSettings, saveGlobalWorkerPolicy, getGlobalWorkerPolicy, updateUser, toastAdd }
}

describe('security.vue worker policy presets', () => {
  it('applyStrictProductionPreset enables enforcement along with the protection flags', async () => {
    const { saveGlobalWorkerPolicy } = setupGlobals()

    const wrapper = mount(SecurityPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    await (wrapper.vm as any).applyStrictProductionPreset()
    await flushPromises()

    expect(saveGlobalWorkerPolicy).toHaveBeenCalledTimes(1)
    const savedPolicy = saveGlobalWorkerPolicy.mock.calls[0][0]
    expect(savedPolicy.enabled).toBe(true)
    expect(savedPolicy.block_privileged).toBe(true)
    expect(savedPolicy.block_host_network).toBe(true)
    expect(savedPolicy.block_host_pid).toBe(true)
    expect(savedPolicy.block_host_ipc).toBe(true)
    expect(savedPolicy.block_docker_socket).toBe(true)
  })

  it('rejects blank password fields before updating the user', async () => {
    const { updateUser, toastAdd } = setupGlobals()

    const wrapper = mount(SecurityPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).changePasswordForm.oldPassword = ''
    ;(wrapper.vm as any).changePasswordForm.password = 'new-password'
    ;(wrapper.vm as any).changePasswordForm.passwordConfirm = 'new-password'
    await (wrapper.vm as any).handleChangePassword()

    expect(updateUser).not.toHaveBeenCalled()
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'All password fields are required' }))
  })

  it('trims SSO group mappings and rejects blank groups before posting', async () => {
    setupGlobals()

    const wrapper = mount(SecurityPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    const fetchMock = vi.mocked(fetch)
    fetchMock.mockClear()

    ;(wrapper.vm as any).ssoGroupRoleForm.group = '   '
    ;(wrapper.vm as any).ssoGroupRoleForm.role = 'admin'
    await (wrapper.vm as any).createSSOGroupRole()
    expect(fetchMock).not.toHaveBeenCalled()

    ;(wrapper.vm as any).ssoGroupRoleForm.group = '  wireops-admins  '
    await (wrapper.vm as any).createSSOGroupRole()

    expect(fetchMock).toHaveBeenCalled()
    const request = fetchMock.mock.calls[0]![1] as RequestInit
    expect(request.method).toBe('POST')
    expect(JSON.parse(request.body as string)).toEqual({ group: 'wireops-admins', role: 'admin' })
  })

  it('keeps invalid retention values out of app settings', async () => {
    const { saveAppSettings, toastAdd } = setupGlobals()

    const wrapper = mount(SecurityPage, {
      global: { stubs: { transition: false } },
      shallow: true,
    })
    await flushPromises()

    ;(wrapper.vm as any).appSettings.audit_retention_days = 30
    ;(wrapper.vm as any).appSettings.job_run_retention_days = 7
    ;(wrapper.vm as any).updateAuditRetentionDays('')
    ;(wrapper.vm as any).updateJobRunRetentionDays('2.5')

    expect((wrapper.vm as any).appSettings.audit_retention_days).toBe(30)
    expect((wrapper.vm as any).appSettings.job_run_retention_days).toBe(7)

    ;(wrapper.vm as any).updateAuditRetentionDays('90')
    ;(wrapper.vm as any).updateJobRunRetentionDays('14')

    expect((wrapper.vm as any).appSettings.audit_retention_days).toBe(90)
    expect((wrapper.vm as any).appSettings.job_run_retention_days).toBe(14)

    ;(wrapper.vm as any).appSettings.audit_retention_days = Number.NaN
    await (wrapper.vm as any).handleSaveAppSettings()

    expect(saveAppSettings).not.toHaveBeenCalled()
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Invalid retention settings' }))
  })
})
