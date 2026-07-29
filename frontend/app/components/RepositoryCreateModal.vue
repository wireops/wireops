<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { AUTH_TYPE } from '~/constants/repositoryAuth'

const { PLATFORM_OPTIONS, platformIconUrl } = useRepositoryPlatform()
const { $pb } = useNuxtApp()
const { testCredentials, listGitProviders, listGitProviderOrgs, listGitProviderRepos, listGitProviderBranches } = useApi()
const toast = useToast()
const { announce } = useA11yAnnouncer()

const isOpen = defineModel<boolean>('open', { default: false })
const props = defineProps<{ repository?: Record<string, any> }>()
const emit = defineEmits<{
  (e: 'created' | 'updated'): void
}>()

const isEditMode = computed(() => !!props.repository)
const form = ref({ name: '', git_url: '', branch: 'main', platform: 'github', repository_key: '' })
const keys = ref<Record<string, any>[]>([])
const urlScheme = ref<'http' | 'ssh'>('http')
const gitUrlError = ref('')
const isPrivate = ref(false)
const saving = ref(false)
const testingConnection = ref(false)
const showCreateKey = ref(false)
const initializing = ref(false)

const gitProviders = ref<{ slug: string, name: string, configured: boolean }[]>([])
const connectedProviderSlug = ref('')
const orgs = ref<{ login: string, avatar_url?: string }[]>([])
const selectedOrg = ref('')
const repos = ref<{ full_name: string, name: string, owner: string, private: boolean, default_branch: string, clone_url: string }[]>([])
const selectedRepo = ref('')
const branches = ref<{ name: string }[]>([])
const loadingRepos = ref(false)
const loadingBranches = ref(false)

const orgOptions = computed(() => [
  { label: 'My repositories', value: '' },
  ...orgs.value.map(org => ({ label: org.login, value: org.login })),
])
const repoOptions = computed(() => repos.value.map(repo => ({ label: repo.full_name, value: repo.full_name })))
const branchOptions = computed(() => branches.value.map(branch => ({ label: branch.name, value: branch.name })))

async function loadGitProviders() {
  try {
    gitProviders.value = await listGitProviders()
  } catch {
    gitProviders.value = []
  }
}

// activateConnection wires up a (possibly already-connected) provider key:
// the org/repo/branch pickers, isPrivate, and the lock on the manual
// "Repository Key" picker all key off connectedProviderSlug. Shared by a
// fresh "Connect" click and by auto-detecting an existing global connection
// on open, so both paths behave identically.
async function activateConnection(slug: string, keyId: string) {
  form.value.repository_key = keyId
  isPrivate.value = true
  connectedProviderSlug.value = slug
  selectedOrg.value = ''
  selectedRepo.value = ''
  repos.value = []
  branches.value = []
  try {
    orgs.value = await listGitProviderOrgs(slug, keyId)
  } catch {
    orgs.value = []
  }
  await loadReposForOrg()
}

// Every repository_keys row for a given provider is the same global,
// reusable credential (the backend upserts on connect/reconnect instead of
// creating a new row per click) — so if one already exists, reuse it
// instead of forcing another OAuth round trip.
function findConnectedKey(slug: string) {
  return keys.value.find(key => key.auth_type === AUTH_TYPE.OAUTH_TOKEN && key.oauth_provider === slug)
}

async function handleConnected(slug: string, result: { keyId: string, login: string }) {
  await loadKeys()
  await activateConnection(slug, result.keyId)
}

async function loadReposForOrg() {
  if (!connectedProviderSlug.value || !form.value.repository_key) return
  loadingRepos.value = true
  selectedRepo.value = ''
  branches.value = []
  try {
    repos.value = await listGitProviderRepos(connectedProviderSlug.value, form.value.repository_key, selectedOrg.value)
  } catch (error: any) {
    repos.value = []
    toast.add({ title: 'Failed to list repositories', description: describePocketBaseError(error), color: 'error' })
  } finally {
    loadingRepos.value = false
  }
}

async function selectRepo(fullName: string) {
  selectedRepo.value = fullName
  const repo = repos.value.find(r => r.full_name === fullName)
  if (!repo) return
  form.value.git_url = repo.clone_url
  form.value.branch = repo.default_branch
  if (!form.value.name.trim()) form.value.name = repo.name

  loadingBranches.value = true
  try {
    branches.value = await listGitProviderBranches(connectedProviderSlug.value, form.value.repository_key, fullName)
  } catch {
    branches.value = []
  } finally {
    loadingBranches.value = false
  }
}

const urlPlaceholder = computed(() =>
  urlScheme.value === 'ssh' ? 'git@github.com:user/repo.git' : 'https://github.com/user/repo.git'
)
const requiredKeyType = computed(() => urlScheme.value === 'ssh' ? AUTH_TYPE.SSH_KEY : AUTH_TYPE.BASIC)
const compatibleKeys = computed(() => keys.value.filter(key => key.auth_type === requiredKeyType.value))
const keyOptions = computed(() => compatibleKeys.value.map(key => ({
  label: key.name,
  value: key.id,
  description: key.auth_type === AUTH_TYPE.SSH_KEY ? 'SSH key' : key.git_username || 'Username / password',
})))

function inferUrlScheme(url: string): 'http' | 'ssh' | '' {
  const trimmed = url.trim()
  if (trimmed.startsWith('git@') || trimmed.startsWith('ssh://')) return 'ssh'
  if (/^https?:\/\//.test(trimmed)) return 'http'
  return ''
}

function applyInferredUrlScheme() {
  const inferred = inferUrlScheme(form.value.git_url)
  if (!inferred || inferred === urlScheme.value) return
  urlScheme.value = inferred
  gitUrlError.value = ''
  form.value.repository_key = ''
  isPrivate.value = inferred === 'ssh'
}

async function loadKeys(selectID?: string) {
  keys.value = await $pb.collection('repository_keys').getFullList({ sort: 'name' })
  if (selectID) form.value.repository_key = selectID
}

watch(isOpen, async (open) => {
  if (!open) return
  initializing.value = true
  gitUrlError.value = ''
  connectedProviderSlug.value = ''
  orgs.value = []
  repos.value = []
  branches.value = []
  selectedOrg.value = ''
  selectedRepo.value = ''
  await loadGitProviders()
  await loadKeys()
  const repository = props.repository
  if (repository) {
    form.value = {
      name: repository.name || '',
      git_url: repository.git_url || '',
      branch: repository.branch || 'main',
      platform: repository.platform || 'github',
      repository_key: repository.repository_key || '',
    }
    urlScheme.value = inferUrlScheme(repository.git_url || '') || 'http'
    isPrivate.value = urlScheme.value === 'ssh' || !!repository.repository_key
  } else {
    form.value = { name: '', git_url: '', branch: 'main', platform: 'github', repository_key: '' }
    urlScheme.value = 'http'
    isPrivate.value = false

    // New repository: if a provider is already globally connected, jump
    // straight to its org/repo picker instead of making the user click
    // "Connect" again for every repo they add.
    const configuredProvider = gitProviders.value.find(p => p.configured)
    const existingKey = configuredProvider ? findConnectedKey(configuredProvider.slug) : undefined
    if (configuredProvider && existingKey) {
      await activateConnection(configuredProvider.slug, existingKey.id)
    }
  }
  await nextTick()
  initializing.value = false
})

watch(urlScheme, (scheme) => {
  if (initializing.value) return
  gitUrlError.value = ''
  form.value.repository_key = ''
  connectedProviderSlug.value = ''
  isPrivate.value = scheme === 'ssh'
})
watch(isPrivate, (enabled) => {
  if (!enabled && urlScheme.value === 'http') {
    form.value.repository_key = ''
    connectedProviderSlug.value = ''
  }
})
watch(() => form.value.git_url, () => {
  gitUrlError.value = ''
  if (!initializing.value) applyInferredUrlScheme()
})

function describePocketBaseError(error: any): string {
  const data = error?.response?.data
  if (data && typeof data === 'object') {
    for (const value of Object.values(data)) {
      const message = (value as any)?.message
      if (typeof message === 'string' && message.trim()) return message
    }
  }
  return error?.response?.message || error?.message || 'Unknown error'
}

function validateGitUrl(): string {
  applyInferredUrlScheme()
  const url = form.value.git_url.trim()
  if (!url) return 'Git URL is required'
  if (urlScheme.value === 'http' && !/^https?:\/\//.test(url))
    return 'URL must start with http:// or https://'
  if (urlScheme.value === 'ssh' && !url.startsWith('git@') && !url.startsWith('ssh://'))
    return 'URL must start with git@ or ssh://'
  return ''
}

function validateForm(): string {
  const urlError = validateGitUrl()
  if (urlError) return urlError
  if (!form.value.name.trim()) return 'Name is required'
  if ((urlScheme.value === 'ssh' || isPrivate.value) && !form.value.repository_key)
    return 'Select a repository key'
  return ''
}

async function testConnection() {
  const error = validateForm()
  if (error) {
    gitUrlError.value = validateGitUrl()
    toast.add({ title: 'Cannot test connection', description: error, color: 'error' })
    return
  }
  testingConnection.value = true
  try {
    const result = await testCredentials({
      git_url: form.value.git_url,
      repository_key_id: form.value.repository_key || '',
      auth_type: form.value.repository_key ? requiredKeyType.value : AUTH_TYPE.NONE,
    })
    if (result.success === 'true') {
      toast.add({ title: 'Connection successful!', color: 'success' })
      announce('Repository connection test succeeded')
    } else {
      toast.add({ title: 'Connection failed', description: result.error, color: 'error' })
    }
  } catch (error: any) {
    toast.add({ title: 'Test failed', description: describePocketBaseError(error), color: 'error' })
  } finally {
    testingConnection.value = false
  }
}

async function submit() {
  const error = validateForm()
  if (error) {
    gitUrlError.value = validateGitUrl()
    toast.add({ title: 'Invalid repository', description: error, color: 'error' })
    return
  }
  saving.value = true
  try {
    const payload = {
      name: form.value.name.trim(),
      git_url: form.value.git_url.trim(),
      branch: form.value.branch || 'main',
      platform: form.value.platform,
      repository_key: form.value.repository_key || '',
    }
    if (props.repository) {
      await $pb.collection('repositories').update(props.repository.id, payload)
      toast.add({ title: 'Repository updated', color: 'success' })
      emit('updated')
    } else {
      await $pb.collection('repositories').create({ ...payload, status: 'connected' })
      toast.add({ title: 'Repository created', color: 'success' })
      emit('created')
    }
    isOpen.value = false
  } catch (error: any) {
    toast.add({
      title: props.repository ? 'Failed to update repository' : 'Failed to create repository',
      description: describePocketBaseError(error),
      color: 'error',
    })
  } finally {
    saving.value = false
  }
}

async function handleKeySaved(key: Record<string, any>) {
  await loadKeys(key.id)
  isPrivate.value = true
}
</script>

<template>
  <UModal v-model:open="isOpen" scrollable :ui="{ content: 'sm:max-w-2xl w-full' }">
    <template #content>
      <UCard class="sm:min-w-[640px] w-full">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-git-branch" class="w-5 h-5 text-gray-500" />
            <h2 class="font-semibold">{{ isEditMode ? 'Edit Repository' : 'Add Repository' }}</h2>
          </div>
        </template>

        <form class="space-y-4" @submit.prevent="submit">
          <UFormField label="Name" required>
            <AppTextInput v-model="form.name" placeholder="my-app" />
          </UFormField>

          <div class="flex items-end gap-3">
            <UFormField label="Platform" required class="flex-1">
              <USelectMenu v-model="form.platform" :items="PLATFORM_OPTIONS" value-key="value" class="w-full" :search-input="false">
                <template #leading>
                  <img v-if="platformIconUrl(form.platform)" :src="platformIconUrl(form.platform)!" class="w-4 h-4 object-contain" alt="">
                </template>
                <template #item-leading="{ item }">
                  <img v-if="platformIconUrl(item.value)" :src="platformIconUrl(item.value)!" class="w-4 h-4 object-contain" alt="">
                </template>
              </USelectMenu>
            </UFormField>
            <UFormField label="Protocol">
              <URadioGroup
                v-model="urlScheme"
                :items="[{ label: 'HTTP', value: 'http' }, { label: 'SSH', value: 'ssh' }]"
                orientation="horizontal"
              />
            </UFormField>
          </div>

          <div v-if="urlScheme === 'http' && gitProviders.some(p => p.configured)" class="flex flex-wrap items-center gap-2">
            <template v-for="provider in gitProviders.filter(p => p.configured)" :key="provider.slug">
              <UBadge v-if="connectedProviderSlug === provider.slug" color="success" variant="soft" class="py-1.5">
                Connected to {{ provider.name }}
              </UBadge>
              <ConnectGithubButton v-else-if="provider.slug === 'github'" @connected="result => handleConnected(provider.slug, result)" />
            </template>
          </div>

          <template v-if="connectedProviderSlug">
            <div class="flex items-end gap-3">
              <UFormField label="Organization" class="flex-1">
                <USelectMenu
                  v-model="selectedOrg"
                  :items="orgOptions"
                  value-key="value"
                  class="w-full"
                  @update:model-value="loadReposForOrg"
                />
              </UFormField>
              <UFormField label="Repository" required class="flex-1">
                <USelectMenu
                  v-model="selectedRepo"
                  :items="repoOptions"
                  value-key="value"
                  placeholder="Select a repository"
                  :loading="loadingRepos"
                  class="w-full"
                  @update:model-value="selectRepo"
                />
              </UFormField>
            </div>
          </template>

          <UFormField label="Git URL" required :error="gitUrlError">
            <AppTextInput v-model="form.git_url" :placeholder="urlPlaceholder" />
          </UFormField>

          <UFormField label="Branch">
            <USelectMenu
              v-if="connectedProviderSlug && branchOptions.length > 0"
              v-model="form.branch"
              :items="branchOptions"
              value-key="value"
              :loading="loadingBranches"
              class="w-full"
            />
            <AppTextInput v-else v-model="form.branch" placeholder="main" />
          </UFormField>

          <div v-if="urlScheme === 'http'" class="flex items-center gap-2">
            <UCheckbox v-model="isPrivate" label="Private Repository" />
            <span class="text-xs text-gray-500">Public repositories do not need a key.</span>
          </div>

          <div v-if="(urlScheme === 'ssh' || isPrivate) && !connectedProviderSlug" class="flex items-end gap-2">
            <UFormField label="Repository Key" required class="flex-1">
              <USelectMenu
                v-model="form.repository_key"
                :items="keyOptions"
                value-key="value"
                placeholder="Select a reusable key"
                class="w-full"
              />
            </UFormField>
            <UButton
              label="New Key"
              icon="i-lucide-plus"
              variant="outline"
              color="neutral"
              @click="showCreateKey = true"
            />
          </div>
          <p v-if="(urlScheme === 'ssh' || isPrivate) && !connectedProviderSlug && compatibleKeys.length === 0" class="text-xs text-amber-600 dark:text-amber-400">
            No compatible keys yet. Create one to continue.
          </p>

          <div class="flex justify-between items-center pt-4 border-t border-gray-100 dark:border-gray-800">
            <UButton
              label="Test Connection"
              icon="i-lucide-plug"
              variant="outline"
              color="neutral"
              :loading="testingConnection"
              @click="testConnection"
            />
            <div class="flex gap-2">
              <CancelButton @click="isOpen = false" />
              <UButton type="submit" :label="isEditMode ? 'Save' : 'Create'" :loading="saving" />
            </div>
          </div>
        </form>
      </UCard>
    </template>
  </UModal>

  <RepositoryKeyModal
    v-model:open="showCreateKey"
    :default-auth-type="requiredKeyType"
    :git-url="form.git_url"
    @saved="handleKeySaved"
  />
</template>
