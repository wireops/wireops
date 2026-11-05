<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import EnvValueInput from './EnvValueInput.vue'
import { ROW_ACTION_OUTLINE } from '../utils/rowActionStyle'

type TargetType = 'stack' | 'job'

const props = defineProps<{
  open: boolean
  targetType: TargetType
  targetId: string
  globals: any[]
  bindings: any[]
  localKeys?: string[]
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  changed: []
}>()

const { $pb } = useNuxtApp()
const { canOperate } = usePermissions()
const toast = useToast()
const { load: loadProviderOptions, providerOptions, hasActiveBackends, iconFor, avatarFor } = useSecretProviderOptions()

const search = ref('')
const savingId = ref('')
// Attach/detach always go through a confirm step; this holds the pending one.
const pending = ref<{ action: 'attach' | 'detach', variable: any } | null>(null)
const showCreate = ref(false)
const creating = ref(false)
const newKey = ref('')
const newValue = ref('')
const newSecret = ref(true)
const newProvider = ref('internal')

const bindingCollection = computed(() => props.targetType === 'stack' ? 'stack_global_env_vars' : 'job_global_env_vars')
const targetField = computed(() => props.targetType === 'stack' ? 'stack' : 'job')
const localKeySet = computed(() => new Set((props.localKeys || []).filter(Boolean)))

const bindingByGlobalId = computed(() => {
  const map: Record<string, any> = {}
  for (const binding of props.bindings) {
    if (binding.global_env_var) map[binding.global_env_var] = binding
  }
  return map
})

const attachedCount = computed(() => Object.keys(bindingByGlobalId.value).length)

// Attached variables first, then alphabetical — keeps what's already linked
// at the top while the full catalog stays reachable from the same list.
const visibleVariables = computed(() => {
  const query = search.value.trim().toLowerCase()
  const matches = query
    ? props.globals.filter(variable => variable.key?.toLowerCase().includes(query))
    : [...props.globals]
  return matches.sort((a, b) => {
    const aAttached = bindingByGlobalId.value[a.id] ? 0 : 1
    const bAttached = bindingByGlobalId.value[b.id] ? 0 : 1
    if (aAttached !== bAttached) return aAttached - bAttached
    return String(a.key).localeCompare(String(b.key))
  })
})

function resetCreateForm() {
  newKey.value = ''
  newValue.value = ''
  newSecret.value = true
  newProvider.value = 'internal'
}

function close() {
  emit('update:open', false)
}

watch(() => props.open, (open) => {
  if (open) {
    loadProviderOptions()
    return
  }
  search.value = ''
  pending.value = null
  showCreate.value = false
  resetCreateForm()
})

function requestAction(action: 'attach' | 'detach', variable: any) {
  if (!canOperate.value || savingId.value) return
  pending.value = { action, variable }
}

function cancelPending() {
  if (savingId.value) return
  pending.value = null
}

async function confirmPending() {
  const current = pending.value
  if (!current) return
  if (current.action === 'attach') await attach(current.variable)
  else await detach(current.variable)
  pending.value = null
}

async function attach(variable: any) {
  if (!canOperate.value || savingId.value) return
  savingId.value = variable.id
  try {
    await $pb.collection(bindingCollection.value).create({
      [targetField.value]: props.targetId,
      global_env_var: variable.id,
    }, { requestKey: null })
    emit('changed')
    toast.add({ title: `${variable.key} attached`, color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to attach global variable', description: error?.message, color: 'error' })
  } finally {
    savingId.value = ''
  }
}

async function detach(variable: any) {
  if (!canOperate.value || savingId.value) return
  const existing = bindingByGlobalId.value[variable.id]
  if (!existing) return
  savingId.value = variable.id
  try {
    await $pb.collection(bindingCollection.value).delete(existing.id, { requestKey: null })
    emit('changed')
    toast.add({ title: `${variable.key} detached`, color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to detach global variable', description: error?.message, color: 'error' })
  } finally {
    savingId.value = ''
  }
}

async function createAndAttach() {
  const key = newKey.value.trim()
  if (!key || !canOperate.value || creating.value) return
  creating.value = true
  let created: any
  try {
    created = await $pb.collection('global_env_vars').create({
      key,
      value: newValue.value,
      secret: newSecret.value,
      secret_provider: newSecret.value ? newProvider.value : '',
    }, { requestKey: null })
  } catch (error: any) {
    toast.add({ title: 'Failed to create global variable', description: error?.message, color: 'error' })
    creating.value = false
    return
  }

  try {
    await $pb.collection(bindingCollection.value).create({
      [targetField.value]: props.targetId,
      global_env_var: created.id,
    }, { requestKey: null })
    toast.add({ title: `${key} created and attached`, color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Created, but failed to attach', description: error?.message, color: 'warning' })
  } finally {
    resetCreateForm()
    showCreate.value = false
    creating.value = false
    emit('changed')
  }
}
</script>

<template>
  <UModal :open="open" :ui="{ content: 'w-full sm:max-w-lg' }" @update:open="value => emit('update:open', value)">
    <template #content>
      <AppPanelCard>
        <template #header>
          <div class="flex items-center justify-between gap-3">
            <div>
              <h3 class="font-semibold">Global Variables</h3>
              <p class="text-xs text-gray-500 mt-0.5">{{ attachedCount }} attached · changes apply immediately.</p>
            </div>
            <CloseButton size="xs" @click="close" />
          </div>
        </template>

        <div class="w-full space-y-4">
          <AppTextInput v-model="search" icon="i-lucide-search" placeholder="Search global variables..." />

          <div v-if="visibleVariables.length" class="max-h-80 divide-y divide-gray-200 overflow-y-auto dark:divide-carbon-800">
            <div
              v-for="variable in visibleVariables"
              :key="variable.id"
              class="grid grid-cols-1 gap-2 py-3 sm:grid-cols-[minmax(0,1fr)_2rem_6rem] sm:items-center"
            >
              <div class="min-w-0">
                <AppTextInput :model-value="variable.key" disabled class="font-mono" />
                <p v-if="bindingByGlobalId[variable.id] && localKeySet.has(variable.key)" class="mt-1 text-xs text-amber-600 dark:text-amber-400">
                  Overridden by a local variable with the same key
                </p>
              </div>
              <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-2 sm:contents">
                <div
                  class="flex h-8 w-full items-center justify-center rounded-md bg-gray-100 text-gray-500 sm:w-8 sm:bg-transparent dark:bg-carbon-800 dark:text-gray-400 sm:dark:bg-transparent"
                  :class="[ROW_ACTION_OUTLINE, variable.secret ? 'bg-amber-400/15 text-amber-500 dark:bg-amber-400/10 dark:text-amber-400' : '']"
                >
                  <UIcon
                    :name="variable.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
                    class="h-4 w-4"
                    :title="variable.secret ? 'Secret' : 'Plain text'"
                  />
                </div>
                <UButton
                  v-if="bindingByGlobalId[variable.id]"
                  icon="i-lucide-unlink"
                  label="Detach"
                  size="xs"
                  variant="ghost"
                  color="warning"
                  class="h-8 w-full justify-center !bg-amber-500/10 !text-amber-600 hover:!bg-amber-500/15 sm:!bg-transparent sm:hover:!bg-amber-500/10 dark:!text-amber-400"
                  :class="ROW_ACTION_OUTLINE"
                  :loading="savingId === variable.id"
                  :disabled="!canOperate || (!!savingId && savingId !== variable.id)"
                  :aria-label="`Detach ${variable.key}`"
                  @click="requestAction('detach', variable)"
                />
                <UButton
                  v-else
                  icon="i-lucide-link"
                  label="Attach"
                  size="xs"
                  variant="ghost"
                  color="success"
                  class="h-8 w-full justify-center !bg-green-500/10 !text-green-600 hover:!bg-green-500/15 sm:!bg-transparent sm:hover:!bg-green-500/10 dark:!text-green-400"
                  :class="ROW_ACTION_OUTLINE"
                  :loading="savingId === variable.id"
                  :disabled="!canOperate || (!!savingId && savingId !== variable.id)"
                  :aria-label="`Attach ${variable.key}`"
                  @click="requestAction('attach', variable)"
                />
              </div>
            </div>
          </div>
          <p v-else class="py-6 text-center text-sm text-gray-500">
            {{ globals.length ? 'No variables match your search' : 'No global variables yet' }}
          </p>

          <div v-if="canOperate" class="border-t border-gray-200 pt-4 dark:border-carbon-800">
            <UButton
              v-if="!showCreate"
              icon="i-lucide-plus"
              label="New global variable"
              size="xs"
              variant="outline"
              color="neutral"
              :disabled="!!savingId"
              @click="showCreate = true"
            />
            <form v-else class="space-y-3" @submit.prevent="createAndAttach">
              <UFormField label="Key" required>
                <AppTextInput v-model="newKey" placeholder="KEY" class="font-mono" />
              </UFormField>

              <UFormField v-if="newSecret && hasActiveBackends" label="Provider">
                <AppSelectInput v-model="newProvider" :items="providerOptions" :searchable="false" class="w-full" />
              </UFormField>

              <UFormField label="Value">
                <IntegrationsVaultReferencePicker v-if="newSecret && newProvider === 'vault'" v-model="newValue" />
                <IntegrationsInfisicalReferencePicker v-else-if="newSecret && newProvider === 'infisical'" v-model="newValue" />
                <EnvValueInput
                  v-else
                  v-model="newValue"
                  placeholder="value"
                  :type="newSecret ? 'password' : 'text'"
                  :icon="newSecret ? iconFor(newProvider) : undefined"
                  :avatar="newSecret ? avatarFor(newProvider) : undefined"
                  class="font-mono"
                />
              </UFormField>

              <UButton
                type="button"
                :icon="newSecret ? 'i-lucide-lock' : 'i-lucide-variable'"
                :color="newSecret ? 'warning' : 'neutral'"
                variant="soft"
                size="xs"
                class="w-full justify-center"
                :aria-pressed="newSecret"
                :aria-label="newSecret ? 'Set as plain text' : 'Set as secret'"
                @click="newSecret = !newSecret"
              >
                {{ newSecret ? 'Secret' : 'Plain text' }}
              </UButton>

              <div class="grid grid-cols-2 gap-2">
                <CancelButton type="button" block @click="showCreate = false; resetCreateForm()" />
                <UButton
                  type="submit"
                  label="Create & attach"
                  icon="i-lucide-check"
                  color="success"
                  block
                  :loading="creating"
                  :disabled="!newKey.trim()"
                />
              </div>
            </form>
          </div>
        </div>

        <UModal :open="!!pending" @update:open="value => { if (!value) cancelPending() }">
          <template #content>
            <AppPanelCard>
              <template #header>
                <div class="flex items-center gap-2" :class="pending?.action === 'detach' ? 'text-amber-600' : 'text-green-600'">
                  <UIcon :name="pending?.action === 'detach' ? 'i-lucide-unlink' : 'i-lucide-link'" class="h-5 w-5" />
                  <h3 class="font-semibold text-gray-900 dark:text-white">
                    {{ pending?.action === 'detach' ? 'Detach Global Variable' : 'Attach Global Variable' }}
                  </h3>
                </div>
              </template>

              <p class="text-sm text-gray-500">
                {{ pending?.action === 'detach' ? 'Detach' : 'Attach' }}
                <span class="font-mono font-semibold text-gray-900 dark:text-gray-100">{{ pending?.variable.key }}</span>
                {{ pending?.action === 'detach' ? 'from' : 'to' }} this {{ targetType }}?
              </p>
              <p
                v-if="pending?.action === 'attach' && localKeySet.has(pending.variable.key)"
                class="mt-2 text-xs text-amber-600 dark:text-amber-400"
              >
                A local variable with the same key exists and will override it.
              </p>

              <template #footer>
                <div class="flex justify-end gap-2">
                  <CancelButton @click="cancelPending" />
                  <UButton
                    v-if="pending?.action === 'detach'"
                    label="Detach"
                    icon="i-lucide-unlink"
                    color="warning"
                    :loading="!!savingId"
                    :aria-label="`Confirm detach ${pending.variable.key}`"
                    @click="confirmPending"
                  />
                  <UButton
                    v-else
                    label="Attach"
                    icon="i-lucide-link"
                    color="success"
                    :loading="!!savingId"
                    :aria-label="`Confirm attach ${pending?.variable.key}`"
                    @click="confirmPending"
                  />
                </div>
              </template>
            </AppPanelCard>
          </template>
        </UModal>

        <template #footer>
          <div class="flex justify-end">
            <UButton label="Done" color="neutral" variant="outline" @click="close" />
          </div>
        </template>
      </AppPanelCard>
    </template>
  </UModal>
</template>
