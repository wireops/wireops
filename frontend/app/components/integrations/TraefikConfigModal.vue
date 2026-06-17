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
})

watch(() => props.integration, (newVal) => {
  if (newVal) {
    const config = newVal.config || {}
    form.value.scheme = config.scheme || 'https'
    form.value.port = config.port || ''
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
    const success = await saveIntegration('traefik', props.integration.enabled, {
      scheme: form.value.scheme,
      port: form.value.port,
    })
    if (success) {
      toast.add({ title: 'Traefik integration saved', color: 'success' })
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
    title="Configure Traefik Integration"
    description="Set up Traefik scheme and port settings for generating app routes."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <UFormField label="Scheme">
          <UInput v-model="form.scheme" placeholder="https" class="w-full font-mono text-sm" />
          <p class="text-xs text-gray-500 mt-1">Default is https</p>
        </UFormField>
        <UFormField label="Port">
          <UInput v-model="form.port" placeholder="443" class="w-full font-mono text-sm" />
          <p class="text-xs text-gray-500 mt-1">Optional port to append to the URL</p>
        </UFormField>
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
