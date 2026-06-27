<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const { $pb } = useNuxtApp()
const { testCredentials } = useApi()
const toast = useToast()
const { announce } = useA11yAnnouncer()

const isOpen = defineModel<boolean>('open', { default: false })
const props = defineProps<{
  repositoryKey?: Record<string, any>
  defaultAuthType?: 'ssh_key' | 'basic'
  gitUrl?: string
}>()
const emit = defineEmits<{
  (e: 'saved', key: Record<string, any>): void
}>()

const isEditMode = computed(() => !!props.repositoryKey)
const saving = ref(false)
const testing = ref(false)
const form = ref({
  name: '',
  auth_type: 'basic' as 'ssh_key' | 'basic',
  ssh_private_key: '',
  ssh_passphrase: '',
  ssh_known_host: '',
  git_username: '',
  git_password: '',
})

watch(isOpen, (open) => {
  if (!open) return
  const key = props.repositoryKey
  form.value = {
    name: key?.name || '',
    auth_type: key?.auth_type || props.defaultAuthType || 'basic',
    ssh_private_key: '',
    ssh_passphrase: '',
    ssh_known_host: key?.ssh_known_host || '',
    git_username: key?.git_username || '',
    git_password: '',
  }
})

function errorMessage(error: any): string {
  const data = error?.response?.data
  if (data && typeof data === 'object') {
    for (const value of Object.values(data)) {
      const message = (value as any)?.message
      if (message) return message
    }
  }
  return error?.response?.message || error?.message || 'Unknown error'
}

function validationError(): string {
  const needsNewSecret = !isEditMode.value
  if (!form.value.name.trim()) return 'Name is required'
  if (form.value.auth_type === 'ssh_key' && needsNewSecret && !form.value.ssh_private_key.trim())
    return 'SSH private key is required'
  if (form.value.auth_type === 'basic' && !form.value.git_username.trim())
    return 'Username is required'
  if (form.value.auth_type === 'basic' && needsNewSecret && !form.value.git_password)
    return 'Password or token is required'
  return ''
}

function buildPayload() {
  const payload: Record<string, any> = {
    name: form.value.name.trim(),
    auth_type: form.value.auth_type,
  }

  if (form.value.auth_type === 'ssh_key') {
    payload.ssh_known_host = form.value.ssh_known_host
    payload.git_username = ''
    payload.git_password = ''
    if (form.value.ssh_private_key) payload.ssh_private_key = form.value.ssh_private_key
    if (form.value.ssh_passphrase) payload.ssh_passphrase = form.value.ssh_passphrase
  } else {
    payload.git_username = form.value.git_username.trim()
    payload.ssh_private_key = ''
    payload.ssh_passphrase = ''
    payload.ssh_known_host = ''
    if (form.value.git_password) payload.git_password = form.value.git_password
  }
  return payload
}

async function testConnection() {
  if (!props.gitUrl) return
  const error = validationError()
  if (error) {
    toast.add({ title: 'Invalid key', description: error, color: 'error' })
    return
  }
  testing.value = true
  try {
    const result = await testCredentials({
      ...(props.repositoryKey?.id ? { repository_key_id: props.repositoryKey.id } : {}),
      git_url: props.gitUrl,
      ...buildPayload(),
    })
    if (result.success === 'true') {
      toast.add({ title: 'Connection successful', color: 'success' })
    } else {
      toast.add({ title: 'Connection failed', description: result.error, color: 'error' })
    }
  } catch (error: any) {
    toast.add({ title: 'Connection failed', description: errorMessage(error), color: 'error' })
  } finally {
    testing.value = false
  }
}

async function submit() {
  const error = validationError()
  if (error) {
    toast.add({ title: 'Invalid key', description: error, color: 'error' })
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    const key = props.repositoryKey?.id
      ? await $pb.collection('repository_keys').update(props.repositoryKey.id, payload)
      : await $pb.collection('repository_keys').create(payload)
    toast.add({ title: isEditMode.value ? 'Key updated' : 'Key created', color: 'success' })
    announce(`Repository key ${form.value.name} ${isEditMode.value ? 'updated' : 'created'}`)
    emit('saved', key)
    isOpen.value = false
  } catch (error: any) {
    toast.add({
      title: isEditMode.value ? 'Failed to update key' : 'Failed to create key',
      description: errorMessage(error),
      color: 'error',
    })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <UModal v-model:open="isOpen" scrollable :ui="{ content: 'sm:max-w-xl w-full' }">
    <template #content>
      <UCard class="sm:min-w-[560px] w-full">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-key-round" class="w-5 h-5 text-yellow-400" />
            <h2 class="font-semibold">{{ isEditMode ? 'Edit Repository Key' : 'Add Repository Key' }}</h2>
          </div>
        </template>

        <form class="space-y-4" @submit.prevent="submit">
          <UFormField label="Name" required>
            <UInput v-model="form.name" placeholder="GitHub production" class="w-full" />
          </UFormField>

          <UFormField v-if="!isEditMode" label="Type" required>
            <URadioGroup
              v-model="form.auth_type"
              :items="[
                { label: 'Username / Password', value: 'basic' },
                { label: 'SSH Key', value: 'ssh_key' }
              ]"
              orientation="horizontal"
            />
          </UFormField>

          <template v-if="form.auth_type === 'basic'">
            <UFormField label="Username" required>
              <UInput v-model="form.git_username" class="w-full" />
            </UFormField>
            <UFormField label="Password / Token" :required="!isEditMode">
              <UInput
                v-model="form.git_password"
                type="password"
                :placeholder="isEditMode ? 'Leave empty to keep current' : ''"
                class="w-full"
              />
            </UFormField>
          </template>

          <template v-else>
            <UFormField label="SSH Private Key" :required="!isEditMode">
              <UTextarea
                v-model="form.ssh_private_key"
                :placeholder="isEditMode ? 'Leave empty to keep current key' : 'Paste your private key here'"
                :rows="8"
                class="w-full font-mono text-xs"
              />
            </UFormField>
            <UFormField label="Passphrase">
              <UInput
                v-model="form.ssh_passphrase"
                type="password"
                :placeholder="isEditMode ? 'Leave empty to keep current' : 'Optional'"
                class="w-full"
              />
            </UFormField>
            <UFormField label="Known Hosts">
              <UTextarea
                v-model="form.ssh_known_host"
                placeholder="Optional known_hosts entry"
                :rows="3"
                class="w-full font-mono text-xs"
              />
            </UFormField>
          </template>

          <div class="flex justify-between items-center pt-4 border-t border-gray-100 dark:border-gray-800">
            <UButton
              v-if="gitUrl"
              label="Test Connection"
              icon="i-lucide-plug"
              variant="outline"
              color="neutral"
              :loading="testing"
              @click="testConnection"
            />
            <div v-else />
            <div class="flex gap-2">
              <UButton label="Cancel" variant="outline" @click="isOpen = false" />
              <UButton type="submit" :label="isEditMode ? 'Save' : 'Create Key'" :loading="saving" />
            </div>
          </div>
        </form>
      </UCard>
    </template>
  </UModal>
</template>
