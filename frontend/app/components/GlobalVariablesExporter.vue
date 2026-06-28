<script setup lang="ts">
type TargetType = 'stack' | 'job'

const props = defineProps<{
  targetType: TargetType
  targetId: string
  localKeys?: string[]
}>()

const emit = defineEmits<{
  changed: []
}>()

const { $pb } = useNuxtApp()
const { canOperate } = usePermissions()
const { subscribe } = useRealtime()
const toast = useToast()

const globals = ref<any[]>([])
const bindings = ref<any[]>([])
const loading = ref(false)
const savingId = ref('')
const showAddModal = ref(false)
const addSearch = ref('')
const variableToDetach = ref<any | null>(null)

const bindingCollection = computed(() => props.targetType === 'stack' ? 'stack_global_env_vars' : 'job_global_env_vars')
const targetField = computed(() => props.targetType === 'stack' ? 'stack' : 'job')
const localKeySet = computed(() => new Set((props.localKeys || []).filter(Boolean)))

const bindingByGlobalId = computed(() => {
  const map: Record<string, any> = {}
  for (const binding of bindings.value) {
    if (binding.global_env_var) map[binding.global_env_var] = binding
  }
  return map
})

const selectedCount = computed(() => Object.keys(bindingByGlobalId.value).length)
const attachedVariables = computed(() => globals.value.filter(variable => bindingByGlobalId.value[variable.id]))
const availableVariables = computed(() => globals.value.filter(variable => !bindingByGlobalId.value[variable.id]))
const filteredAvailableVariables = computed(() => {
  const query = addSearch.value.trim().toLowerCase()
  if (!query) return availableVariables.value
  return availableVariables.value.filter(variable =>
    variable.key?.toLowerCase().includes(query) ||
    variable.description?.toLowerCase().includes(query)
  )
})

async function load() {
  if (!props.targetId) return
  loading.value = true
  try {
    const [globalRecords, bindingRecords] = await Promise.all([
      $pb.collection('global_env_vars').getFullList({ sort: 'key', requestKey: null }),
      $pb.collection(bindingCollection.value).getFullList({
        filter: `${targetField.value} = "${props.targetId}"`,
        sort: 'created',
        requestKey: null,
      }),
    ])
    globals.value = globalRecords
    bindings.value = bindingRecords
  } catch (error: any) {
    toast.add({ title: 'Failed to load global variables', description: error?.message, color: 'error' })
  } finally {
    loading.value = false
  }
}

async function attachGlobal(variable: any) {
  if (!canOperate.value) return
  savingId.value = variable.id
  try {
    await $pb.collection(bindingCollection.value).create({
      [targetField.value]: props.targetId,
      global_env_var: variable.id,
    }, {
      requestKey: null,
    })
    await load()
    showAddModal.value = false
    addSearch.value = ''
    emit('changed')
    toast.add({ title: 'Global variable attached', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to attach global variable', description: error?.message, color: 'error' })
  } finally {
    savingId.value = ''
  }
}

function openDetachModal(variable: any) {
  if (!canOperate.value) return
  variableToDetach.value = variable
}

function cancelDetach() {
  variableToDetach.value = null
}

async function confirmDetachGlobal() {
  const variable = variableToDetach.value
  if (!variable) return
  if (!canOperate.value) return
  const existing = bindingByGlobalId.value[variable.id]
  if (!existing) return
  savingId.value = variable.id
  try {
    await $pb.collection(bindingCollection.value).delete(existing.id, { requestKey: null })
    variableToDetach.value = null
    await load()
    emit('changed')
    toast.add({ title: 'Global variable detached', color: 'success' })
  } catch (error: any) {
    toast.add({ title: 'Failed to detach global variable', description: error?.message, color: 'error' })
  } finally {
    savingId.value = ''
  }
}

onMounted(() => {
  load()
  subscribe('global_env_vars', () => load())
  subscribe(bindingCollection.value, (event: any) => {
    if (event.record?.[targetField.value] === props.targetId) load()
  })
})
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center justify-between gap-3">
        <div>
          <h3 class="font-semibold">Global Variables</h3>
          <p class="text-xs text-gray-500 mt-0.5">{{ selectedCount }} attached</p>
        </div>
        <div class="flex items-center gap-2">
          <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" size="xs" :loading="loading" @click="load" />
          <UButton to="/secrets" icon="i-lucide-key-round" label="Secrets" variant="outline" size="xs" />
          <UButton
            v-if="canOperate"
            icon="i-lucide-plus"
            label="Add Global Variable"
            size="xs"
            :disabled="availableVariables.length === 0"
            @click="showAddModal = true"
          />
        </div>
      </div>
    </template>

    <div v-if="attachedVariables.length" class="divide-y divide-gray-200 dark:divide-carbon-800">
      <div v-for="variable in attachedVariables" :key="variable.id" class="py-2">
        <div class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem_2rem_2rem] sm:items-center">
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
          <UButton
            icon="i-lucide-unlink"
            variant="ghost"
            color="warning"
            size="xs"
            class="h-8 w-8 justify-center p-0"
            :disabled="!canOperate"
            :loading="savingId === variable.id"
            :aria-label="`Detach ${variable.key}`"
            @click="openDetachModal(variable)"
          />
          <div class="h-8 w-8" />
        </div>
      </div>
    </div>
    <div v-else class="py-6 text-center">
      <UIcon name="i-lucide-key-round" class="mx-auto h-6 w-6 text-gray-400" />
      <p class="mt-2 text-sm text-gray-500">No global variables attached</p>
    </div>

    <UModal v-model:open="showAddModal">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center justify-between gap-3">
              <div>
                <h3 class="font-semibold">Add Global Variable</h3>
                <p class="text-xs text-gray-500 mt-0.5">Select a global variable to attach.</p>
              </div>
              <UButton icon="i-lucide-x" variant="ghost" color="neutral" size="xs" @click="showAddModal = false" />
            </div>
          </template>

          <div class="w-full space-y-4">
            <UInput v-model="addSearch" icon="i-lucide-search" placeholder="Search global variables..." class="w-full" />

            <div v-if="filteredAvailableVariables.length" class="max-h-80 divide-y divide-gray-200 overflow-y-auto dark:divide-carbon-800">
              <div v-for="variable in filteredAvailableVariables" :key="variable.id" class="grid grid-cols-1 gap-2 py-3 sm:grid-cols-[minmax(0,1fr)_2rem_auto] sm:items-center">
                <UInput :model-value="variable.key" disabled class="font-mono opacity-60" />
                <div class="flex h-8 w-8 items-center justify-center">
                  <UIcon
                    :name="variable.secret ? 'i-lucide-lock' : 'i-lucide-variable'"
                    class="h-4 w-4"
                    :class="variable.secret ? 'text-amber-400' : 'text-gray-400'"
                    :title="variable.secret ? 'Secret' : 'Plain text'"
                  />
                </div>
                <UButton
                  icon="i-lucide-link"
                  label="Attach"
                  size="xs"
                  :loading="savingId === variable.id"
                  @click="attachGlobal(variable)"
                />
              </div>
            </div>
            <p v-else class="py-6 text-center text-sm text-gray-500">No available global variables</p>
          </div>
        </UCard>
      </template>
    </UModal>

    <UModal :open="!!variableToDetach" @update:open="value => { if (!value) cancelDetach() }">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2 text-amber-600">
              <UIcon name="i-lucide-unlink" class="h-5 w-5" />
              <h3 class="font-semibold text-gray-900 dark:text-white">Detach Global Variable</h3>
            </div>
          </template>

          <p class="text-sm text-gray-500">
            Detach <span class="font-mono font-semibold text-gray-900 dark:text-gray-100">{{ variableToDetach?.key }}</span> from this {{ props.targetType }}?
          </p>

          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton label="Cancel" variant="outline" color="neutral" @click="cancelDetach" />
              <UButton
                label="Detach"
                icon="i-lucide-unlink"
                color="warning"
                :loading="savingId === variableToDetach?.id"
                @click="confirmDetachGlobal"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>
  </UCard>
</template>
