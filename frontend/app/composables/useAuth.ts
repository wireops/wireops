import type { RecordModel } from 'pocketbase'

export function useAuth() {
  const { $pb } = useNuxtApp()
  const user = useState('auth_user', () => $pb.authStore.record)

  const login = async (email: string, password: string) => {
    const record = await $pb.collection('users').authWithPassword(email, password)
    user.value = record.record
    return record
  }

  const logout = () => {
    $pb.authStore.clear()
    user.value = null
    navigateTo('/login')
  }

  const changePassword = async (oldPassword: string, password: string, passwordConfirm: string) => {
    const userId = $pb.authStore.record?.id
    if (!userId) throw new Error('Not authenticated')
    return $pb.collection('users').update(userId, { oldPassword, password, passwordConfirm })
  }

  const requestPasswordReset = async (email: string) => {
    return $pb.collection('users').requestPasswordReset(email)
  }

  const confirmPasswordReset = async (token: string, password: string, passwordConfirm: string) => {
    return $pb.collection('users').confirmPasswordReset(token, password, passwordConfirm)
  }

  const getSSOProviders = async (): Promise<{ name: string; displayName: string }[]> => {
    try {
      const methods = await $pb.collection('sso_users').listAuthMethods()
      return (methods.oauth2?.providers ?? []).map((p: any) => ({
        name: p.name as string,
        displayName: (p.displayName as string) || p.name,
      }))
    } catch {
      return []
    }
  }

  const loginWithSSO = async (providerName: string) => {
    const ssoAuth = await $pb.collection('sso_users').authWithOAuth2({ provider: providerName })

    // Clear the SSO token immediately - we don't want it to be used for admin endpoints
    // The SSO token is only valid for sso_users collection, not for app users
    $pb.authStore.clear()

    const config = useRuntimeConfig()
    const baseURL = (config.public.pocketbaseUrl as string).replace(/\/$/, '')

    try {
      const elevated = await $fetch<{ token: string; record: RecordModel }>(
        `${baseURL}/api/custom/auth/elevate`,
        {
          method: 'POST',
          body: { token: ssoAuth.token },
          headers: { 'X-Wireops-Origin': 'ui' },
        }
      )

      $pb.authStore.save(elevated.token, elevated.record)
      user.value = elevated.record
    } catch (err) {
      user.value = null
      throw err
    }
  }

  const isAuthenticated = computed(() => $pb.authStore.isValid)

  return { user, login, logout, changePassword, requestPasswordReset, confirmPasswordReset, isAuthenticated, getSSOProviders, loginWithSSO }
}
