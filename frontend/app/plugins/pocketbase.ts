import PocketBase, { LocalAuthStore } from 'pocketbase'
import { resolveBackendBaseUrl } from '~/composables/useBaseUrl'

export default defineNuxtPlugin(() => {
  const config = useRuntimeConfig()
  const pb = new PocketBase(
    resolveBackendBaseUrl(config.public.pocketbaseUrl as string),
    new LocalAuthStore('wireops_auth')
  )

  pb.beforeSend = async (url, options) => {
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

  return {
    provide: {
      pb,
    },
  }
})
