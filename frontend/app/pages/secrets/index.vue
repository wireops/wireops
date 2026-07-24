<script setup lang="ts">
const { $pb } = useNuxtApp()
const { canOperate } = usePermissions()
const { subscribe } = useRealtime()
const toast = useToast()
const route = useRoute()
const router = useRouter()

const activeTab = ref(route.query.tab === 'repository-keys' ? 'repository-keys' : 'global-variables')
const keysPanel = ref<any>()
const tabs = [
  { label: 'Global Variables', value: 'global-variables', icon: 'i-lucide-variable' },
  { label: 'Repository Keys', value: 'repository-keys', icon: 'i-lucide-key-round' },
]
const globals = ref<any[]>([])
const stackBindings = ref<any[]>([])
const jobBindings = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const deletingId = ref('')
const creating = ref(false)
const showCreateModal = ref(false)
const variableToDelete = ref<any | null>(null)
const showSopsEncryptModal = ref(false)
const search = ref('')

const filteredGlobals = computed(() => {
  if (!search.value.trim()) return globals.value
  const query = search.value.trim().toLowerCase()
  return globals.value.filter(v => v.key.toLowerCase().includes(query))
})

const { load: loadProviderOptions, providerOptions, hasActiveBackends, iconFor, avatarFor, labelFor } = useSecretProviderOptions()

const form = ref({
  key: '',
  value: '',
  secret: true,
  secret_provider: 'internal',
})

const editingId = ref('')
const editForm = ref({
  key: '',
  value: '',
  secret: false,
  secret_provider: 'internal',
})

// vault/infisical "value" is just a reference to where the secret lives
// (e.g. "mount/data/path#field") — not the secret itself — so it isn't
// sensitive and shouldn't be masked/hidden like an internal-provider secret.
function isInternalSecret(variable: any) {
  return variable.secret && (!variable.secret_provider || variable.secret_provider === 'internal')
}

function providerOf(variable: any) {
  return variable.secret_provider || 'internal'
}

// Once a secret var is stored under a provider, its provider is locked —
// only newly-created vars, or plain vars being converted to secret for the
// first time, get to pick one.
const editingOriginal = computed(() => globals.value.find(v => v.id === editingId.value))
const canChangeEditProvider = computed(() => !editingOriginal.value?.secret)

const usage = computed(() => {
  const counts: Record<string, { stacks: number, jobs: number }> = {}
  for (const variable of globals.value) {
    counts[variable.id] = { stacks: 0, jobs: 0 }
  }
  for (const binding of stackBindings.value) {
    if (binding.global_env_var) {
      counts[binding.global_env_var] ||= { stacks: 0, jobs: 0 }
      counts[binding.global_env_var].stacks++
    }
  }
  for (const binding of jobBindings.value) {
    if (binding.global_env_var) {
      counts[binding.global_env_var] ||= { stacks: 0, jobs: 0 }
      counts[binding.global_env_var].jobs++
    }
  }
  return counts
})

function usageTotal(variable: any) {
  const count = usage.value[variable.id]
  return (count?.stacks || 0) + (count?.jobs || 0)
}

async function load() {
  loading.value = true
  try {
    const [globalRecords, stackRecords, jobRecords] = await Promise.all([
      $pb.collection('global_env_vars').getFullList({ sort: 'key', requestKey: null }),
      $pb.collection('stack_global_env_vars').getFullList({ sort: 'created', requestKey: null }),
      $pb.collection('job_global_env_vars').getFullList({ sort: 'created', requestKey: null }),
    ])
    globals.value = globalRecords
    stackBindings.value = stackRecords
    jobBindings.value = jobRecords
  } catch (error: any) {
    toast.add({ title: 'Failed to load secrets', description: error?.message, color: 'error' })
  } finally {
    loading.value = false
  }
}

async function refreshActiveTab() {
  if (activeTab.value === 'repository-keys') {
    await (keysPanel.value?.refresh?.() || refreshNuxtData('repository_keys_panel'))
    return
  }
  await load()
}

watch(activeTab, (tab) => {
  const nextQuery = { ...route.query }
  if (tab === 'repository-keys') {
    nextQuery.tab = tab
  } else {
    delete nextQuery.tab
  }
  if (route.query.tab !== nextQuery.tab) router.replace({ query: nextQuery })
  if (tab === 'repository-keys') refreshNuxtData('repository_keys_panel')
})

watch(() => route.query.tab, (tab) => {
  activeTab.value = tab === 'repository-keys' ? 'repository-keys' : 'global-variables'
})

watch(showCreateModal, (open) => {
  if (!open && !saving.value) resetForm()
})

function resetForm() {
  form.value = { key: '', value: '', secret: true, secret_provider: 'internal' }
}

function startCreate() {
  editingId.value = ''
  showCreateModal.value = false
  resetForm()
  creating.value = true
}

function cancelCreate() {
  creating.value = false
  showCreateModal.value = false
  resetForm()
}

function openCreateModal() {
  editingId.value = ''
  creating.value = false
  resetForm()
  showCreateModal.value = true
}

async function createVariable() {
  if (!form.value.key.trim() || !canOperate.value) return
  saving.value = true
  try {
    await $pb.collection('global_env_vars').create({
      key: form.value.key.trim(),
      value: form.value.value,
      secret: form.value.secret,
      secret_provider: form.value.secret ? form.value.secret_provider : '',
    }, {
      requestKey: null,
    })
    resetForm()
    creating.value = false
    showCreateModal.value = false
    await load()
    toast.add({ title: 'Global variable created', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to create variable', description: error?.message, color: 'error' })
  } finally {
    saving.value = false
  }
}

function startEdit(variable: any) {
  creating.value = false
  showCreateModal.value = false
  editingId.value = variable.id
  editForm.value = {
    key: variable.key,
    value: isInternalSecret(variable) ? '' : variable.value,
    secret: variable.secret,
    secret_provider: variable.secret_provider || 'internal',
  }
}

function cancelEdit() {
  editingId.value = ''
}

async function saveEdit(variable: any) {
  if (!editForm.value.key.trim() || !canOperate.value) return
  saving.value = true
  try {
    const payload: Record<string, any> = {
      key: editForm.value.key.trim(),
      secret: editForm.value.secret,
      secret_provider: editForm.value.secret ? editForm.value.secret_provider : '',
    }
    if (!variable.secret || editForm.value.value || variable.secret !== editForm.value.secret) {
      payload.value = editForm.value.value
    }
    await $pb.collection('global_env_vars').update(variable.id, payload, { requestKey: null })
    editingId.value = ''
    await load()
    toast.add({ title: 'Global variable updated', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to update variable', description: error?.message, color: 'error' })
  } finally {
    saving.value = false
  }
}

function openDeleteVariableModal(variable: any) {
  if (!canOperate.value || usageTotal(variable) > 0) return
  variableToDelete.value = variable
}

function cancelDeleteVariable() {
  variableToDelete.value = null
}

async function confirmDeleteVariable() {
  const variable = variableToDelete.value
  if (!variable) return
  if (!canOperate.value || usageTotal(variable) > 0) return
  deletingId.value = variable.id
  try {
    await $pb.collection('global_env_vars').delete(variable.id, { requestKey: null })
    variableToDelete.value = null
    await load()
    toast.add({ title: 'Global variable deleted', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to delete variable', description: error?.message, color: 'error' })
  } finally {
    deletingId.value = ''
  }
}

onMounted(() => {
  load()
  loadProviderOptions()
  subscribe('global_env_vars', () => load())
  subscribe('stack_global_env_vars', () => load())
  subscribe('job_global_env_vars', () => load())
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="flex items-center gap-3 text-2xl font-bold text-gray-900 dark:text-wire-200">
          <span class="flex h-9 w-9 items-center justify-center rounded-lg bg-yellow-400/10">
            <UIcon name="i-lucide-key-round" class="h-5 w-5 text-yellow-400" />
          </span>
          Secrets
        </h1>
      </div>
      <div class="flex items-center gap-2">
        <UButton v-if="canOperate" icon="i-lucide-file-lock-2" label="Encrypt for SOPS" variant="outline" @click="showSopsEncryptModal = true" />
        <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" :loading="activeTab === 'global-variables' && loading" @click="refreshActiveTab" />
      </div>
    </div>

    <UTabs v-model="activeTab" :items="tabs" />

    <UCard v-if="activeTab === 'global-variables'">
      <template #header>
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="flex items-center gap-2">
              <h3 class="font-semibold">Global Variables</h3>
              <UBadge :label="`${globals.length}`" color="neutral" variant="subtle" />
            </div>
            <p class="text-xs text-gray-500 mt-0.5">Reusable variables and secrets for stacks and jobs.</p>
          </div>
          <UButton
            v-if="canOperate"
            icon="i-lucide-plus"
            label="Add"
            class="sm:hidden"
            :disabled="showCreateModal"
            @click="openCreateModal"
          />
          <UButton
            v-if="canOperate"
            icon="i-lucide-plus"
            label="Add"
            class="hidden sm:inline-flex"
            :disabled="creating"
            @click="startCreate"
          />
        </div>
      </template>

      <div v-if="globals.length" role="search" aria-label="Filter global variables">
        <AppTextInput v-model="search" icon="i-lucide-search" placeholder="Search variables..." class="w-full" aria-label="Search global variables" />
      </div>

      <div v-if="creating || filteredGlobals.length" class="divide-y divide-gray-200 dark:divide-carbon-800">
        <form v-if="creating" class="grid grid-cols-1 gap-2 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center" @submit.prevent="createVariable">
          <AppTextInput v-model="form.key" placeholder="KEY" class="font-mono" />
          <div class="flex items-center gap-1">
            <AppSelectInput
              v-if="form.secret && hasActiveBackends"
              v-model="form.secret_provider"
              :items="providerOptions"
              :searchable="false"
              content-width
              class="font-mono shrink-0"
            />
            <IntegrationsVaultReferencePicker v-if="form.secret && form.secret_provider === 'vault'" v-model="form.value" />
            <IntegrationsInfisicalReferencePicker v-else-if="form.secret && form.secret_provider === 'infisical'" v-model="form.value" />
            <AppTextInput
              v-else
              v-model="form.value"
              placeholder="value"
              :type="form.secret ? 'password' : 'text'"
              :icon="form.secret ? iconFor(form.secret_provider) : undefined"
              :avatar="form.secret ? avatarFor(form.secret_provider) : undefined"
              class="font-mono w-full"
            />
          </div>
          <div class="grid grid-cols-3 gap-2 sm:contents">
            <UButton
              type="button"
              :icon="form.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
              :color="form.secret ? 'warning' : 'neutral'"
              variant="soft"
              size="xs"
              class="h-8 w-full justify-center p-0 sm:w-8"
              :class="form.secret ? '!bg-amber-400/15 !text-amber-500 dark:!bg-amber-400/10 dark:!text-amber-400' : '!bg-gray-100 !text-gray-500 sm:!bg-transparent dark:!bg-carbon-800 dark:!text-gray-400 sm:dark:!bg-transparent'"
              :aria-pressed="form.secret"
              :aria-label="form.secret ? 'Set as plain text' : 'Set as secret'"
              :title="form.secret ? 'Secret' : 'Plain text'"
              @click="form.secret = !form.secret"
            />
            <UButton type="submit" icon="i-lucide-check" variant="ghost" color="success" size="xs" class="h-8 w-full justify-center !bg-green-500/10 p-0 !text-green-600 hover:!bg-green-500/15 sm:w-8 sm:!bg-transparent sm:!text-inherit sm:hover:!bg-transparent dark:!text-green-400" :loading="saving" :disabled="!form.key.trim()" aria-label="Create variable" />
            <UButton type="button" icon="i-lucide-x" variant="ghost" color="neutral" size="xs" class="h-8 w-full justify-center !bg-gray-100 p-0 !text-gray-600 hover:!bg-gray-200 sm:w-8 sm:!bg-transparent sm:!text-inherit sm:hover:!bg-transparent dark:!bg-carbon-800 dark:!text-gray-400 dark:hover:!bg-carbon-700 sm:dark:!bg-transparent sm:dark:hover:!bg-transparent" aria-label="Cancel" @click="cancelCreate" />
          </div>
        </form>

        <div v-for="variable in filteredGlobals" :key="variable.id" class="py-2">
          <div v-if="editingId === variable.id" class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center">
            <AppTextInput v-model="editForm.key" placeholder="KEY" class="font-mono" />
            <div class="flex items-center gap-1">
              <AppSelectInput
                v-if="editForm.secret && hasActiveBackends && canChangeEditProvider"
                v-model="editForm.secret_provider"
                :items="providerOptions"
                :searchable="false"
                content-width
                class="font-mono shrink-0"
              />
              <IntegrationsVaultReferencePicker v-if="editForm.secret && editForm.secret_provider === 'vault'" v-model="editForm.value" />
              <IntegrationsInfisicalReferencePicker v-else-if="editForm.secret && editForm.secret_provider === 'infisical'" v-model="editForm.value" />
              <AppTextInput
                v-else
                v-model="editForm.value"
                :placeholder="editForm.secret ? '(unchanged if empty)' : 'value'"
                :type="editForm.secret ? 'password' : 'text'"
                :icon="editForm.secret ? iconFor(editForm.secret_provider) : undefined"
                :avatar="editForm.secret ? avatarFor(editForm.secret_provider) : undefined"
                :title="editForm.secret ? (!canChangeEditProvider ? `Locked to ${labelFor(editForm.secret_provider)}` : labelFor(editForm.secret_provider)) : undefined"
                class="font-mono w-full"
              />
            </div>
            <UButton
              type="button"
              :icon="editForm.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
              :color="editForm.secret ? 'warning' : 'neutral'"
              variant="soft"
              size="xs"
              class="h-8 w-8 justify-center p-0"
              :disabled="variable.secret"
              :aria-pressed="editForm.secret"
              :aria-label="editForm.secret ? 'Set as plain text' : 'Set as secret'"
              :title="variable.secret ? 'Secret cannot be converted to plain text' : editForm.secret ? 'Secret' : 'Plain text'"
              @click="editForm.secret = !editForm.secret"
            />
            <UButton icon="i-lucide-check" variant="ghost" color="success" size="xs" class="h-8 w-8 justify-center p-0" :loading="saving" aria-label="Save variable" @click="saveEdit(variable)" />
            <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" class="h-8 w-8 justify-center p-0" aria-label="Cancel" @click="cancelEdit" />
          </div>

          <div v-else class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center">
            <AppTextInput :model-value="variable.key" disabled class="font-mono" />
            <AppTextInput
              v-if="isInternalSecret(variable)"
              model-value="••••••••"
              disabled
              type="password"
              :icon="iconFor(providerOf(variable))"
              :title="`Stored via ${labelFor(providerOf(variable))}`"
              class="font-mono"
            />
            <AppTextInput
              v-else
              :model-value="variable.value"
              disabled
              :icon="variable.secret ? iconFor(providerOf(variable)) : undefined"
              :avatar="variable.secret ? avatarFor(providerOf(variable)) : undefined"
              :title="variable.secret ? `Stored via ${labelFor(providerOf(variable))}` : undefined"
              class="font-mono"
            />
            <div class="grid grid-cols-3 gap-2 sm:contents">
              <div
                class="flex h-8 w-full items-center justify-center rounded-md bg-gray-100 text-gray-500 sm:w-8 sm:bg-transparent dark:bg-carbon-800 dark:text-gray-400 sm:dark:bg-transparent"
                :class="variable.secret ? 'bg-amber-400/15 text-amber-500 dark:bg-amber-400/10 dark:text-amber-400' : ''"
              >
                <UIcon
                  :name="variable.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
                  class="h-4 w-4"
                  :title="variable.secret ? (isInternalSecret(variable) ? 'Secret' : `Secret (${variable.secret_provider} reference)`) : 'Plain text'"
                />
              </div>
              <UButton v-if="canOperate" icon="i-lucide-pencil" variant="ghost" color="neutral" size="xs" class="h-8 w-full justify-center bg-sky-500/10 p-0 text-sky-600 hover:bg-sky-500/15 sm:w-8 sm:bg-transparent sm:text-inherit sm:hover:bg-transparent dark:text-sky-400" aria-label="Edit variable" @click="startEdit(variable)" />
              <div v-else class="h-8 w-full sm:w-8" />
              <UTooltip v-if="canOperate" :text="usageTotal(variable) > 0 ? 'Detach this variable from stacks and jobs before deleting it' : 'Delete variable'" class="w-full sm:w-8">
                <UButton
                  icon="i-lucide-trash-2"
                  variant="ghost"
                  color="error"
                  size="xs"
                  class="h-8 w-full justify-center p-0 sm:w-8 sm:!bg-transparent sm:!text-inherit sm:hover:!bg-transparent"
                  :class="usageTotal(variable) > 0 ? '!bg-gray-100 !text-gray-400 hover:!bg-gray-100 dark:!bg-carbon-800 dark:!text-gray-500 dark:hover:!bg-carbon-800 sm:dark:!bg-transparent' : '!bg-red-500/10 !text-red-600 hover:!bg-red-500/15 dark:!text-red-400'"
                  :disabled="usageTotal(variable) > 0"
                  :loading="deletingId === variable.id"
                  aria-label="Delete variable"
                  @click="openDeleteVariableModal(variable)"
                />
              </UTooltip>
              <div v-else class="h-8 w-full sm:w-8" />
            </div>
          </div>
        </div>
      </div>

      <div v-else-if="globals.length" class="py-10 text-center" role="status" aria-live="polite">
        <UIcon name="i-lucide-search-x" class="mx-auto h-8 w-8 text-gray-400" />
        <p class="mt-2 text-sm text-gray-500">No variables found</p>
        <p class="mt-1 text-xs text-gray-400">Try adjusting your search</p>
      </div>

      <div v-else class="py-10 text-center">
        <UIcon name="i-lucide-key-round" class="mx-auto h-8 w-8 text-gray-400" />
        <p class="mt-2 text-sm text-gray-500">No global variables configured</p>
      </div>
    </UCard>

    <UModal v-model:open="showCreateModal" :ui="{ content: 'w-full sm:max-w-md' }">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-key-round" class="h-5 w-5 text-yellow-400" />
              <h3 class="font-semibold text-gray-900 dark:text-white">Create Secret</h3>
            </div>
          </template>

          <form class="space-y-4" @submit.prevent="createVariable">
            <UFormField label="Key" required>
              <AppTextInput v-model="form.key" placeholder="KEY" class="font-mono" />
            </UFormField>

            <UFormField v-if="form.secret && hasActiveBackends" label="Provider">
              <AppSelectInput v-model="form.secret_provider" :items="providerOptions" :searchable="false" class="w-full" />
            </UFormField>

            <UFormField label="Value">
              <IntegrationsVaultReferencePicker v-if="form.secret && form.secret_provider === 'vault'" v-model="form.value" />
              <IntegrationsInfisicalReferencePicker v-else-if="form.secret && form.secret_provider === 'infisical'" v-model="form.value" />
              <AppTextInput
                v-else
                v-model="form.value"
                placeholder="value"
                :type="form.secret ? 'password' : 'text'"
                :icon="form.secret ? iconFor(form.secret_provider) : undefined"
                :avatar="form.secret ? avatarFor(form.secret_provider) : undefined"
                class="font-mono"
              />
            </UFormField>

            <UButton
              type="button"
              :icon="form.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
              :color="form.secret ? 'warning' : 'neutral'"
              variant="soft"
              class="w-full justify-center"
              :aria-pressed="form.secret"
              :aria-label="form.secret ? 'Set as plain text' : 'Set as secret'"
              @click="form.secret = !form.secret"
            >
              {{ form.secret ? 'Secret' : 'Plain text' }}
            </UButton>

            <div class="grid grid-cols-2 gap-2 pt-2">
              <UButton type="button" label="Cancel" variant="outline" color="neutral" block @click="cancelCreate" />
              <UButton type="submit" label="Create" icon="i-lucide-check" color="success" block :loading="saving" :disabled="!form.key.trim()" />
            </div>
          </form>
        </UCard>
      </template>
    </UModal>

    <UModal :open="!!variableToDelete" @update:open="value => { if (!value) cancelDeleteVariable() }">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2 text-red-600">
              <UIcon name="i-lucide-alert-triangle" class="h-5 w-5" />
              <h3 class="font-semibold text-gray-900 dark:text-white">Delete Global Variable</h3>
            </div>
          </template>

          <div class="space-y-2">
            <p class="text-sm text-gray-500">
              Are you sure you want to delete <span class="font-mono font-semibold text-gray-900 dark:text-gray-100">{{ variableToDelete?.key }}</span>?
            </p>
            <p class="text-xs text-red-500 font-medium">This action cannot be undone.</p>
          </div>

          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton label="Cancel" variant="outline" color="neutral" @click="cancelDeleteVariable" />
              <UButton
                label="Delete"
                icon="i-lucide-trash-2"
                color="error"
                :loading="deletingId === variableToDelete?.id"
                @click="confirmDeleteVariable"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>

    <RepositoryKeysPanel v-if="activeTab === 'repository-keys'" ref="keysPanel" />

    <EncryptSopsSecretsModal v-model:open="showSopsEncryptModal" />
  </div>
</template>
