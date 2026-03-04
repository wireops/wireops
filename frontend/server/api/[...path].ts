import { proxyRequest } from 'h3'

export default defineEventHandler((event) => {
  const path = event.context.params?.path
  const pathStr = Array.isArray(path) ? path.join('/') : (path ?? '')
  const base = (useRuntimeConfig().public.pocketbaseUrl as string).replace(/\/$/, '')
  return proxyRequest(event, `${base}/api/${pathStr}`)
})
