<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const { $pb } = useNuxtApp()
const { triggerJobRun, cancelJobRun, getJobDefinition } = useApi()
const { subscribe } = useRealtime()
const toast = useToast()

const jobId = computed(() => route.params.id as string)
const activeTab = ref('runs')
const tabs = [
  { id: 'runs', label: 'Runs', icon: 'i-lucide-history' },
  { id: 'env', label: 'Env Vars', icon: 'i-lucide-key' },
  { id: 'definition', label: 'Definition', icon: 'i-lucide-file-code' },
]

const { data: job, refresh: refreshJob } = useAsyncData(`job_${jobId.value}`, () =>
  $pb.collection('scheduled_jobs').getOne(jobId.value, { expand: 'repository' })
)

const { data: runs, refresh: refreshRuns } = useAsyncData(`job_runs_${jobId.value}`, () =>
  $pb.collection('job_runs').getFullList({
    filter: `job = "${jobId.value}"`,
    sort: '-created',
    expand: 'agent',
    perPage: 50,
  })
)

const { data: envVars, refresh: refreshEnvVars } = useAsyncData(`job_env_${jobId.value}`, () =>
  $pb.collection('job_env_vars').getFullList({ filter: `job = "${jobId.value}"`, sort: 'key' })
)

const definition = ref<any>(null)
const definitionError = ref('')

async function loadDefinition() {
  definitionError.value = ''
  try {
    definition.value = await getJobDefinition(jobId.value)
  } catch (e: any) {
    definitionError.value = e?.message || 'Failed to load definition'
  }
}

onMounted(() => {
  loadDefinition()
  subscribe('job_runs', (data: any) => {
    if (data.record?.job === jobId.value) refreshRuns()
  })
  subscribe('scheduled_jobs', (data: any) => {
    if (data.record?.id === jobId.value) { refreshJob(); loadDefinition() }
  })
})

// Env var editing
const newEnvKey = ref('')
const newEnvValue = ref('')
const newEnvSecret = ref(false)
const addingEnv = ref(false)

async function addEnvVar() {
  if (!newEnvKey.value.trim()) return
  addingEnv.value = true
  try {
    await $pb.collection('job_env_vars').create({
      job: jobId.value,
      key: newEnvKey.value.trim(),
      value: newEnvValue.value,
      secret: newEnvSecret.value,
    })
    newEnvKey.value = ''
    newEnvValue.value = ''
    newEnvSecret.value = false
    refreshEnvVars()
    toast.add({ title: 'Env var added', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to add env var', description: e?.message, color: 'error' })
  } finally {
    addingEnv.value = false
  }
}

async function deleteEnvVar(envId: string) {
  try {
    await $pb.collection('job_env_vars').delete(envId)
    refreshEnvVars()
    toast.add({ title: 'Env var removed', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to remove env var', description: e?.message, color: 'error' })
  }
}

// Cancel running job
const cancellingRunId = ref<string | null>(null)
async function cancelRun(run: any) {
  if (run.status !== 'running') return
  cancellingRunId.value = run.id
  try {
    await cancelJobRun(run.id)
    toast.add({ title: 'Job cancelled', description: 'Container stopped.', color: 'success' })
    setTimeout(() => refreshRuns(), 500)
  } catch (e: any) {
    toast.add({ title: 'Failed to cancel', description: e?.message, color: 'error' })
  } finally {
    cancellingRunId.value = null
  }
}

// Manual run
const triggering = ref(false)
async function runNow() {
  triggering.value = true
  try {
    await triggerJobRun(jobId.value)
    toast.add({ title: 'Job dispatched', description: 'A manual run has been queued.', color: 'success' })
    setTimeout(() => refreshRuns(), 1000)
  } catch (e: any) {
    toast.add({ title: 'Failed to trigger', description: e?.message, color: 'error' })
  } finally {
    triggering.value = false
  }
}

// Enable toggle
async function toggleEnabled() {
  if (!job.value) return
  try {
    await $pb.collection('scheduled_jobs').update(jobId.value, { enabled: !job.value.enabled })
    refreshJob()
    toast.add({ title: job.value.enabled ? 'Job disabled' : 'Job enabled', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed', description: e?.message, color: 'error' })
  }
}

// Expanded output rows
const expandedRun = ref<string | null>(null)

function runStatusColor(status: string) {
  switch (status) {
    case 'success': return 'success'
    case 'error': return 'error'
    case 'running': return 'primary'
    case 'stalled': return 'warning'
    default: return 'neutral'
  }
}

function runStatusIcon(status: string) {
  switch (status) {
    case 'success': return 'i-lucide-check-circle'
    case 'error': return 'i-lucide-x-circle'
    case 'running': return 'i-lucide-loader'
    case 'stalled': return 'i-lucide-pause-circle'
    default: return 'i-lucide-clock'
  }
}

function formatDate(d: string) {
  if (!d) return '—'
  return new Date(d).toLocaleString()
}

function formatRelative(d: string) {
  if (!d) return '—'
  const diff = Date.now() - new Date(d).getTime()
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return `${Math.floor(diff / 86_400_000)}d ago`
}
</script>

<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="flex items-start justify-between gap-4">
      <div class="flex items-start gap-3">
        <NuxtLink to="/jobs" class="mt-1">
          <UButton icon="i-lucide-arrow-left" variant="ghost" size="xs" color="neutral" />
        </NuxtLink>
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-wire-200 flex items-center gap-2">
            <UIcon name="i-lucide-calendar-clock" class="w-6 h-6 text-yellow-400" />
            {{ definition?.title || job?.job_file || '…' }}
          </h1>
          <p v-if="definition?.description" class="text-sm text-gray-500 dark:text-wire-200/60 mt-0.5">
            {{ definition.description }}
          </p>
          <div class="flex items-center gap-2 mt-1 flex-wrap">
            <span class="text-xs font-mono text-gray-400 dark:text-wire-200/40">
              {{ job?.expand?.repository?.name }} / {{ job?.job_file }}
            </span>
            <UBadge v-if="definition?.cron" :label="definition.cron" variant="subtle" color="neutral" size="xs" class="font-mono" />
            <UBadge label="EPHEMERAL" variant="subtle" color="primary" size="xs" class="font-mono" />
          </div>
        </div>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <USwitch :model-value="job?.enabled" size="sm" @update:model-value="toggleEnabled" />
        <UButton
          icon="i-lucide-play"
          label="Run now"
          :loading="triggering"
          :disabled="!job?.enabled"
          @click="runNow"
        />
      </div>
    </div>

    <!-- Tabs -->
    <div class="flex gap-1 border-b border-gray-200 dark:border-carbon-800">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        class="flex items-center gap-1.5 px-4 py-2 text-sm font-medium transition-colors border-b-2 -mb-px"
        :class="activeTab === tab.id
          ? 'border-yellow-400 text-yellow-400'
          : 'border-transparent text-gray-500 dark:text-wire-200/50 hover:text-gray-800 dark:hover:text-wire-200'"
        @click="activeTab = tab.id"
      >
        <UIcon :name="tab.icon" class="w-4 h-4" />
        {{ tab.label }}
      </button>
    </div>

    <!-- Runs tab -->
    <div v-if="activeTab === 'runs'" class="space-y-3">
      <div v-if="!runs || runs.length === 0" class="text-center py-10 text-gray-500 dark:text-wire-200/50 text-sm">
        No runs yet. Trigger a manual run or wait for the cron schedule.
      </div>
      <div v-else class="space-y-2">
        <div
          v-for="run in runs"
          :key="run.id"
          class="rounded-xl border border-gray-200 dark:border-carbon-700 bg-gray-50 dark:bg-carbon-800/40 overflow-hidden"
        >
          <div
            class="flex items-center justify-between px-4 py-3 cursor-pointer select-none"
            @click="expandedRun = expandedRun === run.id ? null : run.id"
          >
            <div class="flex items-center gap-3">
              <UIcon
:name="runStatusIcon(run.status)" class="w-4 h-4" :class="{
                'text-green-500': run.status === 'success',
                'text-red-500': run.status === 'error',
                'text-yellow-400 animate-spin': run.status === 'running',
                'text-amber-400': run.status === 'stalled',
                'text-gray-400': run.status === 'pending',
              }" />
              <div>
                <div class="flex items-center gap-2">
                  <UBadge :label="run.status" :color="runStatusColor(run.status)" variant="subtle" size="xs" />
                  <UBadge :label="run.trigger" variant="subtle" color="neutral" size="xs" />
                  <span v-if="run.expand?.agent?.hostname" class="text-xs text-gray-400 dark:text-wire-200/40 font-mono">
                    {{ run.expand.agent.hostname }}
                  </span>
                </div>
                <p class="text-xs text-gray-400 dark:text-wire-200/40 mt-0.5">{{ formatRelative(run.created) }}</p>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <UTooltip v-if="run.status === 'running'" text="Cancel job run">
                <UButton
                  icon="i-lucide-trash-2"
                  variant="ghost"
                  size="xs"
                  color="error"
                  :loading="cancellingRunId === run.id"
                  @click.stop="cancelRun(run)"
                />
              </UTooltip>
              <span v-if="run.duration_ms" class="text-xs text-gray-400 dark:text-wire-200/40 font-mono">
                {{ run.duration_ms }}ms
              </span>
              <UIcon
                :name="expandedRun === run.id ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
                class="w-4 h-4 text-gray-400"
              />
            </div>
          </div>
          <div v-if="expandedRun === run.id" class="border-t border-gray-200 dark:border-carbon-700">
            <!-- Meta: container + commit -->
            <div class="flex flex-wrap gap-x-6 gap-y-1 px-4 py-2 bg-gray-50/60 dark:bg-carbon-800/60 text-xs font-mono">
              <span v-if="run.container_name" class="flex items-center gap-1.5 text-gray-500 dark:text-wire-200/50">
                <UIcon name="i-lucide-box" class="w-3.5 h-3.5 shrink-0" />
                <span class="text-gray-700 dark:text-wire-200 select-all">{{ run.container_name }}</span>
              </span>
              <span v-if="run.commit_sha" class="flex items-center gap-1.5 text-gray-500 dark:text-wire-200/50">
                <UIcon name="i-lucide-git-commit-horizontal" class="w-3.5 h-3.5 shrink-0" />
                <span class="text-gray-700 dark:text-wire-200 select-all">{{ run.commit_sha.slice(0, 12) }}</span>
              </span>
            </div>
            <!-- Output -->
            <div v-if="run.output" class="p-3">
              <pre class="text-xs font-mono text-gray-800 dark:text-wire-200 bg-gray-100 dark:bg-carbon-950 rounded-lg px-4 py-3 whitespace-pre-wrap break-words max-h-64 overflow-y-auto">{{ run.output }}</pre>
            </div>
            <div v-else class="px-4 py-2">
              <p class="text-xs text-gray-400 dark:text-wire-200/40 italic">No output recorded.</p>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Env Vars tab -->
    <div v-if="activeTab === 'env'" class="space-y-4">
      <p class="text-sm text-gray-500 dark:text-wire-200/60">
        Secret key/value pairs injected at runtime. These are not committed to the repository.
      </p>

      <div v-if="envVars && envVars.length > 0" class="space-y-2">
        <div
          v-for="env in envVars"
          :key="env.id"
          class="flex items-center justify-between px-4 py-3 rounded-lg bg-gray-50 dark:bg-carbon-800/40 border border-gray-200 dark:border-carbon-700"
        >
          <div class="flex items-center gap-3 min-w-0">
            <UIcon
              :name="env.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
              class="w-4 h-4 shrink-0"
              :class="env.secret ? 'text-amber-400' : 'text-gray-400'"
            />
            <code class="text-sm font-mono text-gray-800 dark:text-wire-200">{{ env.key }}</code>
            <span v-if="!env.secret && env.value" class="text-sm text-gray-500 dark:text-wire-200/50 truncate max-w-xs font-mono">
              = {{ env.value }}
            </span>
            <span v-else-if="env.secret" class="text-xs text-amber-400/70">encrypted</span>
          </div>
          <UButton
            icon="i-lucide-trash-2"
            variant="ghost"
            size="xs"
            color="error"
            @click="deleteEnvVar(env.id)"
          />
        </div>
      </div>

      <!-- Add new env var -->
      <UCard>
        <template #header>
          <span class="text-sm font-semibold">Add variable</span>
        </template>
        <div class="flex flex-col sm:flex-row gap-3">
          <UInput v-model="newEnvKey" placeholder="KEY" class="font-mono flex-1" />
          <UInput v-model="newEnvValue" placeholder="value" class="font-mono flex-1" :type="newEnvSecret ? 'password' : 'text'" />
          <div class="flex items-center gap-2">
            <USwitch v-model="newEnvSecret" size="xs" />
            <span class="text-xs text-gray-500">Secret</span>
          </div>
          <UButton
            icon="i-lucide-plus"
            label="Add"
            :loading="addingEnv"
            :disabled="!newEnvKey.trim()"
            @click="addEnvVar"
          />
        </div>
      </UCard>
    </div>

    <!-- Definition tab -->
    <div v-if="activeTab === 'definition'" class="space-y-4">
      <div v-if="definitionError" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500">{{ definitionError }}</p>
      </div>

      <UCard v-else-if="definition">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-file-code" class="w-4 h-4 text-yellow-400" />
            <span class="text-sm font-semibold">{{ job?.job_file }}</span>
            <UBadge label="read-only" variant="subtle" color="neutral" size="xs" />
          </div>
        </template>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
          <div class="space-y-1">
            <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40">Image</p>
            <code class="font-mono text-gray-800 dark:text-wire-200">{{ definition.image }}</code>
          </div>
          <div class="space-y-1">
            <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40">Cron</p>
            <code class="font-mono text-gray-800 dark:text-wire-200">{{ definition.cron }}</code>
          </div>
          <div class="space-y-1">
            <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40">Mode</p>
            <UBadge :label="definition.mode || 'once'" variant="subtle" color="neutral" size="sm" />
          </div>
          <div class="space-y-1 sm:col-span-2">
            <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40">Command</p>
            <code class="font-mono text-gray-800 dark:text-wire-200 break-all">{{ Array.isArray(definition.command) ? definition.command.join(' ') : definition.command }}</code>
          </div>
          <div v-if="definition.tags?.length" class="space-y-1 sm:col-span-2">
            <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40">Agent tags</p>
            <div class="flex flex-wrap gap-1">
              <UBadge
                v-for="tag in definition.tags"
                :key="tag"
                :label="tag"
                variant="subtle"
                color="primary"
                size="sm"
                class="font-mono"
              />
            </div>
          </div>
          <div v-if="definition.volumes?.length" class="space-y-1 sm:col-span-2">
            <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40">Volumes</p>
            <div class="flex flex-wrap gap-1">
              <code
                v-for="v in definition.volumes"
                :key="v"
                class="block font-mono text-gray-800 dark:text-wire-200 text-xs bg-gray-100 dark:bg-carbon-800 px-2 py-1 rounded"
              >{{ v }}</code>
            </div>
          </div>
          <div v-if="definition.network" class="space-y-1">
            <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40">Network</p>
            <UBadge :label="definition.network" variant="subtle" color="info" size="sm" class="font-mono" />
          </div>
        </div>
      </UCard>

      <div v-else class="text-center py-10">
        <USkeleton class="h-40 w-full rounded-xl" />
      </div>
    </div>
  </div>
</template>
