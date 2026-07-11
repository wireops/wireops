import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import SecurityPage from '../security.vue'

function setupGlobals() {
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
    },
  })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  ;(globalThis as any).usePermissions = () => ({ isAdmin: ref(true) })

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
    saveAppSettings: vi.fn(),
    getGlobalWorkerPolicy,
    saveGlobalWorkerPolicy,
    listAuditLogs: vi.fn().mockResolvedValue({ items: [], totalItems: 0 }),
  })

  return { saveGlobalWorkerPolicy, getGlobalWorkerPolicy }
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
})
