<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'

type TargetType = 'stack' | 'job'

const props = defineProps<{
  targetType: TargetType
  targetId: string
}>()

const emit = defineEmits<{
  keysChanged: [keys: string[]]
}>()

const { $pb } = useNuxtApp()
const { customGet } = useApi()
const { subscribe } = useRealtime()
const toast = useToast()
const { load: loadProviderOptions, providerOptions, hasActiveBackends, iconFor, avatarFor, labelFor } = useSecretProviderOptions()

// SOPS-managed keys (decrypted from secrets.yaml, GitOps stacks only) are
// read-only/immutable here: key names only, the server never sends values to
// the browser. They override UI-defined vars with the same key at deploy
// time, so surface that precedence in the UI too.
const sopsKeys = ref<string[]>([])
const sopsSourceFile = ref('')
const sopsError = ref('')

async function loadSopsEnvVars() {
  if (props.targetType !== 'stack') return
  try {
    const res = await customGet<{ keys: string[]; available: boolean; source_file?: string; error?: string }>(
      `/api/custom/stacks/${props.targetId}/sops-env-vars`
    )
    sopsKeys.value = res.available ? res.keys : []
    sopsSourceFile.value = res.source_file || ''
    sopsError.value = res.error || ''
  } catch {
    sopsKeys.value = []
    sopsSourceFile.value = ''
    sopsError.value = 'Could not load SOPS override information'
  }
}

const envVars = ref<any[]>([])
const loading = ref(false)
const saving = ref(false)
const deletingId = ref('')
const creating = ref(false)
const showCreateModal = ref(false)
const editingEnvId = ref<string | null>(null)
const envToDelete = ref<any | null>(null)

const newEnvKey = ref('')
const newEnvValue = ref('')
const newEnvSecret = ref(true)
const newEnvProvider = ref('internal')
const editEnvKey = ref('')
const editEnvValue = ref('')
const editEnvSecret = ref(false)
const editEnvProvider = ref('internal')

// vault/infisical "value" is just a reference to where the secret lives
// (e.g. "mount/data/path#field") — not the secret itself — so it isn't
// sensitive and shouldn't be masked/hidden like an internal-provider secret.
function isInternalSecret(env: any) {
  return env.secret && (!env.secret_provider || env.secret_provider === 'internal')
}

function providerOf(env: any) {
  return env.secret_provider || 'internal'
}

// Once a secret var is stored under a provider, its provider is locked —
// only newly-created vars, or plain vars being converted to secret for the
// first time, get to pick one.
const editingOriginal = computed(() => envVars.value.find(env => env.id === editingEnvId.value))
const canChangeEditProvider = computed(() => !editingOriginal.value?.secret)

const collection = computed(() => props.targetType === 'stack' ? 'stack_env_vars' : 'job_env_vars')
const targetField = computed(() => props.targetType === 'stack' ? 'stack' : 'job')

function emitKeys() {
  emit('keysChanged', envVars.value.map(env => env.key).filter(Boolean))
}

function resetNewEnv() {
  newEnvKey.value = ''
  newEnvValue.value = ''
  newEnvSecret.value = true
  newEnvProvider.value = 'internal'
}

function startCreateEnv() {
  editingEnvId.value = null
  showCreateModal.value = false
  resetNewEnv()
  creating.value = true
}

function cancelCreateEnv() {
  creating.value = false
  showCreateModal.value = false
  resetNewEnv()
}

function openCreateEnvModal() {
  editingEnvId.value = null
  creating.value = false
  resetNewEnv()
  showCreateModal.value = true
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
      secret_provider: newEnvSecret.value ? newEnvProvider.value : '',
    }, { requestKey: null })
    resetNewEnv()
    creating.value = false
    showCreateModal.value = false
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
  showCreateModal.value = false
  editingEnvId.value = env.id
  editEnvKey.value = env.key
  editEnvValue.value = isInternalSecret(env) ? '' : env.value
  editEnvSecret.value = env.secret
  editEnvProvider.value = env.secret_provider || 'internal'
}

function cancelEditEnv() {
  editingEnvId.value = null
}

async function saveEditEnv(id: string) {
  if (!editEnvKey.value.trim()) return
  saving.value = true
  try {
    const original = envVars.value.find(env => env.id === id)
    const data: Record<string, any> = {
      key: editEnvKey.value.trim(),
      secret: editEnvSecret.value,
      secret_provider: editEnvSecret.value ? editEnvProvider.value : '',
    }
    if (!editEnvSecret.value || !original?.secret || editEnvValue.value !== '') {
      data.value = editEnvValue.value
    }
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
  loadSopsEnvVars()
  loadProviderOptions()
  subscribe(collection.value, (event: any) => {
    if (event.record?.[targetField.value] === props.targetId) load()
  })
})

watch(showCreateModal, (open) => {
  if (!open && !saving.value) resetNewEnv()
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
          <UButton icon="i-lucide-plus" label="Add" size="xs" class="sm:hidden" :disabled="showCreateModal" @click="openCreateEnvModal" />
          <UButton icon="i-lucide-plus" label="Add" size="xs" class="hidden sm:inline-flex" :disabled="creating" @click="startCreateEnv" />
          <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" size="xs" :loading="loading" @click="load(); loadSopsEnvVars()" />
        </div>
      </div>
    </template>

    <div>
      <p v-if="sopsError" class="flex items-center gap-1.5 py-1 text-xs text-amber-600 dark:text-amber-400">
        <UIcon name="i-lucide-triangle-alert" class="h-3.5 w-3.5 shrink-0" />
        SOPS: {{ sopsError }}
      </p>
      <div v-if="creating || envVars.length || sopsKeys.length" class="divide-y divide-gray-200 dark:divide-gray-800">
        <form v-if="creating" class="grid grid-cols-1 gap-2 py-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center" @submit.prevent="addEnvVar">
          <AppTextInput v-model="newEnvKey" placeholder="KEY" class="font-mono" />
          <div class="flex items-center gap-1">
            <AppSelectInput
              v-if="newEnvSecret && hasActiveBackends"
              v-model="newEnvProvider"
              :items="providerOptions"
              :searchable="false"
              content-width
              class="font-mono shrink-0"
            />
            <IntegrationsVaultReferencePicker v-if="newEnvSecret && newEnvProvider === 'vault'" v-model="newEnvValue" />
            <IntegrationsInfisicalReferencePicker v-else-if="newEnvSecret && newEnvProvider === 'infisical'" v-model="newEnvValue" />
            <AppTextInput
              v-else
              v-model="newEnvValue"
              placeholder="value"
              :type="newEnvSecret ? 'password' : 'text'"
              :icon="newEnvSecret ? iconFor(newEnvProvider) : undefined"
              :avatar="newEnvSecret ? avatarFor(newEnvProvider) : undefined"
              class="font-mono w-full"
            />
          </div>
          <div class="grid grid-cols-3 gap-2 sm:contents">
            <UButton
              type="button"
              :icon="newEnvSecret ? 'i-lucide-lock' : 'i-lucide-variable'"
              :color="newEnvSecret ? 'warning' : 'neutral'"
              variant="soft"
              size="xs"
              class="h-8 w-full justify-center p-0 sm:w-8"
              :class="newEnvSecret ? '!bg-amber-400/15 !text-amber-500 dark:!bg-amber-400/10 dark:!text-amber-400' : '!bg-gray-100 !text-gray-500 sm:!bg-transparent dark:!bg-carbon-800 dark:!text-gray-400 sm:dark:!bg-transparent'"
              :aria-pressed="newEnvSecret"
              :aria-label="newEnvSecret ? 'Set as plain text' : 'Set as secret'"
              :title="newEnvSecret ? 'Secret' : 'Plain text'"
              @click="newEnvSecret = !newEnvSecret"
            />
            <UButton type="submit" icon="i-lucide-check" variant="ghost" color="success" size="xs" class="h-8 w-full justify-center !bg-green-500/10 p-0 !text-green-600 hover:!bg-green-500/15 sm:w-8 sm:!bg-transparent sm:!text-inherit sm:hover:!bg-transparent dark:!text-green-400" :loading="saving" :disabled="!newEnvKey.trim()" aria-label="Add environment variable" />
            <UButton type="button" icon="i-lucide-x" variant="ghost" color="neutral" size="xs" class="h-8 w-full justify-center !bg-gray-100 p-0 !text-gray-600 hover:!bg-gray-200 sm:w-8 sm:!bg-transparent sm:!text-inherit sm:hover:!bg-transparent dark:!bg-carbon-800 dark:!text-gray-400 dark:hover:!bg-carbon-700 sm:dark:!bg-transparent sm:dark:hover:!bg-transparent" aria-label="Cancel" @click="cancelCreateEnv" />
          </div>
        </form>

        <div v-for="env in envVars" :key="env.id" class="py-2">
          <template v-if="editingEnvId === env.id">
            <div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center">
              <AppTextInput v-model="editEnvKey" placeholder="KEY" class="font-mono" />
              <div class="flex items-center gap-1">
                <AppSelectInput
                  v-if="editEnvSecret && hasActiveBackends && canChangeEditProvider"
                  v-model="editEnvProvider"
                  :items="providerOptions"
                  :searchable="false"
                  content-width
                  class="font-mono shrink-0"
                />
                <IntegrationsVaultReferencePicker v-if="editEnvSecret && editEnvProvider === 'vault'" v-model="editEnvValue" />
                <IntegrationsInfisicalReferencePicker v-else-if="editEnvSecret && editEnvProvider === 'infisical'" v-model="editEnvValue" />
                <AppTextInput
                  v-else
                  v-model="editEnvValue"
                  :placeholder="editEnvSecret ? '(unchanged if empty)' : 'value'"
                  :type="editEnvSecret ? 'password' : 'text'"
                  :icon="editEnvSecret ? iconFor(editEnvProvider) : undefined"
                  :avatar="editEnvSecret ? avatarFor(editEnvProvider) : undefined"
                  :title="editEnvSecret ? (!canChangeEditProvider ? `Locked to ${labelFor(editEnvProvider)}` : labelFor(editEnvProvider)) : undefined"
                  class="font-mono w-full"
                />
              </div>
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
              <AppTextInput :model-value="env.key" disabled class="font-mono" />
              <AppTextInput
                v-if="isInternalSecret(env)"
                model-value="••••••••"
                disabled
                type="password"
                :icon="iconFor(providerOf(env))"
                :title="`Stored via ${labelFor(providerOf(env))}`"
                class="font-mono"
              />
              <AppTextInput
                v-else
                :model-value="env.value"
                disabled
                :icon="env.secret ? iconFor(providerOf(env)) : undefined"
                :avatar="env.secret ? avatarFor(providerOf(env)) : undefined"
                :title="env.secret ? `Stored via ${labelFor(providerOf(env))}` : undefined"
                class="font-mono"
              />
              <div class="grid grid-cols-3 gap-2 sm:contents">
                <div
                  class="flex h-8 w-full items-center justify-center rounded-md bg-gray-100 text-gray-500 sm:w-8 sm:bg-transparent dark:bg-carbon-800 dark:text-gray-400 sm:dark:bg-transparent"
                  :class="env.secret ? 'bg-amber-400/15 text-amber-500 dark:bg-amber-400/10 dark:text-amber-400' : ''"
                >
                  <UIcon
                    :name="env.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
                    class="h-4 w-4"
                    :title="env.secret ? (isInternalSecret(env) ? 'Secret' : `Secret (${env.secret_provider} reference)`) : 'Plain text'"
                  />
                </div>
                <UButton icon="i-lucide-pencil" variant="ghost" size="xs" class="h-8 w-full justify-center bg-sky-500/10 p-0 text-sky-600 hover:bg-sky-500/15 sm:w-8 sm:bg-transparent sm:text-inherit sm:hover:bg-transparent dark:text-sky-400" aria-label="Edit environment variable" @click="startEditEnv(env)" />
                <UButton icon="i-lucide-trash-2" variant="ghost" color="error" size="xs" class="h-8 w-full justify-center !bg-red-500/10 p-0 !text-red-600 hover:!bg-red-500/15 sm:w-8 sm:!bg-transparent sm:!text-inherit sm:hover:!bg-transparent dark:!text-red-400" :loading="deletingId === env.id" aria-label="Delete environment variable" @click="openDeleteEnvModal(env)" />
              </div>
            </div>
            <p v-if="sopsKeys.includes(env.key)" class="mt-1 flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400">
              <UIcon name="i-lucide-file-lock-2" class="h-3 w-3" />
              Overridden by SOPS at deploy time
            </p>
          </template>
        </div>

        <div v-for="key in sopsKeys" :key="`sops-${key}`" class="py-2">
          <div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center">
            <AppTextInput :model-value="key" disabled class="font-mono" />
            <AppTextInput
              model-value="••••••••"
              disabled
              type="password"
              icon="i-lucide-file-lock-2"
              :title="`Decrypted from ${sopsSourceFile || 'secrets.yaml'} via SOPS`"
              class="font-mono"
            />
            <div class="grid grid-cols-3 gap-2 sm:contents">
              <div class="flex h-8 w-full items-center justify-center rounded-md bg-amber-400/15 text-amber-500 sm:w-8 dark:bg-amber-400/10 dark:text-amber-400">
                <UIcon name="i-lucide-file-lock-2" class="h-4 w-4" :title="`Managed by SOPS (${sopsSourceFile || 'secrets.yaml'}) — immutable here`" />
              </div>
              <UBadge label="SOPS" color="warning" variant="subtle" size="sm" class="col-span-2 justify-center sm:col-span-2" />
            </div>
          </div>
        </div>
      </div>
      <p v-else class="py-2 text-sm text-gray-500">No environment variables defined</p>
    </div>

    <UModal v-model:open="showCreateModal" :ui="{ content: 'w-full sm:max-w-md' }">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-variable" class="h-5 w-5 text-yellow-400" />
              <h3 class="font-semibold text-gray-900 dark:text-white">Create Environment Variable</h3>
            </div>
          </template>

          <form class="space-y-4" @submit.prevent="addEnvVar">
            <UFormField label="Key" required>
              <AppTextInput v-model="newEnvKey" placeholder="KEY" class="font-mono" />
            </UFormField>

            <UFormField v-if="newEnvSecret && hasActiveBackends" label="Provider">
              <AppSelectInput v-model="newEnvProvider" :items="providerOptions" :searchable="false" class="w-full" />
            </UFormField>

            <UFormField label="Value">
              <IntegrationsVaultReferencePicker v-if="newEnvSecret && newEnvProvider === 'vault'" v-model="newEnvValue" />
              <IntegrationsInfisicalReferencePicker v-else-if="newEnvSecret && newEnvProvider === 'infisical'" v-model="newEnvValue" />
              <AppTextInput
                v-else
                v-model="newEnvValue"
                placeholder="value"
                :type="newEnvSecret ? 'password' : 'text'"
                :icon="newEnvSecret ? iconFor(newEnvProvider) : undefined"
                :avatar="newEnvSecret ? avatarFor(newEnvProvider) : undefined"
                class="font-mono"
              />
            </UFormField>

            <UButton
              type="button"
              :icon="newEnvSecret ? 'i-lucide-lock' : 'i-lucide-variable'"
              :color="newEnvSecret ? 'warning' : 'neutral'"
              variant="soft"
              class="w-full justify-center"
              :aria-pressed="newEnvSecret"
              :aria-label="newEnvSecret ? 'Set as plain text' : 'Set as secret'"
              @click="newEnvSecret = !newEnvSecret"
            >
              {{ newEnvSecret ? 'Secret' : 'Plain text' }}
            </UButton>

            <div class="grid grid-cols-2 gap-2 pt-2">
              <UButton type="button" label="Cancel" variant="outline" color="neutral" block @click="cancelCreateEnv" />
              <UButton type="submit" label="Create" icon="i-lucide-check" color="success" block :loading="saving" :disabled="!newEnvKey.trim()" />
            </div>
          </form>
        </UCard>
      </template>
    </UModal>

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
