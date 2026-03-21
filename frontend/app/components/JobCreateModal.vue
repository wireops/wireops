<script setup lang="ts">
const { $pb } = useNuxtApp()
const { getJobFiles } = useApi()
const toast = useToast()

const props = defineProps<{
  repos: any[]
}>()

const emit = defineEmits<{
  created: []
  cancel: []
}>()

const form = ref({
  repository: '',
  job_file: '',
  enabled: true,
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

watch(() => form.value.repository, async (repoId) => {
  form.value.job_file = ''
  repoFiles.value = []
  if (!repoId) return

  loadingFiles.value = true
  try {
    repoFiles.value = (await getJobFiles(repoId)) || []
  } catch {
    toast.add({ title: 'Failed to fetch repository files', color: 'error' })
  } finally {
    loadingFiles.value = false
  }
})

async function submit() {
  errorMsg.value = ''
  if (!form.value.repository || !form.value.job_file) {
    errorMsg.value = 'Repository and job file are required.'
    return
  }

  submitting.value = true
  try {
    await $pb.collection('scheduled_jobs').create({
      repository: form.value.repository,
      job_file: form.value.job_file,
      enabled: form.value.enabled,
      status: 'active',
    })
    toast.add({ title: 'Job created', color: 'success' })
    emit('created')
  } catch (e: any) {
    errorMsg.value = e?.message || 'Unexpected error'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-calendar-clock" class="w-5 h-5 text-yellow-400" />
        <h2 class="font-semibold text-lg">New Scheduled Job</h2>
      </div>
    </template>

    <div class="space-y-4">
      <p class="text-sm text-gray-500 dark:text-wire-200/60">
        Select a repository and a <code class="bg-gray-100 dark:bg-carbon-800 px-1 rounded text-xs">job.yaml</code> file.
        All configuration (cron, tags, image, command) is read from that file.
      </p>

      <!-- Repository -->
      <UFormField label="Repository" required>
        <USelect
          v-model="form.repository"
          :items="repoItems"
          placeholder="Select a repository"
          class="w-full"
        />
      </UFormField>

      <!-- Job file -->
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

      <!-- Enabled -->
      <UFormField label="Enable immediately">
        <USwitch v-model="form.enabled" />
      </UFormField>

      <!-- Error -->
      <div v-if="errorMsg" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500">{{ errorMsg }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <UButton label="Cancel" variant="outline" @click="emit('cancel')" />
        <UButton
          label="Create Job"
          icon="i-lucide-plus"
          :loading="submitting"
          :disabled="!form.repository || !form.job_file"
          @click="submit"
        />
      </div>
    </template>
  </UCard>
</template>
