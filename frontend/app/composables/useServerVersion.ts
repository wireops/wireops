import { computed } from 'vue'

// Cached across every caller in the app - the server version doesn't change
// without a restart, so there's no need to refetch it per worker card.
export function useServerVersion() {
  const { canManageSettings } = usePermissions()
  const { getSystemInfo } = useApi()

  const { data } = useAsyncData('server_version', async () => {
    if (!canManageSettings.value) return null
    try {
      const info = await getSystemInfo()
      return info.version || null
    } catch {
      return null
    }
  })

  return { serverVersion: computed(() => data.value) }
}
