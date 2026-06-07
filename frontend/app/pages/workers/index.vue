<script setup lang="ts">
const { getWorkers, createWorkerToken } = useApi()
const toast = useToast()

const { data: workers, pending, refresh } = useAsyncData('workers', getWorkers)

const issuedToken = ref('')
const issuedTokenExpiresAt = ref('')
const showTokenModal = ref(false)
const isGenerating = ref(false)
const showRevoked = ref(false)
const isAutoRefreshPaused = ref(false)

const actualWorkers = computed(() => {
  if (!workers.value) return []
  return workers.value.filter(w => w.status !== WORKER_STATUS.PENDING)
})

const sortedWorkers = computed(() => {
  return [...actualWorkers.value].sort((a, b) => {
    if (workerStatus(a) === WORKER_STATUS.REVOKED && workerStatus(b) !== WORKER_STATUS.REVOKED) return 1
    if (workerStatus(a) !== WORKER_STATUS.REVOKED && workerStatus(b) === WORKER_STATUS.REVOKED) return -1
    return 0
  })
})

const revokedCount = computed(() => actualWorkers.value.filter(a => workerStatus(a) === WORKER_STATUS.REVOKED).length)
const activeCount = computed(() => actualWorkers.value.filter(a => workerStatus(a) === WORKER_STATUS.ACTIVE || workerStatus(a) === WORKER_STATUS.OFFLINE).length)

const visibleWorkers = computed(() =>
  showRevoked.value ? sortedWorkers.value : sortedWorkers.value.filter(a => workerStatus(a) !== WORKER_STATUS.REVOKED)
)

function workerStatus(worker: any) {
  return String(worker?.status || '').toUpperCase()
}

function isWorkerClickable(worker: any) {
  return workerStatus(worker) !== WORKER_STATUS.REVOKED
}

function openWorker(worker: any) {
  if (!isWorkerClickable(worker)) return
  navigateTo(`/workers/${worker.id}`)
}

async function generateToken() {
  isGenerating.value = true
  issuedToken.value = ''
  try {
    const res = await createWorkerToken()
    issuedToken.value = res.token
    issuedTokenExpiresAt.value = res.expires_at
    showTokenModal.value = true
    toast.add({ title: 'Worker token generated', color: 'success' })
    refresh()
  } catch (e: any) {
    toast.add({ title: 'Failed to generate token', description: e?.message, color: 'error' })
  } finally {
    isGenerating.value = false
  }
}

function formatDate(dateStr: string) {
  if (!dateStr) return 'Never'
  try {
    return new Date(dateStr).toISOString()
  } catch {
    return dateStr
  }
}

function formatRelative(dateStr: string) {
  if (!dateStr) return 'Never'
  const diff = Date.now() - new Date(dateStr).getTime()
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return `${Math.floor(diff / 86_400_000)}d ago`
}

function hasVisibleTokenExpiry(worker: any) {
  if (!worker?.token_expires) return false
  if (worker.token_status === TOKEN_STATUS.ACTIVE) return false
  if (worker.token_expires.startsWith('0001-01-01')) return false
  return true
}

const workerBootstrapCommand = computed(() =>
  `docker run -d \\
  -e WIREOPS_SERVER=https://your-wireops-server.local \\
  -e WIREOPS_WORKER_TOKEN=${issuedToken.value} \\
  ghcr.io/wireops/wireops-worker:latest`
)



let refreshInterval: any

onMounted(() => {
  refreshInterval = setInterval(() => {
    if (!isAutoRefreshPaused.value) refresh()
  }, 10000)
})

onUnmounted(() => {
  if (refreshInterval) clearInterval(refreshInterval)
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="flex items-center gap-3 text-2xl font-bold text-gray-900 dark:text-wire-200">
        <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
          <UIcon name="i-lucide-network" class="w-5 h-5 text-yellow-400" />
        </div>
        Workers
      </h1>
      <UButton icon="i-lucide-key-round" label="Generate Token" :loading="isGenerating" class="shadow-[0_0_16px_rgba(255,198,0,0.35)] hover:shadow-[0_0_24px_rgba(255,198,0,0.55)] transition-shadow" @click="generateToken" />
    </div>

    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">
            Workers
            <span v-if="activeCount > 0" class="ml-1.5 text-yellow-400">({{ activeCount }})</span>
          </h3>
          <div class="flex items-center gap-3">
            <div v-if="revokedCount > 0" class="flex items-center gap-2">
              <span class="text-xs text-gray-400 dark:text-wire-200/40">Show revoked ({{ revokedCount }})</span>
              <USwitch v-model="showRevoked" size="xs" />
            </div>
            <UTooltip :text="isAutoRefreshPaused ? 'Resume auto-refresh' : 'Pause auto-refresh'">
              <UButton :icon="isAutoRefreshPaused ? 'i-lucide-play' : 'i-lucide-pause'" variant="ghost" size="xs" color="neutral" @click="isAutoRefreshPaused = !isAutoRefreshPaused" />
            </UTooltip>
            <UTooltip text="Refresh manually">
              <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" color="neutral" :loading="pending" @click="() => refresh()" />
            </UTooltip>
          </div>
        </div>
      </template>

      <div v-if="pending && !workers" class="space-y-4">
        <USkeleton v-for="i in 3" :key="i" class="h-16 w-full" />
      </div>

      <div v-else-if="!workers || visibleWorkers.length === 0" class="text-center py-12">
        <div class="w-14 h-14 rounded-full bg-wire-400/10 border border-wire-400/20 flex items-center justify-center mx-auto mb-3">
          <UIcon name="i-lucide-network" class="w-7 h-7 text-wire-400" />
        </div>
        <h3 class="text-lg font-medium text-gray-900 dark:text-wire-200 mb-1">No active workers</h3>
        <p class="text-gray-500 dark:text-wire-200/50 text-sm">Once a worker registers, it will appear here.</p>
      </div>

      <div v-else class="space-y-3">
        <div
          v-for="worker in visibleWorkers"
          :key="worker.id"
          class="flex items-center gap-4 p-4 bg-gray-50 dark:bg-carbon-800/40 rounded-xl border border-gray-200 dark:border-carbon-700 transition-all"
          :class="[
            workerStatus(worker) === WORKER_STATUS.REVOKED ? 'opacity-50' : '',
            isWorkerClickable(worker) ? 'cursor-pointer hover:shadow-[0_0_0_2px_rgba(255,198,0,0.35),0_0_20px_rgba(255,198,0,0.12)]' : 'cursor-default'
          ]"
          :role="isWorkerClickable(worker) ? 'link' : undefined"
          :tabindex="isWorkerClickable(worker) ? 0 : undefined"
          :aria-disabled="isWorkerClickable(worker) ? undefined : 'true'"
          @click="openWorker(worker)"
          @keydown.enter="openWorker(worker)"
          @keydown.space.prevent="openWorker(worker)"
        >
          <div class="relative shrink-0">
            <div class="w-10 h-10 rounded-lg bg-gray-100 dark:bg-carbon-700/60 flex items-center justify-center">
              <UIcon name="i-lucide-server" class="w-5 h-5 text-wire-400" />
            </div>
            <div
              class="absolute -bottom-1 -right-1 w-3 h-3 rounded-full"
              :class="[
                workerStatus(worker) === WORKER_STATUS.ACTIVE
                  ? 'bg-yellow-400 shadow-[0_0_8px_rgba(255,198,0,0.7)]'
                  : workerStatus(worker) === WORKER_STATUS.REVOKED
                    ? 'bg-gray-400'
                    : 'bg-red-500 shadow-[0_0_6px_rgba(239,68,68,0.6)]'
              ]"
            />
          </div>
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <h3 class="font-medium text-gray-900 dark:text-wire-200">{{ worker.hostname }}</h3>
              <BadgeStatus :status="worker.status" />
            </div>
            <div class="hidden sm:flex items-center gap-2 mt-1">
              <p class="text-xs text-gray-400 dark:text-wire-200/40 font-mono w-36 truncate" :title="worker.id">
                ID: {{ worker.id }}
              </p>
              <span class="text-gray-300 dark:text-carbon-700 text-xs">•</span>
              <p class="text-xs text-gray-400 dark:text-wire-200/40">
                Last seen: {{ formatRelative(worker.last_seen) }}
              </p>
              <span class="text-gray-300 dark:text-carbon-700 text-xs">•</span>
              <p class="text-xs text-gray-400 dark:text-wire-200/40">
                Jobs: {{ worker.job_count ?? worker.jobs?.length ?? 0 }}
              </p>
              <template v-if="hasVisibleTokenExpiry(worker)">
                <span class="text-gray-300 dark:text-carbon-700 text-xs">•</span>
                <p class="text-xs text-gray-400 dark:text-wire-200/40">
                  Token expires: {{ formatDate(worker.token_expires) }}
                </p>
              </template>
            </div>
            <div v-if="worker.tags?.length" class="flex flex-wrap items-center gap-1 mt-1.5">
              <UBadge v-for="tag in worker.tags" :key="tag" :label="tag" variant="subtle" color="neutral" size="xs" class="font-mono" />
            </div>
          </div>
        </div>
      </div>
    </UCard>

    <UModal v-model:open="showTokenModal" :ui="{ content: 'sm:max-w-4xl' }">
      <template #content>
        <UCard v-if="issuedToken" class="w-full">
          <template #header>
            <div class="flex items-center gap-2 text-yellow-400 font-semibold">
              <UIcon name="i-lucide-key" class="w-4 h-4" />
              <span>New Worker Token</span>
            </div>
          </template>

          <div class="flex w-full flex-col gap-4">
            <p class="text-sm text-gray-500 dark:text-wire-200/60">
              This token is valid until <strong>{{ formatDate(issuedTokenExpiresAt) }}</strong>.
            </p>
            <ExecutableCommand label="Token" :content="issuedToken" />
            <ExecutableCommand label="Executable Command" :content="workerBootstrapCommand" button-label="Copy Command" multiline />
          </div>

          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton label="Close" variant="outline" @click="showTokenModal = false" />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>


  </div>
</template>
