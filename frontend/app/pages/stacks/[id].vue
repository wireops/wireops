<script setup lang="ts">
import type { IntegrationAction } from '~/composables/useIntegrations'

const route = useRoute()
const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()
const { copy } = useCopy()
const { triggerSync, triggerRollback, forceRedeploy, getServices, getStackResources, deleteStack, getComposeFile, getWebhookUrl, getContainerStats, getContainerLogs, getRepoCommits, transferStack } = useApi()
const { getStackIntegrationActions } = useIntegrations()

function formatUptime(startedAt: string): string {
  if (!startedAt) return '-'
  const start = new Date(startedAt).getTime()
  const now = Date.now()
  const diff = Math.floor((now - start) / 1000)
  if (diff < 0) return '-'
  const days = Math.floor(diff / 86400)
  const hours = Math.floor((diff % 86400) / 3600)
  const mins = Math.floor((diff % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}
const { validateComposePath, validateComposeFile } = useValidation()
const toast = useToast()
const { platformIconUrl } = useRepositoryPlatform()

const stackId = route.params.id as string

const { data: stack, refresh: refreshStack, error: stackError } = useAsyncData(`stack_${stackId}`, () =>
  $pb.collection('stacks').getOne(stackId, { expand: 'repository,worker' })
)

const workerOffline = computed(() => {
  const worker = stack.value?.expand?.worker
  if (!worker) return false
  if (worker.status && worker.status !== 'ACTIVE') return true
  return false
})

const effectiveStackStatus = computed(() => {
  if (workerOffline.value) return 'unknown'
  return stack.value?.status || 'unknown'
})
watch(stackError, (err) => {
  if (err) navigateTo('/stacks')
})

const { data: logs, refresh: refreshLogs } = useAsyncData(`logs_${stackId}`, () =>
  $pb.collection('sync_logs').getList(1, 20, {
    filter: `stack = "${stackId}"`,
    sort: '-created',
  })
)

const { data: envVars, refresh: refreshEnv } = useAsyncData(`env_${stackId}`, () =>
  $pb.collection('stack_env_vars').getFullList({
    filter: `stack = "${stackId}"`,
    sort: 'key',
  })
)

const { data: workers } = useAsyncData('workers_for_stacks', () =>
  $pb.collection('workers').getFullList({ filter: 'status = "ACTIVE"', sort: 'hostname' })
)

const { data: webhookUrl } = useAsyncData(`webhook_url_${stackId}`, () => getWebhookUrl(stackId))

const workerOptions = computed(() =>
  (workers.value || []).map((a: any) => ({ label: a.hostname, value: a.id }))
)

const services = ref<any[]>([])
const containerStats = ref<Record<string, any>>({})
const volumes = ref<{ name: string; driver: string; mountpoint: string; scope: string }[]>([])
const networks = ref<{ name: string; driver: string; scope: string; subnet?: string; gateway?: string }[]>([])

function formatBytes(bytes: number): string {
  if (bytes == null || isNaN(bytes)) return '-'
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
}

async function loadServices() {
  try {
    services.value = await getServices(stackId) as any[]
    loadAllStats()
    loadIntegrationActions()
  } catch { services.value = [] }
}


const integrationActions = ref<Record<string, IntegrationAction[]>>({})

async function loadIntegrationActions() {
  try {
    integrationActions.value = await getStackIntegrationActions(stackId)
  } catch {
    integrationActions.value = {}
  }
}

async function loadResources() {
  try {
    const res = await getStackResources(stackId)
    volumes.value = res.volumes ?? []
    networks.value = res.networks ?? []
  } catch {
    volumes.value = []
    networks.value = []
  }
}

async function loadAllStats() {
  for (const s of services.value) {
    if (s.status === 'running' && s.container_id) {
      try {
        const stats = await getContainerStats(stackId, s.container_id)
        containerStats.value[s.container_id] = stats
      } catch { /* skip */ }
    }
  }
}

const serviceTree = computed(() => {
  const map: Record<string, any[]> = {}
  for (const s of services.value || []) {
    if (!map[s.service_name]) map[s.service_name] = []
    map[s.service_name]?.push(s)
  }
  return Object.entries(map).map(([name, containers]) => ({ name, containers }))
})

// Container logs viewer
const showLogsModal = ref(false)
const logsContent = ref('')
const logsContainerName = ref('')
async function openContainerLogs(containerId: string, containerName: string) {
  logsContainerName.value = containerName || containerId
  logsContent.value = 'Loading...'
  showLogsModal.value = true
  try {
    const res = await getContainerLogs(stackId, containerId, 200)
    logsContent.value = res.logs || '(no logs)'
  } catch {
    logsContent.value = 'Failed to load logs'
  }
}

// Container action confirmation
const showContainerConfirmModal = ref(false)
const containerActionState = ref<{ id: string, name: string, action: 'stop' | 'restart' | null }>({
  id: '',
  name: '',
  action: null
})

function openContainerActionModal(containerId: string, containerName: string, action: 'stop' | 'restart') {
  containerActionState.value = {
    id: containerId,
    name: containerName || containerId,
    action
  }
  showContainerConfirmModal.value = true
}

// Repo commits for rollback
const repoCommits = ref<{ sha: string; message: string; author: string; date: string }[]>([])
async function loadRepoCommits() {
  const repoId = stack.value?.repository
  if (!repoId) return
  try {
    repoCommits.value = await getRepoCommits(repoId)
  } catch { repoCommits.value = [] }
}
watch(stack, (val) => {
  if (val?.repository) loadRepoCommits()
}, { immediate: true })

const commitOptions = computed(() =>
  repoCommits.value.map(c => ({
    label: `${c.sha.slice(0, 7)} - ${c.message.slice(0, 50)}${c.message.length > 50 ? '...' : ''}`,
    value: c.sha,
  }))
)

// Compose file viewer
const showComposeModal = ref(false)
const composeContent = ref('')
const composeFilename = ref('')
async function openComposeViewer() {
  try {
    const res = await getComposeFile(stackId)
    composeContent.value = res.content
    composeFilename.value = res.filename
    showComposeModal.value = true
  } catch (e: any) {
    toast.add({ title: e?.message || 'Failed to load compose file', color: 'error' })
  }
}
function downloadComposeFile() {
  const blob = new Blob([composeContent.value], { type: 'text/yaml' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = composeFilename.value || 'docker-compose.yml'
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  toast.add({ title: 'Compose file downloaded', color: 'success' })
}

const activeTab = ref('overview')
const tabs = [
  { label: 'Overview', value: 'overview', icon: 'i-lucide-info' },
  { label: 'Variables', value: 'env', icon: 'i-lucide-variable' },
  { label: 'Sync Logs', value: 'logs', icon: 'i-lucide-scroll-text' },
]

// Edit stack
const editing = ref(false)
const editForm = ref<any>({})
function startEdit() {
  editForm.value = { ...stack.value }
  editing.value = true
}
const editErrors = ref<{ compose_path?: string; compose_file?: string }>({})
async function saveEdit() {
  editErrors.value = {}
  const pathErr = validateComposePath(editForm.value.compose_path || '')
  const fileErr = validateComposeFile(editForm.value.compose_file || '')
  if (pathErr) editErrors.value.compose_path = pathErr
  if (fileErr) editErrors.value.compose_file = fileErr
  if (pathErr || fileErr) return

  await $pb.collection('stacks').update(stackId, {
    name: editForm.value.name,
    worker: editForm.value.worker,
    compose_path: editForm.value.compose_path,
    compose_file: editForm.value.compose_file,
    poll_interval: editForm.value.poll_interval,
  })
  editing.value = false
  refreshStack()
}

const newEnvKey = ref('')
const newEnvValue = ref('')
const newEnvSecret = ref(false)

async function addEnvVar() {
  if (!newEnvKey.value) return
  await $pb.collection('stack_env_vars').create({
    stack: stackId,
    key: newEnvKey.value,
    value: newEnvValue.value,
    secret: newEnvSecret.value,
  })
  newEnvKey.value = ''
  newEnvValue.value = ''
  newEnvSecret.value = false
  refreshEnv()
}
const editingEnvId = ref<string | null>(null)
const editEnvKey = ref('')
const editEnvValue = ref('')
const editEnvSecret = ref(false)

function startEditEnv(ev: any) {
  editingEnvId.value = ev.id
  editEnvKey.value = ev.key
  editEnvValue.value = ev.secret ? '' : ev.value
  editEnvSecret.value = ev.secret
}
function cancelEditEnv() {
  editingEnvId.value = null
}
async function saveEditEnv(id: string) {
  if (!editEnvKey.value) return
  const data: any = {
    key: editEnvKey.value,
    secret: editEnvSecret.value,
  }
  if (editEnvValue.value) data.value = editEnvValue.value
  await $pb.collection('stack_env_vars').update(id, data)
  editingEnvId.value = null
  refreshEnv()
}
async function deleteEnvVar(id: string) {
  await $pb.collection('stack_env_vars').delete(id)
  refreshEnv()
}

// Sync & rollback
async function handleSync() {
  try {
    await triggerSync(stackId)
    toast.add({ title: 'Sync triggered', color: 'success' })
    setTimeout(() => { refreshLogs(); refreshStack() }, 3000)
  } catch (e: any) {
    toast.add({ title: e?.message || 'Sync failed', color: 'error' })
  }
}

const rollbackSha = ref('')
const showRollbackModal = ref(false)
async function handleRollback() {
  if (!rollbackSha.value) return
  try {
    await triggerRollback(stackId, rollbackSha.value)
    showRollbackModal.value = false
    toast.add({ title: 'Rollback triggered — stack will be paused', color: 'warning' })
    rollbackSha.value = ''
    setTimeout(() => { refreshLogs(); refreshStack() }, 3000)
  } catch (e: any) {
    toast.add({ title: e?.message || 'Rollback failed', color: 'error' })
  }
}

// Pause / Resume
const showPauseModal = ref(false)

async function togglePause() {
  if (effectiveStackStatus.value !== 'paused') {
    showPauseModal.value = true
    return
  }
  await $pb.collection('stacks').update(stackId, { status: 'active' })
  refreshStack()
}

async function confirmPause() {
  await $pb.collection('stacks').update(stackId, { status: 'paused' })
  showPauseModal.value = false
  refreshStack()
}

// Force redeploy
const showForceRedeploy = ref(false)
const forceOpts = ref({ recreate_containers: true, recreate_volumes: false, recreate_networks: false })
async function handleForceRedeploy() {
  try {
    await forceRedeploy(stackId, forceOpts.value)
    showForceRedeploy.value = false
    toast.add({ title: 'Force redeploy triggered', color: 'info' })
    forceOpts.value = { recreate_containers: true, recreate_volumes: false, recreate_networks: false }
    setTimeout(() => { refreshStack(); loadServices(); loadResources() }, 5000)
  } catch (e: any) {
    toast.add({ title: e?.message || 'Force redeploy failed', color: 'error' })
  }
}


// Delete stack modal
const showDeleteModal = ref(false)
async function onStackDeleted() {
  showDeleteModal.value = false
  navigateTo('/stacks')
}

// Transfer stack modal
const showTransferModal = ref(false)
function onTransferDone() {
  showTransferModal.value = false
  // Switch to Sync Logs tab so the user can watch the transfer progress in real-time
  activeTab.value = 'logs'
  // Refresh logs immediately — the sync log entry is created before the goroutine
  // starts working, so the 'running' state should already be visible.
  refreshLogs()
  // Refresh the stack record after a delay for the worker field to update
  setTimeout(() => { refreshStack(); refreshLogs() }, 4000)
}

const statusColor = (s: string) => {
  switch (s) {
    case 'active': case 'success': case 'done': case 'running': return 'success'
    case 'syncing': return 'info'
    case 'error': case 'exited': return 'error'
    case 'paused': case 'pending': case 'queued': return 'warning'
    default: return 'neutral'
  }
}

onMounted(() => {
  loadServices()
  loadResources()
  
  // Subscribe to stack changes
  subscribe('stacks', (e) => {
    if (e.record?.id === stackId) {
      refreshStack()
      loadServices()
      loadResources()
    }
  })

  // Subscribe to sync logs changes
  subscribe('sync_logs', (e) => {
    if (e.record?.stack === stackId) {
      refreshLogs()
    }
  })

  // Subscribe to env vars changes
  subscribe('stack_env_vars', (e) => {
    if (e.record?.stack === stackId) {
      refreshEnv()
    }
  })

  // Keyboard shortcut: Cmd/Ctrl + S to trigger sync
  const handleKeydown = (event: KeyboardEvent) => {
    if ((event.metaKey || event.ctrlKey) && event.key === 's') {
      event.preventDefault()
      handleSync()
    }
  }
  window.addEventListener('keydown', handleKeydown)
  onUnmounted(() => {
    window.removeEventListener('keydown', handleKeydown)
  })
})
</script>

<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <div class="flex items-center gap-3 min-w-0">
        <UButton icon="i-lucide-arrow-left" variant="ghost" size="sm" to="/stacks" />
        <h1 class="flex items-center gap-3 text-xl sm:text-2xl font-bold truncate">
          <div class="flex items-center justify-center w-8 h-8 sm:w-9 sm:h-9 rounded-lg bg-yellow-400/10 shrink-0">
            <UIcon name="i-lucide-layers" class="w-4 h-4 sm:w-5 sm:h-5 text-yellow-400" />
          </div>
          {{ stack?.name }}
        </h1>
        <BadgeStatus v-if="stack" :status="effectiveStackStatus" />
      </div>
      <div v-if="stack?.containers_list?.length" class="mt-2 sm:mt-0 sm:ml-4 flex-1">
        <StackContainersList :containers="stack.containers_list" />
      </div>
      <div class="grid grid-cols-3 sm:flex sm:items-center gap-2 sm:shrink-0">
        <UButton
          :icon="effectiveStackStatus === 'paused' ? 'i-lucide-play' : 'i-lucide-pause'"
          :label="effectiveStackStatus === 'paused' ? 'Resume' : 'Pause'"
          :color="effectiveStackStatus === 'paused' ? 'success' : 'primary'"
          variant="outline"
          block
          @click="togglePause"
        />
        <UButton icon="i-lucide-recycle" label="Redeploy" variant="outline" block @click="showForceRedeploy = true" />
        <UButton icon="i-lucide-refresh-cw" label="Sync Now" block class="shadow-[0_0_16px_rgba(255,198,0,0.35)] hover:shadow-[0_0_24px_rgba(255,198,0,0.55)] transition-shadow" @click="handleSync" />
      </div>
    </div>

    <UTabs v-model="activeTab" :items="tabs" />

    <!-- Overview -->
    <div v-if="activeTab === 'overview'" class="space-y-4">
      <UCard>
        <template #header>
          <div class="flex justify-between items-center">
            <h3 class="font-semibold">Stack Configuration</h3>
            <UButton v-if="!editing" icon="i-lucide-pencil" variant="ghost" size="xs" @click="startEdit" />
          </div>
        </template>
        <div v-if="!editing" class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
          <div>
            <span class="text-gray-500">Repository:</span>
            <NuxtLink
              v-if="stack?.expand?.repository"
              :to="`/repositories/${stack.expand.repository.id}`"
              class="inline-flex items-center gap-1.5 text-primary hover:underline ml-1"
            >
              <img
                v-if="platformIconUrl(stack.expand.repository.platform)"
                :src="platformIconUrl(stack.expand.repository.platform)!"
                class="w-3.5 h-3.5 object-contain shrink-0"
                alt=""
              >
              <UIcon v-else name="i-lucide-git-branch" class="w-3.5 h-3.5 shrink-0" />
              {{ stack.expand.repository.name }}
            </NuxtLink>
          </div>
          <div>
            <span class="text-gray-500">Worker:</span>
            <span class="ml-1">{{ stack?.expand?.worker?.hostname || 'Unknown' }}</span>
          </div>
          <div><span class="text-gray-500">Compose Path:</span> {{ stack?.compose_path || '.' }}</div>
          <div>
            <span class="text-gray-500">Compose File:</span>
            <button
              class="ml-1 text-yellow-400 hover:text-yellow-300 font-mono underline underline-offset-2 decoration-dotted transition-colors cursor-pointer"
              @click="openComposeViewer"
            >{{ stack?.compose_file || 'docker-compose.yml' }}</button>
          </div>
          <div><span class="text-gray-500">Poll Interval:</span> {{ stack?.poll_interval || 60 }}s</div>
          <div><span class="text-gray-500">Last Synced:</span> {{ stack?.last_synced_at ? new Date(stack.last_synced_at).toLocaleString() : 'Never' }}</div>
          <div class="col-span-2 flex items-center gap-2">
            <span class="text-gray-500">Revision:</span>
            <button 
              v-if="stack?.expand?.repository?.last_commit_sha"
              class="font-mono text-sm hover:bg-gray-100 dark:hover:bg-gray-800 px-1.5 py-0.5 rounded transition-colors cursor-pointer"
              :title="`Copy ${stack.expand.repository.last_commit_sha}`"
              @click="copy(stack.expand.repository.last_commit_sha, 'Commit SHA')"
            >
              {{ stack.expand.repository.last_commit_sha.slice(0, 7) }}
            </button>
            <span v-else class="font-mono text-sm">-</span>
            <UButton v-if="stack?.source_type !== 'local'" icon="i-lucide-undo-2" variant="ghost" color="warning" size="xs" title="Rollback" @click="showRollbackModal = true" />
          </div>
        </div>
        <form v-else class="grid grid-cols-1 sm:grid-cols-2 gap-4" @submit.prevent="saveEdit">
          <UFormField label="Name"><UInput v-model="editForm.name" /></UFormField>
          <UFormField label="Worker"><USelect v-model="editForm.worker" :items="workerOptions" /></UFormField>
          <UFormField label="Compose Path" :error="editErrors.compose_path"><UInput v-model="editForm.compose_path" /></UFormField>
          <UFormField label="Compose File" :error="editErrors.compose_file"><UInput v-model="editForm.compose_file" /></UFormField>
          <UFormField label="Poll Interval (s)"><UInput v-model.number="editForm.poll_interval" type="number" /></UFormField>
          <div class="col-span-2 flex justify-end gap-2">
            <UButton label="Cancel" variant="outline" @click="editing = false" />
            <UButton type="submit" label="Save" />
          </div>
        </form>
      </UCard>

      <!-- Webhook Integration -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-webhook" class="w-5 h-5" />
            <h3 class="font-semibold">Webhook Integration</h3>
          </div>
        </template>
        <div class="space-y-3">
          <div>
            <label class="text-xs text-gray-500 uppercase tracking-wide font-semibold">Webhook URL</label>
            <div class="flex items-center gap-2 mt-1">
              <UInput 
                :model-value="webhookUrl ?? ''" 
                readonly 
                class="flex-1 font-mono text-xs"
                placeholder="Loading..."
              />
              <UButton 
                icon="i-lucide-copy" 
                variant="outline" 
                size="sm" 
                :disabled="!webhookUrl"
                title="Copy webhook URL"
                @click="webhookUrl && copy(webhookUrl, 'Webhook URL')"
              />
            </div>
          </div>
          <details class="text-xs">
            <summary class="cursor-pointer text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200 font-medium">
              Show usage examples
            </summary>
            <div class="mt-2 space-y-2 text-gray-600 dark:text-gray-400">
              <div>
                <p class="font-semibold mb-1">GitHub Actions / GitLab CI:</p>
                <pre class="p-2 bg-gray-100 dark:bg-gray-800 rounded overflow-x-auto">curl -L -X POST {{ webhookUrl ?? '...' }}</pre>
              </div>
              <p class="text-xs text-gray-500 italic">Trigger a sync whenever you push to your repository</p>
            </div>
          </details>
        </div>
      </UCard>

      <!-- Services / Containers (Tree) -->
      <UCard>
        <template #header>
          <div class="flex justify-between items-center">
            <h3 class="font-semibold">Services</h3>
            <UTooltip text="Refresh services">
              <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" @click="loadServices" />
            </UTooltip>
          </div>
        </template>
        <div v-if="serviceTree.length" class="space-y-4">
          <div v-for="svc in serviceTree" :key="svc.name">
            <div class="flex items-center gap-2 py-1">
              <UIcon name="i-lucide-box" class="text-gray-400 w-4 h-4" />
              <span class="font-semibold text-sm">{{ svc.name }}</span>
            </div>
            <div class="ml-6 border-l border-gray-200 dark:border-gray-700 pl-3 space-y-2">
              <div
                v-for="c in svc.containers"
                :key="c.container_id"
                class="py-2 px-2 rounded-md transition-colors hover:bg-gray-100 dark:hover:bg-gray-800 group"
              >
                <div class="flex items-center justify-between">
                  <div class="flex items-center gap-2 min-w-0">
                    <BadgeStatus :status="c.status" />
                    <span class="text-sm font-medium truncate">{{ c.container_name || c.container_id }}</span>
                    <button 
                      v-if="c.container_name"
                      class="text-xs text-gray-400 font-mono hover:bg-gray-100 dark:hover:bg-gray-800 px-1 py-0.5 rounded transition-colors cursor-pointer shrink-0"
                      :title="`Copy ${c.container_id}`"
                      @click="copy(c.container_id, 'Container ID')"
                    >
                      {{ c.container_id.slice(0, 12) }}
                    </button>
                  </div>
                  <div class="flex items-center gap-1 shrink-0">
                    <UButton
                      v-if="c.status === 'running'"
                      icon="i-lucide-square"
                      variant="ghost"
                      color="warning"
                      size="xs"
                      title="Stop"
                      @click="openContainerActionModal(c.container_id, c.container_name, 'stop')"
                    />
                    <UButton
                      icon="i-lucide-rotate-cw"
                      variant="ghost"
                      color="info"
                      size="xs"
                      title="Restart"
                      @click="openContainerActionModal(c.container_id, c.container_name, 'restart')"
                    />
                  </div>
                </div>
                <div v-if="containerStats[c.container_id]" class="flex flex-wrap items-center gap-x-4 gap-y-1 mt-1 text-xs text-gray-400">
                  <span class="flex items-center gap-1">
                    <UIcon name="i-lucide-cpu" class="w-3 h-3" />
                    {{ containerStats[c.container_id].cpu_percent != null ? containerStats[c.container_id].cpu_percent.toFixed(2) : '-' }}%
                  </span>
                  <span class="flex items-center gap-1">
                    <UIcon name="i-lucide-memory-stick" class="w-3 h-3" />
                    {{ formatBytes(containerStats[c.container_id].mem_usage) }} / {{ formatBytes(containerStats[c.container_id].mem_limit) }}
                  </span>
                  <span class="flex items-center gap-1">
                    <UIcon name="i-lucide-clock" class="w-3 h-3" />
                    {{ formatUptime(containerStats[c.container_id].started_at) }}
                  </span>
                  <ContainerIntegrationActions
                    :actions="integrationActions[c.container_id] || []"
                    :container-id="c.container_id"
                    :container-name="c.container_name"
                    @show-logs="openContainerLogs"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-4 text-center">No services found. Run a sync first.</p>
      </UCard>

      <!-- Volumes -->
      <UCard>
        <template #header>
          <div class="flex justify-between items-center">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-hard-drive" class="w-5 h-5" />
              <h3 class="font-semibold">Volumes</h3>
            </div>
            <UTooltip text="Refresh volumes">
              <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" @click="loadResources" />
            </UTooltip>
          </div>
        </template>
        <div v-if="volumes.length" class="divide-y divide-gray-100 dark:divide-gray-800">
          <div
            v-for="vol in volumes"
            :key="vol.name"
            class="py-2 px-1 flex flex-col gap-0.5 text-sm"
          >
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-database" class="w-4 h-4 text-gray-400 shrink-0" />
              <span class="font-medium">{{ vol.name }}</span>
              <UBadge :label="vol.driver" variant="subtle" size="xs" />
              <UBadge :label="vol.scope" variant="outline" size="xs" color="neutral" />
            </div>
            <p v-if="vol.mountpoint" class="ml-6 text-xs text-gray-400 font-mono truncate" :title="vol.mountpoint">
              {{ vol.mountpoint }}
            </p>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-4 text-center">No volumes found. Run a sync first.</p>
      </UCard>

      <!-- Networks -->
      <UCard>
        <template #header>
          <div class="flex justify-between items-center">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-network" class="w-5 h-5" />
              <h3 class="font-semibold">Networks</h3>
            </div>
            <UTooltip text="Refresh networks">
              <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" @click="loadResources" />
            </UTooltip>
          </div>
        </template>
        <div v-if="networks.length" class="divide-y divide-gray-100 dark:divide-gray-800">
          <div
            v-for="net in networks"
            :key="net.name"
            class="py-2 px-1 flex flex-col gap-0.5 text-sm"
          >
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-waypoints" class="w-4 h-4 text-gray-400 shrink-0" />
              <span class="font-medium">{{ net.name }}</span>
              <UBadge :label="net.driver" variant="subtle" size="xs" />
              <UBadge :label="net.scope" variant="outline" size="xs" color="neutral" />
            </div>
            <p v-if="net.subnet || net.gateway" class="ml-6 text-xs text-gray-400 font-mono">
              <span v-if="net.subnet">{{ net.subnet }}</span>
              <span v-if="net.subnet && net.gateway"> · </span>
              <span v-if="net.gateway">gw {{ net.gateway }}</span>
            </p>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-4 text-center">No networks found. Run a sync first.</p>
      </UCard>

      <!-- Danger Zone -->
      <UCard>
        <template #header><h3 class="font-semibold text-red-500">Danger Zone</h3></template>
        <div class="space-y-4">
          <!-- Transfer Stack -->
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium">Transfer Stack</p>
              <p class="text-xs text-gray-500">Move this stack to another worker. Data will not be preserved.</p>
            </div>
            <UButton
              label="Transfer Stack"
              color="warning"
              variant="outline"
              size="sm"
              icon="i-lucide-arrow-right-left"
              @click="showTransferModal = true"
            />
          </div>
          <hr class="border-gray-200 dark:border-carbon-700" >
          <!-- Remove Stack -->
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium">Remove Stack</p>
              <p class="text-xs text-gray-500">This will stop all containers and delete the stack permanently.</p>
            </div>
            <UButton
              label="Remove Stack"
              color="error"
              variant="outline"
              size="sm"
              @click="showDeleteModal = true"
            />
          </div>
        </div>
      </UCard>
    </div>

    <!-- Variables -->
    <div v-if="activeTab === 'env'" class="space-y-4">
      <UCard>
        <template #header><h3 class="font-semibold">Environment Variables</h3></template>
        <div v-if="envVars?.length" class="divide-y divide-gray-200 dark:divide-gray-800">
          <!-- Editing row -->
          <div v-for="ev in envVars" :key="ev.id" class="flex flex-col sm:flex-row sm:items-center gap-2 py-2">
            <template v-if="editingEnvId === ev.id">
              <UInput v-model="editEnvKey" placeholder="KEY" class="font-mono" />
              <UInput v-model="editEnvValue" :placeholder="ev.secret ? '(unchanged if empty)' : 'value'" :type="editEnvSecret ? 'password' : 'text'" class="flex-1" />
              <USwitch v-model="editEnvSecret" label="Secret" />
              <UButton icon="i-lucide-check" variant="ghost" color="success" size="xs" @click="saveEditEnv(ev.id)" />
                <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" @click="cancelEditEnv" />
            </template>
            <!-- Display row -->
            <template v-else>
              <UInput :model-value="ev.key" readonly class="font-mono" />
              <UInput v-if="ev.secret" model-value="••••••••" readonly type="password" class="flex-1" />
              <UInput v-else :model-value="ev.value" readonly class="flex-1 font-mono" />
              <div class="flex items-center gap-2">
                <BadgeLabel v-if="ev.secret" label="secret" color="warning" class="uppercase" />
                <UButton icon="i-lucide-pencil" variant="ghost" size="xs" @click="startEditEnv(ev)" />
                <UButton icon="i-lucide-trash-2" variant="ghost" color="error" size="xs" @click="deleteEnvVar(ev.id)" />
              </div>
            </template>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-2">No environment variables defined</p>
      </UCard>
      <UCard>
        <template #header><h3 class="font-semibold">Add Variable</h3></template>
        <form class="flex flex-col gap-4" @submit.prevent="addEnvVar">
          <div class="flex flex-col sm:flex-row sm:items-center gap-2">
            <UInput v-model="newEnvKey" placeholder="KEY" class="font-mono" />
            <UInput v-model="newEnvValue" placeholder="value" :type="newEnvSecret ? 'password' : 'text'" class="flex-1" />
            <UButton type="submit" icon="i-lucide-plus" label="Add" :disabled="!newEnvKey" />
          </div>
          <div class="flex items-center gap-4">
            <USwitch v-model="newEnvSecret" label="Secret (Encrypted in DB)" />
          </div>
        </form>
      </UCard>
    </div>

    <!-- Sync Logs -->
    <div v-if="activeTab === 'logs'">
      <UCard>
        <template #header>
          <div class="flex justify-between items-center">
            <h3 class="font-semibold">Sync History</h3>
            <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" @click="refreshLogs()" />
          </div>
        </template>
        <div v-if="logs?.items?.length" class="divide-y divide-gray-200 dark:divide-gray-800">
          <div v-for="log in logs.items" :key="log.id" class="py-3 space-y-1">
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-2">
                <SyncLogBadge :status="log.status" :trigger="log.trigger" />
                <button 
                  v-if="log.commit_sha"
                  class="font-mono text-xs hover:bg-gray-100 dark:hover:bg-gray-800 px-1 py-0.5 rounded transition-colors cursor-pointer"
                  :title="`Copy ${log.commit_sha}`"
                  @click="copy(log.commit_sha, 'Commit SHA')"
                >
                  {{ log.commit_sha.slice(0, 7) }}
                </button>
              </div>
              <div class="text-xs text-gray-400">
                {{ log.duration_ms ? `${log.duration_ms}ms` : '' }}
                · {{ new Date(log.created).toLocaleString() }}
              </div>
            </div>
            <p v-if="log.commit_message" class="text-xs text-gray-500 truncate">{{ log.commit_message }}</p>
            <ErrorDisplay 
              v-if="log.status === 'error' && log.output" 
              :error="log.output"
              :show-retry="true"
              class="mt-2"
              @retry="handleSync"
            />
            <UAlert
              v-else-if="log.status === 'queued'"
              title="Deployment Queued"
              description="The worker is currently offline. This update has been placed in the deployment queue and will automatically proceed when the worker reconnects."
              icon="i-lucide-list-todo"
              color="warning"
              variant="subtle"
              class="mt-2"
            />
            <details v-else-if="log.output && log.status !== 'error'" class="text-xs">
              <summary class="cursor-pointer text-gray-400 hover:text-gray-600">Show output</summary>
              <pre class="mt-1 p-2 bg-gray-100 dark:bg-gray-800 rounded text-xs overflow-x-auto max-h-48">{{ log.output }}</pre>
            </details>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-4 text-center">No sync logs yet</p>
      </UCard>
    </div>

    <!-- Pause Confirmation Modal -->
    <UModal v-model:open="showPauseModal">
      <template #content>
        <div class="p-6 space-y-5">
          <div class="flex items-start gap-4">
            <div class="flex items-center justify-center w-10 h-10 rounded-lg bg-yellow-400/10 shrink-0">
              <UIcon name="i-lucide-pause" class="w-5 h-5 text-yellow-400" />
            </div>
            <div>
              <h3 class="font-semibold text-gray-900 dark:text-wire-200 text-base">Pause stack?</h3>
              <p class="text-sm text-gray-500 dark:text-wire-200/50 mt-1">
                Auto-sync will be disabled. No further deployments will occur until you resume the stack manually.
              </p>
            </div>
          </div>
          <div class="flex justify-end gap-2 pt-1">
            <UButton label="Cancel" variant="outline" color="neutral" @click="showPauseModal = false" />
            <UButton label="Pause" icon="i-lucide-pause" color="primary" @click="confirmPause" />
          </div>
        </div>
      </template>
    </UModal>

    <!-- Compose File Modal -->
    <UModal v-model:open="showComposeModal">
      <template #content>
        <div class="p-4 space-y-3">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold text-sm">{{ composeFilename }}</h3>
            <div class="flex items-center gap-1">
              <UButton icon="i-lucide-copy" variant="ghost" size="xs" title="Copy" @click="copy(composeContent, 'Compose file')" />
              <UButton icon="i-lucide-download" variant="ghost" size="xs" title="Download" @click="downloadComposeFile" />
              <UButton icon="i-lucide-x" variant="ghost" size="xs" @click="showComposeModal = false" />
            </div>
          </div>
          <div class="overflow-auto max-h-[70vh]">
            <YamlHighlighter :code="composeContent" />
          </div>
        </div>
      </template>
    </UModal>

    <!-- Container Logs Modal -->
    <UModal v-model:open="showLogsModal">
      <template #content>
        <div class="p-4 space-y-3">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold text-sm">{{ logsContainerName }}</h3>
            <UButton icon="i-lucide-x" variant="ghost" size="xs" @click="showLogsModal = false" />
          </div>
          <pre class="p-3 bg-gray-100 dark:bg-gray-800 rounded text-xs font-mono overflow-auto max-h-[70vh] whitespace-pre">{{ logsContent }}</pre>
        </div>
      </template>
    </UModal>

    <!-- Force Redeploy Modal -->
    <UModal v-model:open="showForceRedeploy">
      <template #content>
        <div class="p-4 space-y-4">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Force Redeploy</h3>
            <UButton icon="i-lucide-x" variant="ghost" size="xs" @click="showForceRedeploy = false" />
          </div>
          <p class="text-sm text-gray-500">Redeploy the current stack with the selected options. This will force Docker Compose to recreate the selected resources.</p>
          <div class="space-y-3">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm font-medium">Recreate Containers</p>
                <p class="text-xs text-gray-400">Force recreate all containers even if unchanged</p>
              </div>
              <USwitch v-model="forceOpts.recreate_containers" />
            </div>
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm font-medium">Recreate Volumes</p>
                <p class="text-xs text-gray-400">Recreate anonymous volumes and remove named volumes</p>
              </div>
              <USwitch v-model="forceOpts.recreate_volumes" />
            </div>
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm font-medium">Recreate Networks</p>
                <p class="text-xs text-gray-400">Tear down and recreate all networks (requires full down/up)</p>
              </div>
              <USwitch v-model="forceOpts.recreate_networks" />
            </div>
          </div>
          <UButton label="Force Redeploy" color="info" block @click="handleForceRedeploy" />
        </div>
      </template>
    </UModal>

    <!-- Rollback Modal (git stacks only) -->
    <UModal v-model:open="showRollbackModal">
      <template #content>
        <div class="p-4 space-y-4">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Rollback</h3>
            <UButton icon="i-lucide-x" variant="ghost" size="xs" @click="showRollbackModal = false" />
          </div>
          <UAlert
            color="warning"
            icon="i-lucide-alert-triangle"
            title="Sync will be paused"
            description="After rolling back, the stack will be paused to prevent automatic syncs from undoing the rollback. You can resume syncing manually when ready."
          />
          <div class="space-y-3">
            <div v-if="repoCommits.length" class="space-y-1">
              <p class="text-xs text-gray-500 font-medium">Recent commits</p>
              <div class="border border-gray-200 dark:border-gray-800 rounded-md overflow-hidden">
                <table class="w-full text-xs">
                  <thead class="bg-gray-50 dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800">
                    <tr>
                      <th class="text-left px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Date/Time</th>
                      <th class="text-left px-3 py-2 font-medium text-gray-600 dark:text-gray-400">SHA</th>
                      <th class="text-left px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Message</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-200 dark:divide-gray-800">
                    <tr
                      v-for="c in repoCommits"
                      :key="c.sha"
                      class="hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors cursor-pointer"
                      :class="rollbackSha === c.sha ? 'bg-gray-100 dark:bg-gray-800' : ''"
                      @click="rollbackSha = c.sha"
                    >
                      <td class="px-3 py-2 text-gray-400 whitespace-nowrap">
                        {{ new Date(c.date).toLocaleDateString('en-US') }} {{ new Date(c.date).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }}
                      </td>
                      <td class="px-3 py-2">
                        <span class="font-mono bg-gray-100 dark:bg-gray-700 px-1.5 py-0.5 rounded inline-block">
                          {{ c.sha.slice(0, 7) }}
                        </span>
                      </td>
                      <td class="px-3 py-2 max-w-xs truncate">{{ c.message }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
            <div class="relative">
              <UInput v-model="rollbackSha" placeholder="Or paste a commit SHA" class="font-mono w-full" />
              <button
                v-if="rollbackSha"
                class="absolute right-2 top-1/2 -translate-y-1/2 p-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
                title="Copy SHA"
                type="button"
                @click="copy(rollbackSha, 'Commit SHA')"
              >
                <UIcon name="i-lucide-copy" class="w-4 h-4 text-gray-400" />
              </button>
            </div>
            <UButton label="Rollback" color="warning" block :disabled="!rollbackSha" @click="handleRollback" />
          </div>
        </div>
      </template>
    </UModal>
    <!-- Transfer stack modal -->
    <UModal v-model:open="showTransferModal">
      <template #content>
        <StackTransferModal
          v-if="stack"
          :stack="stack"
          @transferred="onTransferDone"
          @cancel="showTransferModal = false"
        />
      </template>
    </UModal>
    <!-- Delete stack modal -->
    <UModal v-model:open="showDeleteModal">
      <template #content>
        <DeleteStackModal
          v-if="stack"
          :stack="stack"
          @deleted="onStackDeleted"
          @cancel="showDeleteModal = false"
        />
      </template>
    </UModal>
    <!-- Container action confirm modal -->
    <ContainerActionModal
      v-model:open="showContainerConfirmModal"
      :stack-id="stackId"
      :container-id="containerActionState.id"
      :container-name="containerActionState.name"
      :action="containerActionState.action"
      @done="loadServices"
    />
  </div>
</template>
