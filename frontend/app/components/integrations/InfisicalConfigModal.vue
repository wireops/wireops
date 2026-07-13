<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  integration: any
}>()

const emit = defineEmits<{
  saved: []
}>()

const isOpen = defineModel<boolean>('open', { default: false })
const loading = ref(false)
const testing = ref(false)
const hasClientSecret = ref(false)

const form = ref({
  site_url: '',
  client_id: '',
  client_secret: '',
  allowed_project_id: '',
})

watch(() => props.integration, (newVal) => {
  if (newVal) {
    const config = newVal.config || {}
    form.value.site_url = config.site_url || ''
    form.value.client_id = config.client_id || ''
    form.value.client_secret = config.client_secret || ''
    form.value.allowed_project_id = config.allowed_project_id || ''
    hasClientSecret.value = config.client_secret === '••••••••'
  }
}, { immediate: true, deep: true })

const toast = useToast()
const { saveIntegration, testInfisicalIntegration } = useIntegrations()

function close() {
  isOpen.value = false
}

function onClientSecretFocus() {
  if (hasClientSecret.value && form.value.client_secret === '••••••••') {
    form.value.client_secret = ''
    hasClientSecret.value = false
  }
}

async function handleSave() {
  if (!form.value.client_id || !form.value.client_secret) {
    toast.add({ title: 'Client ID and client secret are required', color: 'error' })
    return
  }
  loading.value = true
  try {
    await saveIntegration('infisical', props.integration.enabled, {
      site_url: form.value.site_url,
      client_id: form.value.client_id,
      client_secret: form.value.client_secret,
      allowed_project_id: form.value.allowed_project_id,
    })
    toast.add({ title: 'Infisical backend saved', color: 'success' })
    emit('saved')
    close()
  } catch (e: any) {
    toast.add({ title: 'Error saving settings', description: e.message, color: 'error' })
  } finally {
    loading.value = false
  }
}

async function testConnection() {
  if (!form.value.client_id || !form.value.client_secret) {
    toast.add({ title: 'Client ID and client secret are required', color: 'error' })
    return
  }
  testing.value = true
  try {
    const result = await testInfisicalIntegration({
      site_url: form.value.site_url,
      client_id: form.value.client_id,
      client_secret: form.value.client_secret,
      allowed_project_id: form.value.allowed_project_id,
    })
    if (result.success === 'true') {
      toast.add({ title: 'Connection successful', color: 'success' })
    } else {
      toast.add({ title: 'Connection failed', description: result.error, color: 'error' })
    }
  } catch (e: any) {
    toast.add({ title: 'Connection failed', description: e.message, color: 'error' })
  } finally {
    testing.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="isOpen"
    title="Configure Infisical"
    description="Connect via Universal Auth (machine identity) to resolve secret env vars."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <UFormField label="Site URL">
          <AppTextInput v-model="form.site_url" placeholder="https://app.infisical.com" class="font-mono text-sm" />
          <p class="text-xs text-gray-400 mt-1">Leave empty to use Infisical Cloud.</p>
        </UFormField>

        <UFormField label="Client ID" required>
          <AppTextInput v-model="form.client_id" placeholder="machine identity client id" class="font-mono text-sm" />
        </UFormField>

        <UFormField label="Client Secret" required>
          <AppTextInput
            v-model="form.client_secret"
            :type="hasClientSecret && form.client_secret === '••••••••' ? 'password' : 'text'"
            placeholder="machine identity client secret"
            class="font-mono text-sm"
            @focus="onClientSecretFocus"
          />
          <p class="text-xs text-gray-400 mt-1">Stored encrypted at rest. Only used server-side to authenticate.</p>
        </UFormField>

        <UFormField label="Limit to Project (optional)">
          <AppTextInput v-model="form.allowed_project_id" placeholder="64f1a2b3c4d5e6f7a8b9c0d1" class="font-mono text-sm" />
          <p class="text-xs text-gray-400 mt-1">Restrict operators to this Infisical project only. Leave empty to allow all projects.</p>
        </UFormField>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full items-center gap-2">
        <UButton label="Cancel" variant="outline" @click="close" />
        <UButton
          label="Test Connection"
          icon="i-lucide-plug"
          variant="outline"
          color="neutral"
          :loading="testing"
          @click="testConnection"
        />
        <UButton
          label="Save Settings"
          color="primary"
          class="ml-auto"
          :loading="loading"
          @click="handleSave"
        />
      </div>
    </template>
  </UModal>
</template>
