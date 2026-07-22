<script setup lang="ts">
import type { IntegrationAction } from '~/composables/useIntegrations'
import { stackHasRenderOverrides, stackSourceStatus, stackVisibleDeployStatus, stackWorkerStatus } from '../../utils/stack-status'
import { WORKER_STATUS } from '../../utils/worker'

const route = useRoute()
const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()
const { copy } = useCopy()
const { triggerSync, triggerRollback, forceRedeploy, setRenderOverrides, clearRenderOverrides, getRenderOverridesDiff, deleteStack, getServices, getComposeFile, getWebhookUrl, getContainerStats, getContainerLogs, getRepoCommits, transferStack, getWorkers, stopContainer, restartContainer } = useApi()
const { getStackIntegrationActions } = useIntegrations()
const { validateComposePath, validateComposeFile } = useValidation()
const toast = useToast()
const { platformIconUrl } = useRepositoryPlatform()
const { canOperate } = usePermissions()

const stackId = route.params.id as string

const { data: stack, refresh: refreshStack, error: stackError } = useAsyncData(`stack_${stackId}`, () =>
  $pb.collection('stacks').getOne(stackId, { expand: 'repository,worker' })
)

const { data: workers, refresh: refreshWorkers } = useAsyncData('workers_for_stacks', getWorkers)
const workersById = computed(() =>
  Object.fromEntries((workers.value || []).map((worker: any) => [worker.id, worker]))
)
const sourceStatus = computed(() => stackSourceStatus(stack.value))
const deployStatus = computed(() => stackVisibleDeployStatus(stack.value, workersById.value))
const workerStatus = computed(() => stackWorkerStatus(stack.value, workersById.value))
const workerOffline = computed(() => ['offline', 'revoked'].includes(workerStatus.value.key))
const canSyncDeploy = computed(() => workerStatus.value.key === 'online')
const syncDisabledReason = computed(() => {
  switch (workerStatus.value.key) {
    case 'offline':
      return 'Worker offline'
    case 'revoked':
      return 'Worker revoked'
    default:
      return 'Worker unavailable'
  }
})

// Delete stack modal
const showDeleteModal = ref(false)

watch([stackError, showDeleteModal], ([err, deleting]) => {
  // Once the stack is gone, background refreshes (polling, realtime) will
  // also 404 — but the delete modal handles its own redirect on close, so
  // don't race it away from the user before they've seen the teardown logs.
  // If the modal is dismissed (cancelled, or closed after a background 404)
  // while the stack is already gone, redirect on that transition too.
  if (err && !deleting) navigateTo('/stacks')
})

const { data: logs, refresh: refreshLogs } = useAsyncData(`logs_${stackId}`, () =>
  $pb.collection('sync_logs').getList(1, 20, {
    filter: `stack = "${stackId}"`,
    sort: '-created',
  })
)

// <details :open> can't be bound directly to a derived expression — every
// reactive re-render (worker refresh timer, realtime subscriptions) would
// re-apply that expression and snap a user-toggled details closed again.
// Track open state ourselves instead, seeded open by default for every log.
const expandedTimelineLogIds = ref<Set<string>>(new Set())
const seenTimelineLogIds = ref<Set<string>>(new Set())
watch(logs, (val) => {
  for (const log of val?.items || []) {
    if (!seenTimelineLogIds.value.has(log.id)) {
      seenTimelineLogIds.value.add(log.id)
      expandedTimelineLogIds.value.add(log.id)
    }
  }
}, { immediate: true })

function toggleTimeline(logId: string, event: Event) {
  const open = (event.target as HTMLDetailsElement).open
  if (open) expandedTimelineLogIds.value.add(logId)
  else expandedTimelineLogIds.value.delete(logId)
}

// Only open the live SSE stream while a deploy/redeploy/teardown is actually
// in flight for this stack — avoids an idle connection per open stack page.
const hasRunningLog = computed(() => (logs.value?.items || []).some((log: any) => log.status === 'running'))
const liveStreamStackId = computed(() => (hasRunningLog.value ? stackId : null))
const { lines: liveOutputLines, connected: liveStreamConnected } = useDeployStream(liveStreamStackId)

const localEnvKeys = ref<string[]>([])

const { data: webhookUrl } = useAsyncData(`webhook_url_${stackId}`, () => getWebhookUrl(stackId))

const workerOptions = computed(() =>
  (workers.value || [])
    .filter((a: any) => a.status === WORKER_STATUS.ACTIVE || a.status === WORKER_STATUS.OFFLINE)
    .map((a: any) => ({ label: a.hostname, value: a.id }))
)

const services = ref<any[]>([])
const containerStats = ref<Record<string, any>>({})
const showWebhookIntegration = ref(false)
const showDangerZone = ref(false)
const servicesCard = ref<InstanceType<typeof StackServicesCard> | null>(null)

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

// Container logs viewer
const showLogsModal = ref(false)
const logsContent = ref('')
const logsContainerName = ref('')
const logsWordWrap = ref(false)
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

// Bulk container action confirmation
const showBulkActionModal = ref(false)
const bulkActionState = ref<{ containers: { containerId: string, containerName: string }[], action: 'stop' | 'restart' }>({
  containers: [],
  action: 'restart'
})
const bulkActionLoading = ref(false)

function handleBulkContainerAction(payload: { containers: { containerId: string, containerName: string }[], action: 'stop' | 'restart' }) {
  bulkActionState.value = payload
  showBulkActionModal.value = true
}

async function executeBulkContainerAction() {
  const { containers, action } = bulkActionState.value
  bulkActionLoading.value = true
  try {
    const results = await Promise.allSettled(
      containers.map(c =>
        action === 'stop'
          ? stopContainer(stackId, c.containerId)
          : restartContainer(stackId, c.containerId)
      )
    )
    const failed = results.filter(r => r.status === 'rejected').length
    const succeeded = results.length - failed
    showBulkActionModal.value = false
    if (failed === 0) {
      toast.add({ title: `${action === 'stop' ? 'Stopped' : 'Restarted'} ${succeeded} container${succeeded !== 1 ? 's' : ''}`, color: action === 'stop' ? 'warning' : 'success' })
    } else {
      toast.add({ title: `${succeeded} succeeded, ${failed} failed`, color: 'error' })
    }
    setTimeout(() => servicesCard.value?.refresh(), 1500)
  } finally {
    bulkActionLoading.value = false
  }
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
const isWireopsManaged = computed(() => stack.value?.config_source === 'wireops_file')
async function saveEdit() {
  editErrors.value = {}

  const payload: Record<string, any> = {
    name: editForm.value.name,
    worker: editForm.value.worker,
  }

  // compose_path/compose_file (and other wireops.yaml-derived fields) are
  // immutable once a stack is created from wireops.yaml — the backend
  // rejects any attempt to change them, so don't even send them.
  if (!isWireopsManaged.value) {
    const pathErr = validateComposePath(editForm.value.compose_path || '')
    const fileErr = validateComposeFile(editForm.value.compose_file || '')
    if (pathErr) editErrors.value.compose_path = pathErr
    if (fileErr) editErrors.value.compose_file = fileErr
    if (pathErr || fileErr) return

    payload.compose_path = editForm.value.compose_path
    payload.compose_file = editForm.value.compose_file
  }

  try {
    await $pb.collection('stacks').update(stackId, payload)
    editing.value = false
    refreshStack()
  } catch (err: any) {
    toast.add({ title: 'Failed to save stack', description: err?.message, color: 'error' })
  }
}

// Webhook secret
const webhookSecretConfigured = computed(() => !!stack.value?.webhook_secret)
const webhookSecretInput = ref('')
const savingWebhookSecret = ref(false)

function generateWebhookSecret() {
  webhookSecretInput.value = crypto.randomUUID().replace(/-/g, '')
}

async function saveWebhookSecret() {
  if (!webhookSecretInput.value) return
  savingWebhookSecret.value = true
  try {
    await $pb.collection('stacks').update(stackId, { webhook_secret: webhookSecretInput.value })
    webhookSecretInput.value = ''
    await refreshStack()
    toast.add({ title: 'Webhook secret saved', color: 'success' })
  } catch (err: any) {
    toast.add({ title: 'Failed to save webhook secret', description: err?.message, color: 'error' })
  } finally {
    savingWebhookSecret.value = false
  }
}

// Sync & rollback
const showSyncModal = ref(false)

function openSyncModal() {
  if (!stack.value) return
  if (!canSyncDeploy.value) {
    toast.add({
      title: 'Sync unavailable',
      description: `${syncDisabledReason.value}. Reconnect the worker before syncing this stack.`,
      color: 'warning',
    })
    return
  }
  showSyncModal.value = true
}

function onSyncTriggered() {
  setTimeout(() => { refreshLogs(); refreshStack() }, 3000)
}

async function handleSync() {
  if (!stack.value) return
  if (!canSyncDeploy.value) {
    toast.add({
      title: 'Sync unavailable',
      description: `${syncDisabledReason.value}. Reconnect the worker before syncing this stack.`,
      color: 'warning',
    })
    return
  }
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
  if (stack.value?.status !== 'paused') {
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
    activeTab.value = 'logs'
    toast.add({ title: 'Force redeploy triggered', color: 'info' })
    forceOpts.value = { recreate_containers: true, recreate_volumes: false, recreate_networks: false }
    refreshLogs()
    setTimeout(() => { refreshStack(); refreshLogs(); servicesCard.value?.refresh() }, 5000)
  } catch (e: any) {
    toast.add({ title: e?.message || 'Force redeploy failed', color: 'error' })
  }
}

// Render-time overrides (image/ports/networks) — ephemeral, not committed to git
const showOverridesModal = ref(false)
type OverrideFormEntry = { image: string; ports: string; networks: string }
const overridesForm = ref<Record<string, OverrideFormEntry>>({})
const overrideServiceNames = computed(() => [...new Set(services.value.map((s: any) => s.service_name).filter(Boolean))] as string[])

function getOverrideServiceSlug(name: string) {
  const containersList = (stack.value?.containers_list || []) as { name?: string; slug?: string }[]
  return containersList.find(c => c.name === name)?.slug
}

// services (and therefore overrideServiceNames) can change reactively via realtime
// updates while the modal is open/closing — keep overridesForm in sync so the template
// never indexes a name that isn't there yet.
watch(overrideServiceNames, (names) => {
  const existing = (stack.value?.render_overrides || {}) as Record<string, { image?: string; ports?: string[]; networks?: string[] }>
  for (const name of names) {
    if (!overridesForm.value[name]) {
      const current = existing[name]
      overridesForm.value[name] = {
        image: current?.image || '',
        ports: joinList(current?.ports),
        networks: joinList(current?.networks),
      }
    }
  }
}, { immediate: true })

function joinList(value: unknown): string {
  return Array.isArray(value) ? value.join(', ') : ''
}

type OverrideValue = { image?: string; ports?: string[]; networks?: string[] }
const renderOverridesGit = ref<Record<string, OverrideValue>>({})
const renderOverridesGitError = ref('')

let renderOverridesDiffPending = false

// Called from both the render_overrides watcher and the realtime stack subscribe
// handler, which commonly fire together for the same update — skip starting a
// second fetch while one is already in flight rather than issuing both.
async function loadRenderOverridesDiff() {
  if (!stackHasRenderOverrides(stack.value)) {
    renderOverridesGit.value = {}
    renderOverridesGitError.value = ''
    return
  }
  if (renderOverridesDiffPending) return
  renderOverridesDiffPending = true
  try {
    const res = await getRenderOverridesDiff(stackId)
    renderOverridesGit.value = res.git || {}
    renderOverridesGitError.value = res.git_error || ''
  } catch {
    renderOverridesGit.value = {}
  } finally {
    renderOverridesDiffPending = false
  }
}

function sameList(a?: string[], b?: string[]): boolean {
  const x = a || []
  const y = b || []
  return x.length === y.length && x.every((v, i) => v === y[i])
}

type DiffLine = { text: string; type: 'context' | 'add' | 'remove' }

// Prefixes a line with a diff marker (-/+/space) followed by `indent` spaces of
// actual YAML indentation, so markers line up in column 0 like a real unified diff.
function diffLine(marker: '-' | '+' | ' ', indent: number, text: string): DiffLine {
  return {
    text: `${marker}${' '.repeat(indent)}${text}`,
    type: marker === '-' ? 'remove' : marker === '+' ? 'add' : 'context',
  }
}

const renderOverridesDiffLines = computed<DiffLine[]>(() => {
  const overrides = (stack.value?.render_overrides || {}) as Record<string, OverrideValue>
  const lines: DiffLine[] = []
  if (!Object.keys(overrides).length) return lines

  lines.push(diffLine(' ', 0, 'services:'))
  for (const [name, override] of Object.entries(overrides)) {
    const git = renderOverridesGit.value[name]
    lines.push(diffLine(' ', 2, `${name}:`))

    if (override.image) {
      if (git?.image && git.image !== override.image) {
        lines.push(diffLine('-', 4, `image: ${git.image}`))
        lines.push(diffLine('+', 4, `image: ${override.image}`))
      } else {
        lines.push(diffLine(' ', 4, `image: ${override.image}`))
      }
    }

    if (override.ports?.length) {
      if (git && !sameList(git.ports, override.ports)) {
        lines.push(diffLine('-', 4, 'ports:'))
        for (const p of git.ports || []) lines.push(diffLine('-', 6, `- ${p}`))
        lines.push(diffLine('+', 4, 'ports:'))
        for (const p of override.ports) lines.push(diffLine('+', 6, `- ${p}`))
      } else {
        lines.push(diffLine(' ', 4, 'ports:'))
        for (const p of override.ports) lines.push(diffLine(' ', 6, `- ${p}`))
      }
    }

    if (override.networks?.length) {
      if (git && !sameList(git.networks, override.networks)) {
        lines.push(diffLine('-', 4, 'networks:'))
        for (const n of git.networks || []) lines.push(diffLine('-', 6, `- ${n}`))
        lines.push(diffLine('+', 4, 'networks:'))
        for (const n of override.networks) lines.push(diffLine('+', 6, `- ${n}`))
      } else {
        lines.push(diffLine(' ', 4, 'networks:'))
        for (const n of override.networks) lines.push(diffLine(' ', 6, `- ${n}`))
      }
    }
  }
  return lines
})

function openOverridesModal() {
  const existing = (stack.value?.render_overrides || {}) as Record<string, { image?: string; ports?: string[]; networks?: string[] }>
  const form: Record<string, OverrideFormEntry> = {}
  for (const name of overrideServiceNames.value) {
    const current = existing[name]
    form[name] = {
      image: current?.image || '',
      ports: joinList(current?.ports),
      networks: joinList(current?.networks),
    }
  }
  overridesForm.value = form
  showOverridesModal.value = true
}

function splitList(value: string): string[] | undefined {
  const parts = value.split(',').map((p) => p.trim()).filter(Boolean)
  return parts.length ? parts : undefined
}

async function handleApplyOverrides() {
  const payload: Record<string, { image?: string; ports?: string[]; networks?: string[] }> = {}
  for (const [name, entry] of Object.entries(overridesForm.value)) {
    const override: { image?: string; ports?: string[]; networks?: string[] } = {}
    if (entry.image.trim()) override.image = entry.image.trim()
    const ports = splitList(entry.ports)
    if (ports) override.ports = ports
    const networks = splitList(entry.networks)
    if (networks) override.networks = networks
    if (Object.keys(override).length) payload[name] = override
  }
  if (!Object.keys(payload).length) {
    toast.add({ title: 'No overrides to apply', color: 'warning' })
    return
  }
  try {
    await setRenderOverrides(stackId, payload)
    showOverridesModal.value = false
    activeTab.value = 'logs'
    toast.add({ title: 'Render overrides applied, redeploying', color: 'info' })
    refreshLogs()
    // The PUT above already persists render_overrides, which the realtime 'stacks'
    // subscribe handler picks up and refreshes the diff for — no need to call
    // loadRenderOverridesDiff() again here.
    setTimeout(() => { refreshStack(); refreshLogs(); servicesCard.value?.refresh() }, 5000)
  } catch (e: any) {
    toast.add({ title: e?.message || 'Failed to apply overrides', color: 'error' })
  }
}

async function handleClearOverrides() {
  try {
    await clearRenderOverrides(stackId)
    showOverridesModal.value = false
    activeTab.value = 'logs'
    toast.add({ title: 'Render overrides cleared, reverting to Git state', color: 'info' })
    refreshLogs()
    // Same as apply: the DELETE above already clears render_overrides, and the
    // realtime 'stacks' subscribe handler refreshes the diff for that update.
    setTimeout(() => { refreshStack(); refreshLogs(); servicesCard.value?.refresh() }, 5000)
  } catch (e: any) {
    toast.add({ title: e?.message || 'Failed to clear overrides', color: 'error' })
  }
}

async function onStackDeleted() {
  showDeleteModal.value = false
  navigateTo('/stacks')
}

// Transfer stack modal
const showTransferModal = ref(false)

const dangerZoneActions = computed(() => [
  {
    key: 'transfer',
    label: 'Transfer Stack',
    description: 'Move this stack to another worker. Data will not be preserved.',
    buttonLabel: 'Transfer Stack',
    icon: 'i-lucide-arrow-right-left',
    color: 'warning' as const,
    onClick: () => { showTransferModal.value = true }
  },
  {
    key: 'remove',
    label: 'Remove Stack',
    description: 'This will stop all containers and delete the stack permanently.',
    buttonLabel: 'Remove Stack',
    onClick: () => { showDeleteModal.value = true }
  }
])
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

// stack is loaded async via useAsyncData, so stack.value may still be null when
// onMounted runs — watch (with immediate) instead of a one-shot call, so the diff
// loads once the record actually arrives (and reloads whenever overrides change).
watch(() => stack.value?.render_overrides, () => {
  loadRenderOverridesDiff()
}, { immediate: true })

onMounted(() => {
  loadServices()
  const workerRefreshTimer = window.setInterval(() => {
    refreshWorkers()
  }, 15000)
  
  // Subscribe to stack changes
  subscribe('stacks', (e) => {
    if (e.record?.id === stackId) {
      refreshStack()
      servicesCard.value?.refresh()
      loadRenderOverridesDiff()
    }
  })

  // Subscribe to sync logs changes
  subscribe('sync_logs', (e) => {
    if (e.record?.stack === stackId) {
      refreshLogs()
    }
  })

  subscribe('workers', () => {
    refreshWorkers()
  })

  // Keyboard shortcut: Cmd/Ctrl + S to trigger sync
  const handleKeydown = (event: KeyboardEvent) => {
    if ((event.metaKey || event.ctrlKey) && event.key === 's') {
      event.preventDefault()
      openSyncModal()
    }
  }
  window.addEventListener('keydown', handleKeydown)
  onUnmounted(() => {
    window.clearInterval(workerRefreshTimer)
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
      </div>
      <div v-if="stack?.containers_list?.length" class="mt-2 sm:mt-0 sm:ml-4 flex-1">
        <StackContainersList :containers="stack.containers_list" />
      </div>
      <div class="grid grid-cols-2 sm:flex sm:items-center gap-2 sm:shrink-0">
        <UButton
          :icon="stack?.status === 'paused' ? 'i-lucide-play' : 'i-lucide-pause'"
          :label="stack?.status === 'paused' ? 'Resume' : 'Pause'"
          :color="stack?.status === 'paused' ? 'success' : 'primary'"
          variant="outline"
          block
          @click="togglePause"
        />
        <UButton icon="i-lucide-recycle" label="Redeploy" variant="outline" block @click="showForceRedeploy = true" />
        <UButton icon="i-lucide-sliders-horizontal" label="Overrides" variant="outline" block @click="openOverridesModal" />
        <StackSyncButton
          :can-sync="canSyncDeploy"
          :disabled-reason="syncDisabledReason"
          @click="openSyncModal"
        />
      </div>
    </div>

    <UAlert
      v-if="stackHasRenderOverrides(stack)"
      title="Running with manual overrides"
      description="This stack is deployed with render-time overrides that are not committed to Git. They stay in effect on every deploy — including Sync Now and automatic reconciles — until you clear them below."
      icon="i-lucide-triangle-alert"
      color="warning"
      variant="subtle"
    >
      <template #actions>
        <UButton label="Manage overrides" size="xs" variant="outline" color="warning" @click="openOverridesModal" />
      </template>
    </UAlert>

    <UTabs v-model="activeTab" :items="tabs" />

    <!-- Overview -->
    <div v-if="activeTab === 'overview'" class="space-y-4">
      <UCard v-if="stack">
        <template #header>
          <h3 class="font-semibold">Status</h3>
        </template>
        <div class="grid grid-cols-3 gap-2 sm:gap-3">
          <StackStatusCard
            title="Git"
            :status="sourceStatus"
          />
          <StackStatusCard
            title="Deploy"
            :status="deployStatus"
          />
          <StackStatusCard
            title="Worker"
            :status="workerStatus"
            :tooltip="stack?.expand?.worker?.hostname || 'Unknown worker'"
          />
        </div>
      </UCard>

      <UCard>
        <template #header>
          <div class="flex justify-between items-center">
            <h3 class="font-semibold">Configuration</h3>
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
              <span class="ml-1">
                <WorkerNameLabel :name="stack?.expand?.worker?.hostname || 'Unknown'" />
              </span>
            </div>
          <div><span class="text-gray-500">Compose Path:</span> {{ stack?.compose_path || '.' }}</div>
          <div>
            <span class="text-gray-500">Compose File:</span>
            <button
              class="ml-1 text-yellow-400 hover:text-yellow-300 font-mono underline underline-offset-2 decoration-dotted transition-colors cursor-pointer"
              @click="openComposeViewer"
            >{{ stack?.compose_file || 'docker-compose.yml' }}</button>
          </div>
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
          <UFormField label="Compose Path" :error="editErrors.compose_path">
            <UInput v-model="editForm.compose_path" :disabled="isWireopsManaged" />
          </UFormField>
          <UFormField label="Compose File" :error="editErrors.compose_file">
            <UInput v-model="editForm.compose_file" :disabled="isWireopsManaged" />
          </UFormField>
          <div v-if="isWireopsManaged" class="col-span-2 text-xs text-gray-500">
            Compose path/file are managed by <code>{{ stack?.wireops_file_path }}</code> and can't be edited here.
          </div>
          <div class="col-span-2 flex justify-end gap-2">
            <UButton label="Cancel" variant="outline" @click="editing = false" />
            <UButton type="submit" label="Save" />
          </div>
        </form>
      </UCard>

      <UCard v-if="stackHasRenderOverrides(stack)">
        <template #header>
          <div class="flex justify-between items-center">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-sliders-horizontal" class="w-4 h-4 text-amber-500" />
              <h3 class="font-semibold">Render Overrides</h3>
              <UBadge color="warning" variant="subtle" size="sm">Active</UBadge>
            </div>
            <div class="flex items-center gap-1">
              <UButton icon="i-lucide-pencil" variant="ghost" size="xs" @click="openOverridesModal" />
              <UButton icon="i-lucide-x" label="Clear" variant="ghost" color="neutral" size="xs" @click="handleClearOverrides" />
            </div>
          </div>
        </template>
        <p class="text-xs text-gray-500 mb-3">
          Applied at deploy time, not committed to Git. Stays in effect on every deploy — including Sync Now and automatic reconciles — until cleared.
        </p>
        <p v-if="renderOverridesGitError" class="text-xs text-amber-500 mb-2">{{ renderOverridesGitError }}</p>
        <pre class="text-xs font-mono bg-gray-50 dark:bg-carbon-900/55 rounded-md p-3 overflow-x-auto"><span
          v-for="(line, i) in renderOverridesDiffLines"
          :key="i"
          class="block"
          :class="{
            'text-red-500 dark:text-red-400': line.type === 'remove',
            'text-emerald-600 dark:text-emerald-400': line.type === 'add',
            'text-gray-700 dark:text-wire-200/80': line.type === 'context',
          }"
        >{{ line.text }}</span></pre>
      </UCard>

      <StackServicesCard
        ref="servicesCard"
        :stack-id="stackId"
        :services="services"
        :container-stats="containerStats"
        :integration-actions="integrationActions"
        :containers-list="stack?.containers_list"
        @refresh="loadServices"
        @copy-container-id="copy($event, 'Container ID')"
        @show-logs="openContainerLogs"
        @container-action="openContainerActionModal($event.containerId, $event.containerName, $event.action)"
        @bulk-container-action="handleBulkContainerAction($event)"
      />

      <!-- Webhook Integration -->
      <AccordionCard v-model:open="showWebhookIntegration" title="Webhook Integration" icon="i-lucide-webhook">
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

          <div v-if="canOperate">
            <label class="text-xs text-gray-500 uppercase tracking-wide font-semibold">Webhook Secret</label>
            <div class="flex items-center gap-2 mt-1">
              <UInput
                v-model="webhookSecretInput"
                type="password"
                class="flex-1 font-mono text-xs"
                :placeholder="webhookSecretConfigured ? 'Configured — leave empty to keep current' : 'Not configured — required to enable this webhook'"
              />
              <UButton
                icon="i-lucide-refresh-cw"
                variant="outline"
                size="sm"
                title="Generate secret"
                @click="generateWebhookSecret"
              />
              <UButton
                size="sm"
                :loading="savingWebhookSecret"
                :disabled="!webhookSecretInput"
                @click="saveWebhookSecret"
              >
                Save
              </UButton>
            </div>
            <p class="text-xs text-gray-500 italic mt-1">
              Required before this webhook accepts requests. GitHub sends this as the HMAC key for
              <code>X-Hub-Signature-256</code>.
            </p>
          </div>
          <p v-else-if="!webhookSecretConfigured" class="text-xs text-amber-600 dark:text-amber-400">
            No webhook secret configured — this webhook will reject all requests until an operator sets one.
          </p>

          <details class="text-xs">
            <summary class="cursor-pointer text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200 font-medium">
              Show usage examples
            </summary>
            <div class="mt-2 space-y-2 text-gray-600 dark:text-gray-400">
              <div>
                <p class="font-semibold mb-1">GitHub:</p>
                <pre class="p-2 bg-gray-100 dark:bg-gray-800 rounded overflow-x-auto">curl -X POST {{ webhookUrl ?? '...' }} \
  -H "X-Hub-Signature-256: sha256=&lt;hmac-sha256 of body, keyed with the webhook secret&gt;" \
  -H "Content-Type: application/json" \
  -d '{"ref":"refs/heads/main"}'</pre>
              </div>
              <p class="text-xs text-gray-500 italic">
                Configure this URL and secret as a GitHub webhook (content type
                <code>application/json</code>). Requests without a valid signature are rejected;
                pushes to a branch other than the one tracked by this stack are accepted but skipped.
              </p>
            </div>
          </details>
        </div>
      </AccordionCard>

      <!-- Danger Zone -->
      <DangerZoneCard v-model:open="showDangerZone" :actions="dangerZoneActions" />
    </div>

    <!-- Variables -->
    <div v-if="activeTab === 'env'" class="space-y-4">
      <EnvironmentVariablesCard target-type="stack" :target-id="stackId" @keys-changed="localEnvKeys = $event" />

      <GlobalVariablesExporter target-type="stack" :target-id="stackId" :local-keys="localEnvKeys" />
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
        <div v-if="hasRunningLog || liveOutputLines.length" class="mb-3 rounded-md border border-default bg-elevated p-2">
          <div class="flex items-center gap-1.5 text-xs text-gray-500 mb-1">
            <UIcon
              :name="hasRunningLog ? 'i-lucide-loader-circle' : 'i-lucide-check-circle-2'"
              :class="['w-3.5 h-3.5', hasRunningLog ? 'animate-spin text-blue-500' : 'text-green-500']"
            />
            <span>{{ hasRunningLog ? 'Live output' : 'Live output (finished)' }}</span>
          </div>
          <TerminalOutput v-if="liveOutputLines.length" :lines="liveOutputLines" />
          <p v-else class="text-xs text-gray-400">Waiting for output…</p>
        </div>
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
            <details class="text-xs" :open="expandedTimelineLogIds.has(log.id)" @toggle="toggleTimeline(log.id, $event)">
              <summary class="cursor-pointer text-gray-400 hover:text-gray-600">Show timeline</summary>
              <DeployTimeline :sync-log-id="log.id" class="mt-2" />
            </details>
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
            <UAlert
              v-else-if="log.status === 'noop'"
              title="No Changes"
              :description="log.output || 'The rendered compose file is already current, so no deployment was run.'"
              icon="i-lucide-minus-circle"
              color="neutral"
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

    <!-- Container Logs Drawer -->
    <USlideover v-model:open="showLogsModal" title="Container Logs" class="w-full sm:w-[800px] md:w-[1000px] max-w-full">
      <template #content>
        <div class="p-4 h-full flex flex-col space-y-4">
          <div class="flex items-center justify-between shrink-0">
            <h3 class="font-semibold text-lg">{{ logsContainerName }}</h3>
            <div class="flex items-center gap-2">
              <UTooltip :text="logsWordWrap ? 'Disable Word Wrap' : 'Enable Word Wrap'">
                <UButton
                  :icon="logsWordWrap ? 'i-lucide-wrap-text' : 'i-lucide-align-left'"
                  variant="soft"
                  color="neutral"
                  size="sm"
                  @click="logsWordWrap = !logsWordWrap"
                />
              </UTooltip>
              <UButton icon="i-lucide-x" variant="ghost" size="sm" @click="showLogsModal = false" />
            </div>
          </div>
          <div class="flex-1 overflow-hidden bg-gray-900 rounded-lg">
            <pre class="h-full p-4 text-gray-100 text-xs font-mono overflow-auto" :class="{'whitespace-pre-wrap break-all': logsWordWrap, 'whitespace-pre': !logsWordWrap}">{{ logsContent }}</pre>
          </div>
        </div>
      </template>
    </USlideover>

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

    <!-- Render Overrides Modal -->
    <UModal v-model:open="showOverridesModal">
      <template #content>
        <div class="p-4 space-y-4">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold">Render Overrides</h3>
            <UButton icon="i-lucide-x" variant="ghost" size="xs" @click="showOverridesModal = false" />
          </div>
          <p class="text-sm text-gray-500">
            Override image, ports, or networks per service at deploy time without touching Git. Blank field keeps Git value. Stays active on every deploy until cleared.
          </p>
          <p v-if="!overrideServiceNames.length" class="text-sm text-gray-400 py-4 text-center">
            No services detected yet — load the stack's services before setting overrides.
          </p>
          <div v-else class="space-y-4 max-h-96 overflow-y-auto">
            <div v-for="name in overrideServiceNames" :key="name" class="space-y-2 border border-gray-200 dark:border-carbon-700 rounded-md p-3">
              <div class="flex items-center gap-2">
                <ContainerIcon
                  :name="name"
                  :slug="getOverrideServiceSlug(name)"
                  wrapper-class="w-6 h-6 flex shrink-0 items-center justify-center rounded bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden"
                  icon-class="w-4 h-4 object-contain"
                />
                <p class="text-sm font-medium">{{ name }}</p>
              </div>
              <template v-if="overridesForm[name]">
                <UFormField label="Image" size="sm">
                  <AppTextInput v-model="overridesForm[name].image" placeholder="e.g. nginx:test" aria-label="Image override" />
                </UFormField>
                <UFormField label="Ports" size="sm" hint="comma-separated, e.g. 8081:80, 9090:90">
                  <AppTextInput v-model="overridesForm[name].ports" placeholder="8081:80" aria-label="Ports override" />
                </UFormField>
                <UFormField label="Networks" size="sm" hint="comma-separated network names">
                  <AppTextInput v-model="overridesForm[name].networks" placeholder="proxy" aria-label="Networks override" />
                </UFormField>
              </template>
            </div>
          </div>
          <div class="flex justify-end gap-2 pt-1">
            <UButton
              v-if="stackHasRenderOverrides(stack)"
              label="Clear overrides"
              variant="outline"
              color="neutral"
              @click="handleClearOverrides"
            />
            <UButton label="Apply overrides" color="warning" @click="handleApplyOverrides" />
          </div>
        </div>
      </template>
    </UModal>

    <StackSyncModal
      v-model:open="showSyncModal"
      :stack="stack"
      :disabled="!canSyncDeploy"
      :disabled-reason="`${syncDisabledReason}. Reconnect the worker before syncing this stack.`"
      @synced="onSyncTriggered"
    />

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
          v-if="showDeleteModal"
          :stack="stack"
          :worker-offline="workerOffline"
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

    <!-- Bulk container action confirm modal -->
    <UModal v-model:open="showBulkActionModal">
      <template #content>
        <div class="p-6 space-y-5">
          <!-- Header -->
          <div class="flex items-start gap-4">
            <div
              class="flex items-center justify-center w-10 h-10 rounded-lg shrink-0"
              :class="bulkActionState.action === 'stop' ? 'bg-yellow-400/10' : 'bg-blue-400/10'"
            >
              <UIcon
                :name="bulkActionState.action === 'stop' ? 'i-lucide-square' : 'i-lucide-rotate-cw'"
                class="w-5 h-5"
                :class="bulkActionState.action === 'stop' ? 'text-yellow-400' : 'text-blue-400'"
              />
            </div>
            <div>
              <h3 class="font-semibold text-gray-900 dark:text-white text-base">
                {{ bulkActionState.action === 'stop' ? 'Stop All Containers' : 'Restart All Containers' }}
              </h3>
              <p class="text-sm text-gray-500 dark:text-gray-400 mt-1">
                The following {{ bulkActionState.containers.length }} container{{ bulkActionState.containers.length !== 1 ? 's' : '' }} will be affected:
              </p>
            </div>
          </div>

          <!-- Container list -->
          <div class="rounded-lg border border-gray-200 dark:border-gray-700 divide-y divide-gray-100 dark:divide-gray-700/60 max-h-64 overflow-y-auto">
            <div
              v-for="c in bulkActionState.containers"
              :key="c.containerId"
              class="flex items-center gap-3 px-3 py-2"
            >
              <UIcon name="i-lucide-container" class="w-4 h-4 shrink-0 text-gray-400" />
              <span class="text-sm font-medium text-gray-900 dark:text-white truncate flex-1 min-w-0">{{ c.containerName }}</span>
              <code class="text-xs font-mono text-gray-400 dark:text-gray-500 bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded shrink-0">
                {{ c.containerId.slice(0, 12) }}
              </code>
            </div>
          </div>

          <!-- Actions -->
          <div class="flex justify-end gap-2 pt-1">
            <UButton
              label="Cancel"
              variant="outline"
              color="neutral"
              :disabled="bulkActionLoading"
              @click="showBulkActionModal = false"
            />
            <UButton
              :label="bulkActionState.action === 'stop' ? 'Stop All' : 'Restart All'"
              :color="bulkActionState.action === 'stop' ? 'warning' : 'info'"
              :icon="bulkActionState.action === 'stop' ? 'i-lucide-square' : 'i-lucide-rotate-cw'"
              :loading="bulkActionLoading"
              @click="executeBulkContainerAction"
            />
          </div>
        </div>
      </template>
    </UModal>
  </div>
</template>
