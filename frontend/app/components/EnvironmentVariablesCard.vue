<script setup lang="ts">
type TargetType = 'stack' | 'job'

const props = defineProps<{
  targetType: TargetType
  targetId: string
}>()

const emit = defineEmits<{
  keysChanged: [keys: string[]]
}>()

const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()
const toast = useToast()

const envVars = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const deletingId = ref('')
const creating = ref(false)
const editingEnvId = ref<string | null>(null)
const envToDelete = ref<any | null>(null)

const newEnvKey = ref('')
const newEnvValue = ref('')
const newEnvSecret = ref(false)
const editEnvKey = ref('')
const editEnvValue = ref('')
const editEnvSecret = ref(false)

const collection = computed(() => props.targetType === 'stack' ? 'stack_env_vars' : 'job_env_vars')
const targetField = computed(() => props.targetType === 'stack' ? 'stack' : 'job')

function emitKeys() {
  emit('keysChanged', envVars.value.map(env => env.key).filter(Boolean))
}

function resetNewEnv() {
  newEnvKey.value = ''
  newEnvValue.value = ''
  newEnvSecret.value = false
}

function startCreateEnv() {
  editingEnvId.value = null
  resetNewEnv()
  creating.value = true
}

function cancelCreateEnv() {
  creating.value = false
  resetNewEnv()
}

async function load() {
  if (!props.targetId) return
  loading.value = true
  try {
    envVars.value = await $pb.collection(collection.value).getFullList({
      filter: `${targetField.value} = "${props.targetId}"`,
      sort: 'key',
      requestKey: null,
    })
    emitKeys()
  } catch (error: any) {
    toast.add({ title: 'Failed to load environment variables', description: error?.message, color: 'error' })
  } finally {
    loading.value = false
  }
}

async function addEnvVar() {
  if (!newEnvKey.value.trim()) return
  saving.value = true
  try {
    await $pb.collection(collection.value).create({
      [targetField.value]: props.targetId,
      key: newEnvKey.value.trim(),
      value: newEnvValue.value,
      secret: newEnvSecret.value,
    }, { requestKey: null })
    resetNewEnv()
    creating.value = false
    await load()
    toast.add({ title: 'Env var added', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to add env var', description: error?.message, color: 'error' })
  } finally {
    saving.value = false
  }
}

function startEditEnv(env: any) {
  creating.value = false
  editingEnvId.value = env.id
  editEnvKey.value = env.key
  editEnvValue.value = env.secret ? '' : env.value
  editEnvSecret.value = env.secret
}

function cancelEditEnv() {
  editingEnvId.value = null
}

async function saveEditEnv(id: string) {
  if (!editEnvKey.value.trim()) return
  saving.value = true
  try {
    const data: Record<string, any> = {
      key: editEnvKey.value.trim(),
      secret: editEnvSecret.value,
    }
    if (editEnvValue.value) data.value = editEnvValue.value
    await $pb.collection(collection.value).update(id, data, { requestKey: null })
    editingEnvId.value = null
    await load()
    toast.add({ title: 'Env var updated', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to update env var', description: error?.message, color: 'error' })
  } finally {
    saving.value = false
  }
}

function openDeleteEnvModal(env: any) {
  envToDelete.value = env
}

function cancelDeleteEnv() {
  envToDelete.value = null
}

async function confirmDeleteEnvVar() {
  if (!envToDelete.value) return
  deletingId.value = envToDelete.value.id
  try {
    await $pb.collection(collection.value).delete(envToDelete.value.id, { requestKey: null })
    envToDelete.value = null
    await load()
    toast.add({ title: 'Env var removed', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to remove env var', description: error?.message, color: 'error' })
  } finally {
    deletingId.value = ''
  }
}

onMounted(() => {
  load()
  subscribe(collection.value, (event: any) => {
    if (event.record?.[targetField.value] === props.targetId) load()
  })
})
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center justify-between gap-3">
        <div>
          <h3 class="font-semibold">Environment Variables</h3>
          <p class="text-xs text-gray-500 mt-0.5">Local variables override global variables with the same key.</p>
        </div>
        <div class="flex items-center gap-2">
          <UBadge :label="`${envVars.length}`" color="neutral" variant="subtle" />
          <UButton icon="i-lucide-plus" label="Add" size="xs" :disabled="creating" @click="startCreateEnv" />
          <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" size="xs" :loading="loading" @click="load" />
        </div>
      </div>
    </template>

    <div>
      <div v-if="creating || envVars.length" class="divide-y divide-gray-200 dark:divide-gray-800">
        <form v-if="creating" class="grid grid-cols-1 gap-2 py-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center" @submit.prevent="addEnvVar">
          <UInput v-model="newEnvKey" placeholder="KEY" class="font-mono" />
          <UInput v-model="newEnvValue" placeholder="value" :type="newEnvSecret ? 'password' : 'text'" class="font-mono" />
          <UButton
            type="button"
            :icon="newEnvSecret ? 'i-lucide-lock' : 'i-lucide-variable'"
            :color="newEnvSecret ? 'warning' : 'neutral'"
            variant="soft"
            size="xs"
            class="h-8 w-8 justify-center p-0"
            :aria-pressed="newEnvSecret"
            :aria-label="newEnvSecret ? 'Set as plain text' : 'Set as secret'"
            :title="newEnvSecret ? 'Secret' : 'Plain text'"
            @click="newEnvSecret = !newEnvSecret"
          />
          <UButton type="submit" icon="i-lucide-check" variant="ghost" color="success" size="xs" class="h-8 w-8 justify-center p-0" :loading="saving" :disabled="!newEnvKey.trim()" aria-label="Add environment variable" />
          <UButton type="button" icon="i-lucide-x" variant="ghost" color="neutral" size="xs" class="h-8 w-8 justify-center p-0" aria-label="Cancel" @click="cancelCreateEnv" />
        </form>

        <div v-for="env in envVars" :key="env.id" class="py-2">
          <template v-if="editingEnvId === env.id">
            <div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center">
              <UInput v-model="editEnvKey" placeholder="KEY" class="font-mono" />
              <UInput v-model="editEnvValue" :placeholder="env.secret ? '(unchanged if empty)' : 'value'" :type="editEnvSecret ? 'password' : 'text'" class="font-mono" />
              <UButton
                type="button"
                :icon="editEnvSecret ? 'i-lucide-lock' : 'i-lucide-variable'"
                :color="editEnvSecret ? 'warning' : 'neutral'"
                variant="soft"
                size="xs"
                class="h-8 w-8 justify-center p-0"
                :disabled="env.secret"
                :aria-pressed="editEnvSecret"
                :aria-label="editEnvSecret ? 'Set as plain text' : 'Set as secret'"
                :title="env.secret ? 'Secret cannot be converted to plain text' : editEnvSecret ? 'Secret' : 'Plain text'"
                @click="editEnvSecret = !editEnvSecret"
              />
              <UButton icon="i-lucide-check" variant="ghost" color="success" size="xs" class="h-8 w-8 justify-center p-0" :loading="saving" aria-label="Save environment variable" @click="saveEditEnv(env.id)" />
              <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" class="h-8 w-8 justify-center p-0" aria-label="Cancel" @click="cancelEditEnv" />
            </div>
          </template>

          <template v-else>
            <div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center">
              <UInput :model-value="env.key" disabled class="font-mono opacity-60" />
              <UInput v-if="env.secret" model-value="••••••••" disabled type="password" class="font-mono opacity-60" />
              <UInput v-else :model-value="env.value" disabled class="font-mono opacity-60" />
              <div class="flex h-8 w-8 items-center justify-center">
                <UIcon
                  :name="env.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
                  class="h-4 w-4"
                  :class="env.secret ? 'text-amber-400' : 'text-gray-400'"
                  :title="env.secret ? 'Secret' : 'Plain text'"
                />
              </div>
              <UButton icon="i-lucide-pencil" variant="ghost" size="xs" class="h-8 w-8 justify-center p-0" aria-label="Edit environment variable" @click="startEditEnv(env)" />
              <UButton icon="i-lucide-trash-2" variant="ghost" color="error" size="xs" class="h-8 w-8 justify-center p-0" :loading="deletingId === env.id" aria-label="Delete environment variable" @click="openDeleteEnvModal(env)" />
            </div>
          </template>
        </div>
      </div>
      <p v-else class="py-2 text-sm text-gray-500">No environment variables defined</p>
    </div>

    <UModal :open="!!envToDelete" @update:open="value => { if (!value) cancelDeleteEnv() }">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2 text-red-600">
              <UIcon name="i-lucide-alert-triangle" class="h-5 w-5" />
              <h3 class="font-semibold text-gray-900 dark:text-white">Delete Environment Variable</h3>
            </div>
          </template>

          <p class="text-sm text-gray-500">
            Are you sure you want to delete <span class="font-mono font-semibold text-gray-900 dark:text-gray-100">{{ envToDelete?.key }}</span>?
          </p>

          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton label="Cancel" variant="outline" color="neutral" @click="cancelDeleteEnv" />
              <UButton
                label="Delete"
                icon="i-lucide-trash-2"
                color="error"
                :loading="deletingId === envToDelete?.id"
                @click="confirmDeleteEnvVar"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>
  </UCard>
</template>
