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
const hasToken = ref(false)

const form = ref({
  address: '',
  token: '',
  allowed_mount: '',
})

watch(() => props.integration, (newVal) => {
  if (newVal) {
    const config = newVal.config || {}
    form.value.address = config.address || ''
    form.value.token = config.token || ''
    form.value.allowed_mount = config.allowed_mount || ''
    hasToken.value = config.token === '••••••••'
  }
}, { immediate: true, deep: true })

const toast = useToast()
const { saveIntegration, testVaultIntegration } = useIntegrations()

function close() {
  isOpen.value = false
}

function onTokenFocus() {
  if (hasToken.value && form.value.token === '••••••••') {
    form.value.token = ''
    hasToken.value = false
  }
}

async function handleSave() {
  if (!form.value.address || !form.value.token) {
    toast.add({ title: 'Address and token are required', color: 'error' })
    return
  }
  loading.value = true
  try {
    await saveIntegration('vault', props.integration.enabled, {
      address: form.value.address,
      token: form.value.token,
      allowed_mount: form.value.allowed_mount,
    })
    toast.add({ title: 'Vault backend saved', color: 'success' })
    emit('saved')
    close()
  } catch (e: any) {
    toast.add({ title: 'Error saving settings', description: e.message, color: 'error' })
  } finally {
    loading.value = false
  }
}

async function testConnection() {
  if (!form.value.address || !form.value.token) {
    toast.add({ title: 'Address and token are required', color: 'error' })
    return
  }
  testing.value = true
  try {
    const result = await testVaultIntegration({
      address: form.value.address,
      token: form.value.token,
      allowed_mount: form.value.allowed_mount,
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
    title="Configure HashiCorp Vault"
    description="Connect to a Vault instance to resolve secret env vars via KV v2."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <UFormField label="Vault Address" required>
          <AppTextInput v-model="form.address" placeholder="https://vault.example.com:8200" class="font-mono text-sm" />
        </UFormField>

        <UFormField label="Token" required>
          <AppTextInput
            v-model="form.token"
            :type="hasToken && form.token === '••••••••' ? 'password' : 'text'"
            placeholder="s.xxxxxxxxxxxxxxxxxxxx"
            class="font-mono text-sm"
            @focus="onTokenFocus"
          />
          <p class="text-xs text-gray-400 mt-1">Stored encrypted at rest. Only used server-side to read secrets.</p>
        </UFormField>

        <UFormField label="Limit to Mount (optional)">
          <AppTextInput v-model="form.allowed_mount" placeholder="secret" class="font-mono text-sm" />
          <p class="text-xs text-gray-400 mt-1">Restrict operators to this KV mount only. Leave empty to allow all mounts.</p>
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
