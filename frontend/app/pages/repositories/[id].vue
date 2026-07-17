<script setup lang="ts">
const route = useRoute()
const { $pb } = useNuxtApp()
const { getRepoCommits } = useApi()
const { copy } = useCopy()
const { platformIconUrl, PLATFORM_OPTIONS } = useRepositoryPlatform()
const toast = useToast()

function platformLabel(value: string): string {
  return PLATFORM_OPTIONS.find(p => p.value === value)?.label ?? (value ? value.charAt(0).toUpperCase() + value.slice(1) : '-')
}

const repoId = route.params.id as string

const { data: repo, refresh: refreshRepo } = useAsyncData(`repo_${repoId}`, () =>
  $pb.collection('repositories').getOne(repoId, { expand: 'repository_key' })
)

// Edit repo — delegated to RepositoryCreateModal
const showEdit = ref(false)
const syncing = ref(false)

const commits = ref<{ sha: string; message: string; author: string; date: string }[]>([])
async function loadCommits() {
  try {
    commits.value = await getRepoCommits(repoId)
  } catch { commits.value = [] }
}
onMounted(() => loadCommits())

const { canManageRepos } = usePermissions()

// SOPS age key — auto-generated per repository, public key only (never the
// private key), with an explicit destructive-ish rotate action.
const confirmingRotate = ref(false)
const rotating = ref(false)

async function rotateSopsKey() {
  if (!confirmingRotate.value) {
    confirmingRotate.value = true
    return
  }
  rotating.value = true
  try {
    const { customPost } = useApi()
    await customPost(`/api/custom/repositories/${repoId}/sops-rotate-key`)
    toast.add({ title: 'SOPS age key rotated', description: 'Existing secrets.yaml files encrypted for the old key can no longer be decrypted — re-encrypt them with the new public key.', color: 'warning' })
    await refreshRepo()
  } catch (err: any) {
    toast.add({ title: 'Rotation failed', description: err.data?.error || err.message, color: 'error' })
  } finally {
    rotating.value = false
    confirmingRotate.value = false
  }
}

async function syncRepo() {
  if (!canManageRepos.value) {
    toast.add({ title: 'Permission denied', description: 'You do not have permission to sync this repository', color: 'red' })
    return
  }
  syncing.value = true
  try {
    const { customPost } = useApi()
    await customPost(`/api/custom/repositories/${repoId}/sync`)
    toast.add({ title: 'Repository synced successfully', color: 'green' })
    await refreshRepo()
    await loadCommits()
  } catch (err: any) {
    toast.add({ title: 'Sync failed', description: err.data?.error || err.message, color: 'red' })
  } finally {
    syncing.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-3">
        <UButton icon="i-lucide-arrow-left" variant="ghost" size="sm" to="/repositories" />
        <h1 class="flex items-center gap-3 text-2xl font-bold">
          <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10 shrink-0">
            <UIcon name="i-lucide-git-branch" class="w-5 h-5 text-yellow-400" />
          </div>
          {{ repo?.name }}
        </h1>
        <BadgeStatus v-if="repo" :status="repo.status" />
      </div>
    </div>

    <!-- Git Connection -->
    <UCard>
      <template #header>
        <div class="flex justify-between items-center">
          <h3 class="font-semibold">Git Connection</h3>
          <div class="flex gap-2">
            <UButton v-if="repo && canManageRepos" icon="i-lucide-refresh-cw" variant="ghost" size="xs" :loading="syncing" aria-label="Sync repository" title="Sync repository" @click="syncRepo" />
            <UButton v-if="repo && canManageRepos" icon="i-lucide-pencil" variant="ghost" size="xs" aria-label="Edit repository" title="Edit repository" @click="showEdit = true" />
          </div>
        </div>
      </template>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
        <div class="flex items-center gap-1.5">
          <span class="text-gray-500">Platform:</span>
          <img
            v-if="repo?.platform && platformIconUrl(repo.platform)"
            :src="platformIconUrl(repo.platform)!"
            class="w-4 h-4 object-contain shrink-0"
            alt=""
          >
          <span>{{ repo?.platform ? platformLabel(repo.platform) : '-' }}</span>
        </div>
        <div><span class="text-gray-500">Git URL:</span> <span class="font-mono">{{ repo?.git_url }}</span></div>
        <div><span class="text-gray-500">Branch:</span> {{ repo?.branch || 'main' }}</div>
        <div><span class="text-gray-500">Repository Key:</span> {{ repo?.expand?.repository_key?.name || 'None (public)' }}</div>
        <div><span class="text-gray-500">Last SHA:</span> <span class="font-mono">{{ repo?.last_commit_sha?.slice(0, 7) || '-' }}</span></div>
        <div><span class="text-gray-500">Last Fetched:</span> {{ repo?.last_fetched_at ? new Date(repo.last_fetched_at).toLocaleString() : 'Never' }}</div>
      </div>
    </UCard>

    <!-- SOPS -->
    <UCard>
      <template #header>
        <div class="flex justify-between items-center">
          <h3 class="font-semibold">SOPS</h3>
          <UBadge variant="subtle" color="primary" size="sm">Always active</UBadge>
        </div>
      </template>
      <div class="space-y-3 text-sm">
        <p class="text-gray-500">
          Drop a <code class="font-mono">secrets.yaml</code> next to <code class="font-mono">wireops.yaml</code> in this
          repository, encrypted with the age public key below. Values decrypted from it override env vars set in the UI.
        </p>
        <div class="flex items-center gap-2">
          <span class="text-gray-500 shrink-0">Public key:</span>
          <code class="font-mono text-xs bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded break-all">{{ repo?.sops_age_public_key || '-' }}</code>
          <UButton
            v-if="repo?.sops_age_public_key"
            icon="i-lucide-copy"
            variant="ghost"
            size="xs"
            aria-label="Copy public key"
            title="Copy public key"
            @click="copy(repo!.sops_age_public_key, 'Public key')"
          />
        </div>
        <div v-if="canManageRepos" class="flex items-center gap-2 pt-1">
          <UButton
            :color="confirmingRotate ? 'error' : 'neutral'"
            variant="outline"
            size="xs"
            icon="i-lucide-refresh-ccw"
            :loading="rotating"
            @click="rotateSopsKey"
          >
            {{ confirmingRotate ? 'Confirm rotate (breaks existing secrets.yaml)' : 'Rotate key' }}
          </UButton>
          <UButton v-if="confirmingRotate" variant="ghost" size="xs" @click="confirmingRotate = false">Cancel</UButton>
        </div>
      </div>
    </UCard>

    <!-- Recent Commits -->
    <UCard>
      <template #header>
        <div class="flex justify-between items-center">
          <h3 class="font-semibold">Recent Commits</h3>
          <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" @click="loadCommits" />
        </div>
      </template>
      <div v-if="commits.length" class="divide-y divide-gray-200 dark:divide-gray-800">
        <div v-for="c in commits" :key="c.sha" class="py-2 space-y-1">
          <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1">
            <div class="flex items-center gap-2 min-w-0">
              <button
                class="font-mono text-xs bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded shrink-0 hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors cursor-pointer"
                :title="`Copy ${c.sha}`"
                @click="copy(c.sha, 'Commit SHA')"
              >
                {{ c.sha.slice(0, 7) }}
              </button>
              <span class="text-sm truncate">{{ c.message }}</span>
            </div>
            <span class="text-xs text-gray-400 whitespace-nowrap shrink-0">{{ new Date(c.date).toLocaleString() }}</span>
          </div>
          <div class="text-xs text-gray-400 flex items-center gap-1">
            <UIcon name="i-lucide-user" class="w-3 h-3" />
            {{ c.author }}
          </div>
        </div>
      </div>
      <p v-else class="text-sm text-gray-500 py-2">No commits available. Repository may not be cloned yet.</p>
    </UCard>

    <!-- Edit Repository Modal -->
    <RepositoryCreateModal
      v-model:open="showEdit"
      :repository="repo ?? undefined"
      @updated="refreshRepo"
    />
  </div>
</template>
