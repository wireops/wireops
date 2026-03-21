<script setup lang="ts">
import { ref, computed } from 'vue'
const { PLATFORM_OPTIONS, platformIconUrl } = useRepositoryPlatform()

const { $pb } = useNuxtApp()
const { testCredentials } = useApi()
const toast = useToast()

const isOpen = defineModel<boolean>('open', { default: false })

const props = defineProps<{
  repository?: Record<string, any>
}>()

const emit = defineEmits<{
  (e: 'created' | 'updated'): void
}>()

const isEditMode = computed(() => !!props.repository)

// ── Form ────────────────────────────────────────────────────────────────────
const form = ref({ name: '', git_url: '', branch: 'main', platform: 'github' })
const urlScheme = ref<'http' | 'ssh'>('http')
const gitUrlError = ref('')
const isPrivate = ref(false)
const credForm = ref({
  auth_type: 'basic',
  ssh_private_key: '',
  ssh_passphrase: '',
  ssh_known_host: '',
  git_username: '',
  git_password: '',
})

const existingCredId = ref<string | null>(null)
const saving = ref(false)
const testingConnection = ref(false)

const urlPlaceholder = computed(() =>
  urlScheme.value === 'ssh' ? 'git@github.com:user/repo.git' : 'https://github.com/user/repo.git'
)

// ── Open / reset ─────────────────────────────────────────────────────────────
watch(isOpen, async (val) => {
  if (!val) return

  gitUrlError.value = ''

  if (isEditMode.value && props.repository) {
    const r = props.repository
    form.value = {
      name: r.name ?? '',
      git_url: r.git_url ?? '',
      branch: r.branch ?? 'main',
      platform: r.platform ?? 'github',
    }
    urlScheme.value = r.git_url?.startsWith('git@') ? 'ssh' : 'http'

    try {
      const creds = await $pb.collection('repository_keys').getFullList({
        filter: `repository = "${r.id}"`,
      })
      if (creds.length) {
        const c = creds[0] as any
        existingCredId.value = c.id
        credForm.value = {
          auth_type: c.auth_type || 'basic',
          ssh_private_key: '',
          ssh_passphrase: '',
          ssh_known_host: c.ssh_known_host || '',
          git_username: c.git_username || '',
          git_password: '',
        }
        isPrivate.value = c.auth_type !== 'none'
      } else {
        existingCredId.value = null
        isPrivate.value = urlScheme.value === 'ssh'
        credForm.value = { auth_type: urlScheme.value === 'ssh' ? 'ssh_key' : 'basic', ssh_private_key: '', ssh_passphrase: '', ssh_known_host: '', git_username: '', git_password: '' }
      }
    } catch {
      existingCredId.value = null
      isPrivate.value = false
    }
  } else {
    form.value = { name: '', git_url: '', branch: 'main', platform: 'github' }
    urlScheme.value = 'http'
    isPrivate.value = false
    existingCredId.value = null
    credForm.value = { auth_type: 'basic', ssh_private_key: '', ssh_passphrase: '', ssh_known_host: '', git_username: '', git_password: '' }
  }
})

// Sync auth_type and isPrivate when scheme changes
watch(urlScheme, (scheme) => {
  gitUrlError.value = ''
  if (scheme === 'ssh') {
    credForm.value.auth_type = 'ssh_key'
    isPrivate.value = true
  } else {
    credForm.value.auth_type = 'basic'
  }
})

// Clear URL error as user types
watch(() => form.value.git_url, () => { gitUrlError.value = '' })

// ── Helpers ──────────────────────────────────────────────────────────────────
function validateGitUrl(): string {
  const url = form.value.git_url.trim()
  if (!url) return 'Git URL is required'
  if (urlScheme.value === 'http' && !/^https?:\/\//.test(url))
    return 'URL must start with http:// or https://'
  if (urlScheme.value === 'ssh' && !url.startsWith('git@'))
    return 'URL must start with git@'
  return ''
}

// ── Actions ──────────────────────────────────────────────────────────────────
async function testConnection() {
  const err = validateGitUrl()
  if (err) {
    gitUrlError.value = err
    return
  }
  testingConnection.value = true
  try {
    const result = await testCredentials({
      ...(isEditMode.value && props.repository ? { repository_id: props.repository.id } : {}),
      git_url: form.value.git_url,
      auth_type: isPrivate.value ? credForm.value.auth_type : 'none',
      ssh_private_key: isPrivate.value ? credForm.value.ssh_private_key : '',
      ssh_passphrase: isPrivate.value ? credForm.value.ssh_passphrase : '',
      ssh_known_host: isPrivate.value ? credForm.value.ssh_known_host : '',
      git_username: isPrivate.value ? credForm.value.git_username : '',
      git_password: isPrivate.value ? credForm.value.git_password : '',
    })
    if (result.success === 'true') {
      toast.add({ title: 'Connection successful!', color: 'success' })
    } else {
      toast.add({ title: 'Connection failed', description: result.error, color: 'error' })
    }
  } catch (error: any) {
    toast.add({ title: 'Test failed', description: error.message, color: 'error' })
  } finally {
    testingConnection.value = false
  }
}

async function saveCredentials(repositoryId: string) {
  if (!isPrivate.value && !existingCredId.value) return

  const payload: any = {
    ...credForm.value,
    auth_type: isPrivate.value ? credForm.value.auth_type : 'none',
    repository: repositoryId,
  }
  if (!payload.ssh_private_key) delete payload.ssh_private_key
  if (!payload.ssh_passphrase) delete payload.ssh_passphrase
  if (!payload.git_password) delete payload.git_password

  if (existingCredId.value) {
    await $pb.collection('repository_keys').update(existingCredId.value, payload)
  } else if (isPrivate.value) {
    await $pb.collection('repository_keys').create(payload)
  }
}

async function submit() {
  const urlErr = validateGitUrl()
  if (urlErr) {
    gitUrlError.value = urlErr
    return
  }

  saving.value = true
  try {
    if (isEditMode.value && props.repository) {
      await $pb.collection('repositories').update(props.repository.id, {
        name: form.value.name,
        git_url: form.value.git_url,
        branch: form.value.branch,
        platform: form.value.platform,
      })
      await saveCredentials(props.repository.id)
      toast.add({ title: 'Repository updated', color: 'success' })
      isOpen.value = false
      emit('updated')
    } else {
      const repoRecord = await $pb.collection('repositories').create({
        ...form.value,
        status: 'connected',
      })
      await saveCredentials(repoRecord.id)
      toast.add({ title: 'Repository created', color: 'success' })
      isOpen.value = false
      emit('created')
    }
  } catch (err: any) {
    toast.add({
      title: isEditMode.value ? 'Failed to update repository' : 'Failed to create repository',
      description: err.message,
      color: 'error',
    })
  } finally {
    saving.value = false
  }
}

function cancel() {
  isOpen.value = false
}
</script>

<template>
  <UModal
    v-model:open="isOpen"
    scrollable
    :ui="{ content: 'sm:max-w-2xl w-full' }"
  >
    <template #content>
      <UCard class="sm:min-w-[640px] w-full">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-git-branch" class="w-5 h-5 text-gray-500" />
            <h2 class="font-semibold">{{ isEditMode ? 'Edit Repository' : 'Add Repository' }}</h2>
          </div>
        </template>

        <form class="space-y-4" @submit.prevent="submit">
          <!-- Name -->
          <UFormField label="Name" required class="w-full">
            <UInput v-model="form.name" placeholder="my-app" class="w-full" />
          </UFormField>

          <!-- Platform + URL scheme -->
          <div class="flex items-end gap-3 w-full">
            <UFormField label="Platform" required class="flex-1">
              <USelectMenu
                v-model="form.platform"
                :items="PLATFORM_OPTIONS"
                value-key="value"
                class="w-full"
                :search-input="false"
              >
                <template #leading>
                  <img
                    v-if="platformIconUrl(form.platform)"
                    :src="platformIconUrl(form.platform)!"
                    class="w-4 h-4 object-contain"
                    alt=""
                  >
                </template>
                <template #item-leading="{ item }">
                  <img
                    v-if="platformIconUrl(item.value)"
                    :src="platformIconUrl(item.value)!"
                    class="w-4 h-4 object-contain"
                    alt=""
                  >
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

          <!-- Git URL -->
          <UFormField label="Git URL" required class="w-full" :error="gitUrlError">
            <UInput v-model="form.git_url" :placeholder="urlPlaceholder" class="w-full" />
          </UFormField>

          <!-- Branch -->
          <UFormField label="Branch" class="w-full">
            <UInput v-model="form.branch" placeholder="main" class="w-full" />
          </UFormField>

          <!-- Private Repository (hidden for SSH — always private) -->
          <div v-if="urlScheme === 'http'" class="flex items-center gap-2">
            <UCheckbox v-model="isPrivate" label="Private Repository" />
            <UTooltip text="Enable this if your repository requires authentication.">
              <UIcon name="i-lucide-circle-help" class="w-4 h-4 text-gray-400 cursor-help" />
            </UTooltip>
          </div>

          <!-- HTTP credentials: username + password -->
          <div v-if="urlScheme === 'http' && isPrivate" class="space-y-4">
            <UFormField label="Username" required class="w-full">
              <UInput v-model="credForm.git_username" class="w-full" />
            </UFormField>
            <UFormField label="Password / Token" class="w-full">
              <UInput
                v-model="credForm.git_password"
                type="password"
                :placeholder="isEditMode ? 'Leave empty to keep current' : ''"
                class="w-full"
              />
            </UFormField>
          </div>

          <!-- SSH credentials: private key + passphrase -->
          <div v-if="urlScheme === 'ssh'" class="space-y-4">
            <UFormField label="SSH Private Key" class="w-full">
              <UTextarea
                v-model="credForm.ssh_private_key"
                :placeholder="isEditMode ? 'Leave empty to keep current key' : 'Paste your private key here'"
                :rows="8"
                class="font-mono text-xs w-full"
              />
            </UFormField>
            <UFormField label="Passphrase" class="w-full">
              <UInput
                v-model="credForm.ssh_passphrase"
                type="password"
                :placeholder="isEditMode ? 'Leave empty to keep current' : 'Optional passphrase for encrypted keys'"
                class="w-full"
              />
            </UFormField>
          </div>

          <!-- Actions -->
          <div class="flex justify-between items-center pt-4 mt-2 border-t border-gray-100 dark:border-gray-800">
            <UButton
              label="Test Connection"
              icon="i-lucide-plug"
              variant="outline"
              color="neutral"
              :loading="testingConnection"
              :disabled="!!validateGitUrl()"
              @click="testConnection"
            />
            <div class="flex justify-end gap-2">
              <UButton label="Cancel" variant="outline" @click="cancel" />
              <UButton
                type="submit"
                :label="isEditMode ? 'Save' : 'Create'"
                :loading="saving"
              />
            </div>
          </div>
        </form>
      </UCard>
    </template>
  </UModal>
</template>
