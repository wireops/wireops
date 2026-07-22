<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'

const { $pb } = useNuxtApp()
const toast = useToast()
const { isAdmin } = usePermissions()

const route = useRoute()
const router = useRouter()

function getValidTab(tab: string | null | undefined): string {
  const allowed = ['users']
  if (isAdmin.value) {
    allowed.push('service-accounts')
  }
  if (tab && allowed.includes(tab)) {
    return tab
  }
  return 'users'
}

const activeTab = ref(getValidTab(route.query.tab as string))

const tabs = computed(() => {
  const list = [
    { label: 'Users', value: 'users', icon: 'i-lucide-users' }
  ]
  if (isAdmin.value) {
    list.push({ label: 'Service Accounts', value: 'service-accounts', icon: 'i-lucide-key-round' })
  }
  return list
})

watch(activeTab, (newVal) => {
  if (route.query.tab !== newVal) {
    router.replace({ query: { ...route.query, tab: newVal } })
  }
})

watch(() => route.query.tab, (newVal) => {
  const valid = getValidTab(newVal as string)
  if (activeTab.value !== valid) {
    activeTab.value = valid
  }
})

watch(isAdmin, () => {
  const valid = getValidTab(activeTab.value)
  if (activeTab.value !== valid) {
    activeTab.value = valid
  }
})

const roleOptions = [
  { label: 'Viewer', value: 'viewer' },
  { label: 'Operator', value: 'operator' },
  { label: 'Admin', value: 'admin' },
]

const users = ref<any[]>([])
const usersLoading = ref(false)
const inviteEmail = ref('')
const inviteRole = ref('viewer')
const inviteLoading = ref(false)

async function loadUsers() {
  usersLoading.value = true
  try {
    users.value = await $pb.collection('users').getFullList({ sort: 'created' })
  } catch (e: any) {
    toast.add({ title: 'Failed to load users', description: e?.message, color: 'error' })
  } finally {
    usersLoading.value = false
  }
}

async function sendInvite() {
  if (!inviteEmail.value) return
  inviteLoading.value = true
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/invite`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${$pb.authStore.token}`,
        'X-Wireops-Origin': 'ui',
      },
      body: JSON.stringify({ email: inviteEmail.value, role: inviteRole.value }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error)
    inviteEmail.value = ''
    inviteRole.value = 'viewer'
    toast.add({ title: 'Invitation sent', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to send invite', description: e?.message, color: 'error' })
  } finally {
    inviteLoading.value = false
  }
}

async function toggleUserDisabled(user: any) {
  const action = user.disabled ? 'enable' : 'disable'
  if (!window.confirm(`Are you sure you want to ${action} user ${user.email}?`)) {
    return
  }
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/${user.id}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${$pb.authStore.token}`,
        'X-Wireops-Origin': 'ui',
      },
      body: JSON.stringify({ disabled: !user.disabled }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error)
    user.disabled = !user.disabled
    toast.add({ title: user.disabled ? 'User disabled' : 'User enabled', color: 'success' })
  } catch (e: any) {
    toast.add({ title: `Failed to ${action} user`, description: e?.message, color: 'error' })
    await loadUsers()
  }
}

async function updateUserRole(user: any, role: string) {
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/${user.id}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${$pb.authStore.token}`,
        'X-Wireops-Origin': 'ui',
      },
      body: JSON.stringify({ role }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error)
    user.role = role
    toast.add({ title: 'Role updated', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to update role', description: e?.message, color: 'error' })
    await loadUsers()
  }
}

// --- Service Accounts & API Keys ---
const serviceAccounts = ref<any[]>([])
const serviceAccountsLoading = ref(false)
const showCreateServiceAccountModal = ref(false)
const createSAModalRef = ref<any>(null)
const createdApiKey = ref('')
const showApiKeyModal = ref(false)
const targetAccountName = ref('')
const saSearchQuery = ref('')
const showDisabledSAs = ref(false)
const usersSearchQuery = ref('')
const openApiKeys = ref<Record<string, boolean>>({})

function toggleApiKeyAccordion(saId: string) {
  openApiKeys.value[saId] = !openApiKeys.value[saId]
}

const filteredServiceAccounts = computed(() => {
  return serviceAccounts.value
    .filter((account) => {
      if (!showDisabledSAs.value && !account.enabled) {
        return false
      }
      const query = saSearchQuery.value.toLowerCase().trim()
      if (!query) return true
      return (
        account.name.toLowerCase().includes(query) ||
        (account.description && account.description.toLowerCase().includes(query))
      )
    })
    .sort((a, b) => a.name.localeCompare(b.name))
})

const filteredUsers = computed(() => {
  const query = usersSearchQuery.value.toLowerCase().trim()
  if (!query) return users.value
  return users.value.filter((u) => u.email.toLowerCase().includes(query))
})

async function apiFetch(path: string, options: RequestInit = {}) {
  const res = await fetch(`${$pb.baseURL}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${$pb.authStore.token}`,
      'X-Wireops-Origin': 'ui',
      ...(options.headers || {}),
    },
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error || 'request failed')
  return data
}

async function loadServiceAccounts() {
  if (!isAdmin.value) return
  serviceAccountsLoading.value = true
  try {
    serviceAccounts.value = await apiFetch('/api/custom/service-accounts')
  } catch (e: any) {
    toast.add({ title: 'Failed to load service accounts', description: e?.message, color: 'error' })
  } finally {
    serviceAccountsLoading.value = false
  }
}

async function createServiceAccount(payload: { name: string; description: string; role: string }) {
  try {
    const data = await apiFetch('/api/custom/service-accounts', {
      method: 'POST',
      body: JSON.stringify({ ...payload, enabled: true }),
    })
    showCreateServiceAccountModal.value = false
    createSAModalRef.value?.reset()
    createdApiKey.value = data.api_key
    targetAccountName.value = data.name
    showApiKeyModal.value = true
    await loadServiceAccounts()
    toast.add({ title: 'Service account created', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to create service account', description: e?.message, color: 'error' })
  }
}

async function issueApiKey(account: any) {
  try {
    const data = await apiFetch(`/api/custom/service-accounts/${account.id}/keys`, {
      method: 'POST',
      body: JSON.stringify({}),
    })
    createdApiKey.value = data.api_key
    targetAccountName.value = account.name
    showApiKeyModal.value = true
    await loadServiceAccounts()
    toast.add({ title: 'API key issued', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to issue API key', description: e?.message, color: 'error' })
  }
}

const showRevokeKeyModal = ref(false)
const saToRevokeKey = ref<any>(null)
const revokeKeyLoading = ref(false)

function confirmRevokeApiKey(account: any) {
  saToRevokeKey.value = account
  showRevokeKeyModal.value = true
}

async function executeRevokeApiKey() {
  if (!saToRevokeKey.value) return
  revokeKeyLoading.value = true
  const account = saToRevokeKey.value
  try {
    await apiFetch(`/api/custom/service-accounts/${account.id}/keys`, { method: 'DELETE' })
    await loadServiceAccounts()
    toast.add({ title: 'API key revoked', color: 'success' })
    showRevokeKeyModal.value = false
  } catch (e: any) {
    toast.add({ title: 'Failed to revoke API key', description: e?.message, color: 'error' })
  } finally {
    revokeKeyLoading.value = false
  }
}

const showDisableSAModal = ref(false)
const saToToggleEnabled = ref<any>(null)
const toggleEnabledSALoading = ref(false)

function confirmToggleSAEnabled(account: any) {
  saToToggleEnabled.value = account
  showDisableSAModal.value = true
}

async function executeToggleSAEnabled() {
  if (!saToToggleEnabled.value) return
  toggleEnabledSALoading.value = true
  const account = saToToggleEnabled.value
  const action = account.enabled ? 'disable' : 'enable'
  try {
    await apiFetch(`/api/custom/service-accounts/${account.id}`, {
      method: 'PUT',
      body: JSON.stringify({ enabled: !account.enabled }),
    })
    toast.add({ title: account.enabled ? 'Service account disabled' : 'Service account enabled', color: 'success' })
    showDisableSAModal.value = false
    if (account.enabled) {
      openApiKeys.value[account.id] = false
    }
    await loadServiceAccounts()
  } catch (e: any) {
    toast.add({ title: `Failed to ${action} service account`, description: e?.message, color: 'error' })
  } finally {
    toggleEnabledSALoading.value = false
  }
}

onMounted(() => {
  loadUsers()
  if (isAdmin.value) {
    loadServiceAccounts()
  }
})
</script>

<template>
  <div class="space-y-6">
    <UTabs v-model="activeTab" :items="tabs" />

    <!-- Users Tab -->
    <div v-if="activeTab === 'users'" class="space-y-6">
      <UCard v-if="isAdmin">
        <template #header>
          <h3 class="font-semibold">Invite User</h3>
          <p class="text-xs text-gray-500 mt-0.5">Send a magic-link invitation to a new administrator.</p>
        </template>
        <form class="flex flex-col gap-2 sm:flex-row" @submit.prevent="sendInvite">
          <UInput v-model="inviteEmail" type="email" placeholder="user@example.com" icon="i-lucide-mail" class="flex-1" required />
          <USelectMenu v-model="inviteRole" :items="roleOptions" value-key="value" class="w-full sm:w-40" />
          <UButton type="submit" label="Send Invite" icon="i-lucide-send" :loading="inviteLoading" />
        </form>
      </UCard>

      <UCard>
        <template #header><h3 class="font-semibold">Users</h3></template>
        <div class="space-y-4">
          <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-2 border-b border-gray-100 dark:border-gray-800">
            <UInput v-model="usersSearchQuery" placeholder="Search email..." icon="i-lucide-search" class="w-full" size="sm" />
          </div>
          <div v-if="usersLoading" class="text-sm text-gray-500">Loading...</div>
          <div v-else-if="filteredUsers.length === 0" class="text-sm text-gray-500">No users found.</div>
          <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
            <li v-for="u in filteredUsers" :key="u.id" class="flex items-center justify-between py-3 first:pt-0 last:pb-0">
            <div class="flex items-center gap-3">
              <div class="flex items-center justify-center w-8 h-8 rounded-full" :class="u.disabled ? 'bg-gray-400/10' : 'bg-yellow-400/10'">
                <UIcon name="i-lucide-user" class="w-4 h-4" :class="u.disabled ? 'text-gray-400' : 'text-yellow-400'" />
              </div>
              <div>
                <ULink
                  :to="`/settings/identity/${u.id}`"
                  active-class="text-primary"
                  inactive-class="text-sm font-medium text-gray-900 hover:text-yellow-500 dark:text-white dark:hover:text-yellow-400"
                  :class="{ 'opacity-50': u.disabled }"
                >
                  {{ u.email }}
                </ULink>
                <UBadge v-if="u.is_sso" label="SSO" color="primary" variant="subtle" size="xs" class="ml-2" />
                <p class="text-xs text-gray-500 mt-0.5">Joined {{ new Date(u.created).toLocaleDateString() }}</p>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <template v-if="u.protected">
                <span class="text-sm font-medium text-gray-500 w-36 text-right px-3">Admin</span>
                <UBadge label="Protected" color="warning" variant="subtle" size="xs" />
              </template>
              <template v-else>
                <USelectMenu
                  :model-value="u.role || 'viewer'"
                  :items="roleOptions"
                  value-key="value"
                  class="w-36"
                  :disabled="u.is_sso"
                  @update:model-value="updateUserRole(u, String($event))"
                />
                <UBadge v-if="u.disabled" label="Disabled" color="neutral" variant="subtle" size="xs" />
              </template>
              <UBadge v-if="u.id === $pb.authStore.record?.id" label="You" color="neutral" variant="subtle" size="xs" />
              <UButton
                v-if="!u.protected && u.id !== $pb.authStore.record?.id"
                :icon="u.disabled ? 'i-lucide-user-check' : 'i-lucide-user-x'"
                size="xs"
                variant="ghost"
                :color="u.disabled ? 'success' : 'warning'"
                :title="u.disabled ? 'Enable user' : 'Disable user'"
                @click="toggleUserDisabled(u)"
              />
            </div>
          </li>
          </ul>
        </div>
      </UCard>
    </div>

    <!-- Service Accounts Tab -->
    <div v-if="activeTab === 'service-accounts' && isAdmin" class="space-y-6">
      <UCard>
        <div class="flex justify-between items-center gap-4">
          <div>
            <h3 class="font-semibold text-gray-900 dark:text-white">Service Accounts</h3>
            <p class="text-xs text-gray-500 mt-0.5">Programmatic access for agents and external clients. API keys inherit the service account role.</p>
          </div>
          <UButton
            label="Create Service Account"
            icon="i-lucide-plus"
            color="primary"
            @click="showCreateServiceAccountModal = true"
          />
        </div>
      </UCard>

      <UCard>
        <div class="space-y-4">
          <!-- Search and Filter controls -->
          <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-2 border-b border-gray-100 dark:border-gray-800">
            <div class="flex items-center gap-3 flex-1">
              <UInput v-model="saSearchQuery" placeholder="Search name or description..." icon="i-lucide-search" class="flex-1" size="sm" />
              <div class="flex items-center gap-2 shrink-0">
                <USwitch v-model="showDisabledSAs" size="sm" />
                <span class="text-xs text-gray-500 dark:text-gray-400">Show disabled</span>
              </div>
            </div>
          </div>

          <div v-if="serviceAccountsLoading" class="text-sm text-gray-500">Loading service accounts...</div>
          <div v-else-if="serviceAccounts.length === 0" class="text-sm text-gray-500">No service accounts yet.</div>
          <div v-else-if="filteredServiceAccounts.length === 0" class="text-sm text-gray-500">No matching service accounts found.</div>
          <div v-else class="space-y-3">
            <div v-for="account in filteredServiceAccounts" :key="account.id" class="rounded-lg border border-gray-200 p-3 dark:border-gray-800">
              <div class="flex items-start justify-between gap-3">
                <div class="flex items-center gap-3">
                  <div class="flex items-center justify-center w-8 h-8 rounded-full shrink-0" :class="account.enabled ? 'bg-purple-500/10' : 'bg-gray-400/10'">
                    <UIcon name="i-lucide-bot" class="w-4 h-4" :class="account.enabled ? 'text-purple-500' : 'text-gray-400'" />
                  </div>
                  <div>
                    <div class="flex items-center gap-2">
                      <p class="text-sm font-medium" :class="{ 'opacity-50': !account.enabled }">{{ account.name }}</p>
                      <UBadge
                        :label="account.enabled ? 'ACTIVE' : 'DISABLED'"
                        :color="account.enabled ? 'success' : 'neutral'"
                        variant="subtle"
                        size="sm"
                      />
                      <UBadge :label="account.role.toUpperCase()" color="primary" variant="subtle" size="sm" />
                      <UBadge
                        v-if="account.key && !account.key.revoked"
                        label="API Key Issued"
                        color="success"
                        variant="subtle"
                        size="sm"
                        icon="i-lucide-circle-check"
                      />
                    </div>
                    <p class="text-xs text-gray-500" :class="{ 'opacity-50': !account.enabled }">{{ account.description || 'No description' }}</p>
                    <p v-if="account.created_by_email" class="text-xs text-gray-400 mt-0.5">Created by {{ account.created_by_email }}</p>
                  </div>
                </div>
                <div class="flex items-center gap-2">
                  <UButton
                    v-if="account.enabled"
                    icon="i-lucide-x"
                    size="xs"
                    variant="ghost"
                    color="error"
                    title="Disable service account"
                    @click="confirmToggleSAEnabled(account)"
                  />
                </div>
              </div>
              <div class="mt-3 border border-gray-100 dark:border-gray-800 rounded-md overflow-hidden bg-gray-50/50 dark:bg-gray-900/30">
                <!-- Accordion Header Button -->
                <button
                  type="button"
                  class="flex w-full items-center justify-between gap-2 px-3 py-2 text-left transition-colors"
                  :class="account.enabled ? 'hover:bg-gray-50 dark:hover:bg-gray-900/50 cursor-pointer' : 'opacity-50 cursor-not-allowed'"
                  :disabled="!account.enabled"
                  @click="toggleApiKeyAccordion(account.id)"
                >
                  <div class="flex items-center gap-2 text-xs font-medium text-gray-700 dark:text-gray-300">
                    <UIcon name="i-lucide-key-round" class="w-3.5 h-3.5 text-gray-400 dark:text-gray-500" />
                    <span>API Key</span>
                  </div>
                  <UIcon
                    name="i-lucide-chevron-down"
                    class="w-3.5 h-3.5 text-gray-400 transition-transform duration-200"
                    :class="openApiKeys[account.id] ? 'rotate-180' : ''"
                  />
                </button>
                
                <!-- Accordion Body -->
                <div v-if="openApiKeys[account.id]" class="px-3 pb-3 pt-2 border-t border-gray-100 dark:border-gray-800 bg-white dark:bg-gray-950/20 space-y-2.5">
                  <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-[11px] text-gray-500 dark:text-gray-400">
                    <div>
                      <span class="block font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wider mb-0.5">Key Prefix</span>
                      <span>{{ account.key && !account.key.revoked ? `${account.key.key_prefix}...` : '-' }}</span>
                    </div>
                    <div>
                      <span class="block font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wider mb-0.5">Created</span>
                      <span>{{ account.key && !account.key.revoked ? new Date(account.key.created).toLocaleString() : '-' }}</span>
                    </div>
                    <div>
                      <span class="block font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wider mb-0.5">Expires</span>
                      <span v-if="account.key && !account.key.revoked">
                        <span v-if="account.key.expires_at && !account.key.expires_at.startsWith('0001')">
                          {{ new Date(account.key.expires_at).toLocaleString() }}
                        </span>
                        <span v-else>Never</span>
                      </span>
                      <span v-else>-</span>
                    </div>
                    <div>
                      <span class="block font-semibold text-gray-400 dark:text-gray-500 uppercase tracking-wider mb-0.5">Last Used</span>
                      <span v-if="account.key && !account.key.revoked">
                        <span v-if="account.key.last_used_at && !account.key.last_used_at.startsWith('0001')">
                          {{ new Date(account.key.last_used_at).toLocaleString() }}
                        </span>
                        <span v-else>Never used</span>
                      </span>
                      <span v-else>-</span>
                    </div>
                  </div>
                  <div class="flex items-center justify-between border-t border-gray-50 dark:border-gray-900/50 pt-2">
                    <template v-if="account.key && !account.key.revoked">
                      <span class="text-[10px] text-gray-400 dark:text-gray-500">Revoking this key disables access immediately.</span>
                      <UButton
                        size="xs"
                        variant="ghost"
                        color="error"
                        label="Revoke Key"
                        icon="i-lucide-trash-2"
                        @click="confirmRevokeApiKey(account)"
                      />
                    </template>
                    <template v-else>
                      <span class="text-[10px] text-gray-400 dark:text-gray-500">No active API key. Generate one to allow programmatic access.</span>
                      <UButton
                        size="xs"
                        color="primary"
                        label="Create Key"
                        icon="i-lucide-key-round"
                        :disabled="!account.enabled"
                        @click="issueApiKey(account)"
                      />
                    </template>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </UCard>

      <UModal v-model:open="showCreateServiceAccountModal">
        <template #content>
          <CreateServiceAccountModal
            ref="createSAModalRef"
            @submit="createServiceAccount"
            @cancel="showCreateServiceAccountModal = false"
          />
        </template>
      </UModal>

      <UModal v-model:open="showApiKeyModal">
        <template #content>
          <UCard :ui="{ body: 'p-6' }">
            <template #header>
              <div class="flex items-center gap-2">
                <UIcon name="i-lucide-key-round" class="w-5 h-5 text-gray-500" />
                <h2 class="font-semibold text-lg text-gray-900 dark:text-white">API Key Generated</h2>
              </div>
              <p class="text-xs text-gray-500 mt-1">
                For service account: <strong class="text-gray-900 dark:text-white">{{ targetAccountName }}</strong>
              </p>
            </template>

            <div class="space-y-4">
              <UAlert
                color="neutral"
                variant="subtle"
                title="Copy this key now"
                description="For security reasons, this key will not be shown again."
                icon="i-lucide-triangle-alert"
              />

              <ExecutableCommand
                label="API Key"
                :content="createdApiKey"
                button-label="Copy"
              />

              <div class="flex justify-end pt-2">
                <UButton label="Done" variant="outline" color="neutral" @click="showApiKeyModal = false" />
              </div>
            </div>
          </UCard>
        </template>
      </UModal>

      <!-- Confirmation Modals -->
      <ConfirmModal
        v-model:open="showDisableSAModal"
        title="Disable Service Account"
        :description="`Are you sure you want to disable service account ${saToToggleEnabled?.name}? This will automatically revoke and remove its active API key.`"
        confirm-label="Disable"
        confirm-color="error"
        :loading="toggleEnabledSALoading"
        @confirm="executeToggleSAEnabled"
      />

      <ConfirmModal
        v-model:open="showRevokeKeyModal"
        title="Revoke API Key"
        :description="`Are you sure you want to revoke the active API key for service account ${saToRevokeKey?.name}? This will immediately disable all clients using this key.`"
        confirm-label="Revoke"
        confirm-color="error"
        :loading="revokeKeyLoading"
        @confirm="executeRevokeApiKey"
      />
    </div>
  </div>
</template>
