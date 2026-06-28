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

const form = ref({
  scheme: 'https',
  port: '',
  allow_wildcards: false,
  allow_local_hosts: true,
})

watch(() => props.integration, (newVal) => {
  if (newVal) {
    const config = newVal.config || {}
    form.value.scheme = config.scheme || 'https'
    form.value.port = config.port || ''
    form.value.allow_wildcards = Boolean(config.allow_wildcards)
    form.value.allow_local_hosts = config.allow_local_hosts !== false
  }
}, { immediate: true, deep: true })

const toast = useToast()
const { saveIntegration } = useIntegrations()

function close() {
  isOpen.value = false
}

async function handleSave() {
  loading.value = true
  try {
    const success = await saveIntegration('caddy', props.integration.enabled, {
      scheme: form.value.scheme,
      port: form.value.port,
      allow_wildcards: form.value.allow_wildcards,
      allow_local_hosts: form.value.allow_local_hosts,
    })
    if (success) {
      toast.add({ title: 'Caddy integration saved', color: 'success' })
      emit('saved')
      close()
    } else {
      toast.add({ title: 'Failed to save settings', color: 'error' })
    }
  } catch (e: any) {
    toast.add({ title: 'Error saving settings', description: e.message, color: 'error' })
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="isOpen"
    title="Configure Caddy Integration"
    description="Set up URL generation for Caddy Docker Proxy labels."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Scheme">
            <UInput v-model="form.scheme" placeholder="https" class="w-full font-mono text-sm" />
          </UFormField>
          <UFormField label="Port">
            <UInput v-model="form.port" placeholder="443" class="w-full font-mono text-sm" />
          </UFormField>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Wildcard Hosts">
            <USwitch v-model="form.allow_wildcards" />
          </UFormField>
          <UFormField label="Local Hosts">
            <USwitch v-model="form.allow_local_hosts" />
          </UFormField>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full items-center gap-2">
        <UButton label="Cancel" variant="outline" @click="close" />
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
