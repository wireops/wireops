import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import * as vue from 'vue'
import { ref } from 'vue'
import JobPage from '../[id].vue'

function setupGlobals(job: Record<string, any>) {
  // Nuxt auto-imports Vue's reactivity API globally; vitest doesn't, so the
  // page's <script setup> (which relies on that auto-import) needs it here.
  for (const key of ['ref', 'computed', 'watch', 'watchEffect', 'onMounted', 'onUnmounted', 'onBeforeUnmount', 'nextTick', 'reactive']) {
    (globalThis as any)[key] = (vue as any)[key]
  }

  ;(globalThis as any).useRoute = () => ({ params: { id: job.id } })
  ;(globalThis as any).useRouter = () => ({ push: vi.fn(), replace: vi.fn() })
  const navigateTo = vi.fn().mockResolvedValue(undefined)
  ;(globalThis as any).navigateTo = navigateTo

  const deleteJob = vi.fn().mockResolvedValue(undefined)
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: (name: string) => {
        if (name === 'scheduled_jobs') {
          return {
            getOne: vi.fn().mockResolvedValue(job),
            delete: deleteJob,
            update: vi.fn().mockResolvedValue(job),
          }
        }
        return {}
      },
    },
  })

  ;(globalThis as any).useCopy = () => ({ copy: vi.fn() })
  ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  ;(globalThis as any).useApi = () => ({
    triggerJobRun: vi.fn(),
    cancelJobRun: vi.fn(),
    getJobDefinition: vi.fn().mockResolvedValue(null),
    getJobRaw: vi.fn(),
  })

  // Minimal useAsyncData stand-in: fetches immediately like Nuxt's real one.
  ;(globalThis as any).useAsyncData = (_key: string, fn: () => Promise<any>) => {
    const data = ref<any>(null)
    const refresh = async () => { data.value = await fn() }
    refresh()
    return { data, refresh }
  }

  return { navigateTo, deleteJob }
}

describe('jobs/[id].vue delete job flow', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('closes the modal and redirects to /jobs once a completed job is deleted', async () => {
    const job = { id: 'job-1', name: 'nightly-backup', job_file: 'job.yaml', status: 'completed', enabled: true }
    const { navigateTo, deleteJob } = setupGlobals(job)

    const wrapper = mount(JobPage, { shallow: true })
    await flushPromises()

    // Open the delete modal, same as clicking the danger-zone button.
    ;(wrapper.vm as any).showDeleteModal = true
    await flushPromises()

    // JobDeleteModal's own confirmDelete calls scheduled_jobs.delete then
    // emits 'deleted' — that emit is wired to onJobDeleted via
    // @deleted="onJobDeleted" in the template, so invoking the handler
    // directly here exercises the exact same page-level behavior (lines
    // 18-25) without depending on the shallow-stubbed child's internals.
    await deleteJob(job.id)
    await (wrapper.vm as any).onJobDeleted()

    expect((wrapper.vm as any).showDeleteModal).toBe(false)
    expect(navigateTo).toHaveBeenCalledWith('/jobs', { replace: true })
  })
})
