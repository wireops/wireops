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
]

const form = ref({
  url: '',
  username: 'wireops',
  avatar_url: '',
  mention_on_error: false,
  role_id: '',
  events: ['sync.started', 'sync.done', 'sync.error'] as string[],
})

function resetForm(integration: any) {
  if (!integration) {
    return
  }
  const config = integration.config || {}
  form.value.url = config.url || ''
  form.value.username = config.username || 'wireops'
  form.value.avatar_url = config.avatar_url || ''
  form.value.mention_on_error = Boolean(config.mention_on_error)
  form.value.role_id = config.role_id || ''
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
    username: form.value.username,
    avatar_url: form.value.avatar_url,
    mention_on_error: form.value.mention_on_error,
    role_id: form.value.role_id,
    events: form.value.events,
  }
}

function webhookUrlError() {
  if (hasWebhookUrl.value && form.value.url === '••••••••') {
    return ''
  }
  try {
    const url = new URL(form.value.url.trim())
    const host = url.hostname.toLowerCase().replace(/\.$/, '')
    if (url.protocol !== 'https:') {
      return 'Discord webhook URL must use https'
    }
    if (url.username || url.password) {
      return 'Discord webhook URL must not include credentials'
    }
    if (host !== 'discord.com' && host !== 'discordapp.com') {
      return 'Discord webhook URL host must be discord.com or discordapp.com'
    }
    if (url.port) {
      return 'Discord webhook URL must not include a custom port'
    }
    if (!url.pathname.startsWith('/api/webhooks/')) {
      return 'Discord webhook URL must use the /api/webhooks path'
    }
  } catch {
    return 'Discord webhook URL is invalid'
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
    const success = await saveIntegration('discord', props.integration.enabled, payload())
    if (success) {
      toast.add({ title: 'Discord integration saved', color: 'success' })
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
    await testIntegration('discord', payload())
    toast.add({ title: 'Test event dispatched', description: 'Check your Discord channel', color: 'success' })
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
    title="Configure Discord Integration"
    description="Configure and test Discord sync notifications."
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

        <UFormField label="Discord Webhook URL" required>
          <UInput
            v-model="form.url"
            :type="hasWebhookUrl && form.url === '••••••••' ? 'password' : 'text'"
            placeholder="https://discord.com/api/webhooks/..."
            class="w-full font-mono text-sm"
            @focus="onWebhookUrlFocus"
          />
        </UFormField>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Username (optional)">
            <UInput v-model="form.username" placeholder="wireops" class="w-full text-sm" />
          </UFormField>
          <UFormField label="Avatar URL (optional)">
            <UInput v-model="form.avatar_url" placeholder="https://example.com/avatar.png" class="w-full font-mono text-sm" />
          </UFormField>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UFormField label="Mention on Errors">
            <USwitch v-model="form.mention_on_error" />
          </UFormField>
          <UFormField label="Role ID">
            <UInput
              v-model="form.role_id"
              placeholder="123456789012345678"
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
