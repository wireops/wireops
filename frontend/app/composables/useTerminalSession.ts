import { ref } from 'vue'

/**
 * Drives one interactive terminal session: opens it via REST, tails its
 * output over SSE, and forwards keystrokes/resizes back over REST.
 *
 * Uses fetch + a manual SSE line reader instead of EventSource for the same
 * reason as useDeployStream: EventSource cannot send the Authorization
 * header PocketBase auth requires.
 */
export function useTerminalSession() {
  const { $pb } = useNuxtApp()

  const sessionId = ref<string | null>(null)
  const connected = ref(false)
  const error = ref<string | null>(null)
  const closed = ref(false)

  let abortController: AbortController | null = null
  let openAbortController: AbortController | null = null
  let generation = 0
  let onOutputCb: ((chunk: Uint8Array) => void) | null = null
  let onClosedCb: ((exitCode: number, reason?: string) => void) | null = null

  function authHeaders(): Record<string, string> {
    const token = $pb.authStore.token
    return {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      'X-Wireops-Origin': 'ui',
    }
  }

  function onOutput(cb: (chunk: Uint8Array) => void) {
    onOutputCb = cb
  }

  function onClosed(cb: (exitCode: number, reason?: string) => void) {
    onClosedCb = cb
  }

  function base64ToBytes(b64: string): Uint8Array {
    const bin = atob(b64)
    const bytes = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
    return bytes
  }

  async function tail(id: string, myGeneration: number) {
    const controller = new AbortController()
    abortController = controller

    let reader: ReadableStreamDefaultReader<Uint8Array> | null = null
    try {
      const resp = await fetch(`${$pb.baseURL}/api/custom/terminal/sessions/${id}/stream`, {
        headers: authHeaders(),
        signal: controller.signal,
      })
      if (myGeneration !== generation) return
      if (!resp.ok || !resp.body) {
        error.value = `terminal stream failed: ${resp.status}`
        return
      }
      connected.value = true

      reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (myGeneration !== generation) return
        if (done) break
        buffer += decoder.decode(value, { stream: true })

        const events = buffer.split('\n\n')
        buffer = events.pop() || ''

        for (const event of events) {
          let eventType = 'message'
          let data = ''
          for (const rawLine of event.split('\n')) {
            if (rawLine.startsWith('event: ')) eventType = rawLine.slice('event: '.length)
            else if (rawLine.startsWith('data: ')) data = rawLine.slice('data: '.length)
          }
          if (eventType === 'closed') {
            closed.value = true
            let exitCode = -1
            let reason = ''
            try {
              const parsed = JSON.parse(data)
              exitCode = parsed?.exit_code ?? -1
              reason = parsed?.error || ''
            } catch {
              // ignore malformed payload, still treat as closed
            }
            // Surface a failed exec (e.g. a shell that doesn't exist in the
            // image) as a visible error, not just a bare exit code — the
            // session can close before the terminal ever produced output,
            // so this is the only signal the operator gets.
            if (reason) error.value = reason
            onClosedCb?.(exitCode, reason)
          } else if (data) {
            onOutputCb?.(base64ToBytes(data))
          }
        }
      }
    } catch (err: any) {
      if (myGeneration !== generation) return
      if (err?.name !== 'AbortError') {
        error.value = err?.message || 'terminal stream connection error'
      }
    } finally {
      reader?.cancel().catch(() => {})
      if (myGeneration === generation) {
        connected.value = false
      }
    }
  }

  async function open(stackId: string, containerId: string, rows: number, cols: number, shell?: string[]) {
    error.value = null
    closed.value = false
    generation++
    const myGeneration = generation
    const controller = new AbortController()
    openAbortController = controller

    let res: Response
    try {
      res = await fetch(`${$pb.baseURL}/api/custom/stacks/${stackId}/terminal/sessions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ container_id: containerId, rows, cols, shell }),
        signal: controller.signal,
      })
    } catch (err: any) {
      // Aborted by close()/resetToSelect() before the request completed —
      // the server never created a session, nothing to clean up.
      if (err?.name === 'AbortError') return
      throw err
    }
    const data = await res.json()

    // close()/resetToSelect() ran while the open POST was in flight — the
    // server already created a session, but nothing here still owns it, so
    // close it out from under the abandoned call instead of leaking it.
    if (myGeneration !== generation) {
      if (data?.session_id) {
        fetch(`${$pb.baseURL}/api/custom/terminal/sessions/${data.session_id}/close`, {
          method: 'POST',
          headers: authHeaders(),
        }).catch(() => {})
      }
      return
    }

    if (!res.ok || data?.error) {
      error.value = data?.error || `failed to open terminal: ${res.status}`
      throw new Error(error.value ?? undefined)
    }
    sessionId.value = data.session_id
    tail(data.session_id, generation)
  }

  async function sendInput(text: string) {
    if (!sessionId.value) return
    await fetch(`${$pb.baseURL}/api/custom/terminal/sessions/${sessionId.value}/input`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
      body: JSON.stringify({ data: text }),
    })
  }

  async function resize(rows: number, cols: number) {
    if (!sessionId.value) return
    await fetch(`${$pb.baseURL}/api/custom/terminal/sessions/${sessionId.value}/resize`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
      body: JSON.stringify({ rows, cols }),
    })
  }

  async function close() {
    generation++
    openAbortController?.abort()
    openAbortController = null
    abortController?.abort()
    abortController = null
    connected.value = false
    const id = sessionId.value
    sessionId.value = null
    if (!id) return
    await fetch(`${$pb.baseURL}/api/custom/terminal/sessions/${id}/close`, {
      method: 'POST',
      headers: authHeaders(),
    }).catch(() => {})
  }

  return { sessionId, connected, error, closed, open, sendInput, resize, close, onOutput, onClosed }
}
