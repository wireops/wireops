import PocketBase, { LocalAuthStore } from 'pocketbase'

export default defineNuxtPlugin(() => {
  const config = useRuntimeConfig()
  const pb = new PocketBase(config.public.pocketbaseUrl as string, new LocalAuthStore('wireops_auth'))

  return {
    provide: {
      pb,
    },
  }
})
