<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import JobsDeleteJobRunModal from './DeleteJobRunModal.vue'

const props = defineProps<{
  jobId: string
}>()

const { $pb } = useNuxtApp()
const { cancelJobRun } = useApi()
const toast = useToast()

const page = ref(1)
const perPage = ref(10)

watch(perPage, () => {
  page.value = 1
})
const { data: runsData, refresh: refreshRuns, status } = useAsyncData(() => `job_runs_${props.jobId}_page_${page.value}_${perPage.value}`, () => {
  return $pb.collection('job_runs').getList(page.value, perPage.value, {
    filter: $pb.filter('job = {:id}', { id: props.jobId }),
    sort: '-created',
    expand: 'agent',
  })
}, { watch: [page, perPage] })

const runs = computed(() => runsData.value?.items || [])
const totalItems = computed(() => runsData.value?.totalItems || 0)

const cancellingRunId = ref<string | null>(null)
const expandedRun = ref<string | null>(null)

async function cancelRun(run: any) {
  if (run.status !== 'running') return
  cancellingRunId.value = run.id
  try {
    await cancelJobRun(run.id)
    toast.add({ title: 'Job cancelled', description: 'Container stopped.', color: 'success' })
    setTimeout(() => refreshRuns(), 500)
  } catch (e: any) {
    toast.add({ title: 'Failed to cancel', description: e?.message, color: 'error' })
  } finally {
    cancellingRunId.value = null
  }
}

const isDeleteModalOpen = ref(false)
const runToDelete = ref<any>(null)

function openDeleteModal(run: any) {
  if (run.status !== 'stalled') return
  runToDelete.value = run
  isDeleteModalOpen.value = true
}

function runStatusColor(status: string) {
  switch (status) {
    case 'success': return 'success'
    case 'error': return 'error'
    case 'running': return 'primary'
    case 'stalled': return 'warning'
    default: return 'neutral'
  }
}

function runStatusIcon(status: string) {
  switch (status) {
    case 'success': return 'i-lucide-check-circle'
    case 'error': return 'i-lucide-x-circle'
    case 'running': return 'i-lucide-loader'
    case 'stalled': return 'i-lucide-pause-circle'
    default: return 'i-lucide-clock'
  }
}

function formatRelative(d: string) {
  if (!d) return '—'
  const diff = Date.now() - new Date(d).getTime()
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return `${Math.floor(diff / 86_400_000)}d ago`
}

defineExpose({
  refreshRuns
})
</script>

<template>
  <div class="space-y-3">
    <div v-if="status === 'pending' && runs.length === 0" class="text-center py-10 text-gray-500 text-sm">
      <UIcon name="i-lucide-loader-2" class="w-6 h-6 animate-spin mx-auto mb-2" />
      <p>Loading runs...</p>
    </div>
    <div v-else-if="runs.length === 0" class="text-center py-10 text-gray-500 dark:text-wire-200/50 text-sm">
      No runs yet. Trigger a manual run or wait for the cron schedule.
    </div>
    <div v-else class="space-y-2">
      <div
        v-for="run in runs"
        :key="run.id"
        class="rounded-xl border border-gray-200 dark:border-carbon-700 bg-gray-50 dark:bg-carbon-800/40 overflow-hidden"
      >
        <div
          class="flex items-center justify-between px-4 py-3 cursor-pointer select-none"
          @click="expandedRun = expandedRun === run.id ? null : run.id"
        >
          <div class="flex items-center gap-3">
            <UIcon
              :name="runStatusIcon(run.status)" class="w-4 h-4" :class="{
              'text-green-500': run.status === 'success',
              'text-red-500': run.status === 'error',
              'text-yellow-400 animate-spin': run.status === 'running',
              'text-amber-400': run.status === 'stalled',
              'text-gray-400': run.status === 'pending',
            }" />
            <div>
              <div class="flex items-center gap-2">
                <UBadge :label="run.status" :color="runStatusColor(run.status)" variant="subtle" size="xs" />
                <UBadge :label="run.trigger" variant="subtle" color="neutral" size="xs" />
                <span v-if="run.expand?.agent?.hostname" class="text-xs text-gray-400 dark:text-wire-200/40 font-mono">
                  {{ run.expand.agent.hostname }}
                </span>
              </div>
              <p class="text-xs text-gray-400 dark:text-wire-200/40 mt-0.5">{{ formatRelative(run.created) }}</p>
            </div>
          </div>
          <div class="flex items-center gap-2">
            <UTooltip v-if="run.status === 'running'" text="Cancel job run">
              <UButton
                icon="i-lucide-trash-2"
                variant="ghost"
                size="xs"
                color="error"
                :loading="cancellingRunId === run.id"
                @click.stop="cancelRun(run)"
              />
            </UTooltip>
            <UTooltip v-if="run.status === 'stalled'" text="Remove stalled run">
              <UButton
                icon="i-lucide-trash-2"
                variant="ghost"
                size="xs"
                color="error"
                @click.prevent.stop="openDeleteModal(run)"
              />
            </UTooltip>
            <span v-if="run.duration_ms" class="text-xs text-gray-400 dark:text-wire-200/40 font-mono">
              {{ run.duration_ms }}ms
            </span>
            <UIcon
              :name="expandedRun === run.id ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
              class="w-4 h-4 text-gray-400"
            />
          </div>
        </div>
        <div v-if="expandedRun === run.id" class="border-t border-gray-200 dark:border-carbon-700">
          <div class="flex flex-wrap gap-x-6 gap-y-1 px-4 py-2 bg-gray-50/60 dark:bg-carbon-800/60 text-xs font-mono">
            <span v-if="run.container_name" class="flex items-center gap-1.5 text-gray-500 dark:text-wire-200/50">
              <UIcon name="i-lucide-box" class="w-3.5 h-3.5 shrink-0" />
              <span class="text-gray-700 dark:text-wire-200 select-all">{{ run.container_name }}</span>
            </span>
            <span v-if="run.commit_sha" class="flex items-center gap-1.5 text-gray-500 dark:text-wire-200/50">
              <UIcon name="i-lucide-git-commit-horizontal" class="w-3.5 h-3.5 shrink-0" />
              <span class="text-gray-700 dark:text-wire-200 select-all">{{ run.commit_sha.slice(0, 12) }}</span>
            </span>
          </div>
          <div v-if="run.output" class="p-3">
            <pre class="text-xs font-mono text-gray-800 dark:text-wire-200 bg-gray-100 dark:bg-carbon-950 rounded-lg px-4 py-3 whitespace-pre-wrap break-words max-h-64 overflow-y-auto">{{ run.output }}</pre>
          </div>
          <div v-else class="px-4 py-2">
            <p class="text-xs text-gray-400 dark:text-wire-200/40 italic">No output recorded.</p>
          </div>
        </div>
      </div>
      
      <div v-if="totalItems > perPage" class="flex justify-between items-center mt-4">
        <UPagination
          v-model="page"
          :total="totalItems"
          :page-count="perPage"
        />
        <div class="flex items-center gap-2">
          <span class="text-xs text-gray-500">Per page</span>
          <USelect
            v-model.number="perPage"
            :options="[10, 20, 50]"
            size="xs"
          />
        </div>
      </div>
    </div>

    <!-- Delete Modal -->
    <UModal v-model:open="isDeleteModalOpen">
      <template #content>
        <JobsDeleteJobRunModal
          v-if="runToDelete"
          :run="runToDelete"
          @cancel="isDeleteModalOpen = false"
          @deleted="isDeleteModalOpen = false; refreshRuns()"
        />
      </template>
    </UModal>
  </div>
</template>
