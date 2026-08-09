<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'

const { $pb } = useNuxtApp()
const { canManageRepos } = usePermissions()
const { subscribe } = useRealtime()
const toast = useToast()

const search = ref('')
const showModal = ref(false)
const selectedCredential = ref<Record<string, any> | undefined>()
const showDelete = ref(false)
const deleting = ref(false)
const deleteCredential = ref<Record<string, any> | undefined>()
const searchInput = ref<any>()

const { data, refresh } = useAsyncData('registry_credentials_panel', async () => {
  const [credentials, stacks] = await Promise.all([
    $pb.collection('registry_credentials').getFullList({ sort: 'name' }),
    $pb.collection('stacks').getFullList({ fields: 'id,registry_credential' }),
  ])
  return { credentials, stacks }
})

onMounted(() => {
  refresh()
  subscribe('stacks', () => refresh())
  subscribe('registry_credentials', () => refresh())
})

const usage = computed(() => {
  const counts: Record<string, number> = {}
  for (const stack of data.value?.stacks || []) {
    const credentialID = stack.registry_credential
    if (!credentialID) continue
    counts[credentialID] = (counts[credentialID] || 0) + 1
  }
  return counts
})

const filteredCredentials = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return data.value?.credentials || []
  return (data.value?.credentials || []).filter(cred =>
    cred.name?.toLowerCase().includes(query) ||
    cred.registry_url?.toLowerCase().includes(query)
  )
})

const AUTH_TYPE_LABEL: Record<string, string> = {
  basic: 'Username / Password',
  token: 'Token',
  gcp_service_account: 'GCP Service Account',
}

function authTypeLabel(credential: Record<string, any>): string {
  return AUTH_TYPE_LABEL[credential.auth_type] ?? credential.auth_type
}

function addCredential() {
  selectedCredential.value = undefined
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

function editCredential(credential: Record<string, any>) {
  selectedCredential.value = credential
  showModal.value = true
}

function requestDelete(credential: Record<string, any>) {
  deleteCredential.value = credential
  showDelete.value = true
}

async function confirmDelete() {
  if (!deleteCredential.value) return
  deleting.value = true
  try {
    await $pb.collection('registry_credentials').delete(deleteCredential.value.id)
    toast.add({ title: 'Credential deleted', color: 'success' })
    showDelete.value = false
    await refresh()
  } catch (error: any) {
    toast.add({
      title: 'Failed to delete credential',
      description: error?.response?.message || error?.message,
      color: 'error',
    })
  } finally {
    deleting.value = false
  }
}

defineExpose({
  addCredential,
  clearSearch,
  focusSearch,
  refresh,
})
</script>

<template>
  <AppPanelCard>
    <template #header>
      <div class="flex items-center justify-between gap-3">
        <div>
          <h3 class="font-semibold">Registry Credentials</h3>
          <p class="text-xs text-gray-500 mt-0.5">Reusable container registry credentials workers use to authenticate image pulls.</p>
        </div>
        <UButton v-if="canManageRepos" label="Add Credential" icon="i-lucide-plus" @click="addCredential" />
      </div>
    </template>

    <div v-if="data?.credentials.length" class="space-y-4">
      <AppTextInput ref="searchInput" v-model="search" icon="i-lucide-search" placeholder="Search credentials..." aria-label="Search credentials" class="w-full" />

      <div v-if="filteredCredentials.length" class="space-y-3">
        <div
          v-for="credential in filteredCredentials"
          :key="credential.id"
          class="flex items-center gap-4 p-4 rounded-xl border border-gray-300 dark:border-carbon-700 bg-gray-50 dark:bg-carbon-800/40"
        >
          <div class="w-10 h-10 rounded-lg flex items-center justify-center shrink-0 bg-yellow-400/10">
            <UIcon name="i-lucide-container" class="w-5 h-5 text-yellow-400" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <h4 class="font-semibold truncate">{{ credential.name }}</h4>
              <UBadge color="neutral" variant="soft">{{ authTypeLabel(credential) }}</UBadge>
              <UBadge v-if="credential.insecure" color="warning" variant="soft">Insecure</UBadge>
            </div>
            <p class="text-sm text-gray-500 truncate">
              {{ credential.registry_url }} · {{ usage[credential.id] || 0 }} stacks
            </p>
          </div>
          <div v-if="canManageRepos" class="flex items-center gap-1">
            <UButton
              icon="i-lucide-pencil"
              variant="ghost"
              color="neutral"
              aria-label="Edit credential"
              @click="editCredential(credential)"
            />
            <UTooltip :text="usage[credential.id] ? 'Remove this credential from its stacks before deleting it' : 'Delete credential'">
              <UButton
                :icon="usage[credential.id] ? 'i-lucide-link' : 'i-lucide-trash-2'"
                variant="ghost"
                :color="usage[credential.id] ? 'neutral' : 'error'"
                aria-label="Delete credential"
                :title="usage[credential.id] ? 'Remove this credential from its stacks before deleting it' : 'Delete credential'"
                :disabled="!!usage[credential.id]"
                @click="requestDelete(credential)"
              />
            </UTooltip>
          </div>
        </div>
      </div>
      <p v-else class="text-sm text-gray-500 text-center py-8">No credentials match your search.</p>
    </div>

    <div v-else class="text-center py-12">
      <UIcon name="i-lucide-container" class="w-10 h-10 text-gray-300 mx-auto mb-3" />
      <h3 class="font-medium">No registry credentials yet</h3>
      <p class="text-sm text-gray-500 mt-1">Create one and assign it to a stack so its worker can pull private images.</p>
    </div>
  </AppPanelCard>

  <RegistryCredentialModal
    v-model:open="showModal"
    :credential="selectedCredential"
    @saved="() => refresh()"
  />
  <ConfirmModal
    v-model:open="showDelete"
    title="Delete Registry Credential"
    :description="`Delete ${deleteCredential?.name || 'this credential'}? This cannot be undone.`"
    confirm-label="Delete"
    confirm-color="error"
    :loading="deleting"
    @confirm="confirmDelete"
  />
</template>
