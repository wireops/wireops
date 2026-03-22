<script setup lang="ts">

const { getWorkers, createWorkerSeat, renewWorkerCert, forceRebootstrapWorker } = useApi()
const toast = useToast()

const { data: workers, pending, refresh } = useAsyncData('workers', getWorkers)

const seatToken = ref('')
const isGenerating = ref(false)
const selectedRevokeWorker = ref<any>(null)
const showRevoked = ref(false)

// Sort: active/inactive first, revoked last
const sortedWorkers = computed(() => {
  if (!workers.value) return []
  return [...workers.value].sort((a, b) => {
    if (a.status === 'REVOKED' && b.status !== 'REVOKED') return 1
    if (a.status !== 'REVOKED' && b.status === 'REVOKED') return -1
    return 0
  })
})

const revokedCount = computed(() => workers.value?.filter(a => a.status === 'REVOKED').length ?? 0)
const activeCount = computed(() => workers.value?.filter(a => a.status === 'ACTIVE').length ?? 0)

const visibleWorkers = computed(() =>
  showRevoked.value ? sortedWorkers.value : sortedWorkers.value.filter(a => a.status !== 'REVOKED')
)

function promptRevoke(worker: any) {
  selectedRevokeWorker.value = worker
}

function onWorkerRevoked() {
  selectedRevokeWorker.value = null
  refresh()
}

async function generateSeat() {
  isGenerating.value = true
  seatToken.value = ''
  try {
    const res = await createWorkerSeat()
    seatToken.value = res.seat
    toast.add({ title: 'Seat generated successfully', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to generate seat', description: e?.message, color: 'error' })
  } finally {
    isGenerating.value = false
  }
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text)
  toast.add({ title: 'Copied!', color: 'success' })
}

function formatDate(dateStr: string) {
  if (!dateStr) return 'Never'
  try {
    return new Date(dateStr).toLocaleString()
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

function certStatusColor(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  switch (status) {
    case 'ok': return 'success'
    case 'warning': return 'warning'
    case 'critical':
    case 'expired': return 'error'
    default: return 'neutral'
  }
}

async function handleRenewCert(worker: any) {
  try {
    await renewWorkerCert(worker.id)
    toast.add({ title: 'Renewal requested', description: `Sent renewal signal to ${worker.hostname}`, color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to request renewal', description: e?.message, color: 'error' })
  }
}

async function handleForceRebootstrap(worker: any) {
  if (!window.confirm(`Force re-bootstrap for ${worker.hostname}? This will revoke the current certificate and disconnect the worker.`)) return
  try {
    await forceRebootstrapWorker(worker.id)
    toast.add({ title: 'Re-bootstrap initiated', description: `${worker.hostname} has been disconnected. Provide a new seat token to reconnect.`, color: 'success' })
    refresh()
  } catch (e: any) {
    toast.add({ title: 'Failed', description: e?.message, color: 'error' })
  }
}

const isAutoRefreshPaused = ref(false)
let refreshInterval: any

onMounted(() => {
  refreshInterval = setInterval(() => {
    if (!isAutoRefreshPaused.value) {
      refresh()
    }
  }, 10000)
})

onUnmounted(() => {
  if (refreshInterval) {
    clearInterval(refreshInterval)
  }
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
      <UButton icon="i-lucide-zap" label="Add Worker" :loading="isGenerating" class="shadow-[0_0_16px_rgba(255,198,0,0.35)] hover:shadow-[0_0_24px_rgba(255,198,0,0.55)] transition-shadow" @click="generateSeat" />
    </div>

    <!-- New Seat Token Card -->
    <UCard v-if="seatToken" class="border border-yellow-400/30 shadow-[0_0_24px_rgba(255,198,0,0.08)]">
      <template #header>
        <div class="flex items-center gap-2 text-yellow-400 font-semibold">
          <UIcon name="i-lucide-key" class="w-4 h-4" />
          <span>New Worker Seat Generated</span>
        </div>
      </template>
      <p class="text-sm text-gray-500 dark:text-wire-200/60 mb-4">
        Use the following Bootstrap Token to configure a new wireops worker. This token expires in 15 minutes.
      </p>
      <div class="flex items-center gap-2 bg-gray-100 dark:bg-carbon-800/60 p-2 rounded-lg border border-gray-200 dark:border-carbon-700 break-all">
        <code class="text-sm font-mono flex-1 select-all text-wire-400">{{ seatToken }}</code>
        <UButton
          icon="i-lucide-copy"
          variant="ghost"
          color="neutral"
          size="sm"
          @click="copyToClipboard(seatToken)"
        />
      </div>
      <div class="mt-4 pt-4 border-t border-gray-100 dark:border-carbon-800">
        <p class="text-xs font-semibold mb-2 uppercase text-gray-400 dark:text-wire-200/40 tracking-wider">Example Command (Docker)</p>
        <pre class="bg-gray-900 dark:bg-carbon-950 text-wire-400/80 p-3 rounded-lg text-xs overflow-x-auto font-mono border border-gray-700 dark:border-carbon-800">docker run -d \
  -e WIREOPS_SERVER=https://your-wireops-server:8090 \
  -e WIREOPS_MTLS_SERVER=https://your-wireops-server:8443 \
  -e WIREOPS_BOOTSTRAP_TOKEN={{ seatToken }} \
  -v /var/lib/wireops/worker_pki:/var/lib/wireops/pki \
  ghcr.io/wireops/wireops-worker:latest</pre>
      </div>
    </UCard>

    <!-- Connected Workers -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">
            Connected Workers
            <span v-if="activeCount > 0" class="ml-1.5 text-yellow-400">({{ activeCount }})</span>
          </h3>
          <div class="flex items-center gap-3">
            <div v-if="revokedCount > 0" class="flex items-center gap-2">
              <span class="text-xs text-gray-400 dark:text-wire-200/40">Show revoked ({{ revokedCount }})</span>
              <USwitch v-model="showRevoked" size="xs" />
            </div>
            <UTooltip :text="isAutoRefreshPaused ? 'Resume auto-refresh' : 'Pause auto-refresh'">
              <UButton
                :icon="isAutoRefreshPaused ? 'i-lucide-play' : 'i-lucide-pause'"
                variant="ghost"
                size="xs"
                color="neutral"
                @click="isAutoRefreshPaused = !isAutoRefreshPaused"
              />
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

      <div v-else-if="!workers || workers.length === 0" class="text-center py-12">
        <div class="w-14 h-14 rounded-full bg-wire-400/10 border border-wire-400/20 flex items-center justify-center mx-auto mb-3">
          <UIcon name="i-lucide-network" class="w-7 h-7 text-wire-400" />
        </div>
        <h3 class="text-lg font-medium text-gray-900 dark:text-wire-200 mb-1">No workers connected</h3>
        <p class="text-gray-500 dark:text-wire-200/50 text-sm">Generate a new seat to connect your first worker.</p>
      </div>

      <div v-else class="space-y-3">
        <div
          v-for="worker in visibleWorkers"
          :key="worker.id"
          class="flex items-center justify-between p-4 bg-gray-50 dark:bg-carbon-800/40 rounded-xl border border-gray-200 dark:border-carbon-700 hover:shadow-[0_0_0_2px_rgba(255,198,0,0.35),0_0_20px_rgba(255,198,0,0.12)] transition-all"
          :class="worker.status === 'REVOKED' ? 'opacity-50' : ''"
        >
          <div class="flex items-center gap-4">
            <div class="relative">
              <div class="w-10 h-10 rounded-lg bg-gray-100 dark:bg-carbon-700/60 flex items-center justify-center">
                <UIcon name="i-lucide-server" class="w-5 h-5 text-wire-400" />
              </div>
              <div
                class="absolute -bottom-1 -right-1 w-3 h-3 rounded-full"
                :class="[
                  worker.status === 'ACTIVE'
                    ? 'bg-yellow-400 shadow-[0_0_8px_rgba(255,198,0,0.7)]'
                    : worker.status === 'REVOKED'
                      ? 'bg-gray-400'
                      : 'bg-red-500 shadow-[0_0_6px_rgba(239,68,68,0.6)]'
                ]"
              />
            </div>
            <div>
              <div class="flex items-center gap-2">
                <h3 class="font-medium text-gray-900 dark:text-wire-200">{{ worker.hostname }}</h3>
                <BadgeStatus :status="worker.status" />
              </div>
              <div class="hidden sm:flex items-center gap-2 mt-1">
                <p class="text-xs text-gray-400 dark:text-wire-200/40 font-mono w-32 truncate" :title="worker.id">
                  ID: {{ worker.id }}
                </p>
                <span class="text-gray-300 dark:text-carbon-700 text-xs">•</span>
                <p class="text-xs text-gray-400 dark:text-wire-200/40 font-mono w-48 truncate" :title="worker.fingerprint">
                  Cert: {{ worker.fingerprint.substring(0, 16) }}...
                </p>
                <template v-if="worker.cert_not_after && worker.fingerprint !== 'embedded'">
                  <span class="text-gray-300 dark:text-carbon-700 text-xs">•</span>
                  <UBadge v-if="worker.cert_status" :label="`cert: ${worker.cert_status}`" :color="certStatusColor(worker.cert_status)" size="xs" variant="subtle" />
                </template>
              </div>
              <div v-if="worker.tags?.length" class="flex flex-wrap items-center gap-1 mt-1.5">
                <UBadge
                  v-for="tag in worker.tags"
                  :key="tag"
                  :label="tag"
                  variant="subtle"
                  color="neutral"
                  size="xs"
                  class="font-mono"
                />
              </div>
            </div>
          </div>
          <div class="flex items-center gap-10">
            <div class="hidden sm:flex flex-col items-end gap-1">
              <p class="text-xs font-semibold text-gray-400 dark:text-wire-200/40 uppercase tracking-wider">Health History</p>
              <div class="flex items-center gap-1 mt-1 justify-end w-28">
                <template v-for="(_, idx) in Array(Math.max(0, 10 - (worker.health_history?.length || 0))).fill(null)" :key="'empty'+idx">
                  <div class="w-2 h-2 bg-gray-200 dark:bg-carbon-700 rounded-full" />
                </template>
                <template v-for="(event, idx) in worker.health_history" :key="'evt'+idx">
                  <UTooltip :text="`${event.status} at ${formatDate(event.timestamp)}`" placement="top">
                    <div
                      class="w-2 h-2 rounded-full cursor-help transition-all hover:scale-125"
                      :class="event.status === 'online'
                        ? 'bg-wire-400 dark:bg-yellow-400 shadow-[0_0_4px_rgba(93,168,255,0.8)] dark:shadow-[0_0_4px_rgba(255,198,0,0.7)]'
                        : 'bg-red-500 shadow-[0_0_4px_rgba(239,68,68,0.7)]'"
                    />
                  </UTooltip>
                </template>
              </div>
            </div>
            <div class="text-right">
              <p class="text-xs font-semibold text-gray-400 dark:text-wire-200/40 uppercase tracking-wider">Last seen</p>
              <p class="text-sm text-gray-900 dark:text-wire-200">{{ formatRelative(worker.last_seen) }}</p>
            </div>
            <div class="ml-2 border-l border-gray-200 dark:border-carbon-700 pl-4 flex items-center gap-1">
              <UTooltip v-if="worker.status === 'ACTIVE' && worker.fingerprint !== 'embedded'" text="Renew Certificate">
                <UButton
                  icon="i-lucide-refresh-cw"
                  variant="ghost"
                  color="neutral"
                  size="xs"
                  @click="handleRenewCert(worker)"
                />
              </UTooltip>
              <UTooltip v-if="worker.fingerprint !== 'embedded' && worker.status !== 'REVOKED'" text="Force Re-bootstrap">
                <UButton
                  icon="i-lucide-shield-off"
                  variant="ghost"
                  color="warning"
                  size="xs"
                  @click="handleForceRebootstrap(worker)"
                />
              </UTooltip>
              <UTooltip :text="worker.status === 'REVOKED' ? 'Worker already revoked' : worker.fingerprint === 'embedded' ? 'Embedded worker cannot be revoked' : 'Revoke Worker'">
                <UButton
                  :icon="worker.status === 'REVOKED' ? 'i-lucide-x' : 'i-lucide-trash-2'"
                  :color="worker.status === 'REVOKED' ? 'neutral' : 'error'"
                  variant="ghost"
                  size="xs"
                  :disabled="worker.status === 'REVOKED' || worker.fingerprint === 'embedded'"
                  :class="(worker.status === 'REVOKED' || worker.fingerprint === 'embedded') ? 'opacity-30 cursor-not-allowed' : ''"
                  @click="worker.status !== 'REVOKED' && worker.fingerprint !== 'embedded' && promptRevoke(worker)"
                />
              </UTooltip>
            </div>
          </div>
        </div>
      </div>
    </UCard>
  </div>

  <UModal :open="!!selectedRevokeWorker" @update:open="(val) => { if (!val) selectedRevokeWorker = null }">
    <template #content>
      <WorkerDeleteModal
        v-if="selectedRevokeWorker"
        :worker="selectedRevokeWorker"
        @cancel="selectedRevokeWorker = null"
        @revoked="onWorkerRevoked"
      />
    </template>
  </UModal>
</template>
