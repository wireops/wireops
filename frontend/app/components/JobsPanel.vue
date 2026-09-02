<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { AvailabilitySegment } from './StatusAvailabilityBar.vue'
import { GROUP_ALL, GROUP_UNGROUPED, encodeGroupValue, decodeGroupValue } from '../utils/job-filter'
import { usePaginatedList } from '../composables/usePaginatedList'
import { useListDensity } from '../composables/useListDensity'

const { $pb } = useNuxtApp()
const { listJobs, listJobGroups, triggerJobRun, getWorkers } = useApi()
const { subscribe } = useRealtime()
const toast = useToast()
const { isViewer } = usePermissions()
const { isCompact, toggleDensity } = useListDensity()

const { data: repos, refresh: refreshRepos } = useAsyncData('repos_for_jobs', () =>
  $pb.collection('repositories').getFullList({ sort: 'name' })
)
const { data: jobWorkers, refresh: refreshJobWorkers } = useAsyncData('job_builder_workers', () =>
  getWorkers().catch(() => [])
)

const hasRepos = computed(() => (repos.value?.length ?? 0) > 0)
const hasWorkers = computed(() =>
  (jobWorkers.value || []).some((w: any) => w.status !== WORKER_STATUS.PENDING && w.status !== WORKER_STATUS.REVOKED)
)
const showCreateRepoFromEmpty = ref(false)

const emptyStateStep = computed(() => {
  if (!hasRepos.value) {
    return {
      description: 'Create a repository first, then add a job linked to it.',
      ctaLabel: 'Add Repository',
      action: () => { showCreateRepoFromEmpty.value = true },
    }
  }
  if (!hasWorkers.value) {
    return {
      description: 'Register a worker first, then add a job for it to run.',
      ctaLabel: 'Add Worker',
      action: () => navigateTo('/workers'),
    }
  }
  return {
    description: 'Create a new job to get started.',
    ctaLabel: 'New Job',
    action: () => { refreshRepos().then(() => { showCreate.value = true }) },
  }
})

const route = useRoute()

const searchQuery = ref('')
const statusFilter = ref('all')
const repositoryFilter = ref('all')
function groupQueryToFilterValue(val: unknown): string {
  if (typeof val !== 'string' || val === '') return GROUP_ALL
  return val === GROUP_ALL || val === GROUP_UNGROUPED ? val : encodeGroupValue(val)
}

const groupFilter = ref(groupQueryToFilterValue(route.query.group))
watch(() => route.query.group, (val) => {
  groupFilter.value = groupQueryToFilterValue(val)
})

const {
  page, perPage, items: pagedJobs, totalItems: totalJobs, totalPages, loading: jobsLoading, reload: reloadJobs,
} = usePaginatedList(
  async ({ page: p, perPage: pp }) => {
    const result = await listJobs({
      page: p,
      perPage: pp,
      status: statusFilter.value !== 'all' ? statusFilter.value : undefined,
      repository: repositoryFilter.value !== 'all' ? repositoryFilter.value : undefined,
      search: searchQuery.value.trim() || undefined,
    })
    return { items: result.items, totalItems: result.total_items }
  },
  { perPage: 20, watchDebounced: [searchQuery, statusFilter, repositoryFilter] }
)

// group lives only inside each job's parsed job.yaml (see jobs.go's
// buildJobsFilter comment), not a stored column, so it can't be pushed into
// the paginated query above - it's applied client-side over whatever page is
// currently loaded instead. That means a group filter can under-report
// matches that exist on a different page; accepted trade-off until group
// becomes a synced column (see FEATURE_PROPOSALS.md).
const jobs = computed(() => {
  if (!pagedJobs.value) return []
  const withRuns = pagedJobs.value.map((job: any) => ({
    ...job,
    reversedRecentRuns: job.recent_runs ? [...job.recent_runs].reverse() : []
  }))
  if (groupFilter.value === GROUP_ALL) return withRuns
  return groupFilter.value === GROUP_UNGROUPED
    ? withRuns.filter((job: any) => !job.definition?.group)
    : withRuns.filter((job: any) => job.definition?.group === decodeGroupValue(groupFilter.value))
})

const repositoryOptions = computed(() => [
  { label: 'All Repositories', value: 'all' },
  ...(repos.value || []).map((r: any) => ({ label: r.name, value: r.id }))
])

const { data: jobGroupsData, refresh: refreshJobGroups } = useAsyncData('job_groups', () =>
  listJobGroups().catch(() => ({ groups: [], has_ungrouped: false }))
)

const groupOptions = computed(() => {
  const items = [{ label: 'All groups', value: GROUP_ALL }]
  if (jobGroupsData.value?.has_ungrouped) {
    items.push({ label: 'Ungrouped', value: GROUP_UNGROUPED })
  }
  for (const g of jobGroupsData.value?.groups || []) {
    items.push({ label: g, value: encodeGroupValue(g) })
  }
  return items
})

// Fleet-wide status counts for the availability bar and the "no jobs at
// all" empty state - both need every job, not just the current filtered
// page. A narrow field selection on the raw collection (bypassing the
// job.yaml-parsing custom endpoint entirely) keeps this cheap.
const { data: jobsAggregate, refresh: refreshJobsAggregate } = useAsyncData('jobs_aggregate', () =>
  $pb.collection('scheduled_jobs').getFullList({ fields: 'id,status', requestKey: null })
)

async function refresh() {
  await Promise.all([reloadJobs(), refreshJobsAggregate(), refreshJobGroups()])
}

onMounted(() => {
  subscribe('scheduled_jobs', () => {
    reloadJobs(true)
    refreshJobsAggregate()
    refreshJobGroups()
  })
  subscribe('job_runs', (data: any) => {
    const jobId = data.record?.job
    if (jobId && pagedJobs.value?.some((j: any) => j.id === jobId)) {
      reloadJobs(true)
    }
  })
  subscribe('repositories', () => {
    refreshRepos()
    reloadJobs(true)
  })
  subscribe('workers', () => refreshJobWorkers())
})

const showCreate = ref(false)
const showBuilder = ref(false)

function onCreated() {
  showCreate.value = false
  refresh()
}

async function toggleEnabled(job: any) {
  try {
    await $pb.collection('scheduled_jobs').update(job.id, { enabled: !job.enabled })
    toast.add({ title: job.enabled ? 'Job disabled' : 'Job enabled', color: 'success' })
    reloadJobs()
  } catch (e: any) {
    toast.add({ title: 'Failed to update job', description: e?.message, color: 'error' })
  }
}

async function triggerRun(job: any) {
  try {
    await triggerJobRun(job.id)
    toast.add({ title: 'Job triggered', description: 'A manual run has been dispatched.', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to trigger job', description: e?.message, color: 'error' })
  }
}

const jobStatusSegments: AvailabilitySegment[] = [
  { key: 'active', label: 'Active', barClass: 'bg-emerald-400', dotClass: 'bg-emerald-400', statuses: ['active'] },
  { key: 'stalled', label: 'Stalled', barClass: 'bg-amber-400', dotClass: 'bg-amber-400', statuses: ['stalled'] },
  { key: 'error', label: 'Error', barClass: 'bg-rose-400', dotClass: 'bg-rose-400', statuses: ['error'] },
  { key: 'paused', label: 'Paused', barClass: 'bg-gray-300 dark:bg-carbon-600', dotClass: 'bg-gray-300 dark:bg-carbon-600', statuses: ['paused'] },
]

function statusBorderClass(status: string) {
  switch (status) {
    case 'active': return 'border-l-emerald-400 dark:border-l-emerald-400'
    case 'stalled': return 'border-l-amber-400 dark:border-l-amber-400'
    case 'error': return 'border-l-rose-400 dark:border-l-rose-400'
    case 'paused': return 'border-l-gray-300 dark:border-l-carbon-600'
    default: return 'border-l-gray-300 dark:border-l-carbon-600'
  }
}

function formatRelative(dateStr: string) {
  if (!dateStr || dateStr === '0001-01-01 00:00:00.000Z') return 'Never'
  const diff = Date.now() - new Date(dateStr).getTime()
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return `${Math.floor(diff / 86_400_000)}d ago`
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <h1 class="flex items-center gap-3 text-2xl font-bold text-gray-900 dark:text-wire-200">
        <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
          <UIcon name="i-lucide-calendar-clock" class="w-5 h-5 text-yellow-400" />
        </div>
        <span>
          <span class="block">Jobs</span>
          <span class="block text-sm font-normal text-gray-500 dark:text-wire-200/60">Cron-scheduled one-shot Docker jobs.</span>
        </span>
      </h1>
      <div class="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center sm:gap-3">
        <UButton
          v-if="!isViewer"
          icon="i-lucide-plus"
          label="New Job"
          class="w-full justify-center shadow-[0_0_16px_rgba(255,198,0,0.35)] transition-shadow hover:shadow-[0_0_24px_rgba(255,198,0,0.55)] sm:w-auto"
          @click="refreshRepos().then(() => { showCreate = true })"
        />
        <UButton
          v-if="!isViewer"
          icon="i-lucide-wrench"
          label="Job Builder"
          variant="outline"
          class="w-full justify-center sm:w-auto"
          @click="showBuilder = true"
        />
      </div>
    </div>

    <AppPanelCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">Jobs</h3>
          <div class="flex items-center gap-3">
            <UTooltip :text="isCompact ? 'Switch to comfortable view' : 'Switch to compact view'">
              <UButton
                :icon="isCompact ? 'i-lucide-rows-3' : 'i-lucide-list'"
                variant="ghost"
                color="neutral"
                size="xs"
                :aria-label="isCompact ? 'Switch to comfortable view' : 'Switch to compact view'"
                @click="toggleDensity"
              />
            </UTooltip>
            <RefreshButton @click="refresh()" />
          </div>
        </div>
      </template>

      <div v-if="jobsLoading && !pagedJobs.length" class="space-y-3">
        <USkeleton v-for="i in 3" :key="i" class="h-20 w-full" />
      </div>

      <EmptyState
        v-else-if="!jobsAggregate || jobsAggregate.length === 0"
        icon="i-lucide-calendar-clock"
        title="No jobs yet"
        :description="emptyStateStep.description"
        :cta-label="isViewer ? undefined : emptyStateStep.ctaLabel"
        @cta="emptyStateStep.action"
      />

      <div v-else class="space-y-4">
        <div class="flex flex-row flex-wrap items-center gap-2 sm:gap-3" role="search" aria-label="Filter jobs">
          <AppTextInput
            v-model="searchQuery"
            icon="i-lucide-search"
            placeholder="Search jobs..."
            class="min-w-[140px] flex-1"
            aria-label="Search jobs"
          />
          <AppSelectInput
            v-model="statusFilter"
            :items="[
              { label: 'All Statuses', value: 'all' },
              { label: 'Active', value: 'active' },
              { label: 'Paused', value: 'paused' },
              { label: 'Stalled', value: 'stalled' },
              { label: 'Error', value: 'error' }
            ]"
            placeholder="Filter by status"
            content-width
            class="sm:min-w-28"
            aria-label="Filter jobs by status"
          />
          <AppSelectInput
            v-model="repositoryFilter"
            :items="repositoryOptions"
            placeholder="Filter by repository"
            content-width
            class="sm:min-w-28"
            aria-label="Filter jobs by repository"
          />
          <AppSelectInput
            v-if="groupOptions.length > 1"
            v-model="groupFilter"
            :items="groupOptions"
            placeholder="Filter by group"
            content-width
            class="sm:min-w-28"
            aria-label="Filter jobs by group"
          />
        </div>

        <StatusAvailabilityBar
          v-model="statusFilter"
          :items="jobsAggregate"
          :segments="jobStatusSegments"
          aria-label="Job status availability breakdown"
        />

        <div v-if="!jobsLoading && jobs.length === 0" class="text-center py-12" role="status" aria-live="polite">
          <UIcon name="i-lucide-search-x" class="w-12 h-12 text-gray-300 mx-auto mb-4" />
          <p class="text-gray-500">No jobs found</p>
          <p class="text-xs text-gray-400 mt-1">Try adjusting your search</p>
        </div>

        <div v-else class="space-y-3">
          <div
            v-for="job in jobs"
            :key="job.id"
            class="flex items-center justify-between bg-gray-50 dark:bg-carbon-800/40 rounded-xl border border-gray-300 border-l-4 dark:border-carbon-700 hover:shadow-[0_0_0_2px_rgba(255,198,0,0.35),0_0_20px_rgba(255,198,0,0.12)] transition-all"
            :class="[statusBorderClass(job.status), isCompact ? 'p-2' : 'p-4']"
          >
            <!-- Icon — left, separated -->
            <div class="hidden sm:flex mr-2 border-r border-gray-300 dark:border-carbon-700 pr-4 items-center shrink-0">
              <UIcon name="i-lucide-terminal" class="w-5 h-5 text-wire-400" />
            </div>

            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 mb-1 min-w-0">
                <NuxtLink
                  :to="`/jobs/${job.id}`"
                  class="font-semibold text-gray-900 dark:text-wire-200 hover:text-yellow-400 transition-colors truncate min-w-0"
                >
                  {{ job.name || job.definition?.name || 'Invalid Job' }}
                </NuxtLink>
                <UTooltip v-if="job.definition_error" :text="job.definition_error">
                  <UIcon name="i-lucide-triangle-alert" class="w-4 h-4 text-amber-500 shrink-0" />
                </UTooltip>
                <BadgeStatus :status="job.status" class="shrink-0" />
                <UBadge v-if="job.definition?.group" :label="job.definition.group" color="neutral" variant="outline" size="xs" class="shrink-0" />

                <!-- Last 5 executions dots -->
                <div v-if="!isCompact && job.recent_runs && job.recent_runs.length > 0" class="hidden md:flex items-center gap-1 ml-2 shrink-0">
                  <span class="text-xs text-gray-400 dark:text-wire-200/40 mr-1 select-none">History:</span>
                  <UTooltip
                    v-for="run in job.reversedRecentRuns"
                    :key="run.id"
                    :text="`Run: ${run.status} (${formatRelative(run.created)})`"
                  >
                    <span
                      class="inline-block w-2.5 h-2.5 rounded-full border border-gray-300 dark:border-carbon-700 shrink-0"
                      :class="{
                        'bg-green-500': run.status === 'success',
                        'bg-red-500': run.status === 'error',
                        'bg-yellow-400 animate-pulse': run.status === 'running',
                        'bg-amber-400': run.status === 'stalled',
                        'bg-gray-400': run.status === 'pending',
                      }"
                    />
                  </UTooltip>
                </div>
              </div>

              <p v-if="!isCompact && (job.description || job.definition?.description)" class="text-sm text-gray-500 dark:text-wire-200/50 truncate mt-0.5 mb-1.5">
                {{ job.description || job.definition.description }}
              </p>

              <div class="hidden sm:flex items-center gap-2 flex-wrap">
                <span v-if="!isCompact" class="text-xs text-gray-400 dark:text-wire-200/40 font-mono flex items-center gap-1 mr-2">
                  <UIcon name="i-lucide-git-branch" class="w-3.5 h-3.5" />
                  {{ job.repository.name }} / {{ job.job_file }}
                </span>
                <UBadge
                  v-if="job.definition?.cron"
                  :label="job.definition.cron"
                  variant="subtle"
                  color="neutral"
                  size="xs"
                  class="font-mono"
                />
                <UBadge
                  v-if="!isCompact && job.definition?.image"
                  :label="job.definition.image"
                  variant="subtle"
                  color="info"
                  size="xs"
                  class="font-mono"
                />
                <template v-if="!isCompact && job.definition?.tags?.length">
                  <UBadge
                    v-for="tag in job.definition.tags"
                    :key="tag"
                    :label="tag"
                    variant="subtle"
                    color="primary"
                    size="xs"
                    class="font-mono"
                  />
                </template>
                <UBadge
                  v-if="!isCompact && job.definition?.network"
                  :label="`net: ${job.definition.network}`"
                  variant="subtle"
                  color="info"
                  size="xs"
                  class="font-mono"
                />
              </div>
            </div>

            <!-- Actions and details on the right -->
            <div class="ml-2 border-l border-gray-300 dark:border-carbon-700 pl-4 flex items-center gap-3 shrink-0">
              <div class="hidden md:flex flex-col items-end gap-0.5 mr-2">
                <span class="text-xs text-gray-400 dark:text-wire-200/40 uppercase tracking-wider font-semibold">Last run</span>
                <span class="text-sm text-gray-700 dark:text-wire-200">{{ formatRelative(job.last_run_at) }}</span>
              </div>
              <template v-if="!isViewer">
                <UTooltip :text="job.enabled ? 'Click to disable' : 'Click to enable'">
                  <button
                    type="button"
                    class="flex items-center justify-center rounded-full p-1.5 cursor-pointer transition-opacity hover:opacity-80"
                    :aria-label="job.enabled ? 'Disable job' : 'Enable job'"
                    @click="toggleEnabled(job)"
                  >
                    <UIcon
                      name="i-lucide-power"
                      class="w-4 h-4"
                      :class="job.enabled
                        ? 'text-emerald-400 drop-shadow-[0_0_10px_rgba(52,211,153,1)]'
                        : 'text-gray-400 opacity-70'"
                    />
                  </button>
                </UTooltip>
                <UTooltip text="Run now">
                  <UButton
                    icon="i-lucide-play"
                    variant="ghost"
                    size="xs"
                    color="neutral"
                    :disabled="!job.enabled"
                    @click="triggerRun(job)"
                  />
                </UTooltip>
              </template>
            </div>
          </div>
        </div>

        <div v-if="totalPages > 1" class="flex justify-between items-center pt-2">
          <UPagination
            v-model="page"
            :total="totalJobs"
            :items-per-page="perPage"
          />
          <span class="text-xs text-gray-500">Page {{ page }} of {{ totalPages }}</span>
        </div>
        </div>
      </AppPanelCard>
    </div>

    <JobCreateModal
      v-model:open="showCreate"
      :repos="repos || []" 
      @created="onCreated" 
    />

    <JobBuilderModal
      v-model:open="showBuilder"
      :workers="jobWorkers || []"
    />

    <RepositoryCreateModal v-model:open="showCreateRepoFromEmpty" @created="refreshRepos" />
  </template>
