import { getInstanceSetupStatus } from '~/composables/useSetupStatus'

const PUBLIC_PATHS = ['/login', '/forgot-password', '/reset-password', '/invite', '/setup']

export default defineNuxtRouteMiddleware(async (to, from) => {
  const { $pb } = useNuxtApp()
  const isPublicPath = PUBLIC_PATHS.includes(to.path)
  const status = !$pb.authStore.isValid ? await getInstanceSetupStatus() : null

  if (to.path === '/setup') {
    if (status?.needsSetup === false) {
      return navigateTo($pb.authStore.isValid ? '/' : '/login', { replace: true })
    }
  }

  // Unauthenticated user — decide between /setup and /login
  if (!$pb.authStore.isValid) {
    if (isPublicPath && $pb.authStore.token && $pb.authStore.record) {
      try {
        await $pb.collection('users').authRefresh()
        return navigateTo('/', { replace: true })
      } catch (err: any) {
        if (err?.isAbort || err?.status === 0) {
          return
        }
        $pb.authStore.clear()
      }
    }

    if (to.path === '/setup') return

    if (status?.needsSetup === true) {
      return navigateTo('/setup', { replace: true })
    }

    if (isPublicPath) return
    return navigateTo('/login', { replace: true })
  }

  // Authenticated user — keep them out of auth-only pages
  if (isPublicPath) {
    return navigateTo('/', { replace: true })
  }

  // Verify the session is still valid
  // Only refresh on actual path changes, ignoring query/hash parameter updates
  if (to.path !== from?.path) {
    try {
      await $pb.collection('users').authRefresh()
    } catch (err: any) {
      if (err?.isAbort || err?.status === 0) {
        return
      }
      $pb.authStore.clear()
      return navigateTo('/login', { replace: true })
    }
  }
})
