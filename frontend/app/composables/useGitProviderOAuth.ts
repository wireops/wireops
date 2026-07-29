export function useGitProviderOAuth() {
  const { getGitProviderAuthorizeUrl } = useApi()

  async function connect(slug: string): Promise<{ keyId: string, login: string } | null> {
    const { url } = await getGitProviderAuthorizeUrl(slug)
    const popup = window.open(url, 'wireops-oauth', 'width=600,height=700')
    if (!popup) return null

    return new Promise((resolve) => {
      const closeCheck = window.setInterval(() => {
        if (popup.closed) {
          window.clearInterval(closeCheck)
          window.removeEventListener('message', onMessage)
          resolve(null)
        }
      }, 500)
      function onMessage(ev: MessageEvent) {
        // The callback page is served by the backend, which may be a
        // different origin than this app in dev (e.g. :8090 vs :3000), so
        // it posts with targetOrigin '*'. Verify authenticity by exact
        // window identity instead of origin string matching.
        if (ev.source !== popup) return
        if (ev.data?.type !== 'wireops-git-oauth') return
        window.clearInterval(closeCheck)
        window.removeEventListener('message', onMessage)
        popup.close()
        resolve(ev.data.ok ? { keyId: ev.data.keyId, login: ev.data.login } : null)
      }
      window.addEventListener('message', onMessage)
    })
  }

  return { connect }
}
