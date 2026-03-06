<script setup lang="ts">
const route = useRoute()
const { $pb } = useNuxtApp()
const { getRepoCommits, testCredentials } = useApi()
const { copy } = useCopy()
const toast = useToast()

const repoId = route.params.id as string

const { data: repo, refresh: refreshRepo } = useAsyncData(`repo_${repoId}`, () =>
  $pb.collection('repositories').getOne(repoId)
)

const { data: cred, refresh: refreshCred } = useAsyncData(`cred_${repoId}`, () =>
  $pb.collection('repository_keys').getFullList({
    filter: `repository = "${repoId}"`,
  })
)

// Credentials edit mode
const editingCred = ref(false)
function startEditCred() {
  editingCred.value = true
}
function cancelEditCred() {
  editingCred.value = false
  // Reload credentials from saved state
  if (cred.value?.length) {
    const c = cred.value[0]
    credForm.value = {
      auth_type: c.auth_type || 'none',
      ssh_private_key: '',
      ssh_passphrase: '',
      ssh_known_host: c.ssh_known_host || '',
      git_username: c.git_username || '',
      git_password: '',
    }
  }
}

// Edit repo
const editing = ref(false)
const editForm = ref<any>({})
function startEdit() {
  editForm.value = { ...repo.value }
  editing.value = true
}
async function saveEdit() {
  await $pb.collection('repositories').update(repoId, {
    name: editForm.value.name,
    git_url: editForm.value.git_url,
    branch: editForm.value.branch,
  })
  editing.value = false
  refreshRepo()
}

// Credentials form
const credForm = ref({
  auth_type: 'none',
  ssh_private_key: '',
  ssh_passphrase: '',
  ssh_known_host: '',
  git_username: '',
  git_password: '',
})
const savingCred = ref(false)
const testingConnection = ref(false)
const testResult = ref<{ success: boolean; error?: string } | null>(null)

watch(cred, (val) => {
  if (val?.length) {
    const c = val[0]
    credForm.value = {
      auth_type: c.auth_type || 'none',
      ssh_private_key: '',
      ssh_passphrase: '',
      ssh_known_host: c.ssh_known_host || '',
      git_username: c.git_username || '',
      git_password: '',
    }
  }
}, { immediate: true })

async function testConnection() {
  testingConnection.value = true
  testResult.value = null
  try {
    const result = await testCredentials({
      repository_id: repoId,
      git_url: repo.value?.git_url,
      auth_type: credForm.value.auth_type,
      ssh_private_key: credForm.value.ssh_private_key,
      ssh_passphrase: credForm.value.ssh_passphrase,
      ssh_known_host: credForm.value.ssh_known_host,
      git_username: credForm.value.git_username,
      git_password: credForm.value.git_password,
    })
    testResult.value = { success: result.success === 'true', error: result.error }
    if (result.success === 'true') {
      toast.add({ title: 'Connection successful!', color: 'success' })
    } else {
      toast.add({ title: 'Connection failed', description: result.error, color: 'error' })
    }
  } catch (error: any) {
    testResult.value = { success: false, error: error.message }
    toast.add({ title: 'Test failed', description: error.message, color: 'error' })
  } finally {
    testingConnection.value = false
  }
}

async function saveCred() {
  savingCred.value = true
  try {
    const payload: any = { ...credForm.value, repository: repoId }
    if (!payload.ssh_private_key) delete payload.ssh_private_key
    if (!payload.ssh_passphrase) delete payload.ssh_passphrase
    if (!payload.git_password) delete payload.git_password

    if (cred.value?.length) {
      await $pb.collection('repository_keys').update(cred.value[0].id, payload)
    } else {
      await $pb.collection('repository_keys').create(payload)
    }
    toast.add({ title: 'Credentials saved', color: 'success' })
    testResult.value = null // Clear test result after save
    editingCred.value = false
    refreshCred()
  } finally {
    savingCred.value = false
  }
}

const commits = ref<{ sha: string; message: string; author: string; date: string }[]>([])
async function loadCommits() {
  try {
    commits.value = await getRepoCommits(repoId)
  } catch { commits.value = [] }
}
onMounted(() => loadCommits())

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
          <UButton v-if="!editing" icon="i-lucide-pencil" variant="ghost" size="xs" @click="startEdit" />
        </div>
      </template>
      <div v-if="!editing" class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
        <div><span class="text-gray-500">Git URL:</span> <span class="font-mono">{{ repo?.git_url }}</span></div>
        <div><span class="text-gray-500">Branch:</span> {{ repo?.branch || 'main' }}</div>
        <div><span class="text-gray-500">Last SHA:</span> <span class="font-mono">{{ repo?.last_commit_sha?.slice(0, 7) || '-' }}</span></div>
        <div><span class="text-gray-500">Last Fetched:</span> {{ repo?.last_fetched_at ? new Date(repo.last_fetched_at).toLocaleString() : 'Never' }}</div>
      </div>
      <form v-else class="grid grid-cols-1 sm:grid-cols-2 gap-4" @submit.prevent="saveEdit">
        <UFormField label="Name"><UInput v-model="editForm.name" /></UFormField>
        <UFormField label="Git URL"><UInput v-model="editForm.git_url" /></UFormField>
        <UFormField label="Branch"><UInput v-model="editForm.branch" /></UFormField>
        <div class="col-span-2 flex justify-end gap-2">
          <UButton label="Cancel" variant="outline" @click="editing = false" />
          <UButton type="submit" label="Save" />
        </div>
      </form>
    </UCard>

    <!-- Access (Credentials) -->
    <UCard>
      <template #header>
        <div class="flex justify-between items-center">
          <h3 class="font-semibold">Access</h3>
          <div class="flex gap-2">
            <UButton 
              icon="i-lucide-plug" 
              variant="ghost" 
              size="xs" 
              :loading="testingConnection" 
              title="Test Connection"
              @click="testConnection"
            />
            <UButton v-if="!editingCred" icon="i-lucide-pencil" variant="ghost" size="xs" @click="startEditCred" />
          </div>
        </div>
      </template>
      
      <div v-if="!editingCred" class="space-y-4">
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
          <div><span class="text-gray-500">Auth Type:</span> {{ credForm.auth_type === 'none' ? 'None' : credForm.auth_type === 'ssh_key' ? 'SSH Key' : 'Username / Password' }}</div>
          <div v-if="credForm.auth_type === 'basic'"><span class="text-gray-500">Username:</span> {{ credForm.git_username || '-' }}</div>
          <div v-if="credForm.auth_type === 'ssh_key'" class="sm:col-span-2"><span class="text-gray-500">SSH Key:</span> {{ cred?.length && cred[0].ssh_private_key ? 'Configured' : 'Not set' }}</div>
        </div>
        <UAlert v-if="testResult" :color="testResult.success ? 'success' : 'error'" :icon="testResult.success ? 'i-lucide-check-circle' : 'i-lucide-x-circle'" :title="testResult.success ? 'Connection successful' : 'Connection failed'" :description="testResult.error" />
      </div>

      <form v-else class="flex flex-col gap-4" @submit.prevent="saveCred">
        <UFormField label="Auth Type">
          <USelect
v-model="credForm.auth_type" :items="[
            { label: 'None', value: 'none' },
            { label: 'SSH Key', value: 'ssh_key' },
            { label: 'Username / Password', value: 'basic' },
          ]" />
        </UFormField>

        <template v-if="credForm.auth_type === 'ssh_key'">
          <UFormField label="SSH Private Key">
            <UTextarea v-model="credForm.ssh_private_key" placeholder="Paste private key (leave empty to keep current)" rows="4" class="font-mono text-xs" />
          </UFormField>
          <UFormField label="Passphrase">
            <UInput v-model="credForm.ssh_passphrase" type="password" placeholder="Leave empty to keep current" />
          </UFormField>
          <UFormField label="Known Host">
            <UInput v-model="credForm.ssh_known_host" placeholder="github.com ssh-ed25519 AAAA..." class="font-mono text-xs" />
          </UFormField>
        </template>

        <template v-if="credForm.auth_type === 'basic'">
          <UFormField label="Username">
            <UInput v-model="credForm.git_username" />
          </UFormField>
          <UFormField label="Password / Token">
            <UInput v-model="credForm.git_password" type="password" placeholder="Leave empty to keep current" />
          </UFormField>
        </template>

        <UAlert v-if="testResult" :color="testResult.success ? 'success' : 'error'" :icon="testResult.success ? 'i-lucide-check-circle' : 'i-lucide-x-circle'" :title="testResult.success ? 'Connection successful' : 'Connection failed'" :description="testResult.error" />

        <div class="flex justify-end gap-2">
          <UButton label="Cancel" variant="outline" @click="cancelEditCred" />
          <UButton type="submit" label="Save" :loading="savingCred" />
        </div>
      </form>
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
  </div>
</template>
