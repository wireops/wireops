<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { stackFleetStatus, buildStackStatusFilter } from '../utils/stack-status'
import { GROUP_ALL, GROUP_UNGROUPED, encodeGroupValue, decodeGroupValue } from '../utils/job-filter'
import { usePaginatedList } from '../composables/usePaginatedList'
import type { AvailabilitySegment } from './StatusAvailabilityBar.vue'

const { $pb } = useNuxtApp()
const { getWorkers, listOrphans, purgeOrphan } = useApi()
const { subscribe } = useRealtime()
const toast = useToast()
const { announce } = useA11yAnnouncer()
const { isViewer } = usePermissions()

const route = useRoute()

const searchQuery = ref('')
const searchInputRef = ref<{ $el?: HTMLElement } | HTMLElement | null>(null)
const statusFilter = ref('all')
const workerFilter = ref('all')
const sortBy = ref('name')

function groupQueryToFilterValue(val: unknown): string {
  if (typeof val !== 'string' || val === '') return GROUP_ALL
  return val === GROUP_ALL || val === GROUP_UNGROUPED ? val : encodeGroupValue(val)
}

const groupFilter = ref(groupQueryToFilterValue(route.query.group))
watch(() => route.query.group, (val) => {
  groupFilter.value = groupQueryToFilterValue(val)
})

const sortParam = computed(() => {
  switch (sortBy.value) {
    case 'name': return 'name'
    case 'last_synced': return '-last_synced_at'
    // Approximation: sorts by the raw status column, not the "syncing folds
    // into active/pending" effective status shown in the UI - a syncing
    // stack's position can shift by one bucket versus what's displayed.
    case 'status': return 'status'
    default: return '-updated'
  }
})

function buildStacksFilter() {
  const clauses: string[] = []
  if (searchQuery.value.trim()) {
    // Container-name search (previously matched against containers_list, a
    // JSON field) is dropped here: it can't be expressed as a PocketBase
    // filter without a server-side search index, and keeping it would mean
    // falling back to a full unpaginated fetch defeating the point of this
    // change. Name/group search remains.
    clauses.push($pb.filter('(name ~ {:q} || group ~ {:q})', { q: searchQuery.value.trim() }))
  }
  if (statusFilter.value !== 'all') {
    const statusClause = buildStackStatusFilter(statusFilter.value)
    if (statusClause) clauses.push(statusClause)
  }
  if (workerFilter.value !== 'all') {
    clauses.push($pb.filter('(worker = {:w})', { w: workerFilter.value }))
  }
  if (groupFilter.value !== GROUP_ALL) {
    clauses.push(groupFilter.value === GROUP_UNGROUPED
      ? "(group = '' || group = null)"
      : $pb.filter('(group = {:g})', { g: decodeGroupValue(groupFilter.value) }))
  }
  return clauses.join(' && ')
}

const {
  page, perPage, items: stacks, totalItems: totalStacks, totalPages, loading: stacksLoading, reload: reloadStacks,
} = usePaginatedList(
  async ({ page: p, perPage: pp, sort }) => {
    const result = await $pb.collection('stacks').getList(p, pp, {
      sort,
      filter: buildStacksFilter(),
      expand: 'repository,worker',
      // Debounced search/filter changes can fire overlapping requests to
      // this same collection; usePaginatedList already discards
      // out-of-order responses via its own requestId, so PocketBase's
      // built-in auto-cancel (same requestKey) is redundant here and would
      // otherwise surface as an unhandled ClientResponseError.
      requestKey: null,
    })
    return { items: result.items, totalItems: result.totalItems }
  },
  { perPage: 24, sort: sortParam, watchDebounced: [searchQuery, statusFilter, workerFilter, groupFilter] }
)

// Fleet-wide aggregate used only for the group dropdown and the status
// availability bar - both need counts across every stack, not just the
// current page. A narrow field selection keeps this cheap even though it
// isn't paginated: the expensive part of the main query is the full record
// payload + expand, not these three columns.
const { data: stacksAggregate, refresh: refreshStacksAggregate } = useAsyncData('stacks_aggregate', () =>
  $pb.collection('stacks').getFullList({ fields: 'id,status,group,deployed_at,worker', requestKey: null })
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
  await Promise.all([reloadStacks(), refreshStacksAggregate(), refreshWorkers(), refreshRepos()])
}

// Realtime record events (stacks/repositories channels) can fire several
// times in a burst - e.g. a reconcile touching many stacks - so the
// fleet-wide aggregate refetch is debounced to a single trailing call
// instead of running once per event.
let aggregateDebounceTimer: ReturnType<typeof setTimeout> | undefined
function debouncedRefreshStacksAggregate() {
  clearTimeout(aggregateDebounceTimer)
  aggregateDebounceTimer = setTimeout(() => {
    refreshStacksAggregate()
  }, 300)
}

onMounted(() => {
  window.addEventListener('keydown', handleSlashShortcut)
  subscribe('stacks', () => {
    isUpdating.value = true
    announce('Stacks list updating')
    reloadStacks(true)
    debouncedRefreshStacksAggregate()
    refreshWorkers()
    clearTimeout(updateTimer)
    updateTimer = setTimeout(() => {
      isUpdating.value = false
      announce('Stacks list updated')
    }, 500)
  })
  subscribe('workers', () => {
    refreshWorkers()
    // The "degraded" bucket is filtered server-side on worker.docker_online
    // (see buildStacksFilter/buildStackStatusFilter) - a worker flipping
    // online/offline can change which stacks belong in that page without any
    // stack record itself changing, so the paginated query needs its own
    // refetch here instead of relying on the 'stacks' subscription above.
    if (statusFilter.value === 'degraded') {
      reloadStacks(true)
    }
  })
  subscribe('repositories', () => {
    refreshRepos()
    reloadStacks(true)
    debouncedRefreshStacksAggregate()
  })
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleSlashShortcut)
  clearTimeout(updateTimer)
  clearTimeout(aggregateDebounceTimer)
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

// "syncing" is not its own bucket here: it's a transient state every stack
// passes through on every reconcile, and with several stacks syncing at once
// (routine, since cron intervals overlap) a dedicated segment made this bar
// flicker constantly. stacksForAvailability below resolves each stack to its
// stable status (stackEffectiveStatus) before counting, so a syncing stack
// counts under Active/Paused same as it does everywhere else in the UI.
const stackStatusSegments: AvailabilitySegment[] = [
  { key: 'active', label: 'Active', barClass: 'bg-emerald-400', dotClass: 'bg-emerald-400', statuses: ['active'] },
  { key: 'degraded', label: 'Degraded', barClass: 'bg-orange-400', dotClass: 'bg-orange-400', statuses: ['degraded'] },
  { key: 'paused', label: 'Paused', barClass: 'bg-amber-400', dotClass: 'bg-amber-400', statuses: ['paused', 'pending'], filterValue: 'paused' },
  { key: 'error', label: 'Error', barClass: 'bg-rose-400', dotClass: 'bg-rose-400', statuses: ['error'] },
]

const stacksForAvailability = computed(() =>
  (stacksAggregate.value || []).map((s: any) => ({ ...s, status: stackFleetStatus(s, workersById.value) }))
)

const workerOptions = computed(() => {
  const items = [{ label: 'All workers', value: 'all' }]
  for (const w of workers.value || []) {
    items.push({ label: w.hostname || w.id, value: w.id })
  }
  return items
})

const groupOptions = computed(() => {
  const groups = new Set((stacksAggregate.value || []).map((s: any) => s.group).filter(Boolean))
  const items = [{ label: 'All groups', value: GROUP_ALL }]
  if ((stacksAggregate.value || []).some((s: any) => !s.group)) {
    items.push({ label: 'Ungrouped', value: GROUP_UNGROUPED })
  }
  for (const g of [...groups].sort()) {
    items.push({ label: g as string, value: encodeGroupValue(g as string) })
  }
  return items
})

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

const stackActionItems = [
  [
    { label: 'Import', icon: 'i-lucide-package-plus', onSelect: () => { showImport.value = true } },
    { label: 'Manage Orphans', icon: 'i-lucide-package-search', color: 'warning', onSelect: () => openOrphans() },
  ],
]

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
          <span>
            <span class="block">Stacks</span>
            <span class="block text-sm font-normal text-gray-500 dark:text-wire-200/60">Docker Compose deployments synced from Git and dispatched to workers.</span>
          </span>
        </h1>
        <div v-if="isUpdating" class="flex items-center gap-2 text-sm text-gray-500" role="status" aria-live="polite">
          <UIcon name="i-lucide-loader-2" class="w-4 h-4 animate-spin" />
          <span class="hidden sm:inline">Updating...</span>
        </div>
      </div>
      <div v-if="!isViewer" class="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
        <ActionButton icon="i-lucide-plus" label="Add Stack" class="w-full justify-center sm:w-auto" @click="openCreate()" />
        <UButton icon="i-lucide-wrench" label="Stack Builder" variant="outline" class="w-full justify-center sm:w-auto" @click="showBuilder = true" />
        <UDropdownMenu :items="stackActionItems" :content="{ align: 'end' }">
          <UButton icon="i-lucide-ellipsis-vertical" label="Options" variant="outline" class="w-full justify-center sm:w-auto" aria-label="Stack options" />
        </UDropdownMenu>
      </div>
    </div>

    <AppPanelCard>
      <template #header>
        <div class="flex items-center justify-between">
          <h3 class="font-semibold text-gray-900 dark:text-wire-200">
            Stacks
            <span v-if="totalStacks" class="ml-1.5 text-yellow-400">({{ totalStacks }})</span>
          </h3>
          <div class="flex items-center gap-3">
            <RefreshButton @click="refreshList()" />
          </div>
        </div>
      </template>

      <div v-if="stacksAggregate?.length" class="space-y-4">
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
              { label: 'Pending', value: 'pending' },
              { label: 'Degraded', value: 'degraded' }
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
          <AppSelectInput
            v-if="groupOptions.length > 1"
            v-model="groupFilter"
            :items="groupOptions"
            placeholder="Filter by group"
            content-width
            class="sm:min-w-28"
            aria-label="Filter stacks by group"
          />
          <AppSelectInput
            v-if="workerOptions.length > 1"
            v-model="workerFilter"
            :items="workerOptions"
            placeholder="Filter by worker"
            content-width
            class="sm:min-w-28"
            aria-label="Filter stacks by worker"
          />
        </div>

        <StatusAvailabilityBar
          v-model="statusFilter"
          :items="stacksForAvailability"
          :segments="stackStatusSegments"
          aria-label="Stack status availability breakdown"
        />

        <div v-if="!stacksLoading && totalStacks === 0" class="text-center py-12" role="status" aria-live="polite">
          <UIcon name="i-lucide-search-x" class="w-12 h-12 text-gray-300 mx-auto mb-4" />
          <p class="text-gray-500">No stacks found</p>
          <p class="text-xs text-gray-400 mt-1">Try adjusting your search or filters</p>
        </div>

        <template v-else>
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3">
            <StackCard
              v-for="stack in stacks"
              :key="stack.id"
              :stack="stack"
              :workers-by-id="workersById"
            />
          </div>

          <div v-if="totalPages > 1" class="flex justify-between items-center pt-2">
            <UPagination
              v-model="page"
              :total="totalStacks"
              :items-per-page="perPage"
            />
            <span class="text-xs text-gray-500">Page {{ page }} of {{ totalPages }}</span>
          </div>
        </template>
      </div>

      <EmptyState
        v-else
        icon="i-lucide-inbox"
        title="No stacks configured yet"
        :description="emptyStateStep.description"
        :cta-label="isViewer ? undefined : emptyStateStep.ctaLabel"
        @cta="emptyStateStep.action"
      />
    </AppPanelCard>

    <CreateStackModal v-model:open="showCreate" @created="onCreated" />
    <StackBuilderModal v-model:open="showBuilder" :workers="workers || []" />
    <RepositoryCreateModal v-model:open="showCreateRepoFromEmpty" @created="refreshRepos" />

    <UModal v-model:open="showOrphans" title="Orphan Directories" description="Directories in the repos workspace not linked to any repository." :ui="{ content: 'bg-gray-50 dark:bg-(--ui-bg)' }">
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

    <UModal v-model:open="showDelete" title="Delete Stack" description="Are you sure you want to delete this stack?" :ui="{ content: 'bg-gray-50 dark:bg-(--ui-bg)' }">
      <template #body>
        <DeleteStackModal
          v-if="deleteTarget"
          :stack="deleteTarget"
          @deleted="onDeleted"
          @cancel="showDelete = false"
        />
      </template>
    </UModal>

    <UModal v-model:open="showImport" title="Import Compose Stack" description="Import an existing Docker Compose project into wireops" :ui="{ content: 'bg-gray-50 dark:bg-(--ui-bg)' }">
      <template #body>
        <ImportStackModal
          @imported="onImported"
          @cancel="showImport = false"
        />
      </template>
    </UModal>
  </div>
</template>
