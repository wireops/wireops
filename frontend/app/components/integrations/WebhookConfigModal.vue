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
const hasSecret = ref(false)

const allEvents = [
  { value: 'sync.started', label: 'Started' },
  { value: 'sync.done',    label: 'Done' },
  { value: 'sync.error',   label: 'Error' },
  { value: 'backup.mirror_error', label: 'Backup mirror failed' },
]

const form = ref({
  url: '',
  secret: '',
  headers: [] as { key: string; value: string }[],
  events: ['sync.started', 'sync.done', 'sync.error'] as string[],
})

watch(() => props.integration, (newVal) => {
  if (newVal) {
    const config = newVal.config || {}
    form.value.url = config.url || ''
    form.value.secret = config.secret || ''
    form.value.headers = config.headers ? config.headers.map((h: any) => ({ ...h })) : []
    form.value.events = config.events ? [...config.events] : ['sync.started', 'sync.done', 'sync.error']
    hasSecret.value = config.secret === '••••••••'
  }
}, { immediate: true, deep: true })

const toast = useToast()
const { saveIntegration, testIntegration } = useIntegrations()

function close() {
  isOpen.value = false
}

function addHeader() {
  form.value.headers.push({ key: '', value: '' })
}

function removeHeader(index: number) {
  form.value.headers.splice(index, 1)
}

function toggleEvent(event: string) {
  const idx = form.value.events.indexOf(event)
  if (idx >= 0) {
    form.value.events.splice(idx, 1)
  } else {
    form.value.events.push(event)
  }
}

function onSecretFocus() {
  if (hasSecret.value && form.value.secret === '••••••••') {
    form.value.secret = ''
    hasSecret.value = false
  }
}

async function handleSave() {
  if (!form.value.url) {
    toast.add({ title: 'URL is required', color: 'error' })
    return
  }
  loading.value = true
  try {
    const success = await saveIntegration('webhook', props.integration.enabled, {
      url: form.value.url,
      secret: form.value.secret,
      headers: form.value.headers.filter(h => h.key.trim() !== ''),
      events: form.value.events,
    })
    if (success) {
      toast.add({ title: 'Webhook integration saved', color: 'success' })
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
    toast.add({ title: 'URL is required to test', color: 'error' })
    return
  }
  testLoading.value = true
  try {
    await testIntegration('webhook', {
      url: form.value.url,
      secret: form.value.secret,
      headers: form.value.headers.filter(h => h.key.trim() !== ''),
      events: form.value.events,
    })
    toast.add({ title: 'Test event dispatched', description: 'Check your webhook server', color: 'success' })
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
    title="Configure Webhook Integration"
    description="Configure and test your custom webhook notifications."
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

        <UFormField label="Webhook URL" required>
          <UInput v-model="form.url" placeholder="https://hooks.example.com/wireops" class="w-full font-mono text-sm" />
        </UFormField>

        <UFormField label="HMAC Secret (optional)">
          <UInput
            v-model="form.secret"
            :type="hasSecret && form.secret === '••••••••' ? 'password' : 'text'"
            placeholder="Leave empty to skip signature"
            class="w-full font-mono text-sm"
            @focus="onSecretFocus"
          />
          <p class="text-xs text-gray-400 mt-1">Used to compute <code>X-wireops-Signature: sha256=&lt;hmac&gt;</code> header.</p>
        </UFormField>

        <div>
          <div class="flex items-center justify-between mb-2">
            <label class="block text-sm font-medium">Custom Headers</label>
            <UButton icon="i-lucide-plus" size="xs" variant="outline" label="Add Header" @click="addHeader" />
          </div>
          <div v-if="form.headers.length === 0" class="text-xs text-gray-400 italic">No custom headers.</div>
          <div v-for="(header, i) in form.headers" :key="i" class="flex items-center gap-2 mb-2">
            <UInput v-model="header.key" placeholder="Header name" class="flex-1 font-mono text-sm" />
            <UInput v-model="header.value" placeholder="Value" class="flex-1 font-mono text-sm" />
            <UButton icon="i-lucide-x" size="xs" variant="ghost" color="error" @click="removeHeader(i)" />
          </div>
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
