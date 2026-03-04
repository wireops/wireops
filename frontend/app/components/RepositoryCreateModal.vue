<script setup lang="ts">
import { ref, computed } from 'vue'

const { $pb } = useNuxtApp()
const { testCredentials } = useApi()
const toast = useToast()

const isOpen = defineModel<boolean>('open', { default: false })

const emit = defineEmits<{
  (e: 'created'): void
}>()

const form = ref({
  name: '',
  git_url: '',
  branch: 'main',
})

const isPrivate = ref(false)

const credForm = ref({
  auth_type: 'none',
  ssh_private_key: '',
  ssh_passphrase: '',
  ssh_known_host: '',
  git_username: '',
  git_password: '',
})

const saving = ref(false)
const testingConnection = ref(false)

// Reset form when modal opens
watch(isOpen, (val) => {
  if (val) {
    form.value = { name: '', git_url: '', branch: 'main' }
    isPrivate.value = false
    credForm.value = {
      auth_type: 'basic', // Default to basic if private is toggled
      ssh_private_key: '',
      ssh_passphrase: '',
      ssh_known_host: '',
      git_username: '',
      git_password: '',
    }
  }
})

// Ensures that auth_type defaults to "basic" if private is turned on, 
// and "none" if turned off.
watch(isPrivate, (val) => {
  if (val && credForm.value.auth_type === 'none') {
    credForm.value.auth_type = 'basic'
  } else if (!val) {
    credForm.value.auth_type = 'none'
  }
})

async function testConnection() {
  if (!form.value.git_url) {
    toast.add({ title: 'Connection failed', description: 'Git URL is required.', color: 'error' })
    return
  }

  testingConnection.value = true
  try {
    const payload = {
      git_url: form.value.git_url,
      auth_type: isPrivate.value ? credForm.value.auth_type : 'none',
      ssh_private_key: isPrivate.value ? credForm.value.ssh_private_key : '',
      ssh_passphrase: isPrivate.value ? credForm.value.ssh_passphrase : '',
      ssh_known_host: isPrivate.value ? credForm.value.ssh_known_host : '',
      git_username: isPrivate.value ? credForm.value.git_username : '',
      git_password: isPrivate.value ? credForm.value.git_password : '',
    }

    const result = await testCredentials(payload)
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

async function submit() {
  saving.value = true
  try {
    // 1. Create the repository
    const repoRecord = await $pb.collection('repositories').create({
      ...form.value,
      status: 'connected',
    })

    // 2. Create the credentials if private
    if (isPrivate.value) {
      const payload: any = { ...credForm.value, repository: repoRecord.id }
      if (!payload.ssh_private_key) delete payload.ssh_private_key
      if (!payload.ssh_passphrase) delete payload.ssh_passphrase
      if (!payload.git_password) delete payload.git_password

      await $pb.collection('repository_keys').create(payload)
    }

    toast.add({ title: 'Repository created', color: 'success' })
    isOpen.value = false
    emit('created')
  } catch (err: any) {
    toast.add({ title: 'Failed to create repository', description: err.message, color: 'error' })
  } finally {
    saving.value = false
  }
}

function cancel() {
  isOpen.value = false
}
</script>

<template>
  <UModal v-model:open="isOpen">
    <template #content>
      <UCard class="sm:min-w-[600px]">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-git-branch" class="w-5 h-5 text-gray-500" />
            <h2 class="font-semibold">Add Repository</h2>
          </div>
        </template>
        
        <form class="space-y-5" @submit.prevent="submit">
          <!-- General Fields -->
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <UFormField label="Name" required class="col-span-1">
              <UInput v-model="form.name" placeholder="my-app" />
            </UFormField>
            
            <UFormField label="Branch" class="col-span-1">
              <UInput v-model="form.branch" placeholder="main" />
            </UFormField>

            <UFormField label="Git URL" required class="col-span-1 sm:col-span-2">
              <UInput v-model="form.git_url" placeholder="https://github.com/user/repo.git" />
            </UFormField>
          </div>

          <hr class="border-gray-200 dark:border-carbon-700" />

          <!-- Private Repository Toggle -->
          <div class="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-900/50 rounded-lg border border-gray-200 dark:border-gray-800">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-gray-100">Private Repository</p>
              <p class="text-xs text-gray-500">Requires authentication to pull changes</p>
            </div>
            <USwitch v-model="isPrivate" />
          </div>

          <!-- Authentication Fields -->
          <div v-if="isPrivate" class="space-y-4 pt-2">
            <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <UFormField label="Auth Type" class="col-span-1 sm:col-span-3">
                <USelect 
                  v-model="credForm.auth_type" 
                  :items="[
                    { label: 'Username / Password', value: 'basic' },
                    { label: 'SSH Key', value: 'ssh_key' }
                  ]" 
                />
              </UFormField>
            </div>

            <div v-if="credForm.auth_type === 'basic'" class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <UFormField label="Username" required>
                <UInput v-model="credForm.git_username" />
              </UFormField>
              <UFormField label="Password / Token" required>
                <UInput v-model="credForm.git_password" type="password" />
              </UFormField>
            </div>

            <div v-if="credForm.auth_type === 'ssh_key'" class="space-y-4">
              <UFormField label="SSH Private Key" required>
                <UTextarea 
                  v-model="credForm.ssh_private_key" 
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----..." 
                  :rows="8" 
                  class="font-mono text-xs w-full" 
                />
              </UFormField>
              
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <UFormField label="Passphrase">
                  <UInput v-model="credForm.ssh_passphrase" type="password" />
                </UFormField>
                <UFormField label="Known Host">
                  <UInput 
                    v-model="credForm.ssh_known_host" 
                    placeholder="github.com ssh-ed25519 AAAA..." 
                    class="font-mono text-xs" 
                  />
                </UFormField>
              </div>
            </div>
          </div>

          <!-- Actions -->
          <div class="flex justify-between items-center pt-4 mt-2 border-t border-gray-100 dark:border-gray-800 relative z-10 bg-white dark:bg-gray-900">
            <UButton 
              label="Test Connection" 
              icon="i-lucide-plug" 
              variant="outline"
              color="neutral"
              :loading="testingConnection" 
              @click="testConnection" 
            />
            <div class="flex justify-end gap-2">
              <UButton label="Cancel" variant="outline" @click="cancel" />
              <UButton type="submit" label="Create" :loading="saving" />
            </div>
          </div>
        </form>
      </UCard>
    </template>
  </UModal>
</template>
