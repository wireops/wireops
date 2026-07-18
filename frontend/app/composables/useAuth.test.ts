import { beforeEach, describe, expect, it, vi } from 'vitest'
import { computed } from 'vue'

function makeAuthStore(initialRecord: any = null) {
  return {
    record: initialRecord,
    token: '',
    isValid: !!initialRecord,
    clear: vi.fn(function (this: any) {
      this.record = null
      this.token = ''
      this.isValid = false
    }),
  }
}

describe('useAuth', () => {
  let navigateToMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.resetModules()
    navigateToMock = vi.fn()
    ;(globalThis as any).navigateTo = navigateToMock
    ;(globalThis as any).useState = (_key: string, init: () => any) => ({ value: init() })
    ;(globalThis as any).computed = computed
  })

  it('login authenticates the wireops user and, when the same credentials are a superuser, picks up a real superuser session too', async () => {
    const userRecord = { id: 'user-1', email: 'admin@example.com' }
    const superuserRecord = { id: 'super-1', email: 'admin@example.com' }

    const authWithPassword = vi.fn().mockResolvedValue({ record: userRecord })
    const superuserAuthWithPassword = vi.fn().mockResolvedValue({ record: superuserRecord })

    const pbAuthStore = makeAuthStore()
    const pbSuperuserAuthStore = makeAuthStore()

    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        authStore: pbAuthStore,
        collection: () => ({ authWithPassword }),
      },
      $pbSuperuser: {
        authStore: pbSuperuserAuthStore,
        collection: () => ({ authWithPassword: superuserAuthWithPassword }),
      },
    })

    const { useAuth } = await import('./useAuth')
    const { login, user } = useAuth()

    const result = await login('admin@example.com', 'password123')

    expect(authWithPassword).toHaveBeenCalledWith('admin@example.com', 'password123')
    expect(superuserAuthWithPassword).toHaveBeenCalledWith('admin@example.com', 'password123')
    expect(result.record).toBe(userRecord)
    expect(user.value).toBe(userRecord)
    expect(pbSuperuserAuthStore.clear).not.toHaveBeenCalled()
  })

  it('login clears the privileged superuser store when the same credentials are not a real superuser', async () => {
    const userRecord = { id: 'user-2', email: 'operator@example.com' }

    const authWithPassword = vi.fn().mockResolvedValue({ record: userRecord })
    const superuserAuthWithPassword = vi.fn().mockRejectedValue(new Error('invalid credentials'))

    const pbAuthStore = makeAuthStore()
    const pbSuperuserAuthStore = makeAuthStore()

    ;(globalThis as any).useNuxtApp = () => ({
      $pb: {
        authStore: pbAuthStore,
        collection: () => ({ authWithPassword }),
      },
      $pbSuperuser: {
        authStore: pbSuperuserAuthStore,
        collection: () => ({ authWithPassword: superuserAuthWithPassword }),
      },
    })

    const { useAuth } = await import('./useAuth')
    const { login, user } = useAuth()

    const result = await login('operator@example.com', 'password123')

    expect(result.record).toBe(userRecord)
    expect(user.value).toBe(userRecord)
    expect(pbSuperuserAuthStore.clear).toHaveBeenCalledTimes(1)
  })

  it('logout clears both auth stores, resets the user state, and redirects to login', async () => {
    const pbAuthStore = makeAuthStore({ id: 'user-1' })
    const pbSuperuserAuthStore = makeAuthStore({ id: 'super-1' })

    ;(globalThis as any).useNuxtApp = () => ({
      $pb: { authStore: pbAuthStore, collection: () => ({}) },
      $pbSuperuser: { authStore: pbSuperuserAuthStore, collection: () => ({}) },
    })

    const { useAuth } = await import('./useAuth')
    const { logout, user } = useAuth()

    logout()

    expect(pbAuthStore.clear).toHaveBeenCalledTimes(1)
    expect(pbSuperuserAuthStore.clear).toHaveBeenCalledTimes(1)
    expect(user.value).toBeNull()
    expect(navigateToMock).toHaveBeenCalledWith('/login')
  })
})
