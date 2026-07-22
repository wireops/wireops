<script setup lang="ts">
import { WORKER_STATUS, TOKEN_STATUS } from '../../utils/worker'

const route = useRoute()
const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()
const { copy } = useCopy()
const { getWorkers, getWorkerPolicy, saveWorkerPolicy, resetWorkerPolicy, revokeWorker } = useApi()
const toast = useToast()
const { isViewer } = usePermissions()

if (isViewer.value) {
  await navigateTo('/')
}

const workerId = route.params.id as string

// --- Fetch worker by filtering from the full list (preserves computed fields like health_history, tags, etc.)
const { data: allWorkers, refresh: refreshWorkers } = useAsyncData(`workers_detail`, getWorkers)

const worker = computed(() => allWorkers.value?.find(w => w.id === workerId) ?? null)

const isActive = computed(() => worker.value?.status === WORKER_STATUS.ACTIVE)
const isOffline = computed(() => worker.value?.status === WORKER_STATUS.OFFLINE)
const isRevoked = computed(() => worker.value?.status === WORKER_STATUS.REVOKED)
const associatedJobs = computed(() => worker.value?.jobs ?? [])
const redirectingAfterRevoke = ref(false)

const statusDotClass = computed(() => {
  if (isActive.value) return 'bg-yellow-400 shadow-[0_0_8px_rgba(255,198,0,0.7)]'
  if (isRevoked.value) return 'bg-gray-400'
  return 'bg-red-500 shadow-[0_0_6px_rgba(239,68,68,0.6)]'
})

const statusCardDotClass = computed(() => {
  if (isActive.value) return 'bg-green-400 shadow-[0_0_8px_rgba(74,222,128,0.7)]'
  if (isRevoked.value) return 'bg-gray-400'
  return 'bg-red-500 shadow-[0_0_6px_rgba(239,68,68,0.6)]'
})

watch(worker, async (currentWorker, previousWorker) => {
  if (!previousWorker) return
  if (redirectingAfterRevoke.value) return
  if (currentWorker?.status !== WORKER_STATUS.REVOKED) return
  if (previousWorker?.status === WORKER_STATUS.REVOKED) return

  redirectingAfterRevoke.value = true
  await navigateTo('/workers', { replace: true })
}, { immediate: true })

// --- Stacks assigned to this worker
const { data: stacks, refresh: refreshStacks } = useAsyncData(`stacks_for_worker_${workerId}`, () =>
  $pb.collection('stacks').getFullList({
    filter: `worker = "${workerId}"`,
    sort: 'name',
    fields: 'id,name',
  })
)

// --- Tabs
const activeTab = ref((route.query.tab as string) || 'overview')
const tabs = [
  { label: 'Overview', value: 'overview', icon: 'i-lucide-info' },
  { label: 'Stacks', value: 'stacks', icon: 'i-lucide-layers' },
  { label: 'Jobs', value: 'jobs', icon: 'i-lucide-calendar-clock' },
  { label: 'Policies', value: 'policy', icon: 'i-lucide-shield-check' },
]

// --- Helpers
function formatDate(dateStr: string) {
  if (!dateStr || dateStr.startsWith('0001-01-01')) return 'Never'
  try { return new Date(dateStr).toLocaleString() } catch { return dateStr }
}

function formatRelative(dateStr: string) {
  if (!dateStr || dateStr.startsWith('0001-01-01')) return 'Never'
  const diff = Date.now() - new Date(dateStr).getTime()
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return `${Math.floor(diff / 86_400_000)}d ago`
}

function formatDateISO(dateStr: string) {
  if (!dateStr || dateStr.startsWith('0001-01-01')) return 'Never'
  try { return new Date(dateStr).toISOString() } catch { return dateStr }
}

function hasVisibleTokenExpiry(w: any) {
  if (!w?.token_expires) return false
  if (w.token_status === TOKEN_STATUS.ACTIVE) return false
  if (w.token_expires.startsWith('0001-01-01')) return false
  return true
}

// Health history: last 10 checks, newest last
const healthHistory = computed(() => {
  const history: { status: string; timestamp: string }[] = worker.value?.health_history ?? []
  return history.slice(-10)
})

function healthDotClass(status: string) {
  if (status === 'online') return 'bg-green-400 shadow-[0_0_6px_rgba(74,222,128,0.6)]'
  if (status === 'offline') return 'bg-red-500'
  return 'bg-gray-400'
}

// --- Revoke
const showRevokeModal = ref(false)
const showDangerZone = ref(false)
const revoking = ref(false)

const dangerZoneActions = computed(() => [
  {
    key: 'revoke',
    label: 'Revoke Worker',
    description: 'This worker will be disconnected and its token invalidated. This action cannot be undone.',
    buttonLabel: 'Revoke Worker',
    icon: 'i-lucide-ban',
    onClick: () => { showRevokeModal.value = true }
  }
])

async function confirmRevoke() {
  revoking.value = true
  try {
    await revokeWorker(workerId)
    redirectingAfterRevoke.value = true
    toast.add({ title: 'Worker revoked', color: 'success' })
    showRevokeModal.value = false
    await navigateTo('/workers', { replace: true })
  } catch (e: any) {
    toast.add({ title: 'Failed to revoke worker', description: e?.message, color: 'error' })
  } finally {
    revoking.value = false
  }
}

// --- Policy (inline aba)
const policyLoading = ref(false)
const policySaving = ref(false)
const policyLoaded = ref(false)
const isGlobalPolicyEnabled = ref(true)
const policyForm = ref<{
  inherit: boolean
  allowed_images: string[]
  allowed_volumes: string[]
  allowed_networks: string[]
  allowed_cap_add: string[]
  allowed_devices: string[]
  allowed_security_opt: string[]
  prevent_latest_images: boolean | null
  block_host_volumes: boolean | null
  block_privileged: boolean | null
  block_host_network: boolean | null
  block_host_pid: boolean | null
  block_host_ipc: boolean | null
  block_docker_socket: boolean | null
  allow_render_overrides: boolean | null
}>({
  inherit: false,
  allowed_images: [],
  allowed_volumes: [],
  allowed_networks: [],
  allowed_cap_add: [],
  allowed_devices: [],
  allowed_security_opt: [],
  prevent_latest_images: null,
  block_host_volumes: null,
  block_privileged: null,
  block_host_network: null,
  block_host_pid: null,
  block_host_ipc: null,
  block_docker_socket: null,
  allow_render_overrides: null,
})

async function loadPolicy() {
  policyLoading.value = true
  try {
    const data = await getWorkerPolicy(workerId)
    policyForm.value = {
      inherit: data.inherit ?? false,
      allowed_images: data.effective?.allowed_images ?? [],
      allowed_volumes: data.effective?.allowed_volumes ?? [],
      allowed_networks: data.effective?.allowed_networks ?? [],
      allowed_cap_add: data.effective?.allowed_cap_add ?? [],
      allowed_devices: data.effective?.allowed_devices ?? [],
      allowed_security_opt: data.effective?.allowed_security_opt ?? [],
      prevent_latest_images: data.prevent_latest_images !== undefined ? data.prevent_latest_images : null,
      block_host_volumes: data.block_host_volumes !== undefined ? data.block_host_volumes : null,
      block_privileged: data.block_privileged !== undefined ? data.block_privileged : null,
      block_host_network: data.block_host_network !== undefined ? data.block_host_network : null,
      block_host_pid: data.block_host_pid !== undefined ? data.block_host_pid : null,
      block_host_ipc: data.block_host_ipc !== undefined ? data.block_host_ipc : null,
      block_docker_socket: data.block_docker_socket !== undefined ? data.block_docker_socket : null,
      allow_render_overrides: data.allow_render_overrides !== undefined ? data.allow_render_overrides : null,
    }
    isGlobalPolicyEnabled.value = data.effective?.enabled ?? true
    policyLoaded.value = true
  } catch {
    policyForm.value = {
      inherit: false,
      allowed_images: [],
      allowed_volumes: [],
      allowed_networks: [],
      allowed_cap_add: [],
      allowed_devices: [],
      allowed_security_opt: [],
      prevent_latest_images: null,
      block_host_volumes: null,
      block_privileged: null,
      block_host_network: null,
      block_host_pid: null,
      block_host_ipc: null,
      block_docker_socket: null,
      allow_render_overrides: null,
    }
  } finally {
    policyLoading.value = false
  }
}

async function handleSavePolicy() {
  policySaving.value = true
  try {
    policyForm.value.allowed_images = policyForm.value.allowed_images.filter(i => i.trim() !== '')
    policyForm.value.allowed_volumes = policyForm.value.allowed_volumes.filter(v => v.trim() !== '')
    policyForm.value.allowed_networks = policyForm.value.allowed_networks.filter(n => n.trim() !== '')
    policyForm.value.allowed_cap_add = policyForm.value.allowed_cap_add.filter(c => c.trim() !== '')
    policyForm.value.allowed_devices = policyForm.value.allowed_devices.filter(d => d.trim() !== '')
    policyForm.value.allowed_security_opt = policyForm.value.allowed_security_opt.filter(s => s.trim() !== '')

    // While inherit is on, these fields hold the resolved *effective* (global)
    // values for display only — send null so they stay inherited instead of
    // being frozen as local overrides on save.
    await saveWorkerPolicy(workerId, {
      ...policyForm.value,
      allowed_images: policyForm.value.inherit ? null : policyForm.value.allowed_images,
      allowed_volumes: policyForm.value.inherit ? null : policyForm.value.allowed_volumes,
      allowed_networks: policyForm.value.inherit ? null : policyForm.value.allowed_networks,
      allowed_cap_add: policyForm.value.inherit ? null : policyForm.value.allowed_cap_add,
      allowed_devices: policyForm.value.inherit ? null : policyForm.value.allowed_devices,
      allowed_security_opt: policyForm.value.inherit ? null : policyForm.value.allowed_security_opt,
    })
    toast.add({ title: 'Policy saved', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to save policy', description: e?.message, color: 'error' })
  } finally {
    policySaving.value = false
  }
}



const showResetModal = ref(false)
const resettingPolicy = ref(false)

async function confirmResetPolicy() {
  resettingPolicy.value = true
  try {
    await resetWorkerPolicy(workerId)
    toast.add({ title: 'Policy reset to defaults', color: 'success' })
    showResetModal.value = false
    await loadPolicy()
  } catch (e: any) {
    toast.add({ title: 'Failed to reset policy', description: e?.message, color: 'error' })
  } finally {
    resettingPolicy.value = false
  }
}



// Load policy when tab becomes active
watch(activeTab, (val) => {
  if (val === 'policy' && !policyLoaded.value) loadPolicy()
}, { immediate: true })

// --- Auto-refresh & realtime
let refreshInterval: ReturnType<typeof setInterval>

onMounted(() => {
  refreshInterval = setInterval(() => {
    refreshWorkers()
  }, 10000)

  subscribe('workers', (e) => {
    if (e.record?.id === workerId) refreshWorkers()
  })

  subscribe('stacks', () => {
    refreshStacks()
  })

  subscribe('scheduled_jobs', () => {
    refreshWorkers()
  })

  subscribe('job_runs', () => {
    refreshWorkers()
  })
})

onUnmounted(() => {
  clearInterval(refreshInterval)
})
</script>

<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <div class="flex items-center gap-3 min-w-0">
        <UButton icon="i-lucide-arrow-left" variant="ghost" size="sm" to="/workers" />
        <div v-if="worker" class="flex items-center gap-3 min-w-0">
          <div class="relative shrink-0">
            <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10 shrink-0">
              <UIcon name="i-lucide-server" class="w-5 h-5 text-wire-400" />
            </div>
            <div
              class="absolute -bottom-1 -right-1 w-3 h-3 rounded-full border-2 border-white dark:border-carbon-950"
              :class="statusDotClass"
            />
          </div>
          <div class="min-w-0">
            <h1 class="text-xl sm:text-2xl font-bold text-gray-900 dark:text-wire-200 truncate">
              {{ worker.hostname }}
            </h1>
            <p class="text-xs font-mono text-gray-400 dark:text-wire-200/40 truncate">{{ worker.id }}</p>
          </div>
          <BadgeStatus :status="worker.status" />
        </div>
        <div v-else class="flex items-center gap-3">
          <USkeleton class="w-9 h-9 rounded-lg" />
          <USkeleton class="h-7 w-48 rounded" />
        </div>
      </div>

    </div>

    <!-- Worker not found -->
    <div v-if="allWorkers && !worker" class="text-center py-16">
      <div class="w-14 h-14 rounded-full bg-wire-400/10 border border-wire-400/20 flex items-center justify-center mx-auto mb-3">
        <UIcon name="i-lucide-server-off" class="w-7 h-7 text-wire-400" />
      </div>
      <h3 class="text-lg font-medium text-gray-900 dark:text-wire-200 mb-1">Worker not found</h3>
      <p class="text-gray-500 dark:text-wire-200/50 text-sm mb-4">This worker may have been deleted or the ID is invalid.</p>
      <UButton label="Back to Workers" to="/workers" variant="outline" />
    </div>

    <template v-else-if="worker">
      <UTabs v-model="activeTab" :items="tabs" />

      <!-- ==================== OVERVIEW ==================== -->
      <div v-if="activeTab === 'overview'" class="space-y-4">

        <!-- Status Cards -->
        <div class="grid grid-cols-2 sm:grid-cols-5 gap-3">
          <UCard class="text-center">
            <div class="text-xs text-gray-500 dark:text-wire-200/50 uppercase tracking-wide font-semibold mb-1">Status</div>
            <div class="flex items-center justify-center gap-2">
              <div class="w-2 h-2 rounded-full" :class="statusCardDotClass" />
              <span class="font-semibold text-sm text-gray-900 dark:text-wire-200">{{ worker.status }}</span>
            </div>
          </UCard>
          <UCard class="text-center">
            <div class="text-xs text-gray-500 dark:text-wire-200/50 uppercase tracking-wide font-semibold mb-1">Stacks</div>
            <span class="text-2xl font-bold text-yellow-400">{{ stacks?.length ?? '–' }}</span>
          </UCard>
          <UCard class="text-center">
            <div class="text-xs text-gray-500 dark:text-wire-200/50 uppercase tracking-wide font-semibold mb-1">Jobs</div>
            <span class="text-2xl font-bold text-yellow-400">{{ worker.job_count ?? associatedJobs.length }}</span>
          </UCard>
          <UCard class="text-center">
            <div class="text-xs text-gray-500 dark:text-wire-200/50 uppercase tracking-wide font-semibold mb-1">Last Seen</div>
            <span class="text-sm font-medium text-gray-900 dark:text-wire-200">{{ formatRelative(worker.last_seen) }}</span>
          </UCard>
          <UCard class="text-center">
            <div class="text-xs text-gray-500 dark:text-wire-200/50 uppercase tracking-wide font-semibold mb-1">Health</div>
            <div v-if="healthHistory.length" class="flex items-end justify-center gap-1 h-8">
              <UTooltip
                v-for="(entry, i) in healthHistory"
                :key="i"
                :text="`${entry.status} · ${formatDate(entry.timestamp)}`"
              >
                <div
                  class="w-1.5 rounded-sm min-h-[4px] cursor-default transition-all"
                  :class="[
                    healthDotClass(entry.status),
                    entry.status === 'online' ? 'h-full' : 'h-1/3 opacity-70'
                  ]"
                />
              </UTooltip>
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-wire-200/40">No data</span>
          </UCard>
        </div>

        <!-- System Info & Worker Information -->
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 items-stretch">
          <WorkerSystemInfoCard :worker="worker" class="h-full" />

          <UCard class="h-full">
            <template #header>
              <div class="flex items-center gap-2">
                <h3 class="font-semibold">Worker Information</h3>
                <WorkerVersionBadge :version="worker.version" size="sm" />
              </div>
            </template>
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
              <div>
                <span class="text-gray-500 dark:text-wire-200/50">Hostname</span>
                <div class="mt-0.5 font-medium text-gray-900 dark:text-wire-200">{{ worker.hostname }}</div>
              </div>
              <div>
                <span class="text-gray-500 dark:text-wire-200/50">Worker ID</span>
                <div class="mt-0.5 flex items-center gap-2">
                  <code class="text-xs font-mono text-gray-700 dark:text-wire-200/80 bg-gray-100 dark:bg-carbon-800 px-1.5 py-0.5 rounded break-all">{{ worker.id }}</code>
                  <UButton icon="i-lucide-copy" variant="ghost" size="xs" color="neutral" @click="copy(worker.id, 'Worker ID')" />
                </div>
              </div>
              <div v-if="worker.fingerprint">
                <span class="text-gray-500 dark:text-wire-200/50">Fingerprint</span>
                <div class="mt-0.5 flex items-center gap-2">
                  <code class="text-xs font-mono text-gray-700 dark:text-wire-200/80 bg-gray-100 dark:bg-carbon-800 px-1.5 py-0.5 rounded break-all">{{ worker.fingerprint }}</code>
                  <UButton icon="i-lucide-copy" variant="ghost" size="xs" color="neutral" @click="copy(worker.fingerprint, 'Fingerprint')" />
                </div>
              </div>
              <div>
                <span class="text-gray-500 dark:text-wire-200/50">Last Seen</span>
                <div class="mt-0.5 font-medium text-gray-900 dark:text-wire-200">
                  {{ formatDate(worker.last_seen) }}
                </div>
              </div>
              <div v-if="hasVisibleTokenExpiry(worker)">
                <span class="text-gray-500 dark:text-wire-200/50">Token Expires</span>
                <div class="mt-0.5 font-medium text-gray-900 dark:text-wire-200">{{ formatDateISO(worker.token_expires) }}</div>
              </div>
              <div v-if="worker.token_last_used && !worker.token_last_used.startsWith('0001')">
                <span class="text-gray-500 dark:text-wire-200/50">Token Last Used</span>
                <div class="mt-0.5 font-medium text-gray-900 dark:text-wire-200">{{ formatDate(worker.token_last_used) }}</div>
              </div>
            </div>

            <!-- Tags -->
            <div v-if="worker.tags?.length" class="mt-4 pt-4 border-t border-gray-200 dark:border-carbon-700">
              <span class="text-xs text-gray-500 dark:text-wire-200/50 uppercase tracking-wide font-semibold block mb-2">Tags</span>
              <div class="flex flex-wrap gap-1.5">
                <UBadge
                  v-for="tag in worker.tags"
                  :key="tag"
                  :label="tag"
                  variant="subtle"
                  color="neutral"
                  size="sm"
                  class="font-mono"
                />
              </div>
            </div>
          </UCard>
        </div>

        <!-- Danger Zone -->
        <DangerZoneCard v-if="worker && !isRevoked" v-model:open="showDangerZone" :actions="dangerZoneActions" />
      </div>

      <!-- ==================== STACKS ==================== -->
      <div v-if="activeTab === 'stacks'" class="space-y-4">
        <UCard>
          <template #header>
            <div class="flex items-center justify-between">
              <h3 class="font-semibold">
                Assigned Stacks
                <span v-if="stacks?.length" class="ml-1.5 text-yellow-400">({{ stacks.length }})</span>
              </h3>
              <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" color="neutral" @click="refreshStacks()" />
            </div>
          </template>

          <div v-if="!stacks" class="space-y-3">
            <USkeleton v-for="i in 3" :key="i" class="h-14 w-full" />
          </div>
          <div v-else-if="stacks.length === 0" class="text-center py-10">
            <div class="w-12 h-12 rounded-full bg-gray-100 dark:bg-carbon-800 flex items-center justify-center mx-auto mb-3">
              <UIcon name="i-lucide-layers" class="w-6 h-6 text-gray-400" />
            </div>
            <p class="text-sm text-gray-500 dark:text-wire-200/50">No stacks assigned to this worker.</p>
          </div>
          <div v-else class="divide-y divide-gray-100 dark:divide-carbon-800">
            <NuxtLink
              v-for="stack in stacks"
              :key="stack.id"
              :to="`/stacks/${stack.id}`"
              target="_blank"
              rel="noopener noreferrer"
              class="flex items-center justify-between py-3 px-1 hover:bg-gray-50 dark:hover:bg-carbon-800/40 rounded-lg transition-colors group"
            >
              <div class="flex items-center gap-3 min-w-0">
                <div class="w-8 h-8 rounded-lg bg-yellow-400/10 flex items-center justify-center shrink-0">
                  <UIcon name="i-lucide-layers" class="w-4 h-4 text-yellow-400" />
                </div>
                <div class="min-w-0">
                  <p class="font-medium text-sm text-gray-900 dark:text-wire-200 truncate group-hover:text-yellow-500 transition-colors">{{ stack.name }}</p>
                  <p class="text-xs font-mono text-gray-400 dark:text-wire-200/40 truncate">{{ stack.id }}</p>
                </div>
              </div>
              <UIcon name="i-lucide-chevron-right" class="w-4 h-4 text-gray-300 dark:text-carbon-600 group-hover:text-yellow-400 transition-colors shrink-0 ml-3" />
            </NuxtLink>
          </div>
        </UCard>
      </div>

      <!-- ==================== JOBS ==================== -->
      <div v-if="activeTab === 'jobs'" class="space-y-4">
        <UCard>
          <template #header>
            <div class="flex items-center justify-between">
              <h3 class="font-semibold">
                Associated Jobs
                <span v-if="associatedJobs.length" class="ml-1.5 text-yellow-400">({{ associatedJobs.length }})</span>
              </h3>
              <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" color="neutral" @click="refreshWorkers()" />
            </div>
          </template>

          <div v-if="associatedJobs.length === 0" class="text-center py-10">
            <div class="w-12 h-12 rounded-full bg-gray-100 dark:bg-carbon-800 flex items-center justify-center mx-auto mb-3">
              <UIcon name="i-lucide-calendar-clock" class="w-6 h-6 text-gray-400" />
            </div>
            <p class="text-sm text-gray-500 dark:text-wire-200/50">No jobs associated with this worker.</p>
          </div>
          <div v-else class="divide-y divide-gray-100 dark:divide-carbon-800">
            <NuxtLink
              v-for="job in associatedJobs"
              :key="job.id"
              :to="`/jobs/${job.id}`"
              target="_blank"
              rel="noopener noreferrer"
              class="flex items-center justify-between py-3 px-1 hover:bg-gray-50 dark:hover:bg-carbon-800/40 rounded-lg transition-colors group"
            >
              <div class="flex items-center gap-3 min-w-0">
                <div class="w-8 h-8 rounded-lg bg-yellow-400/10 flex items-center justify-center shrink-0">
                  <UIcon name="i-lucide-calendar-clock" class="w-4 h-4 text-yellow-400" />
                </div>
                <div class="min-w-0">
                  <p class="font-medium text-sm text-gray-900 dark:text-wire-200 truncate group-hover:text-yellow-500 transition-colors">{{ job.name }}</p>
                  <p class="text-xs font-mono text-gray-400 dark:text-wire-200/40 truncate">{{ job.id }}</p>
                  <div v-if="job.common_tags?.length" class="flex flex-wrap gap-1 mt-1">
                    <UBadge v-for="tag in job.common_tags" :key="tag" :label="tag" variant="subtle" color="neutral" size="xs" class="font-mono" />
                  </div>
                </div>
              </div>
              <UIcon name="i-lucide-chevron-right" class="w-4 h-4 text-gray-300 dark:text-carbon-600 group-hover:text-yellow-400 transition-colors shrink-0 ml-3" />
            </NuxtLink>
          </div>
        </UCard>
      </div>

      <!-- ==================== POLICY ==================== -->
      <div v-if="activeTab === 'policy'" class="space-y-4">
        <div v-if="policyLoading" class="py-10 text-center bg-white dark:bg-carbon-900 rounded-xl border border-gray-200 dark:border-carbon-800">
          <UIcon name="i-lucide-loader-circle" class="w-6 h-6 text-gray-400 animate-spin mx-auto" />
          <p class="text-sm text-gray-400 mt-2">Loading policy…</p>
        </div>
        <template v-else>
          <!-- Global Disabled Alert banner -->
          <div v-if="!isGlobalPolicyEnabled" class="py-2">
            <UCard class="border-amber-500/20 bg-amber-500/5 dark:bg-amber-950/10">
              <div class="flex flex-col items-center text-center py-8 space-y-4">
                <div class="w-16 h-16 rounded-full bg-amber-500/10 border border-amber-500/20 flex items-center justify-center animate-pulse">
                  <UIcon name="i-lucide-shield-alert" class="w-8 h-8 text-amber-500" />
                </div>
                <div class="space-y-2 max-w-md">
                  <h3 class="text-lg font-semibold text-gray-900 dark:text-wire-100">Worker Policy System Disabled</h3>
                  <p class="text-sm text-gray-500 dark:text-gray-400">
                    The security policy enforcement system is currently disabled globally. Workers will not validate images, volumes, or networks during stack reconciliation or job runs.
                  </p>
                </div>
                <UButton
                  to="/settings?tab=worker-policies"
                  label="Enable Policy System"
                  icon="i-lucide-external-link"
                  color="warning"
                  variant="solid"
                  class="font-medium shadow-md shadow-yellow-500/10 hover:shadow-yellow-500/20"
                />
              </div>
            </UCard>
          </div>

          <div v-else class="space-y-4">
            <!-- Reset Header Row -->
            <div class="flex items-center justify-between pb-2">
              <div class="flex items-center gap-2">
                <UIcon name="i-lucide-shield-check" class="w-5 h-5 text-yellow-400" />
                <h3 class="font-semibold text-base text-gray-900 dark:text-wire-200">Worker Policy Overrides</h3>
              </div>
              <UButton
                label="Reset to Defaults"
                icon="i-lucide-rotate-ccw"
                variant="ghost"
                color="neutral"
                size="sm"
                :disabled="policyLoading"
                @click="showResetModal = true"
              />
            </div>

            <WorkerPolicyForm v-model="policyForm" @save="handleSavePolicy" />
          </div>
        </template>
      </div>
    </template>

    <!-- Reset Policy Confirmation Modal -->
    <UModal v-model:open="showResetModal">
      <template #content>
        <ResetPolicyModal
          :loading="resettingPolicy"
          @confirm="confirmResetPolicy"
          @cancel="showResetModal = false"
        />
      </template>
    </UModal>



    <!-- Revoke Confirmation Modal -->
    <UModal v-model:open="showRevokeModal">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-ban" class="w-5 h-5 text-red-500" />
              <h2 class="font-semibold text-red-500">Revoke Worker</h2>
            </div>
          </template>
          <div class="space-y-3 text-sm text-gray-500 dark:text-wire-200/60">
            <p>
              Are you sure you want to revoke
              <span class="font-semibold text-gray-900 dark:text-wire-200">{{ worker?.hostname }}</span>?
            </p>
            <p class="text-xs">This worker will be disconnected and its token invalidated.</p>
            <p class="text-xs text-red-500 font-medium">This action cannot be undone.</p>
          </div>
          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton label="Cancel" variant="outline" color="neutral" @click="showRevokeModal = false" />
              <UButton
                label="Revoke Worker"
                color="error"
                icon="i-lucide-ban"
                :loading="revoking"
                @click="confirmRevoke"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>
  </div>
</template>
