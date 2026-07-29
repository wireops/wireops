<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { stackRepositorySubtitle } from '../utils/stack-status'
import type { AvailabilitySegment } from './StatusAvailabilityBar.vue'

const { $pb } = useNuxtApp()
const { getWorkers, listOrphans, purgeOrphan } = useApi()
const { subscribe } = useRealtime()
const toast = useToast()
const { announce } = useA11yAnnouncer()
const { isViewer } = usePermissions()

const { data: stacks, refresh } = useAsyncData('stacks_list', () =>
  $pb.collection('stacks').getFullList({ sort: '-updated', expand: 'repository,worker' })
)
const { data: workers, refresh: refreshWorkers } = useAsyncData('stack_card_workers', () =>
  getWorkers().catch(() => [])
)
const { data: repos, refresh: refreshRepos } = useAsyncData('repos_for_stacks_empty', () =>
  $pb.collection('repositories').getFullList({ fields: 'id', requestKey: null })
)
const hasRepos = computed(() => (repos.value?.length ?? 0) > 0)
const hasWorkers = computed(() =>
  (workers.value || []).some((w: any) => w.status !== WORKER_STATUS.PENDING && w.status !== WORKER_STATUS.REVOKED)
)
const showCreateRepoFromEmpty = ref(false)

const emptyStateStep = computed(() => {
  if (!hasRepos.value) {
    return {
      description: 'Create a repository first, then add a stack linked to it.',
      ctaLabel: 'Add Repository',
      action: () => { showCreateRepoFromEmpty.value = true },
    }
  }
  if (!hasWorkers.value) {
    return {
      description: 'Register a worker first, then add a stack for it to run.',
      ctaLabel: 'Add Worker',
      action: () => navigateTo('/workers'),
    }
  }
  return {
    description: 'Add a stack linked to one of your repositories.',
    ctaLabel: 'Add Stack',
    action: () => openCreate(),
  }
})

const isUpdating = ref(false)

let updateTimer: ReturnType<typeof setTimeout> | undefined

async function refreshList() {
  await Promise.all([refresh(), refreshWorkers(), refreshRepos()])
}

onMounted(() => {
  window.addEventListener('keydown', handleSlashShortcut)
  subscribe('stacks', () => {
    isUpdating.value = true
    announce('Stacks list updating')
    refresh()
    refreshWorkers()
    clearTimeout(updateTimer)
    updateTimer = setTimeout(() => {
      isUpdating.value = false
      announce('Stacks list updated')
    }, 500)
  })
  subscribe('workers', () => {
    refreshWorkers()
  })
  subscribe('repositories', () => {
    refreshRepos()
    refresh()
  })
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleSlashShortcut)
  clearTimeout(updateTimer)
})

const showCreate = ref(false)
const showBuilder = ref(false)

function openCreate() {
  showCreate.value = true
}

function onCreated() {
  refreshList()
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
  refreshList()
}

const workersById = computed(() =>
  Object.fromEntries((workers.value || []).map((worker: any) => [worker.id, worker]))
)

const stackStatusSegments: AvailabilitySegment[] = [
  { key: 'active', label: 'Active', barClass: 'bg-emerald-400', dotClass: 'bg-emerald-400', statuses: ['active'] },
  { key: 'syncing', label: 'Syncing', barClass: 'bg-sky-400', dotClass: 'bg-sky-400', statuses: ['syncing'] },
  { key: 'paused', label: 'Paused', barClass: 'bg-amber-400', dotClass: 'bg-amber-400', statuses: ['paused', 'pending'], filterValue: 'paused' },
  { key: 'error', label: 'Error', barClass: 'bg-rose-400', dotClass: 'bg-rose-400', statuses: ['error'] },
]

const searchQuery = ref('')
const searchInputRef = ref<{ $el?: HTMLElement } | HTMLElement | null>(null)
const statusFilter = ref('all')
const sortBy = ref('name')

function resolveSearchInput() {
  const root = searchInputRef.value instanceof HTMLElement ? searchInputRef.value : searchInputRef.value?.$el
  return root?.querySelector('input') as HTMLInputElement | null
}

function isTypingTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false

  const tagName = target.tagName.toUpperCase()
  const role = target.getAttribute('role')

  return tagName === 'INPUT'
    || tagName === 'TEXTAREA'
    || tagName === 'SELECT'
    || target.isContentEditable
    || role === 'textbox'
    || role === 'combobox'
    || role === 'listbox'
    || role === 'menu'
    || !!target.closest('[contenteditable="true"]')
}

function handleSlashShortcut(event: KeyboardEvent) {
  if (event.key !== '/' || event.ctrlKey || event.metaKey || event.altKey) return
  if (isTypingTarget(event.target)) return

  event.preventDefault()
  const input = resolveSearchInput()
  if (!input) return

  input.focus()
  input.select()
  announce('Stack search focused')
}

const filteredStacks = computed(() => {
  let filtered = stacks.value || []

  if (searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    filtered = filtered.filter((s: any) =>
      s.name.toLowerCase().includes(query) ||
      stackRepositorySubtitle(s).toLowerCase().includes(query) ||
      (s.containers_list || []).some((c: any) => c.name?.toLowerCase().includes(query))
    )
  }

  if (statusFilter.value !== 'all') {
    if (statusFilter.value === 'paused') {
      filtered = filtered.filter((s: any) => s.status === 'paused' || s.status === 'pending')
    } else {
      filtered = filtered.filter((s: any) => s.status === statusFilter.value)
    }
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
  refreshList()
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
    announce(`Removed orphan directory ${dirName}`)
  } catch {
    toast.add({ title: `Failed to purge ${dirName}`, color: 'error' })
    announce(`Failed to remove orphan directory ${dirName}`, 'assertive')
  }
  purgingDir.value = ''
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div class="flex items-center gap-3">
        <h1 class="flex items-center gap-3 text-2xl font-bold text-gray-900 dark:text-wire-200">
          <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
            <UIcon name="i-lucide-layers" class="w-5 h-5 text-yellow-400" />
          </div>
          Stacks
        </h1>
        <div v-if="isUpdating" class="flex items-center gap-2 text-sm text-gray-500" role="status" aria-live="polite">
          <UIcon name="i-lucide-loader-2" class="w-4 h-4 animate-spin" />
          <span class="hidden sm:inline">Updating...</span>
        </div>
      </div>
      <div v-if="!isViewer" class="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
        <ActionButton icon="i-lucide-plus" label="Add Stack" class="w-full justify-center sm:w-auto" @click="openCreate()" />
        <UButton icon="i-lucide-package-plus" label="Import" variant="outline" class="w-full justify-center sm:w-auto" @click="showImport = true" />
        <UButton icon="i-lucide-wrench" label="Stack Builder" variant="outline" class="w-full justify-center sm:w-auto" @click="showBuilder = true" />
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
            <UButton v-if="!isViewer" icon="i-lucide-package-search" label="Manage Orphans" variant="outline" color="warning" size="xs" class="hidden sm:inline-flex" @click="openOrphans" />
            <RefreshButton @click="refreshList()" />
          </div>
        </div>
      </template>

      <div v-if="stacks?.length" class="space-y-4">
        <div class="flex flex-row flex-wrap items-center gap-2 sm:gap-3" role="search" aria-label="Filter stacks">
          <AppTextInput
            ref="searchInputRef"
            v-model="searchQuery"
            icon="i-lucide-search"
            placeholder="Search stacks or services..."
            class="min-w-[140px] flex-1"
            aria-label="Search stacks"
          />
          <AppSelectInput
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
            content-width
            class="sm:min-w-28"
            aria-label="Filter stacks by status"
          />
          <AppSelectInput
            v-model="sortBy"
            :items="[
              { label: 'Updated', value: 'updated' },
              { label: 'Name', value: 'name' },
              { label: 'Last Synced', value: 'last_synced' },
              { label: 'Status', value: 'status' }
            ]"
            placeholder="Sort by"
            content-width
            class="sm:min-w-28"
            aria-label="Sort stacks"
          />
        </div>

        <StatusAvailabilityBar
          v-model="statusFilter"
          :items="stacks"
          :segments="stackStatusSegments"
          aria-label="Stack status availability breakdown"
        />

        <div v-if="filteredStacks.length === 0" class="text-center py-12" role="status" aria-live="polite">
          <UIcon name="i-lucide-search-x" class="w-12 h-12 text-gray-300 mx-auto mb-4" />
          <p class="text-gray-500">No stacks found</p>
          <p class="text-xs text-gray-400 mt-1">Try adjusting your search or filters</p>
        </div>

        <div v-else class="grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3">
          <StackCard
            v-for="stack in filteredStacks"
            :key="stack.id"
            :stack="stack"
            :workers-by-id="workersById"
          />
        </div>
      </div>

      <EmptyState
        v-else
        icon="i-lucide-inbox"
        title="No stacks configured yet"
        :description="emptyStateStep.description"
        :cta-label="isViewer ? undefined : emptyStateStep.ctaLabel"
        @cta="emptyStateStep.action"
      />
    </UCard>

    <CreateStackModal v-model:open="showCreate" @created="onCreated" />
    <StackBuilderModal v-model:open="showBuilder" :workers="workers || []" />
    <RepositoryCreateModal v-model:open="showCreateRepoFromEmpty" @created="refreshRepos" />

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
