<script setup lang="ts">
import { computed, onMounted } from 'vue'

const props = defineProps<{
  syncLogId: string
}>()

const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()

// Canonical, fixed phase order — mirrors internal/constants.DeployPhaseOrder
// (Go) so the timeline always renders the same 10 steps regardless of which
// deploy flow produced them or the order rows were actually written in.
const PHASE_ORDER = [
  'git_fetch', 'lint', 'render', 'secrets_fetch', 'policy_check', 'dispatch',
  'worker_ack', 'compose_up', 'post_check', 'notify',
] as const

const PHASE_LABELS: Record<string, string> = {
  git_fetch: 'Git Fetch',
  lint: 'Lint',
  render: 'Render',
  secrets_fetch: 'Fetch Secrets',
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

// Once a phase has errored, the pipeline aborted there — every later phase
// never ran, so listing them one by one ("0ms", no detail) is just noise.
// Collapse everything after the failing phase into a single placeholder row.
const errorPhaseIndex = computed(() => PHASE_ORDER.findIndex(name => phasesByName.value[name]?.status === 'error'))
const visiblePhases = computed(() => (
  errorPhaseIndex.value === -1 ? PHASE_ORDER : PHASE_ORDER.slice(0, errorPhaseIndex.value + 1)
))
const remainingSkippedCount = computed(() => (
  errorPhaseIndex.value === -1 ? 0 : PHASE_ORDER.length - visiblePhases.value.length
))

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
  <table v-if="hasPhases" class="w-full text-xs border-collapse block sm:table sm:table-fixed">
    <colgroup>
      <col class="w-36">
      <col>
    </colgroup>
    <tbody class="block sm:table-row-group">
      <tr
        v-for="phaseName in visiblePhases"
        :key="phaseName"
        class="flex flex-wrap items-center sm:table-row border-b border-gray-100 dark:border-gray-800 last:border-0 py-1.5 sm:py-0"
      >
        <td class="flex sm:table-cell sm:py-1 sm:pr-3 sm:whitespace-nowrap sm:align-top">
          <div class="flex items-center gap-1.5">
            <UIcon
              :name="statusIcon(phasesByName[phaseName]?.status)"
              :class="['w-3.5 h-3.5 shrink-0', statusColor(phasesByName[phaseName]?.status)]"
            />
            <span class="text-gray-600 dark:text-gray-400">{{ PHASE_LABELS[phaseName] }}</span>
          </div>
        </td>
        <td class="flex sm:table-cell sm:py-1 sm:align-top">
          <span
            class="text-gray-400/50 dark:text-gray-500/50 pl-2 sm:pl-0"
          >{{ formatDuration(phasesByName[phaseName]?.duration_ms) }}</span>
        </td>
      </tr>
      <tr
        v-if="remainingSkippedCount > 0"
        class="flex flex-wrap items-center sm:table-row border-b border-gray-100 dark:border-gray-800 last:border-0 py-1.5 sm:py-0"
      >
        <td class="flex sm:table-cell sm:py-1 sm:pr-3 sm:whitespace-nowrap sm:align-top" colspan="2">
          <div class="flex items-center gap-1.5">
            <UIcon name="i-lucide-minus-circle" class="w-3.5 h-3.5 shrink-0 text-gray-400" />
            <span class="text-gray-400">Skipped ({{ remainingSkippedCount }})</span>
          </div>
        </td>
      </tr>
    </tbody>
  </table>
  <p v-else-if="phasesError" class="text-xs text-red-500">Failed to load deploy timeline.</p>
  <p v-else-if="loaded" class="text-xs text-gray-400">No timeline data for this deploy.</p>
</template>
