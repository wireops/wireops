<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { AUTH_TYPE } from '~/constants/repositoryAuth'

const { PLATFORM_OPTIONS, platformIconUrl } = useRepositoryPlatform()
const { $pb } = useNuxtApp()
const { testCredentials, listGitProviders, listGitProviderOrgs, listGitProviderRepos, listGitProviderBranches } = useApi()
const { connect: connectProvider } = useGitProviderOAuth()
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

// Wizard step: when at least one SCM integration (GitHub, future
// Gitea/Forgejo, ...) is configured on the server, new repositories start
// on a "Source" step with a big-card picker (one card per configured
// provider, plus a "Generic" card for a manual git URL); "Details" then
// collects name/branch/access. With no integrations configured, there's
// nothing to pick between, so it's skipped straight to "Details".
const step = ref<'select' | 'form'>('form')
const connectingSlug = ref('')
const availableProviders = computed(() => gitProviders.value.filter(p => p.configured))
const orgs = ref<{ login: string, avatar_url?: string }[]>([])
const selectedOrg = ref('')
const repos = ref<{ full_name: string, name: string, owner: string, private: boolean, default_branch: string, clone_url: string }[]>([])
const selectedRepo = ref('')
const branches = ref<{ name: string }[]>([])
const loadingOrgs = ref(false)
const loadingRepos = ref(false)
const loadingBranches = ref(false)

const orgOptions = computed(() => [
  { label: 'My repositories', value: '' },
  ...orgs.value.map(org => ({ label: org.login, value: org.login })),
])
const hasSingleOrg = computed(() => orgOptions.value.length === 1)
const repoOptions = computed(() => repos.value.map(repo => ({ label: repo.full_name, value: repo.full_name })))
const branchOptions = computed(() => branches.value.map(branch => ({ label: branch.name, value: branch.name })))

// Steps before the current one get a green check instead of their own icon —
// same convention as CreateStackModal's stepper.
const greenTrigger = 'group-data-[state=completed]:bg-green-500 dark:group-data-[state=completed]:bg-green-600'
const greenToYellowSeparator = 'group-data-[state=completed]:bg-gradient-to-r group-data-[state=completed]:from-green-500 group-data-[state=completed]:to-yellow-500 dark:group-data-[state=completed]:from-green-600'

const stepperItems = computed(() => [
  {
    title: 'Source',
    description: availableProviders.value.length > 0 ? 'Provider or Git URL' : 'Git URL',
    icon: step.value === 'form' ? 'i-lucide-check' : 'i-lucide-git-branch',
    // "Details" is the current step, never fully "completed" while this
    // modal is open, so the separator between them always fades from the
    // completed green into the active yellow — same convention as
    // CreateStackModal's stepper.
    ui: step.value === 'form' ? { trigger: greenTrigger, separator: greenToYellowSeparator } : undefined,
  },
  {
    title: 'Details',
    description: 'Name, branch & access',
    icon: 'i-lucide-settings',
    // Only reachable via a card pick in the Source step, not by clicking
    // ahead on the stepper itself.
    disabled: step.value === 'select',
  },
])

const activeStep = computed({
  get: () => (step.value === 'select' ? 0 : 1),
  set: (val: number) => {
    if (val === 0) backToProviderSelect()
  },
})

async function loadGitProviders() {
  try {
    gitProviders.value = await listGitProviders()
  } catch {
    gitProviders.value = []
  }
}

// beginConnection wires up a (possibly already-connected) provider key and
// switches to the "Details" step immediately — isPrivate, the org/repo
// pickers, and the lock on the manual "Repository Key" picker all key off
// connectedProviderSlug. The org/repo listing itself (loadOrgsAndRepos)
// happens after the step switch, behind loading skeletons, instead of
// blocking the transition: GitHub's API can take a few seconds here and a
// frozen card picker reads as "stuck", not "loading".
function beginConnection(slug: string, keyId: string) {
  form.value.repository_key = keyId
  form.value.platform = slug
  urlScheme.value = 'http'
  isPrivate.value = true
  connectedProviderSlug.value = slug
  selectedOrg.value = ''
  selectedRepo.value = ''
  orgs.value = []
  repos.value = []
  branches.value = []
  step.value = 'form'
}

async function loadOrgsAndRepos(slug: string, keyId: string) {
  loadingOrgs.value = true
  try {
    orgs.value = await listGitProviderOrgs(slug, keyId)
  } catch {
    orgs.value = []
  } finally {
    loadingOrgs.value = false
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

// Generic card: skip provider connection entirely, go to the plain
// git-url "Details" step.
function selectGeneric() {
  connectedProviderSlug.value = ''
  step.value = 'form'
}

// Provider card: reuse an existing global connection if there is one,
// otherwise kick off the OAuth popup for that provider's slug.
async function selectProvider(slug: string) {
  const existingKey = findConnectedKey(slug)
  if (existingKey) {
    beginConnection(slug, existingKey.id)
    await loadOrgsAndRepos(slug, existingKey.id)
    return
  }
  connectingSlug.value = slug
  try {
    const result = await connectProvider(slug)
    if (!result) {
      toast.add({ title: 'Connection cancelled or failed', color: 'warning' })
      return
    }
    await loadKeys()
    beginConnection(slug, result.keyId)
    await loadOrgsAndRepos(slug, result.keyId)
  } catch (error: any) {
    toast.add({ title: 'Failed to connect', description: error?.message || 'Unknown error', color: 'error' })
  } finally {
    connectingSlug.value = ''
  }
}

function backToProviderSelect() {
  form.value = { name: '', git_url: '', branch: 'main', platform: 'github', repository_key: '' }
  urlScheme.value = 'http'
  isPrivate.value = false
  connectedProviderSlug.value = ''
  orgs.value = []
  repos.value = []
  branches.value = []
  selectedOrg.value = ''
  selectedRepo.value = ''
  step.value = 'select'
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
  isPrivate.value = repo.private
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
    step.value = 'form'
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

    // No SCM integration configured at all: nothing to pick between, so
    // treat this as a plain generic git-url repository. Otherwise always
    // start on the card picker (GitHub / future Gitea / Forgejo / Generic)
    // and let the user choose — selectProvider() reuses an existing
    // connection under the hood once they pick a provider card.
    step.value = availableProviders.value.length === 0 ? 'form' : 'select'
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
  // Skip when a provider connection drives isPrivate off the selected
  // repo's visibility (selectRepo()) — only a manual uncheck of "Private
  // Repository" should drop the manually-picked key.
  if (connectedProviderSlug.value) return
  if (!enabled && urlScheme.value === 'http') {
    form.value.repository_key = ''
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
  if (step.value === 'select') return
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
      <form class="w-full" @submit.prevent="submit">
        <AppPanelCard class="sm:min-w-[640px] w-full" :ui="{ body: { base: 'p-6' }, header: { base: 'px-6 py-4' }, footer: { base: 'px-6 py-4' } }">
          <template #header>
            <div class="space-y-4">
              <div class="flex items-center gap-2">
                <UIcon name="i-lucide-git-branch" class="w-5 h-5 text-primary-500" />
                <h2 class="font-semibold text-lg">{{ isEditMode ? 'Edit Repository' : 'Add Repository' }}</h2>
              </div>
              <UStepper v-if="!isEditMode" v-model="activeStep" :items="stepperItems" class="w-full" />
            </div>
          </template>

          <div class="space-y-4">
            <div v-show="step === 'select'" class="space-y-4">
              <p class="text-sm text-gray-500">Where is this repository hosted?</p>
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <button
                  v-for="provider in availableProviders"
                  :key="provider.slug"
                  type="button"
                  class="flex flex-col items-center justify-center gap-3 rounded-lg border border-gray-300 dark:border-gray-800 p-6 text-center cursor-pointer hover:border-primary-500 hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors disabled:opacity-60 disabled:cursor-wait"
                  :disabled="!!connectingSlug"
                  @click="selectProvider(provider.slug)"
                >
                  <GithubIcon v-if="provider.slug === 'github'" icon-class="w-10 h-10 text-gray-700 dark:text-white" />
                  <img v-else-if="platformIconUrl(provider.slug)" :src="platformIconUrl(provider.slug)!" class="w-10 h-10 object-contain" alt="">
                  <UIcon v-else name="i-lucide-git-branch" class="w-10 h-10 text-gray-400" />
                  <span class="font-medium">{{ provider.name }}</span>
                  <span class="text-xs text-gray-500">{{ connectingSlug === provider.slug ? 'Connecting…' : 'Browse your organizations & repos' }}</span>
                </button>

                <button
                  type="button"
                  class="flex flex-col items-center justify-center gap-3 rounded-lg border border-gray-300 dark:border-gray-800 p-6 text-center cursor-pointer hover:border-primary-500 hover:bg-gray-50 dark:hover:bg-gray-900 transition-colors disabled:opacity-60 disabled:cursor-wait"
                  :disabled="!!connectingSlug"
                  @click="selectGeneric"
                >
                  <GenericIcon variant="git" icon-class="w-10 h-10 text-gray-400" />
                  <span class="font-medium">Generic</span>
                  <span class="text-xs text-gray-500">Any Git server via URL</span>
                </button>
              </div>
            </div>

            <div v-show="step === 'form'" class="space-y-4">
              <div class="flex items-end gap-3">
                <UFormField label="Name" required class="flex-1">
                  <AppTextInput v-model="form.name" placeholder="my-app" />
                </UFormField>
                <div
                  v-if="connectedProviderSlug"
                  class="flex items-center gap-2 rounded-lg border border-green-500/40 bg-green-500/10 px-3 py-1.5 shrink-0 mb-[1px]"
                >
                  <UIcon name="i-lucide-check" class="w-4 h-4 text-green-600 dark:text-green-400" />
                  <GithubIcon v-if="connectedProviderSlug === 'github'" icon-class="w-4 h-4 text-gray-700 dark:text-white" />
                  <img v-else-if="platformIconUrl(connectedProviderSlug)" :src="platformIconUrl(connectedProviderSlug)!" class="w-4 h-4 object-contain" alt="">
                  <UIcon v-else name="i-lucide-git-branch" class="w-4 h-4 text-gray-400" />
                  <span class="text-sm font-medium text-green-700 dark:text-green-400">{{ availableProviders.find(p => p.slug === connectedProviderSlug)?.name || connectedProviderSlug }}</span>
                </div>
              </div>

              <div v-if="!connectedProviderSlug" class="flex items-end gap-3">
                <UFormField label="Platform" required class="flex-1">
                  <AppSelectInput v-model="form.platform" :items="PLATFORM_OPTIONS" :searchable="false">
                    <template #leading="{ item }">
                      <GithubIcon v-if="item?.value === 'github'" icon-class="w-4 h-4 text-gray-700 dark:text-white" />
                      <img v-else-if="platformIconUrl(item?.value)" :src="platformIconUrl(item?.value)!" class="w-4 h-4 object-contain" alt="">
                    </template>
                    <template #item-leading="{ item }">
                      <GithubIcon v-if="item.value === 'github'" icon-class="w-4 h-4 text-gray-700 dark:text-white" />
                      <img v-else-if="platformIconUrl(item.value)" :src="platformIconUrl(item.value)!" class="w-4 h-4 object-contain" alt="">
                    </template>
                  </AppSelectInput>
                </UFormField>
                <UFormField label="Protocol">
                  <URadioGroup
                    v-model="urlScheme"
                    :items="[{ label: 'HTTP', value: 'http' }, { label: 'SSH', value: 'ssh' }]"
                    orientation="horizontal"
                  />
                </UFormField>
              </div>

              <template v-if="connectedProviderSlug">
                <div class="flex items-end gap-3">
                  <UFormField label="Organization" class="flex-1">
                    <USkeleton v-if="loadingOrgs" class="h-[38px] w-full rounded-lg" />
                    <AppSelectInput
                      v-else
                      :model-value="selectedOrg"
                      :items="orgOptions"
                      :disabled="hasSingleOrg"
                      @update:model-value="value => { selectedOrg = value; loadReposForOrg() }"
                    />
                  </UFormField>
                  <UFormField label="Repository" required class="flex-1">
                    <USkeleton v-if="loadingOrgs" class="h-[38px] w-full rounded-lg" />
                    <AppSelectInput
                      v-else
                      :model-value="selectedRepo"
                      :items="repoOptions"
                      placeholder="Select a repository"
                      :loading="loadingRepos"
                      @update:model-value="selectRepo"
                    />
                  </UFormField>
                </div>
              </template>

              <UFormField label="Git URL" required :error="gitUrlError">
                <AppTextInput v-model="form.git_url" :placeholder="urlPlaceholder" />
              </UFormField>

              <UFormField label="Branch">
                <AppSelectInput
                  v-if="connectedProviderSlug && branchOptions.length > 0"
                  v-model="form.branch"
                  :items="branchOptions"
                  :loading="loadingBranches"
                />
                <AppTextInput v-else v-model="form.branch" placeholder="main" />
              </UFormField>

              <div v-if="urlScheme === 'http' && !connectedProviderSlug" class="flex items-center gap-2">
                <UCheckbox v-model="isPrivate" label="Private Repository" />
                <span class="text-xs text-gray-500">Public repositories do not need a key.</span>
              </div>
              <UBadge v-if="connectedProviderSlug && selectedRepo" :color="isPrivate ? 'warning' : 'success'" variant="soft" class="py-1.5">
                {{ isPrivate ? 'Private repository' : 'Public repository' }}
              </UBadge>

              <div v-if="(urlScheme === 'ssh' || isPrivate) && !connectedProviderSlug" class="flex items-end gap-2">
                <UFormField label="Repository Key" required class="flex-1">
                  <AppSelectInput
                    v-model="form.repository_key"
                    :items="keyOptions"
                    placeholder="Select a reusable key"
                  />
                </UFormField>
                <ActionButton
                  label="New Key"
                  icon="i-lucide-plus"
                  variant="outline"
                  color="primary"
                  @click="showCreateKey = true"
                />
              </div>
              <p v-if="(urlScheme === 'ssh' || isPrivate) && !connectedProviderSlug && compatibleKeys.length === 0" class="text-xs text-amber-600 dark:text-amber-400">
                No compatible keys yet. Create one to continue.
              </p>
            </div>
          </div>

          <template #footer>
            <div class="flex justify-between items-center w-full gap-2">
              <CloseButton
                v-if="step === 'form' && !isEditMode"
                label="Back"
                variant="outline"
                icon="i-lucide-arrow-left"
                aria-label="Back"
                @click="backToProviderSelect"
              />
              <div v-else/>

              <div class="flex gap-2">
                <UButton
                  v-if="step === 'form'"
                  label="Test Connection"
                  icon="i-lucide-plug"
                  variant="outline"
                  color="neutral"
                  :loading="testingConnection"
                  :disabled="!form.git_url.trim() || saving"
                  @click="testConnection"
                />
                <CancelButton :disabled="testingConnection || saving" @click="isOpen = false" />
                <UButton v-if="step === 'form'" type="submit" :label="isEditMode ? 'Save' : 'Create'" :loading="saving" :disabled="testingConnection" />
              </div>
            </div>
          </template>
        </AppPanelCard>
      </form>
    </template>
  </UModal>

  <RepositoryKeyModal
    v-model:open="showCreateKey"
    :default-auth-type="requiredKeyType"
    :git-url="form.git_url"
    @saved="handleKeySaved"
  />
</template>
