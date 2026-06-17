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
  url: '',
})

watch(() => props.integration, (newVal) => {
  if (newVal) {
    const config = newVal.config || {}
    form.value.url = config.url || ''
  }
}, { immediate: true, deep: true })

const toast = useToast()
const { saveIntegration } = useIntegrations()

function close() {
  isOpen.value = false
}

async function handleSave() {
  if (!form.value.url) {
    toast.add({ title: 'URL is required', color: 'error' })
    return
  }
  loading.value = true
  try {
    const success = await saveIntegration('dozzle', props.integration.enabled, {
      url: form.value.url,
    })
    if (success) {
      toast.add({ title: 'Dozzle integration saved', color: 'success' })
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
    title="Configure Dozzle Integration"
    description="Set up your Dozzle instance URL to view container logs directly."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <UFormField label="Dozzle URL" required>
          <UInput v-model="form.url" placeholder="http://dozzle.local:8080" class="w-full font-mono text-sm" />
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
