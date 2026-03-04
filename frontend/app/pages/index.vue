<script setup lang="ts">
const { $pb } = useNuxtApp()
const { subscribe } = useRealtime()

const { data: stacks, refresh } = useAsyncData('stacks', () =>
  $pb.collection('stacks').getFullList({ sort: '-updated', expand: 'repository' })
)

const { data: repos, refresh: refreshRepos } = useAsyncData('repos_count', () =>
  $pb.collection('repositories').getFullList({ fields: 'id' })
)

const showCreateRepo = ref(false)

function onRepoCreated() {
  refreshRepos()
}

const stats = computed(() => {
  const s = stacks.value || []
  return {
    repos: repos.value?.length || 0,
    stacks: s.length,
    active: s.filter((r: any) => r.status === 'active').length,
    error: s.filter((r: any) => r.status === 'error').length,
    paused: s.filter((r: any) => r.status === 'paused').length,
  }
})


const statusColor = (status: string) => {
  switch (status) {
    case 'active': case 'success': case 'connected': return 'success'
    case 'error': return 'error'
    case 'paused': case 'running': case 'pending': return 'warning'
    default: return 'neutral'
  }
}

// Realtime updates
const isUpdating = ref(false)

onMounted(() => {
  // Subscribe to stacks changes
  subscribe('stacks', () => {
    isUpdating.value = true
    refresh()
    setTimeout(() => { isUpdating.value = false }, 500)
  })

})
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-3">
        <h1 class="flex items-center gap-3 text-2xl font-bold">
          <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
            <UIcon name="i-lucide-layout-dashboard" class="w-5 h-5 text-yellow-400" />
          </div>
          Dashboard
        </h1>
        <div v-if="isUpdating" class="flex items-center gap-2 text-sm text-wire-400">
          <UIcon name="i-lucide-loader-2" class="w-4 h-4 animate-spin" />
          <span class="hidden sm:inline">Updating...</span>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <BadgeStatus :status="'active'" class="hidden sm:flex uppercase" />
        <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" size="sm" @click="refresh()" />
      </div>
    </div>

    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg bg-wire-700/20">
            <UIcon name="i-lucide-git-branch" class="w-5 h-5 text-wire-400" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Repositories</p>
            <p class="text-2xl font-bold">{{ stats.repos }}</p>
          </div>
        </div>
      </UCard>
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg bg-wire-400/10">
            <UIcon name="i-lucide-container" class="w-5 h-5 text-wire-400" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Stacks</p>
            <p class="text-2xl font-bold">{{ stats.stacks }}</p>
          </div>
        </div>
      </UCard>
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg bg-yellow-400/10">
            <UIcon name="i-lucide-zap" class="w-5 h-5 text-yellow-400" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Active</p>
            <p class="text-2xl font-bold text-yellow-400">{{ stats.active }}</p>
          </div>
        </div>
      </UCard>
      <UCard>
        <div class="flex items-center gap-3">
          <div class="p-2 rounded-lg bg-red-500/10">
            <UIcon name="i-lucide-alert-triangle" class="w-5 h-5 text-red-400" />
          </div>
          <div>
            <p class="text-sm text-wire-200/60">Error</p>
            <p class="text-2xl font-bold text-red-400">{{ stats.error }}</p>
          </div>
        </div>
      </UCard>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Left Column: Recent Activity -->
      <RecentSyncActivity />

      <!-- Right Column: Widgets -->
      <div class="space-y-6">
        <!-- Quick Actions -->
        <UCard>
          <template #header>
            <h2 class="font-semibold">Quick Actions</h2>
          </template>
          <div class="grid grid-cols-2 gap-3">
            <UButton
              to="/stacks"
              icon="i-lucide-layers"
              label="Manage Stacks"
              color="primary"
              variant="soft"
              block
            />
            <UButton
              to="/repositories"
              icon="i-lucide-git-branch"
              label="Repositories"
              color="primary"
              variant="soft"
              block
            />
            <UButton
              @click="showCreateRepo = true"
              icon="i-lucide-plus"
              label="Create Repository"
              color="primary"
              variant="soft"
              block
            />
            <UButton
              to="/settings"
              icon="i-lucide-settings"
              label="Settings"
              color="neutral"
              variant="soft"
              block
            />
            <UButton
              to="https://github.com/jfxdev/wireops"
              target="_blank"
              icon="i-lucide-github"
              label="Documentation"
              color="neutral"
              variant="soft"
              block
            />
          </div>
        </UCard>

        <!-- Stack Health -->
        <UCard>
          <template #header>
            <h2 class="font-semibold">System Health</h2>
          </template>
          
          <div v-if="stats.error > 0" class="space-y-3">
            <div class="flex items-center gap-2 text-red-400 bg-red-500/10 p-3 rounded-lg border border-red-500/20">
              <UIcon name="i-lucide-alert-circle" class="w-5 h-5" />
              <span class="font-medium text-sm">{{ stats.error }} stack(s) requiring attention</span>
            </div>

            <div class="divide-y divide-carbon-800">
              <div
                v-for="stack in stacks?.filter((s:any) => s.status === 'error').slice(0, 3)"
                :key="stack.id"
                class="py-2 flex items-center justify-between"
              >
                <div class="flex items-center gap-2">
                  <div class="w-2 h-2 rounded-full bg-red-400"></div>
                  <span class="text-sm font-medium">{{ stack.name }}</span>
                </div>
                <UButton
                  :to="`/stacks/${stack.id}`"
                  size="xs"
                  color="neutral"
                  variant="ghost"
                  icon="i-lucide-arrow-right"
                />
              </div>
            </div>
          </div>

          <div v-else class="flex flex-col items-center justify-center py-6 text-center">
            <div class="w-12 h-12 rounded-full bg-yellow-400/10 border border-yellow-400/20 flex items-center justify-center mb-3 shadow-[0_0_16px_rgba(255,198,0,0.1)]">
              <UIcon name="i-lucide-zap" class="w-6 h-6 text-yellow-400" />
            </div>
            <p class="font-medium text-wire-200">All Systems Operational</p>
            <p class="text-sm text-wire-200/50 mt-1">All {{ stats.stacks }} stacks are healthy</p>
          </div>
        </UCard>
      </div>
    </div>
    
    <RepositoryCreateModal v-model:open="showCreateRepo" @created="onRepoCreated" />
  </div>
</template>
