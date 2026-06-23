import type { SetupStatus } from '../types/setup'
import { resolveBackendBaseUrl } from './useBaseUrl'

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

export function invalidateInstanceSetupStatus() {
  cachedSetupStatus = null
  cachedSetupStatusAt = 0
  inflightSetupStatusCheck = null
}

export async function getInstanceSetupStatus(): Promise<SetupStatus | null> {
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
