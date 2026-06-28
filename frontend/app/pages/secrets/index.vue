<script setup lang="ts">
const { $pb } = useNuxtApp()
const { canOperate } = usePermissions()
const { subscribe } = useRealtime()
const toast = useToast()

const globals = ref<any[]>([])
const stackBindings = ref<any[]>([])
const jobBindings = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const deletingId = ref('')
const creating = ref(false)
const variableToDelete = ref<any | null>(null)

const form = ref({
  key: '',
  value: '',
  secret: false,
})

const editingId = ref('')
const editForm = ref({
  key: '',
  value: '',
  secret: false,
})

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

function resetForm() {
  form.value = { key: '', value: '', secret: false }
}

function startCreate() {
  editingId.value = ''
  resetForm()
  creating.value = true
}

function cancelCreate() {
  creating.value = false
  resetForm()
}

async function createVariable() {
  if (!form.value.key.trim() || !canOperate.value) return
  saving.value = true
  try {
    await $pb.collection('global_env_vars').create({
      key: form.value.key.trim(),
      value: form.value.value,
      secret: form.value.secret,
    }, {
      requestKey: null,
    })
    resetForm()
    creating.value = false
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
  editingId.value = variable.id
  editForm.value = {
    key: variable.key,
    value: variable.secret ? '' : variable.value,
    secret: variable.secret,
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
      <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" :loading="loading" @click="load" />
    </div>

    <UCard>
      <template #header>
        <div class="flex items-center justify-between gap-3">
          <div class="flex items-center gap-2">
            <h3 class="font-semibold">Global Variables</h3>
            <UBadge :label="`${globals.length}`" color="neutral" variant="subtle" />
          </div>
          <UButton
            v-if="canOperate"
            icon="i-lucide-plus"
            label="Add"
            size="xs"
            :disabled="creating"
            @click="startCreate"
          />
        </div>
      </template>

      <div v-if="creating || globals.length" class="divide-y divide-gray-200 dark:divide-carbon-800">
        <form v-if="creating" class="grid grid-cols-1 gap-2 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center" @submit.prevent="createVariable">
          <UInput v-model="form.key" placeholder="KEY" class="font-mono" />
          <UInput v-model="form.value" placeholder="value" :type="form.secret ? 'password' : 'text'" class="font-mono" />
          <UButton
            type="button"
            :icon="form.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
            :color="form.secret ? 'warning' : 'neutral'"
            variant="soft"
            size="xs"
            class="h-8 w-8 justify-center p-0"
            :aria-pressed="form.secret"
            :aria-label="form.secret ? 'Set as plain text' : 'Set as secret'"
            :title="form.secret ? 'Secret' : 'Plain text'"
            @click="form.secret = !form.secret"
          />
          <UButton type="submit" icon="i-lucide-check" variant="ghost" color="success" size="xs" class="h-8 w-8 justify-center p-0" :loading="saving" :disabled="!form.key.trim()" aria-label="Create variable" />
          <UButton type="button" icon="i-lucide-x" variant="ghost" color="neutral" size="xs" class="h-8 w-8 justify-center p-0" aria-label="Cancel" @click="cancelCreate" />
        </form>

        <div v-for="variable in globals" :key="variable.id" class="py-2">
          <div v-if="editingId === variable.id" class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center">
            <UInput v-model="editForm.key" placeholder="KEY" class="font-mono" />
            <UInput v-model="editForm.value" :placeholder="variable.secret ? '(unchanged if empty)' : 'value'" :type="editForm.secret ? 'password' : 'text'" class="font-mono" />
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
            <UInput :model-value="variable.key" disabled class="font-mono opacity-60" />
            <UInput v-if="variable.secret" model-value="••••••••" disabled type="password" class="font-mono opacity-60" />
            <UInput v-else :model-value="variable.value" disabled class="font-mono opacity-60" />
            <div class="flex h-8 w-8 items-center justify-center">
              <UIcon
                :name="variable.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
                class="h-4 w-4"
                :class="variable.secret ? 'text-amber-400' : 'text-gray-400'"
                :title="variable.secret ? 'Secret' : 'Plain text'"
              />
            </div>
            <UButton v-if="canOperate" icon="i-lucide-pencil" variant="ghost" color="neutral" size="xs" class="h-8 w-8 justify-center p-0" aria-label="Edit variable" @click="startEdit(variable)" />
            <div v-else class="h-8 w-8" />
            <UTooltip v-if="canOperate" :text="usageTotal(variable) > 0 ? 'Detach this variable from stacks and jobs before deleting it' : 'Delete variable'">
              <UButton
                icon="i-lucide-trash-2"
                variant="ghost"
                color="error"
                size="xs"
                class="h-8 w-8 justify-center p-0"
                :disabled="usageTotal(variable) > 0"
                :loading="deletingId === variable.id"
                aria-label="Delete variable"
                @click="openDeleteVariableModal(variable)"
              />
            </UTooltip>
            <div v-else class="h-8 w-8" />
          </div>
        </div>
      </div>

      <div v-else class="py-10 text-center">
        <UIcon name="i-lucide-key-round" class="mx-auto h-8 w-8 text-gray-400" />
        <p class="mt-2 text-sm text-gray-500">No global variables configured</p>
      </div>
    </UCard>

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
  </div>
</template>
