<script setup lang="ts">
const { $pb } = useNuxtApp()
const { listJobs, triggerJobRun } = useApi()
const { subscribe } = useRealtime()
const toast = useToast()

const { data: repos, refresh: refreshRepos } = useAsyncData('repos_for_jobs', () =>
  $pb.collection('repositories').getFullList({ sort: 'name' })
)

const { data: jobs, refresh, pending } = useAsyncData('jobs_list', () => listJobs())

onMounted(() => {
  subscribe('scheduled_jobs', () => refresh())
})

const showCreate = ref(false)
const selectedDelete = ref<any>(null)

function onCreated() {
  showCreate.value = false
  refresh()
}

function onDeleted() {
  selectedDelete.value = null
  refresh()
}

async function toggleEnabled(job: any) {
  try {
    await $pb.collection('scheduled_jobs').update(job.id, { enabled: !job.enabled })
    toast.add({ title: job.enabled ? 'Job disabled' : 'Job enabled', color: 'success' })
    refresh()
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

function statusColor(status: string) {
  switch (status) {
    case 'active': return 'success'
    case 'stalled': return 'warning'
    case 'paused': return 'neutral'
    default: return 'neutral'
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
    <div class="flex items-center justify-between">
      <h1 class="flex items-center gap-3 text-2xl font-bold text-gray-900 dark:text-wire-200">
        <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
          <UIcon name="i-lucide-calendar-clock" class="w-5 h-5 text-yellow-400" />
        </div>
        Scheduled Jobs
      </h1>
      <UButton
        icon="i-lucide-plus"
        label="New Job"
        class="shadow-[0_0_16px_rgba(255,198,0,0.35)] hover:shadow-[0_0_24px_rgba(255,198,0,0.55)] transition-shadow"
        @click="refreshRepos().then(() => { showCreate = true })"
      />
    </div>

    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">Jobs</h3>
          <UTooltip text="Refresh">
            <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" color="neutral" :loading="pending" @click="() => refresh()" />
          </UTooltip>
        </div>
      </template>

      <div v-if="pending && !jobs" class="space-y-3">
        <USkeleton v-for="i in 3" :key="i" class="h-20 w-full" />
      </div>

      <div v-else-if="!jobs || jobs.length === 0" class="text-center py-12">
        <div class="w-14 h-14 rounded-full bg-wire-400/10 border border-wire-400/20 flex items-center justify-center mx-auto mb-3">
          <UIcon name="i-lucide-calendar-clock" class="w-7 h-7 text-wire-400" />
        </div>
        <h3 class="text-lg font-medium text-gray-900 dark:text-wire-200 mb-1">No jobs yet</h3>
        <p class="text-gray-500 dark:text-wire-200/50 text-sm">Create a new job to get started.</p>
      </div>

      <div v-else class="divide-y divide-gray-100 dark:divide-carbon-800">
        <div
          v-for="job in jobs"
          :key="job.id"
          class="flex items-center justify-between py-4 gap-4"
        >
          <div class="flex items-center gap-4 min-w-0 flex-1">
            <div class="w-9 h-9 rounded-lg bg-gray-100 dark:bg-carbon-700/60 flex items-center justify-center shrink-0">
              <UIcon name="i-lucide-terminal" class="w-4 h-4 text-wire-400" />
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <NuxtLink
                  :to="`/jobs/${job.id}`"
                  class="font-semibold text-gray-900 dark:text-wire-200 hover:text-yellow-400 transition-colors truncate"
                >
                  {{ job.definition?.title || job.job_file }}
                </NuxtLink>
                <UTooltip v-if="job.definition_error" :text="job.definition_error">
                  <UIcon name="i-lucide-triangle-alert" class="w-4 h-4 text-amber-500 shrink-0" />
                </UTooltip>
              </div>

              <p v-if="job.definition?.description" class="text-sm text-gray-500 dark:text-wire-200/50 truncate mt-0.5">
                {{ job.definition.description }}
              </p>

              <div class="flex items-center gap-2 mt-1 flex-wrap">
                <span class="text-xs text-gray-400 dark:text-wire-200/40 font-mono">
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
                  v-if="job.definition?.image"
                  :label="job.definition.image"
                  variant="subtle"
                  color="info"
                  size="xs"
                  class="font-mono"
                />
                <template v-if="job.definition?.tags?.length">
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
                  v-if="job.definition?.network"
                  :label="`net: ${job.definition.network}`"
                  variant="subtle"
                  color="info"
                  size="xs"
                  class="font-mono"
                />
              </div>
            </div>
          </div>

          <div class="flex items-center gap-3 shrink-0">
            <div class="hidden sm:flex flex-col items-end gap-0.5">
              <span class="text-xs text-gray-400 dark:text-wire-200/40 uppercase tracking-wider font-semibold">Last run</span>
              <span class="text-sm text-gray-700 dark:text-wire-200">{{ formatRelative(job.last_run_at) }}</span>
            </div>
            <UBadge :label="job.status" :color="statusColor(job.status)" variant="subtle" size="sm" />
            <USwitch
              :model-value="job.enabled"
              size="xs"
              @update:model-value="toggleEnabled(job)"
            />
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
            <UTooltip text="View details">
              <UButton
                icon="i-lucide-arrow-right"
                variant="ghost"
                size="xs"
                color="neutral"
                :to="`/jobs/${job.id}`"
              />
            </UTooltip>
            <UTooltip text="Delete job">
              <UButton
                icon="i-lucide-trash-2"
                variant="ghost"
                size="xs"
                color="error"
                @click="selectedDelete = job"
              />
            </UTooltip>
          </div>
        </div>
      </div>
    </UCard>
  </div>

  <UModal :open="showCreate" @update:open="(v) => { if (!v) showCreate = false }">
    <template #content>
      <JobCreateModal :repos="repos || []" @created="onCreated" @cancel="showCreate = false" />
    </template>
  </UModal>

  <UModal :open="!!selectedDelete" @update:open="(v) => { if (!v) selectedDelete = null }">
    <template #content>
      <JobDeleteModal
        v-if="selectedDelete"
        :job="selectedDelete"
        @deleted="onDeleted"
        @cancel="selectedDelete = null"
      />
    </template>
  </UModal>
</template>
