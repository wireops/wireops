<script setup lang="ts">
const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()
const { listJobs, getWorkers } = useApi()

const { data: stacks, refresh } = useAsyncData('stacks', () =>
  $pb.collection('stacks').getFullList({ sort: '-updated', expand: 'repository' }).catch(() => [])
)

const { data: jobs, refresh: refreshJobs } = useAsyncData('dashboard_jobs', () =>
  listJobs().catch(() => [])
)

const { data: workers, refresh: refreshWorkers } = useAsyncData('dashboard_workers', () =>
  getWorkers().catch(() => [])
)

const { data: repos, refresh: refreshRepos } = useAsyncData('dashboard_repos', () =>
  $pb.collection('repositories').getFullList({ fields: 'id', requestKey: null }).catch(() => [])
)

const showCreateRepo = ref(false)

async function refreshAll() {
  await Promise.all([refresh(), refreshJobs(), refreshWorkers(), refreshRepos()])
}

const stats = computed(() => {
  const s = stacks.value || []
  const j = jobs.value || []
  const w = (workers.value || []).filter((r: any) => r.status !== WORKER_STATUS.PENDING && r.status !== WORKER_STATUS.REVOKED)
  return {
    stacks: s.length,
    jobs: j.length,
    repos: (repos.value || []).length,
    active: s.filter((r: any) => r.status === 'active').length + j.filter((r: any) => r.status === 'active').length,
    error: s.filter((r: any) => r.status === 'error').length + j.filter((r: any) => r.status === 'error').length,
    paused: s.filter((r: any) => r.status === 'paused').length,
    stalledJobs: j.filter((r: any) => r.status === 'stalled').length,
    workersOnline: w.filter((r: any) => r.status === WORKER_STATUS.ACTIVE).length,
    workersTotal: w.length,
  }
})

const groupCounts = computed(() => {
  const counts = new Map<string, { stacks: number, jobs: number }>()
  const bump = (group: string, key: 'stacks' | 'jobs') => {
    const entry = counts.get(group) || { stacks: 0, jobs: 0 }
    entry[key]++
    counts.set(group, entry)
  }
  for (const s of stacks.value || []) {
    if (s.group) bump(s.group, 'stacks')
  }
  for (const j of jobs.value || []) {
    const g = j.definition?.group
    if (g) bump(g, 'jobs')
  }
  return [...counts.entries()]
    .sort((a, b) => (b[1].stacks + b[1].jobs) - (a[1].stacks + a[1].jobs))
    .slice(0, 5)
    .map(([group, { stacks, jobs }]) => ({ group, stacks, jobs }))
})

const dataReady = computed(() =>
  stacks.value !== null && jobs.value !== null && workers.value !== null && repos.value !== null
)

const gettingStartedSteps = computed(() => [
  {
    key: 'repo',
    icon: 'i-lucide-git-branch',
    title: 'Connect a repository',
    description: 'Point wireops at your Compose repo.',
    done: stats.value.repos > 0,
    ctaLabel: 'Add Repository',
    action: () => { showCreateRepo.value = true },
    viewAction: () => navigateTo('/repositories'),
  },
  {
    key: 'worker',
    icon: 'i-lucide-network',
    title: 'Add a worker',
    description: 'Register a host to run your deploys.',
    done: stats.value.workersTotal > 0,
    ctaLabel: 'Add Worker',
    action: () => navigateTo('/workers'),
    viewAction: () => navigateTo('/workers'),
  },
  {
    key: 'stack',
    icon: 'i-lucide-layers',
    title: 'Create a stack',
    description: 'Deploy a Compose stack from your repository.',
    done: stats.value.stacks > 0,
    ctaLabel: 'Create Stack',
    action: () => navigateTo('/stacks'),
    viewAction: () => navigateTo('/stacks'),
  },
])

const gettingStartedDoneCount = computed(() => gettingStartedSteps.value.filter(s => s.done).length)

const showGettingStarted = computed(() => dataReady.value && gettingStartedDoneCount.value < gettingStartedSteps.value.length)

const isPristine = computed(() =>
  dataReady.value
  && stats.value.stacks === 0 && stats.value.jobs === 0 && stats.value.workersTotal === 0 && stats.value.repos === 0
)


const statusColor = (status: string) => {
  switch (status) {
    case 'active': case 'success': case 'connected': return 'success'
    case 'error': return 'error'
    case 'paused': case 'running': case 'pending': return 'warning'
    default: return 'neutral'
  }
}

// Realtime updates
const isUpdating = ref(false)

onMounted(() => {
  subscribe('stacks', () => {
    isUpdating.value = true
    refresh()
    setTimeout(() => { isUpdating.value = false }, 500)
  })
  subscribe('scheduled_jobs', () => refreshJobs())
  subscribe('workers', () => refreshWorkers())
  subscribe('repositories', () => refreshRepos())
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-3">
        <h1 class="flex items-center gap-3 text-2xl font-bold">
          <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
            <UIcon name="i-lucide-layout-dashboard" class="w-5 h-5 text-yellow-400" />
          </div>
          Dashboard
        </h1>
        <div v-if="isUpdating" class="flex items-center gap-2 text-sm text-wire-400">
          <UIcon name="i-lucide-loader-2" class="w-4 h-4 animate-spin" />
          <span class="hidden sm:inline">Updating...</span>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <BadgeStatus :status="'active'" class="hidden sm:flex uppercase" />
        <RefreshButton label size="sm" @click="refreshAll()" />
      </div>
    </div>

    <UCard v-if="showGettingStarted">
      <template #header>
        <div class="flex items-center justify-between">
          <h2 class="font-semibold">Getting Started</h2>
          <span class="text-sm text-wire-200/50">{{ gettingStartedDoneCount }}/{{ gettingStartedSteps.length }} done</span>
        </div>
      </template>
      <div class="flex flex-col sm:flex-row sm:items-stretch gap-3">
        <template v-for="(step, index) in gettingStartedSteps" :key="step.key">
          <div v-if="index > 0" class="hidden sm:flex items-center justify-center shrink-0 w-6">
            <div
              class="flex items-center justify-center w-6 h-6 rounded-full border shrink-0"
              :class="gettingStartedSteps[index - 1].done ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400' : 'border-carbon-700 bg-carbon-900 text-wire-200/30'"
            >
              <UIcon name="i-lucide-chevron-right" class="w-3.5 h-3.5" />
            </div>
          </div>
          <button
            type="button"
            class="flex-1 w-full text-left rounded-xl border p-4 transition-all duration-200 flex flex-col gap-2 cursor-pointer"
            :class="step.done
              ? 'border-emerald-500/25 bg-emerald-500/[0.03] hover:border-emerald-500/50 hover:bg-emerald-500/[0.06]'
              : 'border-carbon-800 bg-carbon-900/40 hover:border-yellow-400/50 hover:bg-carbon-800/60 hover:shadow-[0_0_16px_rgba(255,198,0,0.12)]'"
            @click="step.done ? step.viewAction() : step.action()"
          >
            <div class="flex items-center gap-2">
              <div
                class="flex items-center justify-center w-7 h-7 rounded-full shrink-0"
                :class="step.done ? 'bg-emerald-500/15 text-emerald-400' : 'bg-yellow-400/10 text-yellow-400'"
              >
                <UIcon :name="step.done ? 'i-lucide-check' : step.icon" class="w-4 h-4" />
              </div>
              <p class="font-medium text-wire-200">{{ step.title }}</p>
            </div>
            <p class="text-sm text-wire-200/50 flex-1">{{ step.description }}</p>
            <span v-if="step.done" class="inline-flex items-center gap-1 text-xs font-medium text-emerald-400 mt-auto">
              <UIcon name="i-lucide-check-circle-2" class="w-3.5 h-3.5" />
              Completed
            </span>
            <span v-else class="inline-flex items-center gap-1 text-xs font-medium text-yellow-400 mt-auto">
              {{ step.ctaLabel }}
              <UIcon name="i-lucide-arrow-right" class="w-3.5 h-3.5" />
            </span>
          </button>
        </template>
      </div>
    </UCard>

    <div v-if="!isPristine" class="grid grid-cols-2 lg:grid-cols-4 gap-3 sm:gap-4">
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg bg-wire-400/10">
            <UIcon name="i-lucide-layers" class="w-5 h-5 text-wire-400" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Stacks</p>
            <p class="text-2xl font-bold">{{ stats.stacks }}</p>
          </div>
        </div>
      </UCard>
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg bg-wire-400/10">
            <UIcon name="i-lucide-calendar-clock" class="w-5 h-5 text-wire-400" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Jobs</p>
            <p class="text-2xl font-bold">{{ stats.jobs }}</p>
          </div>
        </div>
      </UCard>
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg bg-emerald-500/10">
            <UIcon name="i-lucide-circle-check" class="w-5 h-5 text-emerald-400" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Active</p>
            <p class="text-2xl font-bold">{{ stats.active }}</p>
          </div>
        </div>
      </UCard>
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg" :class="stats.error > 0 ? 'bg-red-500/10' : 'bg-gray-400/10 dark:bg-carbon-700/40'">
            <UIcon name="i-lucide-alert-triangle" class="w-5 h-5" :class="stats.error > 0 ? 'text-red-400' : 'text-gray-400 dark:text-gray-500'" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Error</p>
            <p class="text-2xl font-bold" :class="stats.error > 0 ? 'text-red-400' : 'text-gray-400 dark:text-gray-500'">{{ stats.error }}</p>
          </div>
        </div>
      </UCard>
    </div>

    <div v-if="!isPristine" class="grid grid-cols-3 gap-3 sm:gap-4">
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg bg-emerald-500/10">
            <UIcon name="i-lucide-network" class="w-5 h-5 text-emerald-400" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Workers Online</p>
            <p class="text-2xl font-bold">{{ stats.workersOnline }}<span class="text-base font-medium text-wire-200/40">/{{ stats.workersTotal }}</span></p>
          </div>
        </div>
      </UCard>
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg" :class="stats.stalledJobs > 0 ? 'bg-amber-500/10' : 'bg-gray-400/10 dark:bg-carbon-700/40'">
            <UIcon name="i-lucide-pause-circle" class="w-5 h-5" :class="stats.stalledJobs > 0 ? 'text-amber-400' : 'text-gray-400 dark:text-gray-500'" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Stalled Jobs</p>
            <p class="text-2xl font-bold" :class="stats.stalledJobs > 0 ? 'text-amber-400' : 'text-gray-400 dark:text-gray-500'">{{ stats.stalledJobs }}</p>
          </div>
        </div>
      </UCard>
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg" :class="stats.paused > 0 ? 'bg-amber-500/10' : 'bg-gray-400/10 dark:bg-carbon-700/40'">
            <UIcon name="i-lucide-pause-circle" class="w-5 h-5" :class="stats.paused > 0 ? 'text-amber-400' : 'text-gray-400 dark:text-gray-500'" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Paused Stacks</p>
            <p class="text-2xl font-bold" :class="stats.paused > 0 ? 'text-amber-400' : 'text-gray-400 dark:text-gray-500'">{{ stats.paused }}</p>
          </div>
        </div>
      </UCard>
    </div>

    <div v-if="!isPristine" class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Left Column: Recent Activity -->
      <RecentSyncActivity />

      <!-- Right Column: Widgets -->
      <div class="space-y-6">
        <!-- Quick Actions -->
        <UCard>
          <template #header>
            <h2 class="font-semibold">Quick Actions</h2>
          </template>
          <div class="grid grid-cols-2 gap-3">
            <UButton
              to="/stacks"
              icon="i-lucide-layers"
              label="Manage Workloads"
              color="primary"
              variant="soft"
              block
            />
            <UButton
              to="/repositories"
              icon="i-lucide-git-branch"
              label="Repositories"
              color="primary"
              variant="soft"
              block
            />
            <UButton
              icon="i-lucide-plus"
              label="Create Repository"
              color="primary"
              variant="soft"
              block
              @click="showCreateRepo = true"
            />
            <UButton
              to="/settings"
              icon="i-lucide-settings"
              label="Settings"
              color="neutral"
              variant="soft"
              block
            />
            <UButton
              to="https://github.com/wireops/wireops"
              target="_blank"
              icon="i-lucide-github"
              label="Documentation"
              color="neutral"
              variant="soft"
              block
            />
          </div>
        </UCard>

        <!-- Stack Health -->
        <UCard>
          <template #header>
            <h2 class="font-semibold">System Health</h2>
          </template>
          
          <div v-if="stats.error > 0" class="space-y-3">
            <div class="flex items-center gap-2 text-red-400 bg-red-500/10 p-3 rounded-lg border border-red-500/20">
              <UIcon name="i-lucide-alert-circle" class="w-5 h-5" />
              <span class="font-medium text-sm">{{ stats.error }} stack(s) requiring attention</span>
            </div>

            <div class="divide-y divide-carbon-800">
              <div
                v-for="stack in stacks?.filter((s:any) => s.status === 'error').slice(0, 3)"
                :key="stack.id"
                class="py-2 flex items-center justify-between"
              >
                <div class="flex items-center gap-2">
                  <div class="w-2 h-2 rounded-full bg-red-400"/>
                  <span class="text-sm font-medium">{{ stack.name }}</span>
                </div>
                <UButton
                  :to="`/stacks/${stack.id}`"
                  size="xs"
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-arrow-right"
                />
              </div>
            </div>
          </div>

          <div v-else class="flex flex-col items-center justify-center py-6 text-center">
            <div class="w-12 h-12 rounded-full bg-yellow-400/10 border border-yellow-400/20 flex items-center justify-center mb-3 shadow-[0_0_16px_rgba(255,198,0,0.1)]">
              <UIcon name="i-lucide-zap" class="w-6 h-6 text-yellow-400" />
            </div>
            <p class="font-medium text-wire-200">All Systems Operational</p>
            <p class="text-sm text-wire-200/50 mt-1">All {{ stats.stacks }} stacks are healthy</p>
          </div>
        </UCard>

        <!-- By Group -->
        <UCard v-if="groupCounts.length > 0">
          <template #header>
            <h2 class="font-semibold">By Group</h2>
          </template>

          <div class="divide-y divide-carbon-800">
            <div
              v-for="entry in groupCounts"
              :key="entry.group"
              class="py-2 flex items-center justify-between gap-2"
            >
              <span class="text-sm font-medium truncate">{{ entry.group }}</span>
              <div class="flex items-center gap-1 shrink-0">
                <UButton
                  v-if="entry.stacks > 0"
                  :to="`/stacks?group=${encodeURIComponent(entry.group)}`"
                  :label="`${entry.stacks} stack${entry.stacks === 1 ? '' : 's'}`"
                  size="xs"
                  color="neutral"
                  variant="ghost"
                  trailing-icon="i-lucide-arrow-right"
                />
                <UButton
                  v-if="entry.jobs > 0"
                  :to="`/jobs?group=${encodeURIComponent(entry.group)}`"
                  :label="`${entry.jobs} job${entry.jobs === 1 ? '' : 's'}`"
                  size="xs"
                  color="neutral"
                  variant="ghost"
                  trailing-icon="i-lucide-arrow-right"
                />
              </div>
            </div>
          </div>
        </UCard>
      </div>
    </div>
    
    <RepositoryCreateModal v-model:open="showCreateRepo" @created="refreshRepos" />
  </div>
</template>
