<script setup lang="ts">
import { ref, onMounted } from 'vue'

const { $pb } = useNuxtApp()
const toast = useToast()
const { isAdmin } = usePermissions()

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

onMounted(() => {
  loadUsers()
})
</script>

<template>
  <div class="space-y-6">
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
      <div v-if="usersLoading" class="text-sm text-gray-500">Loading...</div>
      <div v-else-if="users.length === 0" class="text-sm text-gray-500">No users found.</div>
      <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
        <li v-for="u in users" :key="u.id" class="flex items-center justify-between py-3 first:pt-0 last:pb-0">
          <div class="flex items-center gap-3">
            <div class="flex items-center justify-center w-8 h-8 rounded-full" :class="u.disabled ? 'bg-gray-400/10' : 'bg-yellow-400/10'">
              <UIcon name="i-lucide-user" class="w-4 h-4" :class="u.disabled ? 'text-gray-400' : 'text-yellow-400'" />
            </div>
            <div>
              <ULink
                :to="`/settings/users/${u.id}`"
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
    </UCard>
  </div>
</template>
