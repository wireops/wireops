<script setup lang="ts">
const { $pb } = useNuxtApp()
const { canManageRepos } = usePermissions()
const { subscribe } = useRealtime()
const toast = useToast()

const search = ref('')
const showModal = ref(false)
const selectedKey = ref<Record<string, any> | undefined>()
const showDelete = ref(false)
const deleting = ref(false)
const deleteKey = ref<Record<string, any> | undefined>()
const searchInput = ref<any>()

const { data, refresh } = useAsyncData('repository_keys_panel', async () => {
  const [keys, repositories] = await Promise.all([
    $pb.collection('repository_keys').getFullList({ sort: 'name' }),
    $pb.collection('repositories').getFullList({ fields: 'id,repository_key' }),
  ])
  return { keys, repositories }
})

onMounted(() => {
  refresh()
  subscribe('repositories', () => refresh())
  subscribe('repository_keys', () => refresh())
})

const usage = computed(() => {
  const counts: Record<string, number> = {}
  for (const repository of data.value?.repositories || []) {
    const keyID = repository.repository_key
    if (!keyID) continue
    counts[keyID] = (counts[keyID] || 0) + 1
  }
  return counts
})

const filteredKeys = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return data.value?.keys || []
  return (data.value?.keys || []).filter(key =>
    key.name?.toLowerCase().includes(query) ||
    key.git_username?.toLowerCase().includes(query)
  )
})

function addKey() {
  selectedKey.value = undefined
  showModal.value = true
}

function focusSearch() {
  nextTick(() => {
    const input = searchInput.value?.$el?.querySelector?.('input')
    input?.focus()
  })
}

function clearSearch() {
  search.value = ''
}

function editKey(key: Record<string, any>) {
  selectedKey.value = key
  showModal.value = true
}

function requestDelete(key: Record<string, any>) {
  deleteKey.value = key
  showDelete.value = true
}

async function confirmDelete() {
  if (!deleteKey.value) return
  deleting.value = true
  try {
    await $pb.collection('repository_keys').delete(deleteKey.value.id)
    toast.add({ title: 'Key deleted', color: 'success' })
    showDelete.value = false
    await refresh()
  } catch (error: any) {
    toast.add({
      title: 'Failed to delete key',
      description: error?.response?.message || error?.message,
      color: 'error',
    })
  } finally {
    deleting.value = false
  }
}

defineExpose({
  addKey,
  clearSearch,
  focusSearch,
  refresh,
})
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex items-center justify-between gap-3">
        <div>
          <h3 class="font-semibold">Repository Keys</h3>
          <p class="text-xs text-gray-500 mt-0.5">Reusable SSH keys and username/password credentials.</p>
        </div>
        <UButton v-if="canManageRepos" label="Add Key" icon="i-lucide-plus" @click="addKey" />
      </div>
    </template>

    <div v-if="data?.keys.length" class="space-y-4">
      <UInput ref="searchInput" v-model="search" icon="i-lucide-search" placeholder="Search keys..." class="w-full sm:max-w-sm" />

      <div v-if="filteredKeys.length" class="space-y-3">
        <div
          v-for="key in filteredKeys"
          :key="key.id"
          class="flex items-center gap-4 p-4 rounded-xl border border-gray-200 dark:border-carbon-700 bg-gray-50 dark:bg-carbon-800/40"
        >
          <div class="w-10 h-10 rounded-lg bg-yellow-400/10 flex items-center justify-center shrink-0">
            <UIcon :name="key.auth_type === 'ssh_key' ? 'i-lucide-key-round' : 'i-lucide-user-key'" class="w-5 h-5 text-yellow-400" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <h4 class="font-semibold truncate">{{ key.name }}</h4>
              <UBadge color="neutral" variant="soft">
                {{ key.auth_type === 'ssh_key' ? 'SSH' : 'Username / Password' }}
              </UBadge>
            </div>
            <p class="text-sm text-gray-500 truncate">
              <template v-if="key.auth_type === 'basic'">{{ key.git_username }}</template>
              <template v-else>Private key</template>
              · {{ usage[key.id] || 0 }} repositories
            </p>
          </div>
          <div v-if="canManageRepos" class="flex items-center gap-1">
            <UButton icon="i-lucide-pencil" variant="ghost" color="neutral" aria-label="Edit key" @click="editKey(key)" />
            <UTooltip :text="usage[key.id] ? 'Remove this key from its repositories before deleting it' : 'Delete key'">
              <UButton
                icon="i-lucide-trash-2"
                variant="ghost"
                color="error"
                aria-label="Delete key"
                :disabled="!!usage[key.id]"
                @click="requestDelete(key)"
              />
            </UTooltip>
          </div>
        </div>
      </div>
      <p v-else class="text-sm text-gray-500 text-center py-8">No keys match your search.</p>
    </div>

    <div v-else class="text-center py-12">
      <UIcon name="i-lucide-key-round" class="w-10 h-10 text-gray-300 mx-auto mb-3" />
      <h3 class="font-medium">No repository keys yet</h3>
      <p class="text-sm text-gray-500 mt-1">Create one key and reuse it across multiple repositories.</p>
    </div>
  </UCard>

  <RepositoryKeyModal
    v-model:open="showModal"
    :repository-key="selectedKey"
    @saved="() => refresh()"
  />
  <ConfirmModal
    v-model:open="showDelete"
    title="Delete Repository Key"
    :description="`Delete ${deleteKey?.name || 'this key'}? This cannot be undone.`"
    confirm-label="Delete"
    confirm-color="error"
    :loading="deleting"
    @confirm="confirmDelete"
  />
</template>
