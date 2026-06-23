import type { SetupStatus } from '~/types/setup'
import { resolveBackendBaseUrl } from '~/composables/useBaseUrl'

const PUBLIC_PATHS = ['/login', '/forgot-password', '/reset-password', '/invite', '/setup']
const SETUP_STATUS_CACHE_MS = 5000
const SETUP_STATUS_TIMEOUT_MS = 3000


let cachedSetupStatus: SetupStatus | null = null
let cachedSetupStatusAt = 0
let inflightSetupStatusCheck: Promise<SetupStatus | null> | null = null

async function fetchInstanceSetupStatus(): Promise<SetupStatus | null> {
  try {
    const config = useRuntimeConfig()
    const baseURL = resolveBackendBaseUrl(config.public.pocketbaseUrl as string)
    const data = await $fetch<Partial<SetupStatus>>(`${baseURL}/api/custom/setup/status`, {
      method: 'GET',
      headers: { 'X-Wireops-Origin': 'ui' },
      timeout: SETUP_STATUS_TIMEOUT_MS,
    })
    return {
      needsSetup: data?.needsSetup === true,
      setupAllowed: data?.setupAllowed === true,
      reason: data?.reason || '',
      requiresBootstrapToken: data?.requiresBootstrapToken === true,
    }
  } catch {
    return null
  }
}

async function instanceSetupStatus(): Promise<SetupStatus | null> {
  const now = Date.now()
  if (cachedSetupStatusAt > 0 && now - cachedSetupStatusAt < SETUP_STATUS_CACHE_MS) {
    return cachedSetupStatus
  }

  if (inflightSetupStatusCheck !== null) {
    return inflightSetupStatusCheck
  }

  inflightSetupStatusCheck = fetchInstanceSetupStatus().then((result) => {
    cachedSetupStatus = result
    if (result !== null) {
      cachedSetupStatusAt = Date.now()
    }
    return result
  }).finally(() => {
    inflightSetupStatusCheck = null
  })

  return inflightSetupStatusCheck
}

export default defineNuxtRouteMiddleware(async (to, from) => {
  const { $pb } = useNuxtApp()
  const isPublicPath = PUBLIC_PATHS.includes(to.path)
  const status = !$pb.authStore.isValid ? await instanceSetupStatus() : null

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
