<script setup lang="ts">
const { getWorkers, createWorkerToken, revokeWorker } = useApi()
const toast = useToast()

const { data: workers, pending, refresh } = useAsyncData('workers', getWorkers)

const issuedToken = ref('')
const issuedTokenStatus = ref('')
const issuedTokenExpiresAt = ref('')
const isGenerating = ref(false)
const showRevoked = ref(false)
const isAutoRefreshPaused = ref(false)

const actualWorkers = computed(() => {
  if (!workers.value) return []
  return workers.value.filter(w => w.status !== WORKER_STATUS.PENDING)
})

const pendingTokens = computed(() => {
  if (!workers.value) return []
  return workers.value.filter(w => w.status === WORKER_STATUS.PENDING)
})

const sortedWorkers = computed(() => {
  return [...actualWorkers.value].sort((a, b) => {
    if (a.status === WORKER_STATUS.REVOKED && b.status !== WORKER_STATUS.REVOKED) return 1
    if (a.status !== WORKER_STATUS.REVOKED && b.status === WORKER_STATUS.REVOKED) return -1
    return 0
  })
})

const revokedCount = computed(() => actualWorkers.value.filter(a => a.status === WORKER_STATUS.REVOKED).length)
const activeCount = computed(() => actualWorkers.value.filter(a => a.status === WORKER_STATUS.ACTIVE || a.status === WORKER_STATUS.OFFLINE).length)

const visibleWorkers = computed(() =>
  showRevoked.value ? sortedWorkers.value : sortedWorkers.value.filter(a => a.status !== WORKER_STATUS.REVOKED)
)

async function generateToken() {
  isGenerating.value = true
  issuedToken.value = ''
  try {
    const res = await createWorkerToken()
    issuedToken.value = res.token
    issuedTokenStatus.value = res.status
    issuedTokenExpiresAt.value = res.expires_at
    toast.add({ title: 'Worker token generated', color: 'success' })
    refresh()
  } catch (e: any) {
    toast.add({ title: 'Failed to generate token', description: e?.message, color: 'error' })
  } finally {
    isGenerating.value = false
  }
}

async function handleRevoke(worker: any) {
  if (worker.is_embedded) return
  const isPending = worker.status === WORKER_STATUS.PENDING
  const confirmMessage = isPending
    ? 'Revoke this pending worker token?'
    : `Revoke ${worker.hostname}?`
  if (!window.confirm(confirmMessage)) return
  try {
    await revokeWorker(worker.id)
    toast.add({ title: isPending ? 'Token revoked' : 'Worker revoked', color: 'success' })
    refresh()
  } catch (e: any) {
    toast.add({ title: isPending ? 'Failed to revoke token' : 'Failed to revoke worker', description: e?.message, color: 'error' })
  }
}

async function copyToClipboard(text: string) {
  await navigator.clipboard.writeText(text)
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

    <UCard v-if="issuedToken" class="border border-yellow-400/30 shadow-[0_0_24px_rgba(255,198,0,0.08)]">
      <template #header>
        <div class="flex items-center gap-2 text-yellow-400 font-semibold">
          <UIcon name="i-lucide-key" class="w-4 h-4" />
          <span>New Worker Token</span>
        </div>
      </template>
      <p class="text-sm text-gray-500 dark:text-wire-200/60 mb-4">
        This token is in <strong>{{ issuedTokenStatus }}</strong> for 1 hour. It becomes <strong>ACTIVE</strong> when the worker connects for the first time.
      </p>
      <div class="flex items-center gap-2 bg-gray-100 dark:bg-carbon-800/60 p-2 rounded-lg border border-gray-200 dark:border-carbon-700 break-all">
        <code class="text-sm font-mono flex-1 select-all text-wire-400">{{ issuedToken }}</code>
        <UButton icon="i-lucide-copy" variant="ghost" color="neutral" size="sm" @click="copyToClipboard(issuedToken)" />
      </div>
      <p class="mt-3 text-xs text-gray-400">Expires: {{ formatDate(issuedTokenExpiresAt) }}</p>
      <div class="mt-4 pt-4 border-t border-gray-100 dark:border-carbon-800">
        <p class="text-xs font-semibold mb-2 uppercase text-gray-400 dark:text-wire-200/40 tracking-wider">Example Command (Docker)</p>
        <pre class="bg-gray-900 dark:bg-carbon-950 text-wire-400/80 p-3 rounded-lg text-xs overflow-x-auto font-mono border border-gray-700 dark:border-carbon-800">docker run -d \
  -e WIREOPS_SERVER=http://your-wireops-server:8443 \
  -e WIREOPS_WORKER_TOKEN={{ issuedToken }} \
  ghcr.io/wireops/wireops-worker:latest</pre>
      </div>
    </UCard>

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
                <UBadge v-if="worker.token_status" :label="`token: ${worker.token_status}`" :color="tokenBadgeColor(worker.token_status)" size="xs" variant="subtle" />
              </div>
              <div class="hidden sm:flex items-center gap-2 mt-1">
                <p class="text-xs text-gray-400 dark:text-wire-200/40 font-mono w-36 truncate" :title="worker.id">
                  ID: {{ worker.id }}
                </p>
                <span class="text-gray-300 dark:text-carbon-700 text-xs">•</span>
                <p class="text-xs text-gray-400 dark:text-wire-200/40">
                  Last seen: {{ formatRelative(worker.last_seen) }}
                </p>
                <template v-if="worker.token_expires">
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
          <div v-if="!worker.is_embedded && worker.status !== 'REVOKED'">
            <UButton icon="i-lucide-ban" color="error" variant="ghost" size="sm" @click="handleRevoke(worker)" />
          </div>
        </div>
      </div>
    </UCard>

    <!-- Tokens Section -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">
            Seat Tokens
            <span v-if="pendingTokens.length > 0" class="ml-1.5 text-yellow-400">({{ pendingTokens.length }})</span>
          </h3>
          <p class="text-xs text-gray-400 dark:text-wire-200/40">Active tokens waiting for a worker to connect and register.</p>
        </div>
      </template>

      <div v-if="pending && !workers" class="space-y-4">
        <USkeleton v-for="i in 2" :key="i" class="h-16 w-full" />
      </div>

      <div v-else-if="!workers || pendingTokens.length === 0" class="text-center py-8">
        <div class="w-12 h-12 rounded-full bg-yellow-400/10 border border-yellow-400/20 flex items-center justify-center mx-auto mb-3">
          <UIcon name="i-lucide-key-round" class="w-6 h-6 text-yellow-400" />
        </div>
        <h3 class="text-base font-medium text-gray-900 dark:text-wire-200 mb-1">No pending tokens</h3>
        <p class="text-gray-500 dark:text-wire-200/50 text-xs">Generate a token at the top to configure a new worker.</p>
      </div>

      <div v-else class="space-y-3">
        <div
          v-for="token in pendingTokens"
          :key="token.id"
          class="flex items-center justify-between p-4 bg-gray-50 dark:bg-carbon-800/40 rounded-xl border border-gray-200 dark:border-carbon-700 hover:shadow-[0_0_0_2px_rgba(255,198,0,0.25),0_0_20px_rgba(255,198,0,0.08)] transition-all"
        >
          <div class="flex items-center gap-4">
            <div class="w-10 h-10 rounded-lg bg-gray-100 dark:bg-carbon-700/60 flex items-center justify-center">
              <UIcon name="i-lucide-key-round" class="w-5 h-5 text-wire-400" />
            </div>
            <div>
              <div class="flex items-center gap-2">
                <span class="font-medium text-gray-900 dark:text-wire-200">Pending Worker Token</span>
              </div>
              <div class="flex items-center gap-2 mt-1">
                <p class="text-xs text-gray-400 dark:text-wire-200/40">
                  Expires: {{ formatDate(token.token_expires) }}
                </p>
              </div>
            </div>
          </div>
          <div>
            <UButton icon="i-lucide-trash" color="error" variant="ghost" size="sm" @click="handleRevoke(token)" />
          </div>
        </div>
      </div>
    </UCard>
  </div>
</template>
