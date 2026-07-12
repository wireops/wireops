<script setup lang="ts">
import { computed, onMounted } from 'vue'

const props = defineProps<{
  syncLogId: string
}>()

const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()

// Canonical, fixed phase order — mirrors internal/constants.DeployPhaseOrder
// (Go) so the timeline always renders the same 8 steps regardless of which
// deploy flow produced them or the order rows were actually written in.
const PHASE_ORDER = [
  'git_fetch', 'render', 'policy_check', 'dispatch',
  'worker_ack', 'compose_up', 'post_check', 'notify',
] as const

const PHASE_LABELS: Record<string, string> = {
  git_fetch: 'Git Fetch',
  render: 'Render',
  policy_check: 'Policy Check',
  dispatch: 'Dispatch',
  worker_ack: 'Worker Received',
  compose_up: 'Compose Up',
  post_check: 'Post-Check',
  notify: 'Notify',
}

const { data: phasesResult, error: phasesError, refresh: refreshPhases } = useAsyncData(
  `sync_log_phases_${props.syncLogId}`,
  () => $pb.collection('sync_log_phases').getList(1, 20, {
    filter: `sync_log = "${props.syncLogId}"`,
    sort: 'seq',
    // PocketBase's default request key is method+path only (no query
    // string), so every DeployTimeline instance's getList() collides on the
    // same key and auto-cancels the others when several mount at once (e.g.
    // every log's timeline open by default on page load) — only the last
    // one to fire would ever resolve. Give each log its own key.
    requestKey: `sync_log_phases_${props.syncLogId}`,
  }),
  { watch: [() => props.syncLogId] }
)

const phasesByName = computed(() => {
  const map: Record<string, any> = {}
  for (const phase of phasesResult.value?.items || []) {
    map[phase.phase] = phase
  }
  return map
})

const hasPhases = computed(() => (phasesResult.value?.items?.length || 0) > 0)
const loaded = computed(() => phasesResult.value !== undefined && phasesResult.value !== null)

onMounted(() => {
  subscribe('sync_log_phases', () => {
    refreshPhases()
  }, `sync_log = "${props.syncLogId}"`)
})

function statusIcon(status: string | undefined) {
  switch (status) {
    case 'success': return 'i-lucide-check-circle-2'
    case 'error': return 'i-lucide-x-circle'
    case 'running': return 'i-lucide-loader-circle'
    case 'skipped': return 'i-lucide-minus-circle'
    default: return 'i-lucide-circle-dashed'
  }
}

function statusColor(status: string | undefined) {
  switch (status) {
    case 'success': return 'text-green-500'
    case 'error': return 'text-red-500'
    case 'running': return 'text-blue-500 animate-spin'
    case 'skipped': return 'text-gray-400'
    default: return 'text-gray-300 dark:text-gray-700'
  }
}

function formatDuration(ms: number | undefined) {
  const value = ms || 0
  if (value < 1000) return `${value}ms`
  return `${(value / 1000).toFixed(1)}s`
}
</script>

<template>
  <table v-if="hasPhases" class="w-full text-xs border-collapse block sm:table">
    <tbody class="block sm:table-row-group">
      <tr
        v-for="phaseName in PHASE_ORDER"
        :key="phaseName"
        class="flex flex-wrap items-center sm:table-row border-b border-gray-100 dark:border-gray-800 last:border-0 py-1.5 sm:py-0"
      >
        <td class="flex sm:table-cell sm:py-1 sm:pr-3 sm:w-px sm:whitespace-nowrap sm:align-top">
          <div class="flex items-center gap-1.5">
            <UIcon
              :name="statusIcon(phasesByName[phaseName]?.status)"
              :class="['w-3.5 h-3.5 shrink-0', statusColor(phasesByName[phaseName]?.status)]"
            />
            <span class="text-gray-600 dark:text-gray-400">{{ PHASE_LABELS[phaseName] }}</span>
          </div>
        </td>
        <td class="flex sm:table-cell sm:py-1 sm:pr-3 sm:w-px sm:whitespace-nowrap sm:align-top">
          <span
            class="text-gray-400/50 dark:text-gray-500/50 pl-2 sm:pl-0"
          >{{ formatDuration(phasesByName[phaseName]?.duration_ms) }}</span>
        </td>
        <td class="w-full sm:table-cell sm:w-auto sm:py-1 sm:align-top">
          <div class="min-w-0 pl-5 sm:pl-0">
            <span
              v-if="phasesByName[phaseName]?.status === 'error' && phasesByName[phaseName]?.detail"
              class="text-red-500 break-words sm:truncate"
              :title="phasesByName[phaseName]?.detail"
            >{{ phasesByName[phaseName]?.detail }}</span>
            <span
              v-else-if="phasesByName[phaseName]?.detail"
              class="text-gray-400 break-words sm:truncate"
              :title="phasesByName[phaseName]?.detail"
            >{{ phasesByName[phaseName]?.detail }}</span>
          </div>
        </td>
      </tr>
    </tbody>
  </table>
  <p v-else-if="phasesError" class="text-xs text-red-500">Failed to load deploy timeline.</p>
  <p v-else-if="loaded" class="text-xs text-gray-400">No timeline data for this deploy.</p>
</template>
