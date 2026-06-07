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
const { getStackFiles } = useApi()
const { validateComposePath, validateComposeFile } = useValidation()
const toast = useToast()

const { data: repos, refresh: refreshRepos } = useAsyncData('repos_for_create_stack', () =>
  $pb.collection('repositories').getFullList({ sort: 'name' })
)

const { data: workers, refresh: refreshWorkers } = useAsyncData('workers_for_create_stack', () =>
  $pb.collection('workers').getFullList({ filter: 'status = "ACTIVE"', sort: 'hostname' })
)

const defaultForm = () => ({
  name: '',
  repository: '',
  worker: '',
  compose_path: '',
  compose_file: 'docker-compose.yml',
  selected_file: '',
  poll_interval: 10,
})

const form = ref(defaultForm())
const repoFiles = ref<string[]>([])
const loadingFiles = ref(false)
const saving = ref(false)
const createErrors = ref<{ worker?: string; compose_path?: string; compose_file?: string; selected_file?: string }>({})

const repoOptions = computed(() =>
  (repos.value || []).map((r: any) => ({ label: `${r.name} (${r.git_url})`, value: r.id }))
)

const workerOptions = computed(() =>
  (workers.value || []).map((a: any) => ({ label: a.hostname, value: a.id }))
)

const fileOptions = computed(() =>
  repoFiles.value.map(f => ({ label: f, value: f }))
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
    repoFiles.value = []
    createErrors.value = {}
    const q = { ...route.query }
    delete q.stack_step
    router.replace({ query: q })
  }
})

watch(() => form.value.repository, async (repoId) => {
  if (!repoId) {
    repoFiles.value = []
    form.value.selected_file = ''
    return
  }
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
})

const currentStep = computed(() => Number(route.query.stack_step) || 1)

const stepperItems = computed(() => [
  {
    title: 'Basic Info',
    description: 'Name & Repository',
    icon: 'i-lucide-info',
  },
  {
    title: 'Configuration',
    description: 'Worker & Compose File',
    icon: 'i-lucide-settings',
    disabled: !form.value.name || !form.value.repository,
  }
])

const activeStep = computed({
  get() {
    return currentStep.value - 1
  },
  set(val) {
    if (val === 1) {
      if (!form.value.name || !form.value.repository) return
      router.push({ query: { ...route.query, stack_step: '2' } })
    } else if (val === 0) {
      router.push({ query: { ...route.query, stack_step: '1' } })
    }
  }
})

function nextStep() {
  if (currentStep.value === 1) {
    if (!form.value.name || !form.value.repository) return
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

  const selected = form.value.selected_file
  if (!selected) {
    createErrors.value.selected_file = 'Please select a compose file'
    return
  }
  if (!form.value.worker) {
    createErrors.value.worker = 'Please select a worker'
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

  saving.value = true
  try {
    await $pb.collection('stacks').create({
      name: form.value.name,
      repository: form.value.repository,
      worker: form.value.worker,
      compose_path: form.value.compose_path,
      compose_file: form.value.compose_file,
      poll_interval: form.value.poll_interval,
      auto_sync: true,
      status: 'pending',
    })
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
              <UFormField label="Name" required>
                <UInput v-model="form.name" placeholder="my-stack" autofocus />
              </UFormField>
              <UFormField label="Repository" required>
                <USelect v-model="form.repository" :items="repoOptions" placeholder="Select a repository" class="w-full" />
              </UFormField>
            </div>

            <div v-show="currentStep === 2" class="space-y-4">
              <UFormField label="Worker" :error="createErrors.worker" required>
                <USelect v-model="form.worker" :items="workerOptions" placeholder="Select a worker" autofocus class="w-full" />
              </UFormField>
              <div class="grid grid-cols-1 gap-4">
                <UFormField
                  label="Compose File"
                  :error="createErrors.selected_file || createErrors.compose_path || createErrors.compose_file"
                  required
                >
                  <div class="flex items-center gap-2">
                    <USelect
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
              <UFormField label="Poll Interval (s)">
                <UInput v-model.number="form.poll_interval" type="number" class="w-full" />
              </UFormField>
            </div>
          </div>

          <template #footer>
            <div class="flex justify-between items-center w-full">
              <UButton v-if="currentStep > 1" label="Back" variant="outline" icon="i-lucide-arrow-left" @click="prevStep" />
              <div v-else/>

              <div class="flex gap-2">
                <UButton label="Cancel" variant="ghost" color="neutral" @click="close" />
                <UButton v-if="currentStep === 1" type="button" label="Next" icon="i-lucide-arrow-right" trailing :disabled="!form.name || !form.repository" @click="nextStep" />
                <UButton v-else type="submit" label="Create" icon="i-lucide-check" :loading="saving" />
              </div>
            </div>
          </template>
        </UCard>
      </form>
    </template>
  </UModal>
</template>
