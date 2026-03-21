<script setup lang="ts">
const { $pb } = useNuxtApp()
const { triggerSync, listOrphans, purgeOrphan } = useApi()
const { subscribe } = useRealtime()
const toast = useToast()
const { platformIconUrl } = useRepositoryPlatform()

const { data: stacks, refresh } = useAsyncData('stacks_list', () =>
  $pb.collection('stacks').getFullList({ sort: '-updated', expand: 'repository,worker' })
)

const isUpdating = ref(false)

onMounted(() => {
  subscribe('stacks', () => {
    isUpdating.value = true
    refresh()
    setTimeout(() => { isUpdating.value = false }, 500)
  })
})

const showCreate = ref(false)

function openCreate() {
  showCreate.value = true
}

function onCreated() {
  refresh()
}

const showDelete = ref(false)
const deleteTarget = ref<any>(null)

function openDelete(stack: any) {
  deleteTarget.value = stack
  showDelete.value = true
}

function onDeleted() {
  showDelete.value = false
  deleteTarget.value = null
  refresh()
}

async function sync(id: string) {
  try {
    await triggerSync(id)
    toast.add({ title: 'Sync triggered', color: 'success' })
  } catch (e: any) {
    toast.add({ title: e?.message || 'Sync failed', color: 'error' })
  }
}

const statusColor = (s: string) => {
  switch (s) {
    case 'active': return 'success'
    case 'syncing': return 'info'
    case 'error': return 'error'
    case 'paused': case 'pending': return 'warning'
    default: return 'neutral'
  }
}

const searchQuery = ref('')
const statusFilter = ref('all')
const sortBy = ref('updated')

const filteredStacks = computed(() => {
  let filtered = stacks.value || []

  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    filtered = filtered.filter((s: any) =>
      s.name.toLowerCase().includes(query) ||
      s.expand?.repository?.name?.toLowerCase().includes(query)
    )
  }

  if (statusFilter.value !== 'all') {
    filtered = filtered.filter((s: any) => s.status === statusFilter.value)
  }

  filtered = [...filtered].sort((a: any, b: any) => {
    switch (sortBy.value) {
      case 'name':
        return a.name.localeCompare(b.name)
      case 'updated':
        return new Date(b.updated).getTime() - new Date(a.updated).getTime()
      case 'last_synced':
        if (!a.last_synced_at) return 1
        if (!b.last_synced_at) return -1
        return new Date(b.last_synced_at).getTime() - new Date(a.last_synced_at).getTime()
      case 'status':
        return a.status.localeCompare(b.status)
      default:
        return 0
    }
  })

  return filtered
})

const showImport = ref(false)

function onImported(_stackId: string) {
  showImport.value = false
  refresh()
}

const showOrphans = ref(false)
const orphans = ref<{ dir_name: string; compose_file: string; has_compose: boolean }[]>([])
const loadingOrphans = ref(false)
const purgingDir = ref('')

async function openOrphans() {
  showOrphans.value = true
  loadingOrphans.value = true
  try {
    orphans.value = await listOrphans()
  } catch { orphans.value = [] }
  loadingOrphans.value = false
}

async function handlePurge(dirName: string) {
  purgingDir.value = dirName
  try {
    await purgeOrphan(dirName)
    orphans.value = orphans.value.filter(o => o.dir_name !== dirName)
    toast.add({ title: `Purged ${dirName}`, color: 'success' })
  } catch {
    toast.add({ title: `Failed to purge ${dirName}`, color: 'error' })
  }
  purgingDir.value = ''
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-3">
        <h1 class="flex items-center gap-3 text-2xl font-bold text-gray-900 dark:text-wire-200">
          <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
            <UIcon name="i-lucide-layers" class="w-5 h-5 text-yellow-400" />
          </div>
          Stacks
        </h1>
        <div v-if="isUpdating" class="flex items-center gap-2 text-sm text-gray-500">
          <UIcon name="i-lucide-loader-2" class="w-4 h-4 animate-spin" />
          <span class="hidden sm:inline">Updating...</span>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <UButton icon="i-lucide-package-plus" label="Import" variant="outline" @click="showImport = true" />
        <UButton icon="i-lucide-plus" label="Add Stack" class="shadow-[0_0_16px_rgba(255,198,0,0.35)] hover:shadow-[0_0_24px_rgba(255,198,0,0.55)] transition-shadow" @click="openCreate()" />
      </div>
    </div>

    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">
            Stacks
            <span v-if="stacks?.length" class="ml-1.5 text-yellow-400">({{ stacks.length }})</span>
          </h3>
          <div class="flex items-center gap-3">
            <UButton icon="i-lucide-package-search" label="Manage Orphans" variant="outline" color="warning" size="xs" class="hidden sm:inline-flex" @click="openOrphans" />
            <UTooltip text="Refresh">
              <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" color="neutral" @click="refresh()" />
            </UTooltip>
          </div>
        </div>
      </template>

      <div v-if="stacks?.length" class="space-y-4">
        <div class="flex flex-col sm:flex-row gap-3">
          <UInput
            v-model="searchQuery"
            icon="i-lucide-search"
            placeholder="Search stacks..."
            class="flex-1"
          />
          <USelect
            v-model="statusFilter"
            :items="[
              { label: 'All', value: 'all' },
              { label: 'Active', value: 'active' },
              { label: 'Paused', value: 'paused' },
              { label: 'Error', value: 'error' },
              { label: 'Syncing', value: 'syncing' },
              { label: 'Pending', value: 'pending' }
            ]"
            placeholder="Filter by status"
            class="w-full sm:w-40"
          />
          <USelect
            v-model="sortBy"
            :items="[
              { label: 'Updated', value: 'updated' },
              { label: 'Name', value: 'name' },
              { label: 'Last Synced', value: 'last_synced' },
              { label: 'Status', value: 'status' }
            ]"
            placeholder="Sort by"
            class="w-full sm:w-40"
          />
        </div>

        <div v-if="filteredStacks.length === 0" class="text-center py-12">
          <UIcon name="i-lucide-search-x" class="w-12 h-12 text-gray-300 mx-auto mb-4" />
          <p class="text-gray-500">No stacks found</p>
          <p class="text-xs text-gray-400 mt-1">Try adjusting your search or filters</p>
        </div>

        <div v-else class="space-y-3">
          <div
            v-for="stack in filteredStacks"
            :key="stack.id"
            class="flex items-center justify-between p-4 bg-gray-50 dark:bg-carbon-800/40 rounded-xl border border-gray-200 dark:border-carbon-700 hover:shadow-[0_0_0_2px_rgba(255,198,0,0.35),0_0_20px_rgba(255,198,0,0.12)] transition-all"
          >
            <NuxtLink :to="`/stacks/${stack.id}`" class="flex-1 min-w-0">
              <div class="flex items-center gap-2 mb-1">
                <h3 class="font-semibold truncate text-gray-900 dark:text-wire-200">{{ stack.name }}</h3>
                <BadgeStatus :status="stack.status" />
              </div>
              <p class="text-sm text-gray-500 dark:text-wire-200/50">
                <template v-if="stack.source_type === 'local'">
                  <UIcon name="i-lucide-hard-drive" class="w-3 h-3 inline" />
                  {{ stack.import_path || 'local import' }}
                </template>
                <template v-else>
                  <img
                    v-if="platformIconUrl(stack.expand?.repository?.platform)"
                    :src="platformIconUrl(stack.expand?.repository?.platform)!"
                    class="w-3 h-3 object-contain inline shrink-0"
                    alt=""
                  >
                  <UIcon v-else name="i-lucide-git-branch" class="w-3 h-3 inline shrink-0" />
                  {{ stack.expand?.repository?.name || 'Unknown repo' }}
                </template>
              </p>

              <div class="flex items-center gap-2 mt-2 text-xs text-gray-400 dark:text-wire-200/40">
                <UBadge v-if="stack.source_type === 'local'" label="local" variant="subtle" color="info" size="xs" />
              </div>
            </NuxtLink>

            <div class="px-4">
              <div v-if="stack.containers_list?.length" class="mt-3 sm:mt-0">
                <StackContainersList :containers="stack.containers_list" />
              </div>
            </div>
            <div class="ml-2 border-l border-gray-200 dark:border-carbon-700 pl-4 flex items-center gap-1">
              <UTooltip text="Sync stack">
                <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" @click.stop="sync(stack.id)" />
              </UTooltip>
              <UTooltip text="Delete stack">
                <UButton icon="i-lucide-trash-2" variant="ghost" color="error" size="xs" @click.stop="openDelete(stack)" />
              </UTooltip>
            </div>
          </div>
        </div>
      </div>

      <div v-else class="text-center py-12">
        <div class="w-14 h-14 rounded-full bg-wire-400/10 border border-wire-400/20 flex items-center justify-center mx-auto mb-3">
          <UIcon name="i-lucide-inbox" class="w-7 h-7 text-wire-400" />
        </div>
        <h3 class="text-lg font-medium text-gray-900 dark:text-wire-200 mb-1">No stacks configured yet</h3>
        <p class="text-gray-500 dark:text-wire-200/50 text-sm">Create a repository first, then add a stack linked to it.</p>
      </div>
    </UCard>

    <CreateStackModal v-model:open="showCreate" @created="onCreated" />

    <UModal v-model:open="showOrphans" title="Orphan Directories" description="Directories in the repos workspace not linked to any repository.">
      <template #body>
        <div v-if="loadingOrphans" class="py-8 text-center">
          <UIcon name="i-lucide-loader-2" class="w-6 h-6 animate-spin text-gray-400 mx-auto" />
        </div>
        <div v-else-if="orphans.length" class="divide-y divide-gray-200 dark:divide-gray-700">
          <div v-for="o in orphans" :key="o.dir_name" class="flex items-center justify-between py-3">
            <div class="min-w-0">
              <p class="text-sm font-mono font-medium truncate">{{ o.dir_name }}</p>
              <div class="flex items-center gap-2 mt-0.5">
                <BadgeLabel v-if="o.has_compose" :label="o.compose_file" color="info" />
                <BadgeLabel v-else label="No compose file" color="neutral" />
              </div>
            </div>
            <UButton
              icon="i-lucide-trash-2"
              label="Purge"
              color="error"
              variant="soft"
              size="xs"
              :loading="purgingDir === o.dir_name"
              @click="handlePurge(o.dir_name)"
            />
          </div>
        </div>
        <p v-else class="py-8 text-center text-sm text-gray-500">No orphan directories found.</p>
      </template>
    </UModal>

    <UModal v-model:open="showDelete" title="Delete Stack" description="Are you sure you want to delete this stack?">
      <template #body>
        <DeleteStackModal
          v-if="deleteTarget"
          :stack="deleteTarget"
          @deleted="onDeleted"
          @cancel="showDelete = false"
        />
      </template>
    </UModal>

    <UModal v-model:open="showImport" title="Import Compose Stack" description="Import an existing Docker Compose project into wireops">
      <template #body>
        <ImportStackModal
          @imported="onImported"
          @cancel="showImport = false"
        />
      </template>
    </UModal>
  </div>
</template>
