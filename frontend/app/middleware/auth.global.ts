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

  // Unauthenticated user — decide between /setup and /login
  if (!$pb.authStore.isValid) {
    if (to.path === '/setup') return

    const needsSetup = await instanceNeedsSetup($pb)
    if (needsSetup) {
      if (to.path !== '/setup') return navigateTo('/setup')
      return
    }

    if (PUBLIC_PATHS.includes(to.path)) return
    return navigateTo('/login')
  }

  // Authenticated user — keep them out of auth-only pages
  if (PUBLIC_PATHS.includes(to.path)) {
    return navigateTo('/')
  }

  // Verify the session is still valid
  try {
    await $pb.collection('_superusers').authRefresh()
  } catch {
    $pb.authStore.clear()
    return navigateTo('/login')
  }
})
