<script setup lang="ts">
import { ref, computed } from 'vue'
import type { InfisicalProject } from '~/composables/useInfisicalBrowse'

const modelValue = defineModel<string>({ default: '' })

const { listInfisicalProjects, getInfisicalProject, browseInfisicalPath } = useInfisicalBrowse()
const toast = useToast()

// Org-wide project listing 403s on some Infisical versions/instances for a
// project-scoped machine identity, so it's tried once on open; if it fails,
// the UI falls back to manual Project ID entry instead of dead-ending.
const listAttempted = ref(false)
const listSupported = ref(true)
const projects = ref<InfisicalProject[]>([])
const projectsLoading = ref(false)
const selectedProjectId = ref<string | undefined>(undefined)

const projectIdInput = ref('')
const projectLoading = ref(false)

const project = ref<InfisicalProject | null>(null)

const selectedEnvironment = ref<string | undefined>(undefined)

const pathSegments = ref<string[]>([])
const currentEntries = ref<{ name: string; is_folder: boolean }[]>([])
const browseLoading = ref(false)
const dialogOpen = ref(false)

const currentPath = computed(() => pathSegments.value.join('/'))

async function attemptListProjects() {
  listAttempted.value = true
  projectsLoading.value = true
  try {
    projects.value = await listInfisicalProjects()
    listSupported.value = true
    // If the backend is restricted to a single project, the list route
    // already filters down to just that one — auto-select it instead of
    // making the user pick from a dropdown with one option.
    if (projects.value.length === 1 && !selectedProjectId.value) {
      selectProjectFromList(projects.value[0]!.id)
    }
  } catch {
    listSupported.value = false
  } finally {
    projectsLoading.value = false
  }
}

function resetSelection() {
  selectedEnvironment.value = undefined
  pathSegments.value = []
  currentEntries.value = []
}

function selectProjectFromList(projectId: string | undefined) {
  selectedProjectId.value = projectId
  project.value = projects.value.find(p => p.id === projectId) || null
  resetSelection()
}

async function loadProjectById() {
  const projectId = projectIdInput.value.trim()
  if (!projectId) return
  projectLoading.value = true
  project.value = null
  resetSelection()
  try {
    project.value = await getInfisicalProject(projectId)
  } catch (e: any) {
    toast.add({ title: 'Failed to load Infisical project', description: e.message, color: 'error' })
  } finally {
    projectLoading.value = false
  }
}

async function selectEnvironment(environment: string | undefined) {
  selectedEnvironment.value = environment
  pathSegments.value = []
  await loadCurrentFolder()
}

async function loadCurrentFolder() {
  if (!project.value || !selectedEnvironment.value) return
  browseLoading.value = true
  try {
    currentEntries.value = await browseInfisicalPath(project.value.id, selectedEnvironment.value, currentPath.value)
  } catch (e: any) {
    toast.add({ title: 'Failed to browse Infisical path', description: e.message, color: 'error' })
    currentEntries.value = []
  } finally {
    browseLoading.value = false
  }
}

async function openEntry(entry: { name: string; is_folder: boolean }) {
  if (entry.is_folder) {
    pathSegments.value.push(entry.name)
    await loadCurrentFolder()
    return
  }
  if (!project.value || !selectedEnvironment.value) return
  const locator = currentPath.value
    ? `${project.value.id}/${selectedEnvironment.value}/${currentPath.value}`
    : `${project.value.id}/${selectedEnvironment.value}`
  modelValue.value = `${locator}#${entry.name}`
  dialogOpen.value = false
}

function goToSegment(index: number) {
  pathSegments.value = pathSegments.value.slice(0, index + 1)
  loadCurrentFolder()
}

function goToRoot() {
  pathSegments.value = []
  loadCurrentFolder()
}

// Parses a "<projectId>/<environment>/<path>#<field>" reference (path
// segments optional). Mirrors parseInfisicalReference in
// internal/secrets/infisical.go.
function parseReference(value: string) {
  const hashIdx = value.lastIndexOf('#')
  if (hashIdx === -1 || hashIdx === value.length - 1) return null
  const field = value.slice(hashIdx + 1)
  const locator = value.slice(0, hashIdx)
  const parts = locator.split('/')
  if (parts.length < 2 || !parts[0] || !parts[1]) return null
  return {
    projectId: parts[0],
    environment: parts[1],
    path: parts.slice(2).filter(Boolean),
    field
  }
}

async function openDialog() {
  dialogOpen.value = true

  const parsed = parseReference(modelValue.value)
  if (!parsed) {
    if (!listAttempted.value) attemptListProjects()
    return
  }

  if (!listAttempted.value) await attemptListProjects()
  if (!project.value || project.value.id !== parsed.projectId) {
    try {
      project.value = await getInfisicalProject(parsed.projectId)
      selectedProjectId.value = parsed.projectId
    } catch {
      return
    }
  }

  selectedEnvironment.value = parsed.environment
  pathSegments.value = parsed.path
  await loadCurrentFolder()
}
</script>

<template>
  <div class="flex items-center gap-2 w-full">
    <AppTextInput
      :model-value="modelValue"
      readonly
      placeholder="Select an Infisical project, environment and secret..."
      class="font-mono text-sm"
    />

    <UButton
      icon="i-lucide-folder-search"
      color="primary"
      size="sm"
      class="shadow-[0_0_12px_rgba(250,204,21,0.55)] hover:shadow-[0_0_16px_rgba(250,204,21,0.75)] transition-shadow"
      aria-label="Browse Infisical"
      @click="openDialog"
    />

    <UModal
      v-model:open="dialogOpen"
      title="Browse Infisical"
      description="Pick a project, environment and secret to build the reference"
    >
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-folder-search" class="w-5 h-5 text-primary-500" />
              <h2 class="font-semibold text-lg">Browse Infisical</h2>
            </div>
          </template>

          <div class="w-full sm:w-[28rem] space-y-4">
            <div v-if="listSupported">
              <p class="text-xs font-medium text-gray-500 mb-1">Project</p>
              <AppSelectInput
                :model-value="selectedProjectId || ''"
                :items="projects.map(p => ({ label: p.name, value: p.id }))"
                :loading="projectsLoading"
                placeholder="Select a project"
                @update:model-value="selectProjectFromList"
              />
            </div>

            <div v-else>
              <p class="text-xs font-medium text-gray-500 mb-1">Project ID</p>
              <p class="text-xs text-gray-400 mb-2">Couldn't list projects automatically — enter the Project ID manually (Project Settings → General).</p>
              <div class="flex items-center gap-2">
                <AppTextInput
                  v-model="projectIdInput"
                  placeholder="Project Settings → General"
                  class="font-mono text-sm w-full"
                  @keyup.enter="loadProjectById"
                />
                <UButton label="Load" size="sm" :loading="projectLoading" :disabled="!projectIdInput.trim()" @click="loadProjectById" />
              </div>
            </div>

            <div v-if="project">
              <p class="text-xs font-medium text-gray-500 mb-1">Environment</p>
              <AppSelectInput
                :model-value="selectedEnvironment || ''"
                :items="project.environments.map(e => ({ label: e.name, value: e.slug }))"
                placeholder="Select an environment"
                @update:model-value="selectEnvironment"
              />
            </div>

            <div v-if="selectedEnvironment">
              <p class="text-xs font-medium text-gray-500 mb-1">Path</p>
              <div class="flex flex-wrap items-center gap-1 text-xs mb-2">
                <button type="button" class="text-primary-500 hover:underline" @click="goToRoot">/</button>
                <template v-for="(seg, i) in pathSegments" :key="i">
                  <span class="text-gray-400">/</span>
                  <button type="button" class="text-primary-500 hover:underline" @click="goToSegment(i)">{{ seg }}</button>
                </template>
              </div>
              <div v-if="browseLoading" class="text-xs text-gray-400">Loading...</div>
              <div v-else class="max-h-56 overflow-y-auto divide-y divide-gray-100 dark:divide-gray-800 border border-gray-100 dark:border-gray-800 rounded">
                <button
                  v-for="entry in currentEntries"
                  :key="entry.name"
                  type="button"
                  class="flex items-center gap-2 w-full text-left px-2 py-1.5 text-sm hover:bg-gray-100 dark:hover:bg-carbon-800"
                  @click="openEntry(entry)"
                >
                  <UIcon :name="entry.is_folder ? 'i-lucide-folder' : 'i-lucide-file-key'" class="h-4 w-4 text-gray-400" />
                  {{ entry.name }}
                </button>
                <p v-if="!currentEntries.length" class="text-xs text-gray-400 py-2 px-2">Empty</p>
              </div>
            </div>
          </div>
        </UCard>
      </template>
    </UModal>
  </div>
</template>
