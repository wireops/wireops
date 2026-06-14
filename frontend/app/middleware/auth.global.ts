const PUBLIC_PATHS = ['/login', '/forgot-password', '/reset-password', '/invite', '/setup']

async function instanceNeedsSetup(pb: any): Promise<boolean> {
  try {
    const data = await pb.send('/api/custom/setup/status', { method: 'GET' })
    return data?.needsSetup === true
  } catch {
    return false
  }
}

export default defineNuxtRouteMiddleware(async (to) => {
  const { $pb } = useNuxtApp()
  const isPublicPath = PUBLIC_PATHS.includes(to.path)

  // Unauthenticated user — decide between /setup and /login
  if (!$pb.authStore.isValid) {
    if (isPublicPath && $pb.authStore.token && $pb.authStore.record) {
      try {
        await $pb.collection('users').authRefresh()
        return navigateTo('/')
      } catch {
        $pb.authStore.clear()
      }
    }

    if (to.path === '/setup') return

    const needsSetup = await instanceNeedsSetup($pb)
    if (needsSetup) {
      if (to.path !== '/setup') return navigateTo('/setup')
      return
    }

    if (isPublicPath) return
    return navigateTo('/login')
  }

  // Authenticated user — keep them out of auth-only pages
  if (isPublicPath) {
    return navigateTo('/')
  }

  // Verify the session is still valid
  try {
    await $pb.collection('users').authRefresh()
  } catch (err: any) {
    $pb.authStore.clear()
    return navigateTo('/login')
  }
})
