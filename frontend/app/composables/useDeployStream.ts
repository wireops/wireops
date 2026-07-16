import { ref, watch, onScopeDispose, type Ref } from 'vue'

// Live output lines pushed by the worker while a deploy/redeploy/teardown is
// running are tagged by the server with a "[phase] " prefix (see
// internal/logstream.Broker.PublishLine). Historical, already-persisted
// sync_logs.output replayed on connect never carries this prefix, so we can
// tell "live progress" apart from "old finished log" without the server
// exposing any extra structure over SSE.
const LIVE_LINE_PATTERN = /^\[(compose_up|teardown)\] (.*)$/

const MAX_BUFFERED_LINES = 500

/**
 * Tails GET /api/custom/stacks/{id}/stream and exposes only the live
 * incremental output lines for an in-progress deploy/redeploy/teardown.
 * Historical sync_logs output replayed by the endpoint on connect is
 * discarded — that's already shown elsewhere (the sync log's own output).
 *
 * Uses fetch + a manual SSE line reader instead of EventSource because
 * EventSource cannot send the Authorization header PocketBase auth requires.
 */
export function useDeployStream(stackId: Ref<string | null | undefined>) {
  const { $pb } = useNuxtApp()

  const lines = ref<string[]>([])
  const connected = ref(false)
  const error = ref<string | null>(null)

  let abortController: AbortController | null = null

  // Bumped on every start()/stop() so a stale in-flight connection (already
  // aborted, but whose fetch/read promise hasn't unwound yet) can tell it's
  // no longer current and skip mutating reactive state — otherwise its
  // delayed rejection/finally could clobber a newer connection's state
  // (e.g. flipping `connected` back to false right after a fresh stream set
  // it true) once the old promise finally settles.
  let generation = 0

  function stop() {
    generation++
    abortController?.abort()
    abortController = null
    connected.value = false
  }

  function reset() {
    lines.value = []
    error.value = null
  }

  async function start(id: string) {
    stop()
    reset()
    const myGeneration = generation
    const controller = new AbortController()
    abortController = controller

    const token = $pb.authStore.token
    let reader: ReadableStreamDefaultReader<Uint8Array> | null = null

    try {
      const resp = await fetch(`${$pb.baseURL}/api/custom/stacks/${id}/stream`, {
        headers: token ? { Authorization: token } : {},
        signal: controller.signal,
      })
      if (myGeneration !== generation) return
      if (!resp.ok || !resp.body) {
        error.value = `stream request failed: ${resp.status}`
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
          for (const rawLine of event.split('\n')) {
            if (!rawLine.startsWith('data: ')) continue
            const text = rawLine.slice('data: '.length)
            const match = text.match(LIVE_LINE_PATTERN)
            if (!match) continue
            lines.value.push(match[2] as string)
            if (lines.value.length > MAX_BUFFERED_LINES) {
              lines.value.splice(0, lines.value.length - MAX_BUFFERED_LINES)
            }
          }
        }
      }
    } catch (err: any) {
      if (myGeneration !== generation) return
      if (err?.name !== 'AbortError') {
        error.value = err?.message || 'stream connection error'
      }
    } finally {
      // Always release the reader lock on the way out, even for a stale
      // generation, so an aborted stream's body doesn't stay locked.
      reader?.cancel().catch(() => {})
      if (myGeneration === generation) {
        connected.value = false
      }
    }
  }

  watch(
    stackId,
    (id) => {
      if (id) {
        start(id)
      } else {
        stop()
      }
    },
    { immediate: true }
  )

  onScopeDispose(() => stop())

  return { lines, connected, error }
}
