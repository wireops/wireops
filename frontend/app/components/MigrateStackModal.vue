<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

const props = defineProps<{
  stack: any // the stack record (must have .id, .name, .repository, .config_source, .compose_path, .compose_file, .wireops_file_path)
}>()

const emit = defineEmits<{
  migrated: []
  cancel: []
}>()

const { $pb } = useNuxtApp()
const { getWireopsFiles, getStackFiles, previewMigrateStack, migrateStack } = useApi()
const toast = useToast()

const { data: repos, refresh: refreshRepos } = useAsyncData('repos_for_migrate_stack', () =>
  $pb.collection('repositories').getFullList({ sort: 'name' })
)

const isWireopsManaged = computed(() => props.stack?.config_source === 'wireops_file')

const targetRepository = ref('')
const wireopsFile = ref('')
const composePath = ref('')
const composeFile = ref('docker-compose.yml')
const teardownOldProject = ref(false)

const wireopsFiles = ref<string[]>([])
const loadingWireopsFiles = ref(false)
const stackFiles = ref<string[]>([])
const loadingStackFiles = ref(false)
const selectedFile = ref('')

const previewing = ref(false)
const migrating = ref(false)
const errorMsg = ref('')
type MigratePreview = Awaited<ReturnType<typeof previewMigrateStack>>
const preview = ref<MigratePreview | null>(null)

const repoOptions = computed(() =>
  (repos.value || [])
    .filter((r: any) => r.id !== props.stack?.repository)
    .map((r: any) => ({ label: `${r.name} (${r.git_url})`, value: r.id }))
)

const wireopsFileOptions = computed(() => wireopsFiles.value.map(f => ({ label: f, value: f })))
const stackFileOptions = computed(() => stackFiles.value.map(f => ({ label: f, value: f })))

async function loadWireopsFiles(repoId: string) {
  loadingWireopsFiles.value = true
  try {
    wireopsFiles.value = await getWireopsFiles(repoId) || []
  } catch {
    wireopsFiles.value = []
  } finally {
    loadingWireopsFiles.value = false
  }
}

async function loadStackFiles(repoId: string) {
  loadingStackFiles.value = true
  try {
    stackFiles.value = await getStackFiles(repoId) || []
  } catch {
    stackFiles.value = []
  } finally {
    loadingStackFiles.value = false
  }
}

watch(targetRepository, (repoId) => {
  wireopsFile.value = ''
  selectedFile.value = ''
  wireopsFiles.value = []
  stackFiles.value = []
  preview.value = null
  errorMsg.value = ''
  if (!repoId) return
  if (isWireopsManaged.value) {
    loadWireopsFiles(repoId)
  } else {
    loadStackFiles(repoId)
  }
})

watch(selectedFile, (file) => {
  if (!file) return
  const parts = file.split('/')
  if (parts.length === 1) {
    composePath.value = '.'
    composeFile.value = file
  } else {
    const name = parts.pop() || ''
    composePath.value = parts.join('/')
    composeFile.value = name
  }
})

// Clear a stale report as soon as the target selection changes underneath it.
watch([wireopsFile, selectedFile], () => {
  preview.value = null
})

const canPreview = computed(() => {
  if (!targetRepository.value) return false
  return isWireopsManaged.value ? !!wireopsFile.value : !!selectedFile.value
})

function requestBody(confirm: boolean) {
  const body: Record<string, unknown> = { repository: targetRepository.value }
  if (isWireopsManaged.value) {
    body.wireops_file = wireopsFile.value
  } else {
    body.compose_path = composePath.value
    body.compose_file = composeFile.value
  }
  if (confirm) {
    body.confirm = true
    body.teardown_old_project = teardownOldProject.value
  }
  return body
}

async function runPreview() {
  if (!canPreview.value) return
  previewing.value = true
  errorMsg.value = ''
  preview.value = null
  try {
    preview.value = await previewMigrateStack(props.stack.id, requestBody(false))
  } catch (e: any) {
    errorMsg.value = e?.message || 'Failed to preview migration'
  } finally {
    previewing.value = false
  }
}

async function confirmMigrate() {
  if (!preview.value) return
  migrating.value = true
  errorMsg.value = ''
  try {
    await migrateStack(props.stack.id, requestBody(true))
    toast.add({
      title: `Stack "${props.stack.name}" migration started`,
      description: 'Repository re-pointed — reconciling against the new source now.',
      color: 'warning',
    })
    emit('migrated')
  } catch (e: any) {
    errorMsg.value = e?.message || 'Unexpected error'
  } finally {
    migrating.value = false
  }
}

function severityColor(severity: string) {
  if (severity === 'critical') return 'text-red-500'
  if (severity === 'warn') return 'text-amber-500'
  return 'text-gray-500 dark:text-wire-400'
}

function severityIcon(severity: string) {
  if (severity === 'critical') return 'i-lucide-circle-x'
  if (severity === 'warn') return 'i-lucide-triangle-alert'
  return 'i-lucide-info'
}

onMounted(refreshRepos)
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-git-branch" class="w-5 h-5 text-warning-500" />
        <h2 class="font-semibold">Migrate to Another Repository</h2>
      </div>
    </template>

    <div class="space-y-4">
      <UFormField label="Target Repository" :help="repoOptions.length === 0 ? 'No other registered repositories available.' : undefined">
        <AppSelectInput
          v-model="targetRepository"
          :items="repoOptions"
          placeholder="Select a target repository"
          :disabled="repoOptions.length === 0"
          class="w-full"
        />
      </UFormField>

      <template v-if="targetRepository">
        <UFormField v-if="isWireopsManaged" label="wireops.yaml file" required>
          <AppSelectInput
            v-model="wireopsFile"
            :items="wireopsFileOptions"
            placeholder="Select a wireops.yaml file"
            :disabled="loadingWireopsFiles"
            :loading="loadingWireopsFiles"
            class="w-full"
          />
        </UFormField>

        <UFormField v-else label="Compose File" required>
          <AppSelectInput
            v-model="selectedFile"
            :items="stackFileOptions"
            placeholder="Select a compose file"
            :disabled="loadingStackFiles"
            :loading="loadingStackFiles"
            class="w-full"
          />
        </UFormField>
      </template>

      <UButton
        label="Preview Migration"
        icon="i-lucide-eye"
        variant="outline"
        :loading="previewing"
        :disabled="!canPreview"
        @click="runPreview"
      />

      <template v-if="preview">
        <div class="space-y-3 rounded-lg border border-gray-300 dark:border-carbon-700/60 p-3">
          <div v-if="!preview.project_name.same" class="flex items-center gap-2 text-xs text-amber-500">
            <UIcon name="i-lucide-triangle-alert" class="w-4 h-4 shrink-0" />
            <span>Project name changes from <span class="font-mono">{{ preview.project_name.source }}</span> to <span class="font-mono">{{ preview.project_name.target }}</span> — the old project's containers won't be cleaned up automatically.</span>
          </div>

          <div v-if="preview.volumes.removed.length" class="space-y-1">
            <p class="text-xs font-medium text-red-500">Volumes not present on target (data will NOT be preserved)</p>
            <div class="flex flex-wrap gap-1.5">
              <UBadge v-for="v in preview.volumes.removed" :key="v" :label="v" color="error" variant="subtle" size="xs" />
            </div>
          </div>

          <div v-if="preview.services.removed.length" class="space-y-1">
            <p class="text-xs font-medium text-amber-500">Services not present on target</p>
            <div class="flex flex-wrap gap-1.5">
              <UBadge v-for="s in preview.services.removed" :key="s" :label="s" color="warning" variant="subtle" size="xs" />
            </div>
          </div>

          <div v-if="preview.sops.status !== 'none'" class="space-y-1">
            <p class="text-xs font-medium" :class="preview.sops.status === 'ok' ? 'text-green-500' : 'text-amber-500'">
              SOPS: {{ preview.sops.status.replace(/_/g, ' ') }}
            </p>
            <CommandLineLabel v-if="preview.sops.target_age_public_key" :command="preview.sops.target_age_public_key" />
          </div>

          <ul v-if="preview.warnings.length" class="space-y-1">
            <li v-for="(w, i) in preview.warnings" :key="i" class="flex items-start gap-2 text-xs" :class="severityColor(w.severity)">
              <UIcon :name="severityIcon(w.severity)" class="w-3.5 h-3.5 mt-0.5 shrink-0" />
              <span>{{ w.message }}</span>
            </li>
          </ul>
          <p v-else class="text-xs text-gray-500 dark:text-wire-400">No warnings.</p>
        </div>

        <UCheckbox
          v-if="!preview.project_name.same"
          v-model="teardownOldProject"
          label="Tear down containers from the old project"
        />
      </template>

      <div v-if="errorMsg" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500">{{ errorMsg }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <CancelButton @click="emit('cancel')" />
        <UButton
          label="Migrate Stack"
          color="warning"
          icon="i-lucide-git-branch"
          :loading="migrating"
          :disabled="!preview"
          @click="confirmMigrate"
        />
      </div>
    </template>
  </UCard>
</template>
