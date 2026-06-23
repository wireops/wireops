export function resolveBackendBaseUrl(configuredUrl?: string | null): string {
  const normalizedConfiguredUrl = (configuredUrl || '').trim().replace(/\/$/, '')

  if (normalizedConfiguredUrl) {
    return normalizedConfiguredUrl
  }

  if (import.meta.client && typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin.replace(/\/$/, '')
  }

  return 'http://localhost:8090'
}
