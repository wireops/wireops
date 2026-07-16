import { describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { useDeployStream } from './useDeployStream'

function sseStreamFrom(chunks: string[]): Response {
  const encoder = new TextEncoder()
  let i = 0
  const stream = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (i < chunks.length) {
        controller.enqueue(encoder.encode(chunks[i]))
        i++
      } else {
        controller.close()
      }
    },
  })
  return new Response(stream, { status: 200 })
}

// Like sseStreamFrom, but the stream stays open after emitting its chunks —
// mirrors a deploy that's still in progress (connection stays "connected"),
// as opposed to sseStreamFrom's stream, which closes right after the last chunk.
function openSseStreamFrom(chunks: string[]): Response {
  const encoder = new TextEncoder()
  let i = 0
  const stream = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (i < chunks.length) {
        controller.enqueue(encoder.encode(chunks[i]))
        i++
      }
      // No controller.close(): simulates an in-progress deploy stream.
    },
  })
  return new Response(stream, { status: 200 })
}

function stubNuxtApp(token = 'test-token') {
  const globals = globalThis as unknown as { useNuxtApp: () => unknown }
  globals.useNuxtApp = () => ({
    $pb: {
      baseURL: 'http://api.test',
      authStore: { token },
    },
  })
}

describe('useDeployStream', () => {
  it('only surfaces lines tagged with the live-phase prefix', async () => {
    stubNuxtApp()
    globalThis.fetch = vi.fn().mockResolvedValue(
      sseStreamFrom([
        'data: some old persisted line\n\n',
        'data: [compose_up] Pulling image\n\n',
        'data: [teardown] Stopping container\n\n',
      ])
    )

    const scope = effectScope()
    let result!: ReturnType<typeof useDeployStream>
    scope.run(() => {
      result = useDeployStream(ref('stack-1'))
    })

    await vi.waitFor(() => {
      expect(result.lines.value).toEqual(['Pulling image', 'Stopping container'])
    })

    scope.stop()
  })

  it('sends the PocketBase auth token as Authorization header', async () => {
    stubNuxtApp('secret-token')
    const fetchMock = vi.fn().mockResolvedValue(sseStreamFrom([]))
    globalThis.fetch = fetchMock

    const scope = effectScope()
    scope.run(() => {
      useDeployStream(ref('stack-1'))
    })

    await vi.waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('http://api.test/api/custom/stacks/stack-1/stream')
    expect((init.headers as Record<string, string>).Authorization).toBe('secret-token')

    scope.stop()
  })

  it('does not let a stale, slow-to-abort connection clobber a newer one\'s state', async () => {
    stubNuxtApp()

    let rejectFirst!: (err: any) => void
    const fetchMock = vi.fn()
      // First connection (stack-A): fetch never settles until we manually
      // reject it later, simulating a real AbortError that arrives after
      // the caller has already moved on to a different stack.
      .mockImplementationOnce(() => new Promise((_, reject) => { rejectFirst = reject }))
      // Second connection (stack-B): resolves immediately with one line.
      .mockImplementationOnce(() => Promise.resolve(openSseStreamFrom(['data: [compose_up] hello\n\n'])))
    globalThis.fetch = fetchMock

    const id = ref('stack-A')
    const scope = effectScope()
    let result!: ReturnType<typeof useDeployStream>
    scope.run(() => {
      result = useDeployStream(id)
    })

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))

    // Switch to a different stack before stack-A's fetch has settled —
    // this aborts stack-A's controller and bumps the generation guard.
    id.value = 'stack-B'

    await vi.waitFor(() => {
      expect(result.connected.value).toBe(true)
      expect(result.lines.value).toEqual(['hello'])
    })

    // Now the stale stack-A connection's fetch finally rejects (as a real
    // aborted fetch eventually would). Its outdated finally block must not
    // flip `connected` back to false for the current (stack-B) connection.
    rejectFirst(Object.assign(new Error('aborted'), { name: 'AbortError' }))
    await nextTick()
    await nextTick()

    expect(result.connected.value).toBe(true)
    expect(result.lines.value).toEqual(['hello'])

    scope.stop()
  })

  it('does not connect when stackId is null', async () => {
    stubNuxtApp()
    const fetchMock = vi.fn().mockResolvedValue(sseStreamFrom([]))
    globalThis.fetch = fetchMock

    const scope = effectScope()
    scope.run(() => {
      useDeployStream(ref(null))
    })
    await nextTick()

    expect(fetchMock).not.toHaveBeenCalled()
    scope.stop()
  })
})
