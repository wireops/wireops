<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const { PLATFORM_OPTIONS, platformIconUrl } = useRepositoryPlatform()
const { $pb } = useNuxtApp()
const { testCredentials } = useApi()
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

const urlPlaceholder = computed(() =>
  urlScheme.value === 'ssh' ? 'git@github.com:user/repo.git' : 'https://github.com/user/repo.git'
)
const requiredKeyType = computed(() => urlScheme.value === 'ssh' ? 'ssh_key' : 'basic')
const compatibleKeys = computed(() => keys.value.filter(key => key.auth_type === requiredKeyType.value))
const keyOptions = computed(() => compatibleKeys.value.map(key => ({
  label: key.name,
  value: key.id,
  description: key.auth_type === 'ssh_key' ? 'SSH key' : key.git_username || 'Username / password',
})))

async function loadKeys(selectID?: string) {
  keys.value = await $pb.collection('repository_keys').getFullList({ sort: 'name' })
  if (selectID) form.value.repository_key = selectID
}

watch(isOpen, async (open) => {
  if (!open) return
  initializing.value = true
  gitUrlError.value = ''
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
    urlScheme.value = repository.git_url?.startsWith('git@') || repository.git_url?.startsWith('ssh://') ? 'ssh' : 'http'
    isPrivate.value = urlScheme.value === 'ssh' || !!repository.repository_key
  } else {
    form.value = { name: '', git_url: '', branch: 'main', platform: 'github', repository_key: '' }
    urlScheme.value = 'http'
    isPrivate.value = false
  }
  await nextTick()
  initializing.value = false
})

watch(urlScheme, (scheme) => {
  if (initializing.value) return
  gitUrlError.value = ''
  form.value.repository_key = ''
  isPrivate.value = scheme === 'ssh'
})
watch(isPrivate, (enabled) => {
  if (!enabled && urlScheme.value === 'http') form.value.repository_key = ''
})
watch(() => form.value.git_url, () => { gitUrlError.value = '' })

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
      auth_type: form.value.repository_key ? requiredKeyType.value : 'none',
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
            <UInput v-model="form.name" placeholder="my-app" class="w-full" />
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

          <UFormField label="Git URL" required :error="gitUrlError">
            <UInput v-model="form.git_url" :placeholder="urlPlaceholder" class="w-full" />
          </UFormField>

          <UFormField label="Branch">
            <UInput v-model="form.branch" placeholder="main" class="w-full" />
          </UFormField>

          <div v-if="urlScheme === 'http'" class="flex items-center gap-2">
            <UCheckbox v-model="isPrivate" label="Private Repository" />
            <span class="text-xs text-gray-500">Public repositories do not need a key.</span>
          </div>

          <div v-if="urlScheme === 'ssh' || isPrivate" class="flex items-end gap-2">
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
          <p v-if="(urlScheme === 'ssh' || isPrivate) && compatibleKeys.length === 0" class="text-xs text-amber-600 dark:text-amber-400">
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
              <UButton label="Cancel" variant="outline" @click="isOpen = false" />
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
