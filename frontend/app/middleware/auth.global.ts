const PUBLIC_PATHS = ['/login', '/forgot-password', '/reset-password', '/invite', '/setup']
const SETUP_STATUS_CACHE_MS = 5000
const SETUP_STATUS_TIMEOUT_MS = 3000

let cachedNeedsSetup: boolean | null = null
let cachedNeedsSetupAt = 0
let inflightNeedsSetupCheck: Promise<boolean | null> | null = null

async function fetchInstanceNeedsSetup(): Promise<boolean | null> {
  try {
    const config = useRuntimeConfig()
    const baseURL = (config.public.pocketbaseUrl as string).replace(/\/$/, '')
    const data = await $fetch<{ needsSetup?: boolean }>(`${baseURL}/api/custom/setup/status`, {
      method: 'GET',
      headers: { 'X-Wireops-Origin': 'ui' },
      timeout: SETUP_STATUS_TIMEOUT_MS,
    })
    return data?.needsSetup === true
  } catch {
    return null
  }
}

async function instanceNeedsSetup(): Promise<boolean | null> {
  const now = Date.now()
  if (cachedNeedsSetupAt > 0 && now-cachedNeedsSetupAt < SETUP_STATUS_CACHE_MS) {
    return cachedNeedsSetup
  }

  if (inflightNeedsSetupCheck !== null) {
    return inflightNeedsSetupCheck
  }

  inflightNeedsSetupCheck = fetchInstanceNeedsSetup().then((result) => {
    cachedNeedsSetup = result
    if (result !== null) {
      cachedNeedsSetupAt = Date.now()
    }
    return result
  }).finally(() => {
    inflightNeedsSetupCheck = null
  })

  return inflightNeedsSetupCheck
}

export default defineNuxtRouteMiddleware(async (to, from) => {
  const { $pb } = useNuxtApp()
  const isPublicPath = PUBLIC_PATHS.includes(to.path)

  if (to.path === '/setup') {
    const needsSetup = await instanceNeedsSetup()
    if (needsSetup === false) {
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

    const needsSetup = await instanceNeedsSetup()
    if (needsSetup === true) {
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
