<script setup lang="ts">
const { $pb } = useNuxtApp()

const { data: repos, refresh } = useAsyncData('repos_list', () =>
  $pb.collection('repositories').getFullList({ sort: '-updated' })
)

// Search and filters
const searchQuery = ref('')
const statusFilter = ref('all')
const sortBy = ref('updated')

const filteredRepos = computed(() => {
  let filtered = repos.value || []
  
  // Search filter
  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    filtered = filtered.filter((r: any) =>
      r.name.toLowerCase().includes(query) ||
      r.git_url.toLowerCase().includes(query)
    )
  }
  
  // Status filter
  if (statusFilter.value !== 'all') {
    filtered = filtered.filter((r: any) => r.status === statusFilter.value)
  }
  
  // Sort
  filtered = [...filtered].sort((a: any, b: any) => {
    switch (sortBy.value) {
      case 'name':
        return a.name.localeCompare(b.name)
      case 'updated':
        return new Date(b.updated).getTime() - new Date(a.updated).getTime()
      case 'last_fetched':
        if (!a.last_fetched_at) return 1
        if (!b.last_fetched_at) return -1
        return new Date(b.last_fetched_at).getTime() - new Date(a.last_fetched_at).getTime()
      default:
        return 0
    }
  })
  
  return filtered
})

const showCreate = ref(false)

const showDelete = ref(false)
const deleteRepoId = ref('')
const deleteRepoName = ref('')

function openDeleteModal(repo: any) {
  deleteRepoId.value = repo.id
  deleteRepoName.value = repo.name
  showDelete.value = true
}

const statusColor = (s: string) => {
  switch (s) {
    case 'connected': return 'success'
    case 'error': return 'error'
    default: return 'neutral'
  }
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="flex items-center gap-3 text-2xl font-bold text-gray-900 dark:text-wire-200">
        <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
          <UIcon name="i-lucide-git-branch" class="w-5 h-5 text-yellow-400" />
        </div>
        Repositories
      </h1>
      <UButton icon="i-lucide-plus" label="Add Repository" class="shadow-[0_0_16px_rgba(255,198,0,0.35)] hover:shadow-[0_0_24px_rgba(255,198,0,0.55)] transition-shadow" @click="showCreate = true" />
    </div>

    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">
            Repositories
            <span v-if="repos?.length" class="ml-1.5 text-yellow-400">({{ repos.length }})</span>
          </h3>
          <UTooltip text="Refresh">
            <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" color="neutral" @click="refresh()" />
          </UTooltip>
        </div>
      </template>

      <div v-if="repos?.length" class="space-y-4">
        <div class="flex flex-col sm:flex-row gap-3">
          <UInput
            v-model="searchQuery"
            icon="i-lucide-search"
            placeholder="Search repositories..."
            class="flex-1"
          />
          <USelect
            v-model="statusFilter"
            :items="[
              { label: 'All', value: 'all' },
              { label: 'Connected', value: 'connected' },
              { label: 'Error', value: 'error' }
            ]"
            placeholder="Filter by status"
            class="w-full sm:w-40"
          />
          <USelect
            v-model="sortBy"
            :items="[
              { label: 'Updated', value: 'updated' },
              { label: 'Name', value: 'name' },
              { label: 'Last Fetched', value: 'last_fetched' }
            ]"
            placeholder="Sort by"
            class="w-full sm:w-40"
          />
        </div>

        <div v-if="filteredRepos.length === 0" class="text-center py-12">
          <UIcon name="i-lucide-search-x" class="w-12 h-12 text-gray-300 mx-auto mb-4" />
          <p class="text-gray-500">No repositories found</p>
          <p class="text-xs text-gray-400 mt-1">Try adjusting your search or filters</p>
        </div>

        <div v-else class="space-y-3">
          <div
            v-for="repo in filteredRepos"
            :key="repo.id"
            class="flex items-center justify-between p-4 bg-gray-50 dark:bg-carbon-800/40 rounded-xl border border-gray-200 dark:border-carbon-700 hover:shadow-[0_0_0_2px_rgba(255,198,0,0.35),0_0_20px_rgba(255,198,0,0.12)] transition-all"
          >
            <NuxtLink :to="`/repositories/${repo.id}`" class="flex-1 min-w-0">
              <div class="flex items-center gap-2 mb-1">
                <h3 class="font-semibold truncate text-gray-900 dark:text-wire-200">{{ repo.name }}</h3>
                <BadgeStatus :status="repo.status" />
              </div>
              <p class="text-sm text-gray-500 dark:text-wire-200/50 font-mono truncate">{{ repo.git_url }}</p>
              <div class="hidden sm:flex items-center gap-4 mt-2 text-xs text-gray-400 dark:text-wire-200/40">
                <span class="flex items-center gap-1">
                  <UIcon name="i-lucide-git-branch" class="w-3 h-3" />
                  {{ repo.branch || 'main' }}
                </span>
                <span v-if="repo.last_fetched_at" class="flex items-center gap-1">
                  <UIcon name="i-lucide-clock" class="w-3 h-3" />
                  {{ new Date(repo.last_fetched_at).toLocaleString() }}
                </span>
                <span v-if="repo.last_commit_sha" class="flex items-center gap-1 font-mono">
                  <UIcon name="i-lucide-git-commit" class="w-3 h-3" />
                  {{ repo.last_commit_sha?.slice(0, 7) }}
                </span>
              </div>
            </NuxtLink>
            <div class="ml-2 border-l border-gray-200 dark:border-carbon-700 pl-4 flex items-center">
              <UTooltip text="Delete repository">
                <UButton icon="i-lucide-trash-2" variant="ghost" color="error" size="xs" @click.stop="openDeleteModal(repo)" />
              </UTooltip>
            </div>
          </div>
        </div>
      </div>

      <div v-else class="text-center py-12">
        <div class="w-14 h-14 rounded-full bg-wire-400/10 border border-wire-400/20 flex items-center justify-center mx-auto mb-3">
          <UIcon name="i-lucide-inbox" class="w-7 h-7 text-wire-400" />
        </div>
        <h3 class="text-lg font-medium text-gray-900 dark:text-wire-200 mb-1">No repositories configured yet</h3>
        <p class="text-gray-500 dark:text-wire-200/50 text-sm">Add a repository to start tracking your compose stacks.</p>
      </div>
    </UCard>

    <RepositoryCreateModal v-model:open="showCreate" @created="refresh" />
    <RepositoryDeleteModal v-model:open="showDelete" :repository-id="deleteRepoId" :repository-name="deleteRepoName" @deleted="refresh" />
  </div>
</template>
