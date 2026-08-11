<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { isValidRegistryUrl } from '../utils/registryUrl'

const AUTH_TYPE = {
  BASIC: 'basic',
  TOKEN: 'token',
  GCP_SERVICE_ACCOUNT: 'gcp_service_account',
} as const

const { $pb } = useNuxtApp()
const { testRegistryCredential } = useApi()
const toast = useToast()
const { announce } = useA11yAnnouncer()

const isOpen = defineModel<boolean>('open', { default: false })
const props = defineProps<{
  credential?: Record<string, any>
}>()
const emit = defineEmits<{
  (e: 'saved', credential: Record<string, any>): void
}>()

const isEditMode = computed(() => !!props.credential)
const saving = ref(false)
const testing = ref(false)
const inFlight = computed(() => saving.value || testing.value)
const jsonHint = ref('')
const form = ref({
  name: '',
  registry_url: '',
  auth_type: AUTH_TYPE.BASIC as string,
  username: '',
  password: '',
  insecure: false,
})

watch(isOpen, (open) => {
  if (!open) return
  const cred = props.credential
  const isGCP = cred?.auth_type === AUTH_TYPE.GCP_SERVICE_ACCOUNT
  form.value = {
    name: cred?.name || '',
    registry_url: cred?.registry_url || '',
    auth_type: cred?.auth_type || AUTH_TYPE.BASIC,
    username: isGCP ? '_json_key' : (cred?.username || ''),
    password: '',
    insecure: isGCP ? false : (cred?.insecure || false),
  }
  jsonHint.value = ''
})

watch(() => form.value.auth_type, (type) => {
  if (type === AUTH_TYPE.GCP_SERVICE_ACCOUNT) {
    form.value.username = '_json_key'
    // GCP credentials always authenticate over HTTPS to a Google-operated
    // registry — insecure/self-signed never applies, so force it off.
    form.value.insecure = false
  } else if (form.value.username === '_json_key') {
    form.value.username = ''
  }
})

watch(() => form.value.password, (value) => {
  if (form.value.auth_type !== AUTH_TYPE.GCP_SERVICE_ACCOUNT || !value) {
    jsonHint.value = ''
    return
  }
  try {
    JSON.parse(value)
    jsonHint.value = ''
  } catch {
    jsonHint.value = 'Invalid JSON'
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
  if (!form.value.registry_url.trim()) return 'Registry URL is required'
  if (!isValidRegistryUrl(form.value.registry_url)) return 'Registry URL is not valid'
  if (form.value.auth_type !== AUTH_TYPE.GCP_SERVICE_ACCOUNT && !form.value.username.trim())
    return 'Username is required'
  if (needsNewSecret && !form.value.password)
    return form.value.auth_type === AUTH_TYPE.GCP_SERVICE_ACCOUNT ? 'Service account JSON key is required' : 'Password or token is required'
  return ''
}

function buildPayload() {
  const payload: Record<string, any> = {
    name: form.value.name.trim(),
    registry_url: form.value.registry_url.trim(),
    auth_type: form.value.auth_type,
    username: form.value.username.trim(),
    insecure: form.value.insecure,
  }
  if (form.value.password) payload.password = form.value.password
  return payload
}

async function testConnection() {
  const error = validationError()
  if (error) {
    toast.add({ title: 'Invalid credential', description: error, color: 'error' })
    return
  }
  testing.value = true
  try {
    const result = await testRegistryCredential({
      ...(props.credential?.id ? { credential_id: props.credential.id } : {}),
      ...buildPayload(),
    })
    if (result.success) {
      toast.add({ title: 'Connection successful', description: result.warning, color: result.warning ? 'warning' : 'success' })
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
    toast.add({ title: 'Invalid credential', description: error, color: 'error' })
    return
  }
  saving.value = true
  try {
    const payload = buildPayload()
    const credential = props.credential?.id
      ? await $pb.collection('registry_credentials').update(props.credential.id, payload)
      : await $pb.collection('registry_credentials').create(payload)
    toast.add({ title: isEditMode.value ? 'Credential updated' : 'Credential created', color: 'success' })
    announce(`Registry credential ${form.value.name} ${isEditMode.value ? 'updated' : 'created'}`)
    emit('saved', credential)
    isOpen.value = false
  } catch (error: any) {
    toast.add({
      title: isEditMode.value ? 'Failed to update credential' : 'Failed to create credential',
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
      <AppPanelCard class="sm:min-w-[560px] w-full">
        <template #header>
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-container" class="w-5 h-5 text-yellow-400" />
            <h2 class="font-semibold">{{ isEditMode ? 'Edit Registry Credential' : 'Add Registry Credential' }}</h2>
          </div>
        </template>

        <form class="space-y-4" @submit.prevent="submit">
          <UFormField label="Name" required>
            <AppTextInput v-model="form.name" placeholder="GHCR production" class="w-full" />
          </UFormField>

          <UFormField label="Registry URL" required>
            <AppTextInput v-model="form.registry_url" placeholder="ghcr.io" class="w-full" />
          </UFormField>

          <UFormField label="Type" required>
            <URadioGroup
              v-model="form.auth_type"
              :items="[
                { label: 'Username / Password', value: AUTH_TYPE.BASIC },
                { label: 'Token', value: AUTH_TYPE.TOKEN },
                { label: 'GCP Service Account', value: AUTH_TYPE.GCP_SERVICE_ACCOUNT }
              ]"
            />
          </UFormField>

          <template v-if="form.auth_type === AUTH_TYPE.GCP_SERVICE_ACCOUNT">
            <UFormField label="Username">
              <AppTextInput model-value="_json_key" disabled class="w-full" />
            </UFormField>
            <UFormField label="Service Account JSON Key" :required="!isEditMode" :error="jsonHint || undefined">
              <AppTextArea
                v-model="form.password"
                :placeholder="isEditMode ? 'Leave empty to keep current key' : 'Paste the service account JSON key here'"
                :rows="10"
                class="w-full font-mono"
              />
            </UFormField>
          </template>
          <template v-else>
            <UFormField label="Username" required>
              <AppTextInput v-model="form.username" class="w-full" />
            </UFormField>
            <UFormField :label="form.auth_type === AUTH_TYPE.TOKEN ? 'Token' : 'Password'" :required="!isEditMode">
              <AppTextInput
                v-model="form.password"
                type="password"
                :placeholder="isEditMode ? 'Leave empty to keep current' : ''"
                class="w-full"
              />
            </UFormField>
          </template>

          <UFormField v-if="form.auth_type !== AUTH_TYPE.GCP_SERVICE_ACCOUNT" label="Insecure registry" description="Plain HTTP or self-signed TLS certificate. The worker's own Docker daemon must already trust this registry via insecure-registries in daemon.json — this toggle does not configure that for you.">
            <USwitch v-model="form.insecure" />
          </UFormField>

          <div class="flex justify-between items-center pt-4 border-t border-gray-100 dark:border-gray-800">
            <UButton
              label="Test Connection"
              icon="i-lucide-plug"
              variant="outline"
              color="neutral"
              :loading="testing"
              :disabled="inFlight"
              @click="testConnection"
            />
            <div class="flex gap-2">
              <CancelButton :disabled="inFlight" @click="isOpen = false" />
              <UButton type="submit" :label="isEditMode ? 'Save' : 'Create Credential'" :loading="saving" :disabled="inFlight" />
            </div>
          </div>
        </form>
      </AppPanelCard>
    </template>
  </UModal>
</template>
