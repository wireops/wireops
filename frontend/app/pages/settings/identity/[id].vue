<script setup lang="ts">
const route = useRoute()
const { $pb } = useNuxtApp()
const toast = useToast()

const userId = computed(() => String(route.params.id || ''))

const { data: user, pending, error, refresh } = useAsyncData(
  () => `settings_user_${userId.value}`,
  () => $pb.collection('users').getOne(userId.value)
)

const isCurrentUser = computed(() => user.value?.id === $pb.authStore.record?.id)

function formatDate(value?: string) {
  if (!value || value.startsWith('0001-01-01')) return 'Never'
  const d = new Date(value)
  if (isNaN(d.getTime())) return value
  return d.toLocaleString()
}

async function toggleDisabled() {
  if (!user.value) return
  const willDisable = !user.value.disabled
  if (!window.confirm(`Are you sure you want to ${willDisable ? 'disable' : 'enable'} this user?`)) return
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/${user.value.id}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${$pb.authStore.token}`,
        'X-Wireops-Origin': 'ui',
      },
      body: JSON.stringify({ disabled: willDisable }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error)
    await refresh()
    toast.add({ title: willDisable ? 'User disabled' : 'User enabled', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to update user', description: e?.message, color: 'error' })
  }
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <UButton
          to="/settings/identity"
          icon="i-lucide-arrow-left"
          label="Back to Identity"
          variant="ghost"
          color="neutral"
          class="-ml-2 mb-2"
        />
        <div class="flex items-center gap-3">
          <div class="flex size-11 items-center justify-center rounded-lg bg-yellow-400/10">
            <UIcon name="i-lucide-user" class="size-5 text-yellow-400" />
          </div>
          <div>
            <h1 class="text-2xl font-bold text-gray-900 dark:text-white">
              {{ user?.email || 'User Profile' }}
            </h1>
            <p class="text-sm text-gray-500">{{ user?.role || 'user' }} account</p>
          </div>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <UBadge v-if="user?.protected" label="Protected" color="warning" variant="subtle" />
        <UBadge v-else-if="user?.disabled" label="Disabled" color="neutral" variant="subtle" />
        <UButton
          v-if="!user?.protected && !isCurrentUser"
          :icon="user?.disabled ? 'i-lucide-user-check' : 'i-lucide-user-x'"
          :label="user?.disabled ? 'Enable User' : 'Disable User'"
          :color="user?.disabled ? 'success' : 'warning'"
          variant="outline"
          size="sm"
          @click="toggleDisabled"
        />
        <UButton
          v-if="isCurrentUser"
          to="/settings/security"
          icon="i-lucide-key-round"
          label="Change Password"
          color="neutral"
          variant="outline"
        />
      </div>
    </div>

    <UCard>
      <template #header>
        <div class="flex items-center justify-between gap-3">
          <h2 class="font-semibold">Profile</h2>
          <UButton
            icon="i-lucide-refresh-cw"
            aria-label="Refresh user profile"
            variant="ghost"
            color="neutral"
            size="sm"
            :loading="pending"
            @click="refresh()"
          />
        </div>
      </template>

      <div v-if="pending" class="text-sm text-gray-500">Loading...</div>

      <div v-else-if="error" class="space-y-3">
        <p class="text-sm text-red-500">Failed to load user profile.</p>
        <UButton
          icon="i-lucide-refresh-cw"
          label="Try Again"
          variant="outline"
          color="neutral"
          @click="refresh()"
        />
      </div>

      <dl v-else-if="user" class="divide-y divide-gray-100 dark:divide-gray-800">
        <div class="grid gap-1 py-3 first:pt-0 sm:grid-cols-3 sm:gap-4">
          <dt class="text-sm text-gray-500">Email</dt>
          <dd class="text-sm font-medium text-gray-900 sm:col-span-2 dark:text-white">{{ user.email }}</dd>
        </div>
        <div class="grid gap-1 py-3 sm:grid-cols-3 sm:gap-4">
          <dt class="text-sm text-gray-500">User ID</dt>
          <dd class="break-all font-mono text-xs text-gray-700 sm:col-span-2 dark:text-gray-300">{{ user.id }}</dd>
        </div>
        <div class="grid gap-1 py-3 sm:grid-cols-3 sm:gap-4">
          <dt class="text-sm text-gray-500">Role</dt>
          <dd class="sm:col-span-2">
            <UBadge :label="user.role || 'viewer'" color="primary" variant="subtle" />
          </dd>
        </div>
        <div class="grid gap-1 py-3 sm:grid-cols-3 sm:gap-4">
          <dt class="text-sm text-gray-500">Status</dt>
          <dd class="flex items-center gap-2 sm:col-span-2">
            <UBadge
              :label="user.verified === false ? 'Unverified' : 'Verified'"
              :color="user.verified === false ? 'warning' : 'success'"
              variant="subtle"
            />
            <UBadge v-if="user.disabled" label="Disabled" color="neutral" variant="subtle" />
            <UBadge v-if="user.protected" label="Protected" color="warning" variant="subtle" />
          </dd>
        </div>
        <div class="grid gap-1 py-3 sm:grid-cols-3 sm:gap-4">
          <dt class="text-sm text-gray-500">Created</dt>
          <dd class="text-sm text-gray-900 sm:col-span-2 dark:text-white">{{ formatDate(user.created) }}</dd>
        </div>
        <div class="grid gap-1 py-3 last:pb-0 sm:grid-cols-3 sm:gap-4">
          <dt class="text-sm text-gray-500">Updated</dt>
          <dd class="text-sm text-gray-900 sm:col-span-2 dark:text-white">{{ formatDate(user.updated) }}</dd>
        </div>
      </dl>
    </UCard>
  </div>
</template>
