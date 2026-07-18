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
const testLoading = ref(false)
const hasWebhookUrl = ref(false)

const allEvents = [
  { value: 'sync.started', label: 'Started' },
  { value: 'sync.done',    label: 'Done' },
  { value: 'sync.error',   label: 'Error' },
  { value: 'backup.mirror_error', label: 'Backup mirror failed' },
]

const form = ref({
  url: '',
  mention_on_error: false,
  mention_text: '',
  events: ['sync.started', 'sync.done', 'sync.error'] as string[],
})

function resetForm(integration: any) {
  if (!integration) {
    return
  }
  const config = integration.config || {}
  form.value.url = config.url || ''
  form.value.mention_on_error = Boolean(config.mention_on_error)
  form.value.mention_text = config.mention_text || ''
  form.value.events = config.events ? [...config.events] : ['sync.started', 'sync.done', 'sync.error']
  hasWebhookUrl.value = config.url === '••••••••'
}

watch(() => props.integration, resetForm, { immediate: true, deep: true })
watch(isOpen, (open) => {
  if (open) {
    resetForm(props.integration)
  }
})

const toast = useToast()
const { saveIntegration, testIntegration } = useIntegrations()

function close() {
  isOpen.value = false
}

function toggleEvent(event: string) {
  const idx = form.value.events.indexOf(event)
  if (idx >= 0) {
    form.value.events.splice(idx, 1)
  } else {
    form.value.events.push(event)
  }
}

function onWebhookUrlFocus() {
  if (hasWebhookUrl.value && form.value.url === '••••••••') {
    form.value.url = ''
    hasWebhookUrl.value = false
  }
}

function payload() {
  return {
    url: form.value.url,
    mention_on_error: form.value.mention_on_error,
    mention_text: form.value.mention_text,
    events: form.value.events,
  }
}

function webhookUrlError() {
  if (hasWebhookUrl.value && form.value.url === '••••••••') {
    return ''
  }
  try {
    const url = new URL(form.value.url.trim())
    if (url.protocol !== 'https:') {
      return 'Slack webhook URL must use https'
    }
    if (url.username || url.password) {
      return 'Slack webhook URL must not include credentials'
    }
    if (url.hostname.toLowerCase().replace(/\.$/, '') !== 'hooks.slack.com') {
      return 'Slack webhook URL host must be hooks.slack.com'
    }
    if (url.port) {
      return 'Slack webhook URL must not include a custom port'
    }
    if (!url.pathname.startsWith('/services/')) {
      return 'Slack webhook URL must use the /services path'
    }
  } catch {
    return 'Slack webhook URL is invalid'
  }
  return ''
}

async function handleSave() {
  if (!form.value.url) {
    toast.add({ title: 'Webhook URL is required', color: 'error' })
    return
  }
  const validationError = webhookUrlError()
  if (validationError) {
    toast.add({ title: validationError, color: 'error' })
    return
  }
  loading.value = true
  try {
    const success = await saveIntegration('slack', props.integration.enabled, payload())
    if (success) {
      toast.add({ title: 'Slack integration saved', color: 'success' })
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

async function handleTest() {
  if (!form.value.url) {
    toast.add({ title: 'Webhook URL is required to test', color: 'error' })
    return
  }
  const validationError = webhookUrlError()
  if (validationError) {
    toast.add({ title: validationError, color: 'error' })
    return
  }
  testLoading.value = true
  try {
    await testIntegration('slack', payload())
    toast.add({ title: 'Test event dispatched', description: 'Check your Slack channel', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Test failed', description: e.message, color: 'error' })
  } finally {
    testLoading.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="isOpen"
    title="Configure Slack Integration"
    description="Configure and test Slack sync notifications."
  >
    <template #body>
      <div class="space-y-4" role="document">
        <div>
          <label class="block text-sm font-medium mb-2">Subscribe Events</label>
          <div class="flex flex-wrap gap-3">
            <label
              v-for="event in allEvents"
              :key="event.value"
              class="flex items-center gap-2 cursor-pointer select-none"
            >
              <UCheckbox
                :model-value="form.events.includes(event.value)"
                @update:model-value="toggleEvent(event.value)"
              />
              <span class="text-sm font-mono">{{ event.label }}</span>
            </label>
          </div>
        </div>

        <UFormField label="Slack Webhook URL" required>
          <UInput
            v-model="form.url"
            :type="hasWebhookUrl && form.url === '••••••••' ? 'password' : 'text'"
            placeholder="https://hooks.slack.com/services/..."
            class="w-full font-mono text-sm"
            @focus="onWebhookUrlFocus"
          />
        </UFormField>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Mention on Errors">
            <USwitch v-model="form.mention_on_error" />
          </UFormField>
          <UFormField label="Mention Text">
            <UInput
              v-model="form.mention_text"
              placeholder="<!subteam^S123456|deploys>"
              class="w-full font-mono text-sm"
              :disabled="!form.mention_on_error"
            />
          </UFormField>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full items-center gap-2">
        <UButton label="Cancel" variant="outline" @click="close" />
        <UButton
          label="Send Test"
          icon="i-lucide-send"
          variant="subtle"
          color="neutral"
          :loading="testLoading"
          :disabled="!form.url"
          @click="handleTest"
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
