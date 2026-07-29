export function useGitProviderOAuth() {
  const { getGitProviderAuthorizeUrl } = useApi()

  async function connect(slug: string): Promise<{ keyId: string, login: string } | null> {
    const { url } = await getGitProviderAuthorizeUrl(slug)
    const popup = window.open(url, 'wireops-oauth', 'width=600,height=700')

    return new Promise((resolve) => {
      function onMessage(ev: MessageEvent) {
        // The callback page is served by the backend, which may be a
        // different origin than this app in dev (e.g. :8090 vs :3000), so
        // it posts with targetOrigin '*'. Verify authenticity by exact
        // window identity instead of origin string matching.
        if (ev.source !== popup) return
        if (ev.data?.type !== 'wireops-git-oauth') return
        window.removeEventListener('message', onMessage)
        popup?.close()
        resolve(ev.data.ok ? { keyId: ev.data.keyId, login: ev.data.login } : null)
      }
      window.addEventListener('message', onMessage)
    })
  }

  return { connect }
}
