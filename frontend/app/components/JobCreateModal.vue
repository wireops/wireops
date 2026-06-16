<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'

const route = useRoute()
const router = useRouter()
const { $pb } = useNuxtApp()
const { getJobFiles, getJobDefinitionFromFile } = useApi()
const toast = useToast()

const props = withDefaults(
  defineProps<{
    repos: any[]
    open?: boolean
  }>(),
  {
    open: false,
  }
)

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'created'): void
}>()

const form = ref({
  repository: '',
  job_file: '',
  enabled: true,
  name: '',
  description: '',
})

const nameError = computed(() => {
  if (!form.value.name.trim()) return 'Name is required.'
  const nameRegex = /^[a-zA-Z0-9\p{L}_ -]+$/u
  if (!nameRegex.test(form.value.name)) {
    return 'Name can only contain alphanumeric characters, spaces, underscores, and hyphens.'
  }
  return ''
})

const repoFiles = ref<string[]>([])
const loadingFiles = ref(false)
const submitting = ref(false)
const errorMsg = ref('')

const repoItems = computed(() =>
  props.repos.map((r: any) => ({ label: `${r.name} (${r.git_url})`, value: r.id }))
)

const fileItems = computed(() =>
  repoFiles.value.map(f => ({ label: f, value: f }))
)

const isMobile = ref(false)

onMounted(() => {
  isMobile.value = window.innerWidth < 768
  const resizeListener = () => { isMobile.value = window.innerWidth < 768 }
  window.addEventListener('resize', resizeListener)
  onUnmounted(() => window.removeEventListener('resize', resizeListener))
})

watch(() => props.open, (val) => {
  if (val) {
    if (!route.query.job_step) {
      router.replace({ query: { ...route.query, job_step: '1' } })
    }
  } else {
    form.value = {
      repository: '',
      job_file: '',
      enabled: true,
      name: '',
      description: '',
    }
    repoFiles.value = []
    errorMsg.value = ''
    const q = { ...route.query }
    delete q.job_step
    router.replace({ query: q })
  }
})

watch(() => form.value.repository, async (repoId) => {
  form.value.job_file = ''
  repoFiles.value = []
  if (!repoId) return

  loadingFiles.value = true
  try {
    const files = (await getJobFiles(repoId)) || []
    if (form.value.repository === repoId) {
      repoFiles.value = files
    }
  } catch {
    if (form.value.repository === repoId) {
      toast.add({ title: 'Failed to fetch repository files', color: 'error' })
    }
  } finally {
    if (form.value.repository === repoId) {
      loadingFiles.value = false
    }
  }
})

watch(() => form.value.job_file, async (file) => {
  form.value.name = ''
  form.value.description = ''
  if (!file || !form.value.repository) return

  const currentRepo = form.value.repository
  const currentFile = file

  try {
    const def = await getJobDefinitionFromFile(currentRepo, currentFile)
    if (def) {
      if (form.value.repository === currentRepo && form.value.job_file === currentFile) {
        form.value.name = def.name || ''
        form.value.description = def.description || ''
      }
    }
  } catch {
    if (form.value.repository === currentRepo && form.value.job_file === currentFile) {
      toast.add({ title: 'Failed to parse job definition file', color: 'error' })
    }
  }
})

const currentStep = computed(() => Number(route.query.job_step) || 1)

function nextStep() {
  if (currentStep.value === 1) {
    if (!form.value.repository) return
    router.push({ query: { ...route.query, job_step: '2' } })
  }
}

function prevStep() {
  if (currentStep.value > 1) {
    router.push({ query: { ...route.query, job_step: String(currentStep.value - 1) } })
  }
}

async function submit() {
  if (currentStep.value === 1) {
    nextStep()
    return
  }

  errorMsg.value = ''
  if (!form.value.repository || !form.value.job_file) {
    errorMsg.value = 'Repository and job file are required.'
    return
  }

  if (nameError.value) {
    errorMsg.value = nameError.value
    return
  }

  submitting.value = true
  try {
    await $pb.collection('scheduled_jobs').create({
      repository: form.value.repository,
      job_file: form.value.job_file,
      enabled: form.value.enabled,
      name: form.value.name.trim(),
      description: form.value.description.trim(),
      status: 'active',
    })
    toast.add({ title: 'Job created', color: 'success' })
    emit('created')
    emit('update:open', false)
  } catch (e: any) {
    const serverMsg = e?.response?.data?.name?.message || e?.data?.data?.name?.message || e?.data?.data?.name
    if (serverMsg) {
      errorMsg.value = typeof serverMsg === 'string' ? serverMsg : 'Name can only contain alphanumeric characters, spaces, underscores, and hyphens.'
    } else {
      errorMsg.value = e?.message || 'Unexpected error'
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <UModal
    :open="props.open"
    :fullscreen="isMobile"
    title="New Scheduled Job"
    description="Select a repository and configure your scheduled job"
    @update:open="emit('update:open', $event)"
  >
    <template #content>
      <UCard :ui="{ ring: '', divide: 'divide-y divide-gray-100 dark:divide-gray-800', base: 'h-full flex flex-col', body: { base: 'flex-1' } }">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-calendar-clock" class="w-5 h-5 text-yellow-400" />
            <h2 class="font-semibold text-lg">New Scheduled Job</h2>
          </div>
        </template>

        <form class="space-y-4" @submit.prevent="submit">
          <p class="text-sm text-gray-500 dark:text-wire-200/60 mb-2">
            {{ currentStep === 1 ? 'Step 1: Select a repository' : 'Step 2: Configuration' }}
          </p>

          <div v-show="currentStep === 1">
            <UFormField label="Repository" required>
              <USelect
                v-model="form.repository"
                :items="repoItems"
                placeholder="Select a repository"
                class="w-full"
              />
            </UFormField>
          </div>

          <div v-show="currentStep === 2" class="space-y-4">
            <p class="text-xs text-gray-500 dark:text-wire-200/60 mb-2">
              Select a <code class="bg-gray-100 dark:bg-carbon-800 px-1 rounded text-xs">job.yaml</code> file. All configuration (cron, image, command) is read from it.
            </p>
            <UFormField label="Job file" :error="repoFiles.length === 0 && !!form.repository && !loadingFiles ? 'No job.yaml files found in this repository' : undefined" required>
              <div class="flex items-center gap-2">
                <USelect
                  v-model="form.job_file"
                  :items="fileItems"
                  :disabled="!form.repository || loadingFiles"
                  placeholder="Select a .yaml file"
                  class="flex-1"
                />
                <UIcon v-if="loadingFiles" name="i-lucide-loader-2" class="w-5 h-5 animate-spin text-gray-400 shrink-0" />
              </div>
            </UFormField>

            <UFormField label="Name" required :error="form.name && nameError ? nameError : undefined">
              <div v-if="form.job_file && form.name" class="text-sm text-gray-600 dark:text-wire-200 bg-gray-50 dark:bg-carbon-900/40 border border-gray-200 dark:border-carbon-800 rounded-lg px-3 py-2 w-full select-none">
                {{ form.name }}
              </div>
              <div v-else class="text-sm text-gray-400 dark:text-wire-200/40 italic bg-gray-50/50 dark:bg-carbon-900/20 border border-dashed border-gray-200 dark:border-carbon-800 rounded-lg px-3 py-2 w-full select-none">
                Pending job.yaml selection...
              </div>
            </UFormField>

            <UFormField label="Description">
              <div v-if="form.job_file" class="text-sm text-gray-600 dark:text-wire-200 bg-gray-50 dark:bg-carbon-900/40 border border-gray-200 dark:border-carbon-800 rounded-lg px-3 py-2 w-full select-none">
                {{ form.description || 'No description specified in job.yaml' }}
              </div>
              <div v-else class="text-sm text-gray-400 dark:text-wire-200/40 italic bg-gray-50/50 dark:bg-carbon-900/20 border border-dashed border-gray-200 dark:border-carbon-800 rounded-lg px-3 py-2 w-full select-none">
                Pending job.yaml selection...
              </div>
            </UFormField>

            <UFormField label="Enable immediately">
              <USwitch v-model="form.enabled" />
            </UFormField>
          </div>

          <div v-if="errorMsg" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 mt-4">
            <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
            <p class="text-sm text-red-500">{{ errorMsg }}</p>
          </div>

          <div class="flex justify-between pt-4 mt-6">
            <UButton v-if="currentStep > 1" label="Back" variant="outline" icon="i-lucide-arrow-left" @click="prevStep" />
            <div v-else/>

            <div class="flex gap-2">
              <UButton label="Cancel" variant="ghost" color="neutral" @click="emit('update:open', false)" />
              <UButton v-if="currentStep === 1" type="button" label="Next" icon="i-lucide-arrow-right" trailing :disabled="!form.repository" @click="nextStep" />
              <UButton
                v-else
                type="submit"
                label="Create Job"
                icon="i-lucide-check"
                :loading="submitting"
                :disabled="!form.repository || !form.job_file || !form.name || !!nameError"
              />
            </div>
          </div>
        </form>
      </UCard>
    </template>
  </UModal>
</template>
