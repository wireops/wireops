import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import { ref } from 'vue'
import WorkersPage from '../index.vue'
import { WORKER_STATUS, TOKEN_STATUS, filterVisibleWorkers, isWorkerClickable, workerStatus } from '../../../utils/worker'

function setupGlobals(createWorkerToken = vi.fn().mockResolvedValue({ token: 'tok-abc123', expires_at: '2030-01-01T00:00:00Z' })) {
  for (const key of ['ref', 'computed', 'watch', 'onMounted', 'onUnmounted', 'nextTick']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  ;(globalThis as any).WORKER_STATUS = WORKER_STATUS
  ;(globalThis as any).TOKEN_STATUS = TOKEN_STATUS
  ;(globalThis as any).filterVisibleWorkers = filterVisibleWorkers
  ;(globalThis as any).isWorkerClickable = isWorkerClickable
  ;(globalThis as any).workerStatus = workerStatus

  ;(globalThis as any).navigateTo = vi.fn()
  ;(globalThis as any).useRoute = () => ({ query: {} })
  ;(globalThis as any).useRouter = () => ({ replace: vi.fn() })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  ;(globalThis as any).usePermissions = () => ({ isViewer: ref(false) })
  ;(globalThis as any).useApi = () => ({
    getWorkers: vi.fn().mockResolvedValue([]),
    createWorkerToken,
  })
  ;(globalThis as any).useAsyncData = (_key: string, fn: () => Promise<any>) => {
    const data = ref<any>(null)
    const pending = ref(false)
    const refresh = async () => { data.value = await fn() }
    refresh()
    return { data, pending, refresh }
  }

  return { createWorkerToken }
}

describe('workers/index.vue worker bootstrap snippets', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('interpolates the freshly issued token and a consistent WORKER_TAGS value across all three snippets', async () => {
    setupGlobals()
    const wrapper = mount(WorkersPage, { shallow: true })
    await flushPromises()

    await (wrapper.vm as any).generateToken()
    await flushPromises()

    const bootstrap: string = (wrapper.vm as any).workerBootstrapCommand
    const envFile: string = (wrapper.vm as any).workerEnvFileExample
    const compose: string = (wrapper.vm as any).workerComposeExample

    expect(bootstrap).toContain('WORKER_TOKEN=tok-abc123')
    expect(bootstrap).toContain('SERVER_URL=')
    expect(envFile).toContain('WORKER_TOKEN=tok-abc123')

    const bootstrapTags = bootstrap.match(/WORKER_TAGS=(\S+)/)?.[1]
    const envTags = envFile.match(/WORKER_TAGS=(\S+)/)?.[1]
    expect(bootstrapTags).toBeTruthy()
    expect(bootstrapTags).toBe(envTags)

    // The compose example delegates env vars to .env via env_file and must
    // not hardcode (and thus risk drifting from) the token or tags.
    expect(compose).not.toContain('tok-abc123')
    expect(compose).toContain('env_file:')
    expect(compose).toContain('- .env')
  })

  it('leaves the token placeholder empty before a token has been issued', () => {
    setupGlobals()
    const wrapper = mount(WorkersPage, { shallow: true })

    expect((wrapper.vm as any).workerBootstrapCommand).toContain('WORKER_TOKEN= \\')
    expect((wrapper.vm as any).workerEnvFileExample).toContain('WORKER_TOKEN=\n')
  })
})
