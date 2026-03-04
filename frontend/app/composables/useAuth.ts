export function useAuth() {
  const { $pb } = useNuxtApp()
  const user = useState('auth_user', () => $pb.authStore.record)

  const login = async (email: string, password: string) => {
    const record = await $pb.collection('_superusers').authWithPassword(email, password)
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
    return $pb.collection('_superusers').update(userId, { oldPassword, password, passwordConfirm })
  }

  const requestPasswordReset = async (email: string) => {
    return $pb.collection('_superusers').requestPasswordReset(email)
  }

  const confirmPasswordReset = async (token: string, password: string, passwordConfirm: string) => {
    return $pb.collection('_superusers').confirmPasswordReset(token, password, passwordConfirm)
  }

  const isAuthenticated = computed(() => $pb.authStore.isValid)

  return { user, login, logout, changePassword, requestPasswordReset, confirmPasswordReset, isAuthenticated }
}
