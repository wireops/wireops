<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const { $pb } = useNuxtApp()
const { triggerJobRun, cancelJobRun, getJobDefinition, getJobRaw } = useApi()
const { copy } = useCopy()
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

// The runs logic has been moved to JobRunsList.vue
const jobRunsListRef = ref<any>(null)

const localEnvKeys = ref<string[]>([])

const definition = ref<any>(null)
const definitionError = ref('')
const definitionErrors = ref<string[]>([])

async function loadDefinition() {
  definitionError.value = ''
  definitionErrors.value = []
  definition.value = null
  try {
    definition.value = await getJobDefinition(jobId.value)
  } catch (e: any) {
    definitionError.value = e?.message || 'Failed to load definition'
    if (e?.data?.errors) {
      definitionErrors.value = e.data.errors
    } else {
      definitionErrors.value = [definitionError.value]
    }
  }
}

// YAML file viewer
const showYamlModal = ref(false)
const yamlContent = ref('')
const yamlFilename = ref('')

async function openYamlViewer() {
  try {
    const res = await getJobRaw(jobId.value)
    yamlContent.value = res.content
    yamlFilename.value = res.filename
    showYamlModal.value = true
  } catch (e: any) {
    toast.add({ title: e?.message || 'Failed to load YAML file', color: 'error' })
  }
}

function downloadYamlFile() {
  const blob = new Blob([yamlContent.value], { type: 'text/yaml' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = yamlFilename.value || 'job.yaml'
  a.click()
  URL.revokeObjectURL(url)
  toast.add({ title: 'YAML file downloaded', color: 'success' })
}

onMounted(() => {
  loadDefinition()
  subscribe('scheduled_jobs', (data: any) => {
    if (data.record?.id === jobId.value) { refreshJob(); loadDefinition() }
  })
})

// Cancel running logic moved to component

// Manual run
const triggering = ref(false)
async function runNow() {
  triggering.value = true
  try {
    await triggerJobRun(jobId.value)
    toast.add({ title: 'Job dispatched', description: 'A manual run has been queued.', color: 'success' })
    setTimeout(() => {
      if (jobRunsListRef.value?.refreshRuns) jobRunsListRef.value.refreshRuns()
    }, 1000)
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

// Render functions moved to component
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
            {{ job?.name || definition?.name || 'Invalid Job' }}
          </h1>
          <p v-if="job?.description || definition?.description" class="text-sm text-gray-500 dark:text-wire-200/60 mt-0.5">
            {{ job?.description || definition.description }}
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
        <JobsJobRunButton
          :enabled="job?.enabled"
          :has-error="definitionErrors.length > 0"
          :loading="triggering"
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
    <div v-if="activeTab === 'runs'">
      <JobsJobRunsList ref="jobRunsListRef" :job-id="jobId" />
    </div>

    <!-- Env Vars tab -->
    <div v-if="activeTab === 'env'" class="space-y-4">
      <EnvironmentVariablesCard target-type="job" :target-id="jobId" @keys-changed="localEnvKeys = $event" />
      <GlobalVariablesExporter target-type="job" :target-id="jobId" :local-keys="localEnvKeys" />
    </div>

    <!-- Definition tab -->
    <div v-if="activeTab === 'definition'" class="space-y-4">
      <div v-if="definitionErrors.length > 0" class="space-y-2">
        <div
          v-for="(err, idx) in definitionErrors"
          :key="idx"
          class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3"
        >
          <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
          <p class="text-sm text-red-500">{{ err }}</p>
        </div>
      </div>

      <UCard v-else-if="definition">
        <template #header>
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-file-code" class="w-4 h-4 text-yellow-400" />
              <span class="text-sm font-semibold">{{ job?.job_file }}</span>
              <UBadge label="read-only" variant="subtle" color="neutral" size="xs" />
            </div>
            <UButton
              label="View YAML"
              color="primary"
              variant="outline"
              size="sm"
              icon="i-lucide-file-code"
              @click="openYamlViewer"
            />
          </div>
        </template>

        <div class="space-y-6">
          <!-- Section 1: Scheduling & Dispatch -->
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-1.5 mb-3">
              <UIcon name="i-lucide-calendar" class="w-4 h-4 text-yellow-400" />
              Scheduling & Dispatch
            </h3>
            <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 text-sm bg-gray-50 dark:bg-carbon-900/20 p-4 rounded-xl border border-gray-200 dark:border-carbon-800">
              <div class="space-y-1">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-calendar-days" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Cron Schedule
                </p>
                <code class="font-mono text-gray-800 dark:text-wire-200">{{ definition.cron }}</code>
              </div>
              <div class="space-y-1">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-git-fork" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Dispatch Mode
                </p>
                <UBadge :label="definition.mode || 'once'" variant="subtle" color="neutral" size="sm" />
              </div>
              <div class="space-y-1">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-tags" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Worker Tags
                </p>
                <div v-if="definition.tags?.length" class="flex flex-wrap gap-1">
                  <UBadge v-for="tag in definition.tags" :key="tag" :label="tag" variant="subtle" color="primary" size="sm" class="font-mono" />
                </div>
                <span v-else class="text-xs text-gray-400 italic">No tags specified</span>
              </div>
            </div>
          </div>

          <!-- Section 2: Container Details -->
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-1.5 mb-3">
              <UIcon name="i-lucide-box" class="w-4 h-4 text-yellow-400" />
              Container Configuration
            </h3>
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm bg-gray-50 dark:bg-carbon-900/20 p-4 rounded-xl border border-gray-200 dark:border-carbon-800">
              <div class="space-y-1 sm:col-span-2">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-box" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Image
                </p>
                <ImageNameLabel :name="definition.image" />
              </div>
              <div class="space-y-1 sm:col-span-2">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-terminal" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Command
                </p>
                <CommandLineLabel :command="Array.isArray(definition.command) ? definition.command.join(' ') : (definition.command || '')" />
              </div>
              <div class="space-y-1">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-network" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Network
                </p>
                <UBadge v-if="definition.network" :label="definition.network" variant="subtle" color="info" size="sm" class="font-mono" />
                <span v-else class="text-xs text-gray-400 italic">Default bridge</span>
              </div>
              <div class="space-y-1">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-hard-drive" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Volumes
                </p>
                <div v-if="definition.volumes?.length" class="flex flex-col gap-1.5">
                  <code v-for="v in definition.volumes" :key="v" class="block font-mono text-gray-800 dark:text-wire-200 text-xs bg-gray-100 dark:bg-carbon-800 px-2 py-1 rounded w-fit select-all">{{ v }}</code>
                </div>
                <span v-else class="text-xs text-gray-400 italic">No volumes mounted</span>
              </div>
            </div>
          </div>

          <!-- Section 3: Resource Limits -->
          <div v-if="definition.resources">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-wire-200 flex items-center gap-1.5 mb-3">
              <UIcon name="i-lucide-cpu" class="w-4 h-4 text-yellow-400" />
              Resource Limits & Timeout
            </h3>
            <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 text-sm bg-gray-50 dark:bg-carbon-900/20 p-4 rounded-xl border border-gray-200 dark:border-carbon-800">
              <div class="space-y-1">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-cpu" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  CPU Limit
                </p>
                <code class="font-mono text-gray-800 dark:text-wire-200">{{ definition.resources.cpu || '—' }}</code>
              </div>
              <div class="space-y-1">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-database" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Memory Limit
                </p>
                <code class="font-mono text-gray-800 dark:text-wire-200">{{ definition.resources.memory || '—' }}</code>
              </div>
              <div class="space-y-1">
                <p class="text-xs font-semibold uppercase tracking-wider text-gray-400 dark:text-wire-200/40 flex items-center gap-1.5">
                  <UIcon name="i-lucide-hourglass" class="w-3.5 h-3.5 text-gray-400 dark:text-wire-200/40 shrink-0" />
                  Timeout Duration
                </p>
                <code class="font-mono text-gray-800 dark:text-wire-200">{{ definition.resources.timeout || '—' }}</code>
              </div>
            </div>
          </div>
        </div>
      </UCard>

      <div v-else class="text-center py-10">
        <USkeleton class="h-40 w-full rounded-xl" />
      </div>
    </div>

    <!-- YAML File Modal -->
    <UModal v-model:open="showYamlModal">
      <template #content>
        <UCard class="w-full max-w-4xl max-h-[85vh] flex flex-col">
          <template #header>
            <div class="flex items-center justify-between">
              <h3 class="font-semibold text-sm">{{ yamlFilename }}</h3>
              <div class="flex items-center gap-1">
                <UButton icon="i-lucide-copy" variant="ghost" size="xs" title="Copy" @click="copy(yamlContent, 'YAML file')" />
                <UButton icon="i-lucide-download" variant="ghost" size="xs" title="Download" @click="downloadYamlFile" />
                <UButton icon="i-lucide-x" variant="ghost" size="xs" @click="showYamlModal = false" />
              </div>
            </div>
          </template>
          <div class="overflow-y-auto max-h-[60vh] -mx-6 -my-4 p-6 bg-gray-950 text-gray-100 font-mono text-xs select-text">
            <YamlHighlighter :code="yamlContent" />
          </div>
        </UCard>
      </template>
    </UModal>
  </div>
</template>
