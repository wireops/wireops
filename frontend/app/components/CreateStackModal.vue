<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
const route = useRoute()
const router = useRouter()

const props = defineProps<{ open: boolean }>()

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'created'): void
}>()

const { $pb } = useNuxtApp()
const { getStackFiles, getWireopsFiles, getWireopsDefinitionFromFile, getWorkers, createStackFromWireops } = useApi()
const { validateComposePath, validateComposeFile } = useValidation()
const toast = useToast()

const { data: repos, refresh: refreshRepos } = useAsyncData('repos_for_create_stack', () =>
  $pb.collection('repositories').getFullList({ sort: 'name' })
)

// Worker tags are reported live by the worker agent (WORKER_TAGS env var),
// not a static DB column — must go through the enriched /api/custom/workers
// route (getWorkers), not a raw $pb.collection('workers') fetch, or the
// wireops.yaml worker.tags filter never matches anything.
const { data: workers, refresh: refreshWorkers } = useAsyncData('workers_for_create_stack', async () => {
  const all = await getWorkers()
  return all.filter(w => w.status === 'ACTIVE').sort((a, b) => a.hostname.localeCompare(b.hostname))
})

const defaultForm = () => ({
  name: '',
  repository: '',
  worker: '',
  compose_path: '',
  compose_file: 'docker-compose.yml',
  selected_file: '',
})

type WireopsDefinition = Awaited<ReturnType<typeof getWireopsDefinitionFromFile>>

const creationMode = ref<'manual' | 'wireops_file'>('wireops_file')
const form = ref(defaultForm())
const repoFiles = ref<string[]>([])
const loadingFiles = ref(false)
const saving = ref(false)
const createErrors = ref<{ worker?: string; compose_path?: string; compose_file?: string; selected_file?: string; wireops_file?: string }>({})

const wireopsFiles = ref<string[]>([])
const loadingWireopsFiles = ref(false)
const selectedWireopsFile = ref('')
const wireopsDefinition = ref<WireopsDefinition | null>(null)
const loadingDefinition = ref(false)
const definitionErrors = ref<string[]>([])

const modeOptions: { label: string; value: 'manual' | 'wireops_file' }[] = [
  { label: 'Manual', value: 'manual' },
  { label: 'From wireops.yaml', value: 'wireops_file' },
]

const repoOptions = computed(() =>
  (repos.value || []).map((r: any) => ({ label: `${r.name} (${r.git_url})`, value: r.id }))
)

function normalizeTag(t: unknown): string {
  return String(t ?? '').trim().toLowerCase()
}

const matchedWorkers = computed(() => {
  const list = workers.value || []
  const rawTags = wireopsDefinition.value?.worker?.tags
  const wantedTags = Array.isArray(rawTags) ? rawTags.map(normalizeTag).filter(Boolean) : []
  if (!wantedTags.length) return list
  return list.filter((w: any) => {
    const workerTags = Array.isArray(w.tags) ? w.tags.map(normalizeTag) : []
    return wantedTags.some(t => workerTags.includes(t))
  })
})

// Fall back to every active worker when the tag filter matches none —
// see the "No worker matches the required tags" UAlert below.
const workerOptions = computed(() => {
  const list = workers.value || []
  const filtered = workerTagsFilterEmpty.value ? list : matchedWorkers.value
  return filtered.map((a: any) => ({ label: a.hostname, value: a.id }))
})

const workerTagsFilterEmpty = computed(() => {
  const tags = wireopsDefinition.value?.worker?.tags
  return !!(Array.isArray(tags) && tags.length && matchedWorkers.value.length === 0)
})

const fileOptions = computed(() =>
  repoFiles.value.map(f => ({ label: f, value: f }))
)

const wireopsFileOptions = computed(() =>
  wireopsFiles.value.map(f => ({ label: f, value: f }))
)

const isMobile = ref(false)

onMounted(() => {
  isMobile.value = window.innerWidth < 768
  const resizeListener = () => { isMobile.value = window.innerWidth < 768 }
  window.addEventListener('resize', resizeListener)
  onUnmounted(() => window.removeEventListener('resize', resizeListener))
})

watch(() => props.open, async (val) => {
  if (val) {
    await Promise.all([refreshRepos(), refreshWorkers()])
    if (!route.query.stack_step) {
      router.replace({ query: { ...route.query, stack_step: '1' } })
    }
  } else {
    form.value = defaultForm()
    creationMode.value = 'wireops_file'
    repoFiles.value = []
    wireopsFiles.value = []
    selectedWireopsFile.value = ''
    wireopsDefinition.value = null
    definitionErrors.value = []
    createErrors.value = {}
    const q = { ...route.query }
    delete q.stack_step
    router.replace({ query: q })
  }
})

async function loadStackFiles(repoId: string) {
  const requestedRepo = repoId
  loadingFiles.value = true
  try {
    const files = await getStackFiles(repoId)
    if (form.value.repository !== requestedRepo) return
    repoFiles.value = files || []
    form.value.selected_file = repoFiles.value.length === 1 ? repoFiles.value[0]! : ''
  } catch {
    if (form.value.repository !== requestedRepo) return
    toast.add({ title: 'Failed to fetch repository files', color: 'error' })
    repoFiles.value = []
    form.value.selected_file = ''
  } finally {
    if (form.value.repository === requestedRepo) {
      loadingFiles.value = false
    }
  }
}

async function loadWireopsFiles(repoId: string) {
  const requestedRepo = repoId
  loadingWireopsFiles.value = true
  try {
    const files = await getWireopsFiles(repoId)
    if (form.value.repository !== requestedRepo) return
    wireopsFiles.value = files || []
    selectedWireopsFile.value = wireopsFiles.value.length === 1 ? wireopsFiles.value[0]! : ''
  } catch {
    if (form.value.repository !== requestedRepo) return
    toast.add({ title: 'Failed to fetch wireops.yaml files', color: 'error' })
    wireopsFiles.value = []
    selectedWireopsFile.value = ''
  } finally {
    if (form.value.repository === requestedRepo) {
      loadingWireopsFiles.value = false
    }
  }
}

watch(() => form.value.repository, async (repoId) => {
  repoFiles.value = []
  wireopsFiles.value = []
  form.value.selected_file = ''
  selectedWireopsFile.value = ''
  wireopsDefinition.value = null
  definitionErrors.value = []
  if (!repoId) return
  if (creationMode.value === 'manual') {
    await loadStackFiles(repoId)
  } else {
    await loadWireopsFiles(repoId)
  }
})

watch(creationMode, async (mode) => {
  wireopsDefinition.value = null
  definitionErrors.value = []
  createErrors.value = {}
  if (!form.value.repository) return
  if (mode === 'manual') {
    await loadStackFiles(form.value.repository)
  } else {
    await loadWireopsFiles(form.value.repository)
  }
})

watch(selectedWireopsFile, async (file) => {
  wireopsDefinition.value = null
  definitionErrors.value = []
  if (!file || !form.value.repository) return
  const requestedFile = file
  const requestedRepo = form.value.repository
  loadingDefinition.value = true
  try {
    const def = await getWireopsDefinitionFromFile(requestedRepo, file)
    if (selectedWireopsFile.value !== requestedFile || form.value.repository !== requestedRepo) return
    wireopsDefinition.value = def
  } catch (e: any) {
    if (selectedWireopsFile.value !== requestedFile || form.value.repository !== requestedRepo) return
    definitionErrors.value = e?.data?.errors || [e?.message || 'Invalid wireops.yaml']
  } finally {
    if (selectedWireopsFile.value === requestedFile && form.value.repository === requestedRepo) {
      loadingDefinition.value = false
    }
  }
})

const currentStep = computed(() => Number(route.query.stack_step) || 1)

const canProceedToStep2 = computed(() => {
  if (!form.value.repository) return false
  if (creationMode.value === 'manual') return !!form.value.name
  return !!wireopsDefinition.value && !wireopsDefinition.value.resolution_error && definitionErrors.value.length === 0
})

const stepperItems = computed(() => [
  {
    title: 'Basic Info',
    description: creationMode.value === 'manual' ? 'Name & Repository' : 'Repository & wireops.yaml',
    icon: 'i-lucide-info',
  },
  {
    title: 'Configuration',
    description: 'Worker & Compose File',
    icon: 'i-lucide-settings',
    disabled: !canProceedToStep2.value,
  }
])

const activeStep = computed({
  get() {
    return currentStep.value - 1
  },
  set(val) {
    if (val === 1) {
      if (!canProceedToStep2.value) return
      router.push({ query: { ...route.query, stack_step: '2' } })
    } else if (val === 0) {
      router.push({ query: { ...route.query, stack_step: '1' } })
    }
  }
})

function nextStep() {
  if (currentStep.value === 1) {
    if (!canProceedToStep2.value) return
    router.push({ query: { ...route.query, stack_step: '2' } })
  }
}

function prevStep() {
  if (currentStep.value > 1) {
    router.push({ query: { ...route.query, stack_step: String(currentStep.value - 1) } })
  }
}

function close() {
  emit('update:open', false)
}

async function handleSubmit() {
  if (currentStep.value === 1) {
    nextStep()
    return
  }

  createErrors.value = {}

  if (!form.value.worker) {
    createErrors.value.worker = 'Please select a worker'
    return
  }

  saving.value = true
  try {
    if (creationMode.value === 'wireops_file') {
      const def = wireopsDefinition.value
      if (!def || def.resolution_error) {
        createErrors.value.wireops_file = def?.resolution_error || 'Select a valid wireops.yaml file'
        return
      }

      // Every wireops.yaml-derived field (name, compose path/file, flags) is
      // computed server-side by re-parsing the file — the client only picks
      // repository/worker and points at the file path. This preview (`def`)
      // is display-only and never sent as the source of truth.
      await createStackFromWireops({
        repository: form.value.repository,
        worker: form.value.worker,
        wireops_file: selectedWireopsFile.value,
      })
    } else {
      const selected = form.value.selected_file
      if (!selected) {
        createErrors.value.selected_file = 'Please select a compose file'
        return
      }

      const parts = selected.split('/')
      if (parts.length === 1) {
        form.value.compose_path = '.'
        form.value.compose_file = selected
      } else {
        form.value.compose_file = parts.pop() || ''
        form.value.compose_path = parts.join('/')
      }

      const pathErr = validateComposePath(form.value.compose_path)
      const fileErr = validateComposeFile(form.value.compose_file)
      if (pathErr) createErrors.value.compose_path = pathErr
      if (fileErr) createErrors.value.compose_file = fileErr
      if (pathErr || fileErr) return

      await $pb.collection('stacks').create({
        name: form.value.name,
        repository: form.value.repository,
        worker: form.value.worker,
        compose_path: form.value.compose_path,
        compose_file: form.value.compose_file,
        auto_sync: true,
        status: 'pending',
        config_source: 'manual',
      })
    }
    emit('update:open', false)
    emit('created')
  } catch (e: any) {
    toast.add({ title: 'Failed to create stack', description: e?.message, color: 'error' })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <UModal
    :open="open"
    :fullscreen="isMobile"
    :ui="{ content: 'sm:max-w-2xl w-full' }"
    @update:open="emit('update:open', $event)"
  >
    <template #content>
      <form class="w-full" @submit.prevent="handleSubmit">
        <UCard class="sm:min-w-[640px] w-full" :ui="{ body: { base: 'p-6' }, header: { base: 'px-6 py-4' }, footer: { base: 'px-6 py-4' } }">
          <template #header>
            <div class="space-y-4">
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <UIcon name="i-lucide-layers" class="w-5 h-5 text-primary-500" />
                  <h2 class="font-semibold text-lg">Add Stack</h2>
                </div>
                <UButton
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-x"
                  class="-my-1"
                  aria-label="Close modal"
                  @click="close"
                />
              </div>

              <UStepper
                v-model="activeStep"
                :items="stepperItems"
                class="w-full"
              />
            </div>
          </template>

          <div class="space-y-4">
            <div v-show="currentStep === 1" class="space-y-4">
              <div class="flex gap-2">
                <UButton
                  v-for="opt in modeOptions"
                  :key="opt.value"
                  :label="opt.label"
                  size="sm"
                  :variant="creationMode === opt.value ? 'solid' : 'outline'"
                  :color="creationMode === opt.value ? 'primary' : 'neutral'"
                  type="button"
                  @click="() => { creationMode = opt.value }"
                />
              </div>

              <UFormField label="Repository" required>
                <AppSelectInput v-model="form.repository" :items="repoOptions" placeholder="Select a repository" class="w-full" />
              </UFormField>

              <template v-if="creationMode === 'manual'">
                <UFormField label="Name" required>
                  <AppTextInput v-model="form.name" placeholder="my-stack" aria-label="Stack name" />
                </UFormField>
              </template>

              <template v-else>
                <UFormField label="wireops.yaml file" required>
                  <div class="flex items-center gap-2">
                    <AppSelectInput
                      v-model="selectedWireopsFile"
                      :items="wireopsFileOptions"
                      placeholder="Select a wireops.yaml file"
                      :disabled="!form.repository || loadingWireopsFiles"
                      class="flex-1"
                    />
                    <UIcon v-if="loadingWireopsFiles" name="i-lucide-loader-2" class="w-5 h-5 animate-spin text-gray-400" />
                  </div>
                </UFormField>

                <UAlert
                  v-if="!loadingWireopsFiles && form.repository && wireopsFiles.length === 0"
                  color="warning"
                  icon="i-lucide-triangle-alert"
                  title="No wireops.yaml found"
                  description="No wireops.yaml or wireops.yml file was found in this repository."
                />

                <div v-if="loadingDefinition" class="flex items-center gap-2 text-sm text-gray-500">
                  <UIcon name="i-lucide-loader-2" class="w-4 h-4 animate-spin" />
                  Parsing wireops.yaml...
                </div>

                <UAlert
                  v-else-if="definitionErrors.length"
                  color="error"
                  icon="i-lucide-triangle-alert"
                  title="Invalid wireops.yaml"
                >
                  <template #description>
                    <ul class="list-disc list-inside">
                      <li v-for="(err, i) in definitionErrors" :key="i">{{ err }}</li>
                    </ul>
                  </template>
                </UAlert>

                <template v-else-if="wireopsDefinition">
                  <UAlert
                    v-if="wireopsDefinition.resolution_error"
                    color="error"
                    icon="i-lucide-file-x"
                    title="Compose file not resolved"
                    :description="wireopsDefinition.resolution_error"
                  />
                  <div v-else class="rounded-lg border border-gray-200 dark:border-wire-700 p-3 space-y-2 text-sm">
                    <div class="flex items-center gap-2 text-gray-900 dark:text-wire-100 font-medium">
                      <UIcon name="i-lucide-tag" class="w-4 h-4" />
                      <span>{{ wireopsDefinition.name }}</span>
                      <span class="text-xs font-normal text-gray-500">(name is set by wireops.yaml, not editable here)</span>
                    </div>
                    <div class="flex items-center gap-2 text-gray-700 dark:text-wire-200">
                      <UIcon name="i-lucide-file-code" class="w-4 h-4" />
                      <span>{{ wireopsDefinition.resolved_compose_path }}/{{ wireopsDefinition.resolved_compose_file }}</span>
                    </div>
                    <div class="flex flex-wrap gap-1.5">
                      <UBadge v-if="wireopsDefinition.deploy_timeout_seconds" :label="`timeout: ${wireopsDefinition.deploy_timeout_seconds}s`" variant="subtle" size="xs" />
                      <UBadge :label="`remove_orphans: ${wireopsDefinition.compose?.remove_orphans ?? true}`" variant="subtle" size="xs" />
                      <UBadge :label="`force_pull: ${wireopsDefinition.compose?.force_pull ?? false}`" variant="subtle" size="xs" />
                      <UBadge v-if="wireopsDefinition.jobs?.wait_running" label="waits for running jobs" color="warning" variant="subtle" size="xs" />
                    </div>
                    <div v-if="wireopsDefinition.worker?.tags?.length" class="flex flex-wrap gap-1.5">
                      <span class="text-xs text-gray-500">worker tags:</span>
                      <UBadge v-for="tag in wireopsDefinition.worker.tags" :key="tag" :label="tag" color="primary" variant="outline" size="xs" />
                    </div>
                  </div>
                </template>
              </template>
            </div>

            <div v-show="currentStep === 2" class="space-y-4">
              <UFormField label="Worker" :error="createErrors.worker" required>
                <AppSelectInput v-model="form.worker" :items="workerOptions" placeholder="Select a worker" class="w-full" />
              </UFormField>
              <UAlert
                v-if="workerTagsFilterEmpty"
                color="warning"
                icon="i-lucide-triangle-alert"
                title="No worker matches the required tags"
                description="Showing every active worker instead — the wireops.yaml worker.tags filter didn't match any of them."
              />

              <template v-if="creationMode === 'manual'">
                <div class="grid grid-cols-1 gap-4">
                  <UFormField
                    label="Compose File"
                    :error="createErrors.selected_file || createErrors.compose_path || createErrors.compose_file"
                    required
                  >
                    <div class="flex items-center gap-2">
                      <AppSelectInput
                        v-model="form.selected_file"
                        :items="fileOptions"
                        placeholder="Select a compose file"
                        :disabled="!form.repository || loadingFiles"
                        class="flex-1"
                      />
                      <UIcon v-if="loadingFiles" name="i-lucide-loader-2" class="w-5 h-5 animate-spin text-gray-400" />
                    </div>
                  </UFormField>
                </div>
              </template>
              <UAlert
                v-else-if="createErrors.wireops_file"
                color="error"
                icon="i-lucide-triangle-alert"
                :description="createErrors.wireops_file"
              />
            </div>
          </div>

          <template #footer>
            <div class="flex justify-between items-center w-full">
              <UButton v-if="currentStep > 1" label="Back" variant="outline" icon="i-lucide-arrow-left" @click="prevStep" />
              <div v-else/>

              <div class="flex gap-2">
                <UButton label="Cancel" variant="ghost" color="neutral" @click="close" />
                <UButton v-if="currentStep === 1" type="button" label="Next" icon="i-lucide-arrow-right" trailing :disabled="!canProceedToStep2" @click="nextStep" />
                <UButton v-else type="submit" label="Create" icon="i-lucide-check" :loading="saving" />
              </div>
            </div>
          </template>
        </UCard>
      </form>
    </template>
  </UModal>
</template>
