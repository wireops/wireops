<script setup lang="ts">
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
const createErrors = ref<{ compose_path?: string; compose_file?: string; selected_file?: string }>({})

const repoOptions = computed(() =>
  (repos.value || []).map((r: any) => ({ label: `${r.name} (${r.git_url})`, value: r.id }))
)

const workerOptions = computed(() =>
  (workers.value || []).map((a: any) => ({ label: a.hostname, value: a.id }))
)

const fileOptions = computed(() =>
  repoFiles.value.map(f => ({ label: f, value: f }))
)

watch(() => props.open, async (val) => {
  if (!val) return
  await Promise.all([refreshRepos(), refreshWorkers()])
  const embedded = workers.value?.find((a: any) => a.fingerprint === 'embedded')
  form.value = { ...defaultForm(), worker: embedded ? embedded.id : '' }
  repoFiles.value = []
  createErrors.value = {}
})

watch(() => form.value.repository, async (repoId) => {
  if (!repoId) {
    repoFiles.value = []
    form.value.selected_file = ''
    return
  }
  loadingFiles.value = true
  try {
    const files = await getStackFiles(repoId)
    repoFiles.value = files || []
    form.value.selected_file = repoFiles.value.length === 1 ? repoFiles.value[0]! : ''
  } catch {
    toast.add({ title: 'Failed to fetch repository files', color: 'error' })
    repoFiles.value = []
    form.value.selected_file = ''
  } finally {
    loadingFiles.value = false
  }
})

async function handleSubmit() {
  createErrors.value = {}

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
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <UModal :open="open" title="Add Stack" description="Create a new stack from a repository" @update:open="emit('update:open', $event)">
    <template #body>
      <form class="flex flex-col gap-4" @submit.prevent="handleSubmit">
        <UFormField label="Name" required>
          <UInput v-model="form.name" placeholder="my-stack" />
        </UFormField>
        <UFormField label="Repository" required>
          <USelect v-model="form.repository" :items="repoOptions" placeholder="Select a repository" />
        </UFormField>
        <UFormField label="Worker" required>
          <USelect v-model="form.worker" :items="workerOptions" placeholder="Select a worker" />
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
          <UInput v-model.number="form.poll_interval" type="number" />
        </UFormField>
        <div class="flex justify-end gap-2 pt-2">
          <UButton label="Cancel" variant="outline" @click="emit('update:open', false)" />
          <UButton type="submit" label="Create" :loading="saving" />
        </div>
      </form>
    </template>
  </UModal>
</template>
