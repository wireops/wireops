const PUBLIC_PATHS = ['/login', '/forgot-password', '/reset-password', '/invite']

export default defineNuxtRouteMiddleware(async (to) => {
  const { $pb } = useNuxtApp()

  if (PUBLIC_PATHS.includes(to.path)) {
    if (to.path === '/login' && $pb.authStore.isValid) {
      return navigateTo('/')
    }
    return
  }

  if (!$pb.authStore.isValid) {
    return navigateTo('/login')
  }

  try {
    await $pb.collection('_superusers').authRefresh()
  } catch {
    $pb.authStore.clear()
    return navigateTo('/login')
  }
})
