<script setup lang="ts">
const { $pb } = useNuxtApp()
const { discoverProjects, importStack } = useApi()
const toast = useToast()

const emit = defineEmits<{
  imported: [stackId: string]
  cancel: []
}>()

// Step 1 state
const step = ref<1 | 2>(1)
const agents = ref<any[]>([])
const selectedAgentId = ref('')
const discovering = ref(false)
const discoveredProjects = ref<{ project_name: string; compose_path: string; services: string[] }[]>([])
const selectedProject = ref<{ project_name: string; compose_path: string; services: string[] } | null>(null)
const stackName = ref('')
const importPath = ref('')
const discoverError = ref('')

// Step 2 state
const acknowledgedRestart = ref(false)
const recreateVolumes = ref(false)
const importing = ref(false)
const importError = ref('')

onMounted(async () => {
  try {
    agents.value = await $pb.collection('agents').getFullList({
      filter: 'status = "ACTIVE"',
      sort: 'hostname',
    })
    const embedded = agents.value.find((a: any) => a.fingerprint === 'embedded')
    if (embedded) selectedAgentId.value = embedded.id
  } catch {
    agents.value = []
  }
})

const agentOptions = computed(() =>
  agents.value.map((a: any) => ({ label: a.hostname, value: a.id }))
)

watch(selectedAgentId, () => {
  discoveredProjects.value = []
  selectedProject.value = null
  discoverError.value = ''
})

async function runDiscover() {
  if (!selectedAgentId.value) return
  discovering.value = true
  discoverError.value = ''
  discoveredProjects.value = []
  selectedProject.value = null
  try {
    discoveredProjects.value = await discoverProjects(selectedAgentId.value)
  } catch (e: any) {
    discoverError.value = e?.message || 'Failed to discover projects'
  } finally {
    discovering.value = false
  }
}

function selectProject(project: typeof discoveredProjects.value[number]) {
  selectedProject.value = project
  stackName.value = project.project_name
  importPath.value = project.compose_path
    ? `${project.compose_path}/docker-compose.yml`
    : ''
}

function goToStep2() {
  if (!stackName.value.trim() || !importPath.value.trim() || !selectedAgentId.value) return
  importError.value = ''
  acknowledgedRestart.value = false
  recreateVolumes.value = false
  step.value = 2
}

async function confirmImport() {
  if (!acknowledgedRestart.value) return
  importing.value = true
  importError.value = ''
  try {
    const result = await importStack({
      name: stackName.value.trim(),
      agent_id: selectedAgentId.value,
      import_path: importPath.value.trim(),
      recreate_volumes: recreateVolumes.value,
    })
    toast.add({
      title: `Stack "${stackName.value}" import started`,
      description: 'wireops labels will be applied — containers will restart shortly.',
      color: 'success',
    })
    emit('imported', result.id)
  } catch (e: any) {
    importError.value = e?.message || 'Unexpected error'
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-package-plus" class="w-5 h-5 text-primary-500" />
        <h2 class="font-semibold">Import Compose Stack</h2>
        <UBadge :label="`Step ${step} of 2`" variant="subtle" class="ml-auto" />
      </div>
    </template>

    <!-- STEP 1: Discovery & configuration -->
    <div v-if="step === 1" class="space-y-5">
      <!-- Agent selector + discover button -->
      <div class="flex gap-2 items-end">
        <UFormField label="Agent" class="flex-1">
          <USelect
            v-model="selectedAgentId"
            :items="agentOptions"
            placeholder="Select an agent"
            class="w-full"
          />
        </UFormField>
        <UButton
          label="Discover"
          icon="i-lucide-search"
          variant="outline"
          :loading="discovering"
          :disabled="!selectedAgentId"
          @click="runDiscover"
        />
      </div>

      <!-- Discovery error -->
      <div v-if="discoverError" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500">{{ discoverError }}</p>
      </div>

      <!-- Discovered projects table -->
      <div v-if="discoveredProjects.length > 0" class="space-y-2">
        <p class="text-sm text-muted">
          {{ discoveredProjects.length }} unmanaged project{{ discoveredProjects.length !== 1 ? 's' : '' }} found. Select one to pre-fill the form.
        </p>
        <div class="divide-y divide-default rounded-lg border border-default overflow-hidden">
          <button
            v-for="project in discoveredProjects"
            :key="project.project_name"
            type="button"
            class="w-full flex items-start gap-3 px-4 py-3 text-left hover:bg-elevated transition-colors"
            :class="selectedProject?.project_name === project.project_name ? 'bg-primary-500/10 border-l-2 border-primary-500' : ''"
            @click="selectProject(project)"
          >
            <UIcon name="i-lucide-layers" class="w-4 h-4 text-muted mt-0.5 shrink-0" />
            <div class="min-w-0">
              <p class="font-medium text-sm">{{ project.project_name }}</p>
              <p class="text-xs text-muted truncate">{{ project.compose_path || 'path unknown' }}</p>
              <p class="text-xs text-muted">{{ project.services.join(', ') }}</p>
            </div>
          </button>
        </div>
      </div>

      <div v-else-if="!discovering && selectedAgentId && discoveredProjects.length === 0 && !discoverError" class="text-sm text-muted text-center py-4">
        No unmanaged Compose projects found. You can still enter a path manually below.
      </div>

      <!-- Manual / pre-filled form -->
      <div class="space-y-3">
        <UFormField label="Stack name" required>
          <UInput v-model="stackName" placeholder="my-stack" class="w-full" />
        </UFormField>
        <UFormField
          label="Compose file path (absolute)"
          help="Absolute path to the docker-compose.yml on the agent host."
          required
        >
          <UInput v-model="importPath" placeholder="/opt/myapp/docker-compose.yml" class="w-full" />
        </UFormField>
      </div>
    </div>

    <!-- STEP 2: Risk acknowledgement -->
    <div v-else class="space-y-4">
      <UAlert
        color="error"
        icon="i-lucide-triangle-alert"
        title="Containers will be restarted"
        description="wireops injects tracking labels by running docker compose up --force-recreate. All services in this project will restart briefly."
      />

      <UAlert
        color="warning"
        icon="i-lucide-database-zap"
        title="Anonymous volumes will be lost"
        description="If any service uses anonymous volumes (not named volumes), they will be destroyed and recreated empty. Named volumes are safe unless you enable the option below."
      />

      <UAlert
        color="info"
        icon="i-lucide-network"
        title="Network note"
        description="If the compose working directory cannot be confirmed, Docker may create a duplicate network. Remove the old network manually after import if needed."
      />

      <!-- Recreate volumes option -->
      <div class="rounded-lg border border-warning-500/30 bg-warning-500/10 p-4 space-y-3">
        <div class="flex items-start gap-3">
          <UCheckbox v-model="recreateVolumes" id="recreate-volumes" />
          <div>
            <label for="recreate-volumes" class="text-sm font-medium cursor-pointer">
              Recreate named volumes
            </label>
            <p class="text-xs text-muted mt-0.5">
              Named volumes will be destroyed and recreated empty.
              <strong class="text-warning-600 dark:text-warning-400">Back up your data before enabling this option.</strong>
            </p>
          </div>
        </div>
      </div>

      <!-- Explicit consent -->
      <div class="rounded-lg border border-default p-4">
        <div class="flex items-start gap-3">
          <UCheckbox v-model="acknowledgedRestart" id="ack-restart" />
          <label for="ack-restart" class="text-sm cursor-pointer">
            I understand that containers will restart and data in anonymous volumes may be lost.
          </label>
        </div>
      </div>

      <!-- Import error -->
      <div v-if="importError" class="flex items-start gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3">
        <UIcon name="i-lucide-circle-x" class="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
        <p class="text-sm text-red-500">{{ importError }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-between gap-2">
        <UButton
          v-if="step === 2"
          label="Back"
          icon="i-lucide-arrow-left"
          variant="outline"
          :disabled="importing"
          @click="step = 1"
        />
        <span v-else />

        <div class="flex gap-2">
          <UButton label="Cancel" variant="outline" :disabled="importing" @click="emit('cancel')" />
          <UButton
            v-if="step === 1"
            label="Continue"
            icon="i-lucide-arrow-right"
            :disabled="!stackName.trim() || !importPath.trim() || !selectedAgentId"
            @click="goToStep2"
          />
          <UButton
            v-else
            label="Import Stack"
            icon="i-lucide-package-plus"
            color="primary"
            :loading="importing"
            :disabled="!acknowledgedRestart"
            @click="confirmImport"
          />
        </div>
      </div>
    </template>
  </UCard>
</template>
