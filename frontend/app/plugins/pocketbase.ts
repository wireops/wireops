import PocketBase, { BaseAuthStore, LocalAuthStore } from 'pocketbase'
import { resolveBackendBaseUrl } from '~/composables/useBaseUrl'

export default defineNuxtPlugin(() => {
  const config = useRuntimeConfig()
  const baseUrl = resolveBackendBaseUrl(config.public.pocketbaseUrl as string)
  const pb = new PocketBase(baseUrl, new LocalAuthStore('wireops_auth'))

  // Secondary client for a real PocketBase "_superusers" session, obtained
  // silently at login when the account's credentials also match a superuser
  // record (e.g. the bootstrap admin - see setupsvc.CreateInitialAdmin).
  // Kept separate from `pb` so app-level RBAC/ownership (keyed to the
  // "users" collection id) is never affected by superuser status.
  // Deliberately in-memory only (BaseAuthStore, no localStorage) - this is a
  // privileged token, so it must not persist across reloads/tabs or be
  // readable from storage by an XSS payload; it's re-derived on every login.
  const pbSuperuser = new PocketBase(baseUrl, new BaseAuthStore())

  const attachOrigin = async (url: string, options: RequestInit) => {
    const headers = Object.fromEntries(new Headers(options.headers || {}).entries())
    headers['X-Wireops-Origin'] = 'ui'

    return {
      url,
      options: {
        ...options,
        headers,
      },
    }
  }

  pb.beforeSend = attachOrigin
  pbSuperuser.beforeSend = attachOrigin

  return {
    provide: {
      pb,
      pbSuperuser,
    },
  }
})
