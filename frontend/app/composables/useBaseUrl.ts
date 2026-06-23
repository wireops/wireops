type LocationLike = {
  origin?: string
  protocol?: string
  hostname?: string
  port?: string
}

export function resolveBackendBaseUrlFromLocation(configuredUrl?: string | null, location?: LocationLike | null): string {
  const normalizedConfiguredUrl = (configuredUrl || '').trim().replace(/\/$/, '')

  if (normalizedConfiguredUrl) {
    return normalizedConfiguredUrl
  }

  if (location?.origin) {
    const { protocol, hostname, port, origin } = location

    // In local frontend dev we usually serve Nuxt on :3000 while PocketBase
    // stays on :8090. Falling back to window.origin would point API calls at
    // the Nuxt dev server and break setup/login flows on fresh instances.
    if ((hostname === 'localhost' || hostname === '127.0.0.1') && port === '3000') {
      return `${protocol}//${hostname}:8090`
    }

    return origin.replace(/\/$/, '')
  }

  return 'http://localhost:8090'
}

export function resolveBackendBaseUrl(configuredUrl?: string | null): string {
  const location = import.meta.client && typeof window !== 'undefined' ? window.location : null
  return resolveBackendBaseUrlFromLocation(configuredUrl, location)
}
