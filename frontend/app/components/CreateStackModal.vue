<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { PendingEnvVar } from '../utils/envFileParser'
const route = useRoute()
const router = useRouter()

const props = defineProps<{ open: boolean }>()

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'created'): void
}>()

const { $pb } = useNuxtApp()
const { getStackFiles, getWireopsFiles, getWireopsDefinitionFromFile, getWorkers, createStackFromWireops, lintCompose, customPost } = useApi()
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
  group: '',
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
const createErrors = ref<{ worker?: string; compose_path?: string; compose_file?: string; selected_file?: string; wireops_file?: string; lint?: string }>({})

const pendingEnvVars = ref<PendingEnvVar[]>([])
const envVarsEditor = ref<{ commitDraft: () => void } | null>(null)

const wireopsFiles = ref<string[]>([])
const loadingWireopsFiles = ref(false)
const selectedWireopsFile = ref('')
const wireopsDefinition = ref<WireopsDefinition | null>(null)
const loadingDefinition = ref(false)
const definitionErrors = ref<string[]>([])

type LintResponse = Awaited<ReturnType<typeof lintCompose>>
const lintReport = ref<LintResponse['report'] | null>(null)
const lintContent = ref('')
const lintFilename = ref('')
const lintConfigError = ref('')
const lintLoading = ref(false)
const composePreview = ref<{ focusOn: (line: number) => void } | null>(null)
// Bumped on every lint request so a slow earlier response cannot overwrite a
// newer one when the user changes the file or worker mid-flight.
let lintRequestId = 0

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

watch(() => props.open, async (val) => {
  if (val) {
    await Promise.all([refreshRepos(), refreshWorkers()])
    // Always land on step 1 on a fresh open, even if the URL still carries a
    // stack_step from a previous session that never made it through close()
    // (e.g. a hard navigation away mid-wizard) — a stale step would otherwise
    // reopen the modal past steps whose state (form, lint report) was just
    // reset below.
    if (route.query.stack_step !== '1') {
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
    pendingEnvVars.value = []
    lintReport.value = null
    lintContent.value = ''
    lintFilename.value = ''
    lintConfigError.value = ''
    lintLoading.value = false
    lintRequestId++
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

// The compose file the stack will deploy, in the {path, file} split the API
// expects. Derived here rather than only inside handleSubmit so the lint step
// can point at exactly the file that is about to be created.
const composeTarget = computed<{ compose_path: string; compose_file: string } | null>(() => {
  if (creationMode.value === 'wireops_file') {
    const def = wireopsDefinition.value
    if (!def || def.resolution_error || !def.resolved_compose_file) return null
    return {
      compose_path: def.resolved_compose_path || '.',
      compose_file: def.resolved_compose_file,
    }
  }
  const selected = form.value.selected_file
  if (!selected) return null
  const parts = selected.split('/')
  if (parts.length === 1) return { compose_path: '.', compose_file: selected }
  const file = parts.pop() || ''
  return { compose_path: parts.join('/'), compose_file: file }
})

// Error-severity findings block creation the same way they block a deploy
// (see internal/sync/renderer.go and the stacks OnRecordCreate hook) — the
// server rejects the create either way, so the button reflects that instead
// of letting the user hit an opaque save failure.
const lintHasErrors = computed(() => (lintReport.value?.errors ?? 0) > 0)

const canProceedToStep2 = computed(() => {
  if (!form.value.repository) return false
  if (creationMode.value === 'manual') return !!form.value.name
  return !!wireopsDefinition.value && !wireopsDefinition.value.resolution_error && definitionErrors.value.length === 0
})

const canProceedToStep3 = computed(() =>
  canProceedToStep2.value && !!form.value.worker && !!composeTarget.value
)

// Steps before the current one get a green check instead of their own icon —
// data-[state=completed] already exists on the trigger/separator (reka-ui
// derives it from the step index vs the active one), this just overrides its
// color from the default primary/yellow so "done" reads differently from
// "active". A separator between two completed steps is solid green; one
// leading into the active step fades from green to yellow, since that leg
// itself isn't done yet.
const greenTrigger = 'group-data-[state=completed]:bg-green-500 dark:group-data-[state=completed]:bg-green-600'
const greenSeparator = 'group-data-[state=completed]:bg-green-500 dark:group-data-[state=completed]:bg-green-600'
const greenToYellowSeparator = 'group-data-[state=completed]:bg-gradient-to-r group-data-[state=completed]:from-green-500 group-data-[state=completed]:to-yellow-500 dark:group-data-[state=completed]:from-green-600'

function stepUi(stepNumber: number) {
  const completed = stepNumber < currentStep.value
  if (!completed) return undefined
  const nextAlsoCompleted = stepNumber + 1 < currentStep.value
  return {
    trigger: greenTrigger,
    separator: nextAlsoCompleted ? greenSeparator : greenToYellowSeparator,
  }
}

const stepperItems = computed(() => [
  {
    title: 'Basic Info',
    description: creationMode.value === 'manual' ? 'Name & Repository' : 'Repository & wireops.yaml',
    icon: currentStep.value > 1 ? 'i-lucide-check' : 'i-lucide-info',
    ui: stepUi(1),
  },
  {
    title: 'Configuration',
    description: 'Worker & Compose File',
    icon: currentStep.value > 2 ? 'i-lucide-check' : 'i-lucide-settings',
    ui: stepUi(2),
    disabled: !canProceedToStep2.value,
  },
  {
    title: 'Environment Variables',
    description: 'Optional',
    icon: currentStep.value > 3 ? 'i-lucide-check' : 'i-lucide-variable',
    ui: stepUi(3),
    disabled: !canProceedToStep3.value,
  },
  {
    title: 'Review',
    description: 'Compose checks',
    icon: 'i-lucide-shield-check',
    disabled: !canProceedToStep3.value,
  },
])

function canReachStep(step: number) {
  if (step >= 3) return canProceedToStep3.value
  if (step === 2) return canProceedToStep2.value
  return true
}

function goToStep(step: number) {
  if (!canReachStep(step)) return
  router.push({ query: { ...route.query, stack_step: String(step) } })
}

const activeStep = computed({
  get() {
    return currentStep.value - 1
  },
  set(val) {
    goToStep(val + 1)
  }
})

function nextStep() {
  const target = currentStep.value + 1
  if (target > stepperItems.value.length) return
  // Surface the reason the step after Configuration is out of reach instead
  // of having the button silently do nothing.
  if (target === 3 && !form.value.worker) {
    createErrors.value.worker = 'Please select a worker'
    return
  }
  // Commit any typed-but-not-added env var row before leaving that step.
  if (currentStep.value === 3) {
    envVarsEditor.value?.commitDraft()
  }
  goToStep(target)
}

function prevStep() {
  if (currentStep.value > 1) {
    router.push({ query: { ...route.query, stack_step: String(currentStep.value - 1) } })
  }
}

async function runLint() {
  const target = composeTarget.value
  if (!form.value.repository || !target) return

  const requestId = ++lintRequestId
  lintLoading.value = true
  // The previous result is deliberately left in place while the new one is
  // fetched: clearing it here would unmount the preview and lose the reader's
  // scroll position on every "Re-run checks". It is replaced below, or
  // cleared only when the new run has nothing to show.
  lintConfigError.value = ''
  try {
    const res = await lintCompose({
      repository: form.value.repository,
      compose_path: target.compose_path,
      compose_file: target.compose_file,
      worker: form.value.worker,
    })
    if (requestId !== lintRequestId) return
    lintReport.value = res.report
    lintContent.value = res.content || ''
    lintFilename.value = res.filename || target.compose_file
    lintConfigError.value = res.config_error || ''
  } catch (e: any) {
    if (requestId !== lintRequestId) return
    // A failed run must not leave the previous file on screen looking current.
    lintReport.value = null
    lintContent.value = ''
    lintFilename.value = ''
    lintConfigError.value = e?.message || 'Failed to check the compose file'
  } finally {
    if (requestId === lintRequestId) lintLoading.value = false
  }
}

function focusOnLine(line: number) {
  composePreview.value?.focusOn(line)
}

// Lint on entering the Review step, and re-lint if the user goes back and
// changes the file or worker before returning.
//
// composeTarget is keyed by value, not identity: it returns a fresh object on
// every recompute, so watching the ref itself would re-lint whenever an
// unrelated dependency changed — and each lint costs a server-side clone/fetch
// plus a `docker compose config` subprocess.
watch(
  [
    currentStep,
    () => form.value.worker,
    () => composeTarget.value && `${composeTarget.value.compose_path}/${composeTarget.value.compose_file}`,
  ],
  ([step]) => {
    if (step === 4) runLint()
  },
)

// Clear the "pick a worker" error as soon as one is picked, rather than
// leaving it on screen until the next submit.
watch(() => form.value.worker, (worker) => {
  if (worker) createErrors.value.worker = undefined
})

// Clear the stale "fix the errors" message as soon as a re-lint comes back
// clean, rather than leaving it up alongside a now-enabled Create button.
watch(lintHasErrors, (hasErrors) => {
  if (!hasErrors) createErrors.value.lint = undefined
})

function close() {
  emit('update:open', false)
}

async function handleSubmit() {
  if (currentStep.value < stepperItems.value.length) {
    nextStep()
    return
  }

  createErrors.value = {}

  if (!form.value.worker) {
    createErrors.value.worker = 'Please select a worker'
    return
  }

  if (lintHasErrors.value) {
    createErrors.value.lint = 'Fix the errors reported by the static checks before creating this stack'
    return
  }

  // If the wizard has pending env vars, the stack is created paused instead
  // of pending — the first auto-deploy (triggered unconditionally on create,
  // see OnRecordAfterCreateSuccess("stacks") in internal/hooks/pb_hooks.go)
  // would otherwise race the env-vars bulk save below and could run without
  // them. Once the vars are saved, the stack is resumed and a sync is
  // triggered immediately instead of waiting for the next cron tick.
  const hasPendingEnvVars = pendingEnvVars.value.length > 0

  saving.value = true
  try {
    let stackId: string
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
      const created = await createStackFromWireops({
        repository: form.value.repository,
        worker: form.value.worker,
        wireops_file: selectedWireopsFile.value,
        paused: hasPendingEnvVars,
      })
      stackId = created.id
    } else {
      const target = composeTarget.value
      if (!target) {
        createErrors.value.selected_file = 'Please select a compose file'
        return
      }

      form.value.compose_path = target.compose_path
      form.value.compose_file = target.compose_file

      const pathErr = validateComposePath(form.value.compose_path)
      const fileErr = validateComposeFile(form.value.compose_file)
      if (pathErr) createErrors.value.compose_path = pathErr
      if (fileErr) createErrors.value.compose_file = fileErr
      if (pathErr || fileErr) return

      const created = await $pb.collection('stacks').create({
        name: form.value.name,
        repository: form.value.repository,
        worker: form.value.worker,
        group: form.value.group,
        compose_path: form.value.compose_path,
        compose_file: form.value.compose_file,
        auto_sync: true,
        status: hasPendingEnvVars ? 'paused' : 'pending',
        config_source: 'manual',
      })
      stackId = created.id
    }

    if (hasPendingEnvVars) {
      let envVarsSaved = false
      try {
        await customPost(`/api/custom/stacks/${stackId}/env-vars/bulk`, {
          mode: 'replace',
          vars: pendingEnvVars.value,
        })
        envVarsSaved = true
      } catch (envErr: any) {
        toast.add({
          title: 'Stack created, but environment variables failed to save',
          description: `${envErr?.data?.error || envErr?.message || 'Unknown error'} — the stack was left paused. Add the variables from the Variables tab and resume it manually.`,
          color: 'warning',
        })
      }

      if (envVarsSaved) {
        try {
          await $pb.collection('stacks').update(stackId, { status: 'active' })
          await customPost(`/api/custom/stacks/${stackId}/sync`)
          toast.add({ title: 'Stack created', description: 'Environment variables saved — deploying now.', color: 'success' })
        } catch {
          toast.add({
            title: 'Stack created and variables saved, but could not resume automatically',
            description: 'Resume the stack manually from its page to deploy it.',
            color: 'warning',
          })
        }
      }
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
    :ui="{ content: currentStep === 4 ? 'sm:max-w-5xl w-full' : 'sm:max-w-2xl w-full' }"
    @update:open="emit('update:open', $event)"
  >
    <template #content>
      <form class="w-full" @submit.prevent="handleSubmit">
        <AppPanelCard class="sm:min-w-[640px] w-full" :ui="{ body: { base: 'p-6' }, header: { base: 'px-6 py-4' }, footer: { base: 'px-6 py-4' } }">
          <template #header>
            <div class="space-y-4">
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <UIcon name="i-lucide-layers" class="w-5 h-5 text-primary-500" />
                  <h2 class="font-semibold text-lg">Add Stack</h2>
                </div>
                <CloseButton
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
                <UFormField label="Group">
                  <AppTextInput v-model="form.group" placeholder="e.g. observability" aria-label="Stack group" />
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
                  <div v-else class="rounded-lg border border-gray-300 dark:border-wire-700 p-3 space-y-2 text-sm">
                    <div class="flex items-center gap-2 text-gray-900 dark:text-wire-200 font-medium">
                      <UIcon name="i-lucide-tag" class="w-4 h-4" />
                      <span>{{ wireopsDefinition.name }}</span>
                      <span class="text-xs font-normal text-gray-500">(name is set by wireops.yaml, not editable here)</span>
                    </div>
                    <div class="flex items-center gap-2 text-gray-700 dark:text-wire-200">
                      <UIcon name="i-lucide-file-code" class="w-4 h-4" />
                      <span>{{ wireopsDefinition.resolved_compose_path }}/{{ wireopsDefinition.resolved_compose_file }}</span>
                    </div>
                    <div class="flex flex-wrap gap-1.5">
                      <UBadge v-if="wireopsDefinition.group" :label="`group: ${wireopsDefinition.group}`" color="neutral" variant="outline" size="xs" />
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

            <div v-show="currentStep === 3" class="space-y-4">
              <EnvironmentVariablesPendingEditor ref="envVarsEditor" v-model="pendingEnvVars" />
            </div>

            <div v-show="currentStep === 4" class="space-y-4 md:space-y-0 md:grid md:grid-cols-2 md:gap-4 md:items-start">
              <div class="space-y-2">
                <p class="text-sm text-gray-700 dark:text-wire-200">
                  Compose file
                  <span class="font-mono text-xs">{{ composeTarget?.compose_path }}/{{ composeTarget?.compose_file }}</span>
                </p>

                <!-- Kept mounted while re-linting and dimmed instead, so the
                     rendered file and its scroll position survive a refresh. -->
                <ComposePreview
                  v-if="lintContent"
                  ref="composePreview"
                  :class="lintLoading ? 'opacity-50 transition-opacity' : 'transition-opacity'"
                  content-class="max-h-[28rem]"
                  :content="lintContent"
                  :filename="lintFilename"
                  :findings="lintReport?.findings || []"
                  @select-line="focusOnLine"
                />
              </div>

              <div class="space-y-2">
                <div class="flex items-start justify-between gap-2">
                  <div class="space-y-1">
                    <p class="text-sm text-gray-700 dark:text-wire-200">Static checks</p>
                    <p class="text-xs text-gray-500 dark:text-wire-400">
                      Warnings and infos are advisory; errors block creating and deploying this stack.
                    </p>
                  </div>
                  <UButton
                    type="button"
                    icon="i-lucide-refresh-cw"
                    variant="ghost"
                    color="neutral"
                    size="xs"
                    aria-label="Re-run checks"
                    :disabled="lintLoading"
                    :ui="{ leadingIcon: lintLoading ? 'animate-spin' : '' }"
                    @click="runLint"
                  />
                </div>

                <LintFindings
                  :report="lintReport"
                  :loading="lintLoading"
                  :config-error="lintConfigError"
                  list-class="max-h-[28rem]"
                  @select-line="focusOnLine"
                />
              </div>
            </div>
          </div>

          <template #footer>
            <div class="flex justify-between items-center w-full gap-2">
              <UButton v-if="currentStep > 1" label="Back" variant="outline" icon="i-lucide-arrow-left" @click="prevStep" />
              <UButton
                v-else-if="creationMode === 'wireops_file' && form.repository"
                label="Sync Repository"
                variant="outline"
                icon="i-lucide-refresh-cw"
                :loading="loadingWireopsFiles"
                @click="loadWireopsFiles(form.repository)"
              />
              <div v-else/>

              <p v-if="currentStep === 4 && createErrors.lint" class="text-xs text-red-500 text-right">
                {{ createErrors.lint }}
              </p>

              <div class="flex gap-2">
                <CancelButton @click="close" />
                <UButton v-if="currentStep === 1" type="button" label="Next" icon="i-lucide-arrow-right" trailing :disabled="!canProceedToStep2" @click="nextStep" />
                <UButton v-else-if="currentStep === 2" type="button" label="Next" icon="i-lucide-arrow-right" trailing :disabled="!canProceedToStep3" @click="nextStep" />
                <UButton v-else-if="currentStep === 3" type="button" label="Next" icon="i-lucide-arrow-right" trailing @click="nextStep" />
                <UButton v-else type="submit" label="Create" icon="i-lucide-check" :loading="saving" :disabled="lintLoading || lintHasErrors" />
              </div>
            </div>
          </template>
        </AppPanelCard>
      </form>
    </template>
  </UModal>
</template>
