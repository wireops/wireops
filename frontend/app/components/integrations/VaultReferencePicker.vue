<script setup lang="ts">
import { ref, computed } from 'vue'
import type { VaultMount } from '~/composables/useVaultBrowse'

const modelValue = defineModel<string>({ default: '' })

const { listVaultMounts, browseVaultPath, listVaultFields } = useVaultBrowse()
const toast = useToast()

const mounts = ref<VaultMount[]>([])
const mountsLoading = ref(false)
const selectedMountPath = ref<string | undefined>(undefined)
const selectedMount = computed(() => mounts.value.find(m => m.path === selectedMountPath.value) || null)

const pathSegments = ref<string[]>([])
const currentEntries = ref<{ name: string; is_folder: boolean }[]>([])
const browseLoading = ref(false)

const selectedSecretPath = ref('')
const fields = ref<string[]>([])
const fieldsLoading = ref(false)
const selectedField = ref('')
const dialogOpen = ref(false)

const currentPath = computed(() => pathSegments.value.join('/'))

async function loadMounts() {
  mountsLoading.value = true
  try {
    mounts.value = await listVaultMounts()
    // If the backend is restricted to a single mount, the list route already
    // filters down to just that one — auto-select it instead of making the
    // user pick from a dropdown with one option.
    if (mounts.value.length === 1 && !selectedMountPath.value) {
      await selectMount(mounts.value[0]!.path)
    }
  } catch (e: any) {
    toast.add({ title: 'Failed to list Vault mounts', description: e.message, color: 'error' })
  } finally {
    mountsLoading.value = false
  }
}

async function selectMount(mountPath: string | undefined) {
  selectedMountPath.value = mountPath
  pathSegments.value = []
  selectedSecretPath.value = ''
  fields.value = []
  selectedField.value = ''
  await loadCurrentFolder()
}

async function loadCurrentFolder() {
  if (!selectedMount.value) return
  browseLoading.value = true
  try {
    currentEntries.value = await browseVaultPath(selectedMount.value.path, currentPath.value, selectedMount.value.version)
  } catch (e: any) {
    toast.add({ title: 'Failed to browse Vault path', description: e.message, color: 'error' })
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
  selectedSecretPath.value = [...pathSegments.value, entry.name].join('/')
  await loadFields()
}

async function loadFields() {
  if (!selectedMount.value || !selectedSecretPath.value) return
  fieldsLoading.value = true
  try {
    fields.value = await listVaultFields(selectedMount.value.path, selectedSecretPath.value, selectedMount.value.version)
  } catch (e: any) {
    toast.add({ title: 'Failed to list secret fields', description: e.message, color: 'error' })
    fields.value = []
  } finally {
    fieldsLoading.value = false
  }
}

function goToSegment(index: number) {
  pathSegments.value = pathSegments.value.slice(0, index + 1)
  selectedSecretPath.value = ''
  fields.value = []
  selectedField.value = ''
  loadCurrentFolder()
}

function goToRoot() {
  pathSegments.value = []
  selectedSecretPath.value = ''
  fields.value = []
  selectedField.value = ''
  loadCurrentFolder()
}

function selectField(field: string) {
  selectedField.value = field
  if (selectedMount.value && selectedSecretPath.value) {
    modelValue.value = `${selectedMount.value.path}/data/${selectedSecretPath.value}#${field}`
    dialogOpen.value = false
  }
}

function openDialog() {
  dialogOpen.value = true
  if (!mounts.value.length) loadMounts()
}
</script>

<template>
  <div class="flex items-center gap-2 w-full">
    <AppTextInput
      :model-value="modelValue"
      readonly
      placeholder="Select a Vault mount, path and field..."
      class="font-mono text-sm"
    />

    <UButton
      icon="i-lucide-folder-search"
      color="primary"
      size="sm"
      class="shadow-[0_0_12px_rgba(250,204,21,0.55)] hover:shadow-[0_0_16px_rgba(250,204,21,0.75)] transition-shadow"
      aria-label="Browse Vault"
      @click="openDialog"
    />

    <UModal
      v-model:open="dialogOpen"
      title="Browse Vault"
      description="Pick a mount, path and field to build the reference"
    >
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-folder-search" class="w-5 h-5 text-primary-500" />
              <h2 class="font-semibold text-lg">Browse Vault</h2>
            </div>
          </template>

          <div class="w-full sm:w-[28rem] space-y-4">
            <div>
              <p class="text-xs font-medium text-gray-500 mb-1">Mount</p>
              <AppSelectInput
                :model-value="selectedMountPath || ''"
                :items="mounts.map(m => ({ label: m.path, value: m.path }))"
                :loading="mountsLoading"
                placeholder="Select a KV mount"
                @update:model-value="selectMount"
              />
            </div>

            <div v-if="selectedMount">
              <p class="text-xs font-medium text-gray-500 mb-1">Path</p>
              <div class="flex flex-wrap items-center gap-1 text-xs mb-2">
                <button type="button" class="text-primary-500 hover:underline" @click="goToRoot">{{ selectedMount.path }}</button>
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

            <div v-if="selectedSecretPath">
              <p class="text-xs font-medium text-gray-500 mb-1">Field</p>
              <div v-if="fieldsLoading" class="text-xs text-gray-400">Loading...</div>
              <div v-else class="flex flex-wrap gap-1">
                <UButton
                  v-for="field in fields"
                  :key="field"
                  :label="field"
                  size="xs"
                  :variant="selectedField === field ? 'solid' : 'outline'"
                  :color="selectedField === field ? 'primary' : 'neutral'"
                  @click="selectField(field)"
                />
                <p v-if="!fields.length" class="text-xs text-gray-400">No fields found</p>
              </div>
            </div>
          </div>
        </UCard>
      </template>
    </UModal>
  </div>
</template>
