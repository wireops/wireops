import { computed } from 'vue'

// Cached across every caller in the app - the server version doesn't change
// without a restart, so there's no need to refetch it per worker card.
export function useServerVersion() {
  const { canManageSettings } = usePermissions()
  const { getSystemInfo } = useApi()

  // useAsyncData's cache is keyed and shared across every caller in the app -
  // a plain 'server_version' key would let a session that lost manage-settings
  // permission (role downgrade, different user without a full reload) still
  // read whatever an earlier authorized caller cached. Partition the key by
  // permission so an unauthorized caller never touches the authorized entry.
  const cacheKey = canManageSettings.value ? 'server_version' : 'server_version_unauthorized'

  const { data } = useAsyncData(cacheKey, async () => {
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
