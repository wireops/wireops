/** Fetches a terminal session's asciicast-v2-style replay transcript. */
export function useTerminalHistory() {
  const { $pb } = useNuxtApp()

  function authHeaders(): Record<string, string> {
    const token = $pb.authStore.token
    return {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      'X-Wireops-Origin': 'ui',
    }
  }

  async function transcript(sessionId: string): Promise<string> {
    const res = await fetch(`${$pb.baseURL}/api/custom/terminal/sessions/${encodeURIComponent(sessionId)}/transcript`, {
      headers: authHeaders(),
    })
    if (!res.ok) throw new Error(`failed to load transcript: ${res.status}`)
    return res.text()
  }

  return { transcript }
}
