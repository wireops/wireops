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
  (e: 'created'): void
  (e: 'updated'): void
}>()

const isEditMode = computed(() => !!props.repository)

// ── Form ────────────────────────────────────────────────────────────────────
const form = ref({ name: '', git_url: '', branch: 'main', platform: 'github' })
const isPrivate = ref(false)
const credForm = ref({
  auth_type: 'none',
  ssh_private_key: '',
  ssh_passphrase: '',
  ssh_known_host: '',
  git_username: '',
  git_password: '',
})

// Existing credential record id — used to update instead of create in edit mode
const existingCredId = ref<string | null>(null)

const saving = ref(false)
const testingConnection = ref(false)

// ── Open / reset ─────────────────────────────────────────────────────────────
watch(isOpen, async (val) => {
  if (!val) return

  if (isEditMode.value && props.repository) {
    const r = props.repository
    form.value = {
      name: r.name ?? '',
      git_url: r.git_url ?? '',
      branch: r.branch ?? 'main',
      platform: r.platform ?? 'github',
    }

    // Fetch existing credentials for this repository
    try {
      const creds = await $pb.collection('repository_keys').getFullList({
        filter: `repository = "${r.id}"`,
      })
      if (creds.length) {
        const c = creds[0] as any
        existingCredId.value = c.id
        credForm.value = {
          auth_type: c.auth_type || 'none',
          ssh_private_key: '',       // never prefilled for security
          ssh_passphrase: '',
          ssh_known_host: c.ssh_known_host || '',
          git_username: c.git_username || '',
          git_password: '',          // never prefilled for security
        }
        isPrivate.value = c.auth_type !== 'none'
      } else {
        existingCredId.value = null
        isPrivate.value = false
        credForm.value = { auth_type: 'none', ssh_private_key: '', ssh_passphrase: '', ssh_known_host: '', git_username: '', git_password: '' }
      }
    } catch {
      existingCredId.value = null
      isPrivate.value = false
    }
  } else {
    form.value = { name: '', git_url: '', branch: 'main', platform: 'github' }
    isPrivate.value = false
    existingCredId.value = null
    credForm.value = { auth_type: 'basic', ssh_private_key: '', ssh_passphrase: '', ssh_known_host: '', git_username: '', git_password: '' }
  }
})

// auth_type follows the checkbox
watch(isPrivate, (val) => {
  if (val && credForm.value.auth_type === 'none') {
    credForm.value.auth_type = 'basic'
  } else if (!val) {
    credForm.value.auth_type = 'none'
  }
})

// ── Helpers ──────────────────────────────────────────────────────────────────
function isValidGitUrl(url: string): boolean {
  return /^(https?:\/\/|git@[\w.-]+:[\w./-]+(\.git)?$|ssh:\/\/)/.test(url.trim())
}

// ── Actions ──────────────────────────────────────────────────────────────────
async function testConnection() {
  if (!form.value.git_url) {
    toast.add({ title: 'Connection failed', description: 'Git URL is required.', color: 'error' })
    return
  }
  if (!isValidGitUrl(form.value.git_url)) {
    toast.add({ title: 'Invalid Git URL', description: 'Enter a valid URL (e.g. https://github.com/user/repo.git or git@github.com:user/repo.git).', color: 'error' })
    return
  }
  testingConnection.value = true
  try {
    const result = await testCredentials({
      // In edit mode, send the repository_id so the backend fills in saved
      // sensitive fields (password, SSH key) when the form fields are empty.
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

          <!-- Platform -->
          <UFormField label="Platform" required class="w-full">
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
                />
              </template>
              <template #item-leading="{ item }">
                <img
                  v-if="platformIconUrl(item.value)"
                  :src="platformIconUrl(item.value)!"
                  class="w-4 h-4 object-contain"
                  alt=""
                />
              </template>
            </USelectMenu>
          </UFormField>

          <!-- Git URL -->
          <UFormField label="Git URL" required class="w-full">
            <UInput v-model="form.git_url" placeholder="https://github.com/user/repo.git" class="w-full" />
          </UFormField>

          <!-- Branch -->
          <UFormField label="Branch" class="w-full">
            <UInput v-model="form.branch" placeholder="main" class="w-full" />
          </UFormField>

          <!-- Private Repository -->
          <div class="flex items-center gap-2">
            <UCheckbox v-model="isPrivate" label="Private Repository" />
            <UTooltip text="Enable this if your repository requires authentication. You can add credentials below.">
              <UIcon name="i-lucide-circle-help" class="w-4 h-4 text-gray-400 cursor-help" />
            </UTooltip>
          </div>

          <!-- Authentication Tabs -->
          <div v-if="isPrivate">
            <UTabs
              :items="[
                { label: 'User / Pass', value: 'basic' },
                { label: 'SSH Key', value: 'ssh_key' },
              ]"
              v-model="credForm.auth_type"
              class="w-full"
            >
              <template #content="{ item }">
                <!-- User/Pass fields -->
                <div v-if="item.value === 'basic'" class="space-y-4 pt-3">
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

                <!-- SSH Key fields -->
                <div v-if="item.value === 'ssh_key'" class="space-y-4 pt-3">
                  <UFormField label="SSH Private Key" class="w-full">
                    <UTextarea
                      v-model="credForm.ssh_private_key"
                      :placeholder="isEditMode
                        ? 'Leave empty to keep current key'
                        : '-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA...\n-----END OPENSSH PRIVATE KEY-----'"
                      :rows="8"
                      class="font-mono text-xs w-full"
                    />
                  </UFormField>
                </div>
              </template>
            </UTabs>
          </div>

          <!-- Actions -->
          <div class="flex justify-between items-center pt-4 mt-2 border-t border-gray-100 dark:border-gray-800">
            <UButton
              label="Test Connection"
              icon="i-lucide-plug"
              variant="outline"
              color="neutral"
              :loading="testingConnection"
              :disabled="!isValidGitUrl(form.git_url)"
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
