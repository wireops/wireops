<script setup lang="ts">
import { ref } from 'vue'

const toast = useToast()
const { getSyncEventsWebhook, setSyncEventsWebhook, setNotificationsEnabled, deleteSyncEventsWebhook, testSyncEventsWebhook } = useApi()

// --- Sync Event Webhook ---
const allEvents = [
  { value: 'sync.started', label: 'Started' },
  { value: 'sync.done',    label: 'Done' },
  { value: 'sync.error',   label: 'Error' },
]

const notificationsEnabled = ref(false)

const webhookForm = ref({
  provider: 'webhook' as 'webhook' | 'ntfy',
  events: ['sync.started', 'sync.done', 'sync.error'] as string[],
  webhook: {
    url: '',
    secret: '',
    headers: [] as { key: string; value: string }[],
  },
  ntfy: {
    url: 'https://ntfy.sh',
    topic: '',
    user: '',
    password: '',
    template: '',
  }
})
const webhookLoading = ref(false)
const webhookTestLoading = ref(false)
const webhookHasSecret = ref(false)
const ntfyHasSecret = ref(false)

const { data: webhookConfig, refresh: refreshWebhook } = useAsyncData('sync_events_webhook', async () => {
  const cfg = await getSyncEventsWebhook() as any
  if (cfg) {
    notificationsEnabled.value = cfg.enabled ?? false
    webhookForm.value.provider = cfg.provider || 'webhook'
    webhookForm.value.events = cfg.events || []

    if (cfg.provider === 'ntfy') {
      webhookForm.value.ntfy.url = cfg.url || 'https://ntfy.sh'
      webhookForm.value.ntfy.topic = cfg.ntfy_topic || ''
      webhookForm.value.ntfy.user = cfg.ntfy_user || ''
      webhookForm.value.ntfy.password = cfg.secret || ''
      webhookForm.value.ntfy.template = cfg.ntfy_template || ''
      ntfyHasSecret.value = cfg.secret === '••••••••'
      webhookForm.value.webhook.url = ''
      webhookForm.value.webhook.secret = ''
      webhookForm.value.webhook.headers = []
      webhookHasSecret.value = false
    } else {
      webhookForm.value.webhook.url = cfg.url || ''
      webhookForm.value.webhook.secret = cfg.secret || ''
      webhookHasSecret.value = cfg.secret === '••••••••'
      try {
        const parsed = cfg.headers ? JSON.parse(cfg.headers) : []
        webhookForm.value.webhook.headers = Array.isArray(parsed)
          ? parsed
              .filter((item: unknown): item is Record<string, unknown> =>
                typeof item === 'object' && item !== null && 'key' in item && 'value' in item,
              )
              .map(item => ({ key: String(item.key ?? ''), value: String(item.value ?? '') }))
          : []
      } catch { webhookForm.value.webhook.headers = [] }
      webhookForm.value.ntfy.url = 'https://ntfy.sh'
      webhookForm.value.ntfy.topic = ''
      webhookForm.value.ntfy.user = ''
      webhookForm.value.ntfy.password = ''
      webhookForm.value.ntfy.template = ''
      ntfyHasSecret.value = false
    }
  }
  return cfg
})

function addHeader() {
  webhookForm.value.webhook.headers.push({ key: '', value: '' })
}

function removeHeader(index: number) {
  webhookForm.value.webhook.headers.splice(index, 1)
}

function toggleEvent(event: string) {
  const idx = webhookForm.value.events.indexOf(event)
  if (idx >= 0) {
    webhookForm.value.events.splice(idx, 1)
  } else {
    webhookForm.value.events.push(event)
  }
}

function prepareWebhookPayload() {
  const isNtfy = webhookForm.value.provider === 'ntfy'
  let secretToSend = ''
  let urlToSend = ''
  let headersToSend = '[]'
  
  if (isNtfy) {
     secretToSend = ntfyHasSecret.value && webhookForm.value.ntfy.password === '••••••••'
      ? '••••••••'
      : webhookForm.value.ntfy.password
     urlToSend = webhookForm.value.ntfy.url
  } else {
     secretToSend = webhookHasSecret.value && webhookForm.value.webhook.secret === '••••••••'
      ? '••••••••'
      : webhookForm.value.webhook.secret
     urlToSend = webhookForm.value.webhook.url
     headersToSend = JSON.stringify(webhookForm.value.webhook.headers.filter((h: any) => h.key))
  }
  return { isNtfy, secretToSend, urlToSend, headersToSend }
}

async function saveWebhook() {
  const isNtfy = webhookForm.value.provider === 'ntfy'
  if (!isNtfy && !webhookForm.value.webhook.url) {
    toast.add({ title: 'URL is required', color: 'error' })
    return
  }
  if (isNtfy && !webhookForm.value.ntfy.topic) {
    toast.add({ title: 'Topic is required', color: 'error' })
    return
  }
  webhookLoading.value = true
  try {
    const { secretToSend, urlToSend, headersToSend } = prepareWebhookPayload()

    await setSyncEventsWebhook({
      provider: webhookForm.value.provider,
      url: urlToSend,
      secret: secretToSend,
      events: webhookForm.value.events,
      headers: headersToSend,
      ntfy_user: isNtfy ? webhookForm.value.ntfy.user : '',
      ntfy_topic: isNtfy ? webhookForm.value.ntfy.topic : '',
      ntfy_template: isNtfy ? webhookForm.value.ntfy.template : '',
    })
    toast.add({ title: 'Notification settings saved', color: 'success' })
    await refreshWebhook()
  } catch (e: any) {
    toast.add({ title: 'Failed to save settings', description: e?.message, color: 'error' })
  } finally {
    webhookLoading.value = false
  }
}

async function deleteWebhook() {
  webhookLoading.value = true
  try {
    await deleteSyncEventsWebhook()
    notificationsEnabled.value = false
    webhookForm.value = {
      provider: 'webhook',
      events: ['sync.started', 'sync.done', 'sync.error'],
      webhook: { url: '', secret: '', headers: [] },
      ntfy: { url: 'https://ntfy.sh', topic: '', user: '', password: '', template: '' }
    }
    webhookHasSecret.value = false
    ntfyHasSecret.value = false
    toast.add({ title: 'Webhook removed', color: 'success' })
    await refreshWebhook()
  } catch (e: any) {
    toast.add({ title: 'Failed to remove', color: 'error' })
  } finally {
    webhookLoading.value = false
  }
}

async function sendTestWebhook() {
  webhookTestLoading.value = true
  try {
    const isNtfy = webhookForm.value.provider === 'ntfy'
    const { secretToSend, urlToSend, headersToSend } = prepareWebhookPayload()

    await testSyncEventsWebhook({
      provider: webhookForm.value.provider,
      url: urlToSend,
      secret: secretToSend,
      events: webhookForm.value.events,
      headers: headersToSend,
      enabled: notificationsEnabled.value,
      ntfy_user: isNtfy ? webhookForm.value.ntfy.user : '',
      ntfy_topic: isNtfy ? webhookForm.value.ntfy.topic : '',
      ntfy_template: isNtfy ? webhookForm.value.ntfy.template : '',
    })
    toast.add({ title: 'Test event dispatched', description: 'Check your notification provider', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Test failed', description: e?.message, color: 'error' })
  } finally {
    webhookTestLoading.value = false
  }
}

const providers = [
  { label: 'Webhook', value: 'webhook', icon: 'i-lucide-webhook' },
  { label: 'Ntfy',    value: 'ntfy',    icon: 'i-lucide-bell' },
]

function onProviderChange(val: string) {
  webhookForm.value.provider = val as 'webhook' | 'ntfy'
}

async function onEnabledChange(val: boolean) {
  try {
    await setNotificationsEnabled(val)
    notificationsEnabled.value = val
    toast.add({
      title: val ? 'Notifications enabled' : 'Notifications disabled',
      color: val ? 'success' : 'neutral',
    })
  } catch (e: any) {
    toast.add({ title: 'Failed to update notifications', description: e?.message, color: 'error' })
  }
}

function onWebhookSecretFocus() {
  if (webhookHasSecret.value && webhookForm.value.webhook.secret === '••••••••') {
    webhookForm.value.webhook.secret = ''
    webhookHasSecret.value = false
  }
}

function onNtfyPasswordFocus() {
  if (ntfyHasSecret.value && webhookForm.value.ntfy.password === '••••••••') {
    webhookForm.value.ntfy.password = ''
    ntfyHasSecret.value = false
  }
}
</script>

<template>
  <div class="flex flex-col md:flex-row gap-6 items-start">
    <div class="w-full md:w-2/5">
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <div>
              <h3 class="font-semibold">Notification Provider</h3>
              <p class="text-xs text-gray-500 mt-0.5">Select the provider to receive sync event notifications.</p>
            </div>
            <USwitch :model-value="notificationsEnabled" @update:model-value="onEnabledChange" />
          </div>
        </template>
        <div class="flex flex-col gap-2">
          <UButton
            v-for="p in providers"
            :key="p.value"
            :label="p.label"
            :icon="p.icon"
            :variant="webhookForm.provider === p.value ? 'solid' : 'outline'"
            block
            @click="onProviderChange(p.value)"
          />
        </div>
      </UCard>
    </div>

    <div class="w-full md:w-3/5">
      <UCard>
        <template #header>
          <h3 class="font-semibold">Sync Event Notifications</h3>
          <p class="text-xs text-gray-500 mt-0.5">Send notifications when a sync job starts, completes, or fails.</p>
        </template>

        <div class="space-y-4">
          <!-- Common: Events -->
          <div>
            <label class="block text-sm font-medium mb-2">Events</label>
            <div class="flex flex-wrap gap-3">
              <label
                v-for="event in allEvents"
                :key="event.value"
                class="flex items-center gap-2 cursor-pointer select-none"
              >
                <UCheckbox
                  :model-value="webhookForm.events.includes(event.value)"
                  @update:model-value="toggleEvent(event.value)"
                />
                <span class="text-sm font-mono">{{ event.label }}</span>
              </label>
            </div>
          </div>

          <div v-if="webhookForm.provider === 'webhook'" class="space-y-4 pt-4 border-t border-gray-150 dark:border-carbon-800/60">
            <div>
              <label for="webhook-url" class="block text-sm font-medium mb-1">URL <span class="text-red-500">*</span></label>
              <UInput id="webhook-url" v-model="webhookForm.webhook.url" placeholder="https://hooks.example.com/wireops" class="w-full font-mono text-sm" />
            </div>
            <div>
              <label for="webhook-secret" class="block text-sm font-medium mb-1">HMAC Secret <span class="text-xs text-gray-400 font-normal">(optional)</span></label>
              <UInput
                id="webhook-secret"
                v-model="webhookForm.webhook.secret"
                :type="webhookHasSecret && webhookForm.webhook.secret === '••••••••' ? 'password' : 'text'"
                placeholder="Leave empty to skip signature"
                class="w-full font-mono text-sm"
                @focus="onWebhookSecretFocus"
              />
              <p class="text-xs text-gray-400 mt-1">Used to compute <code>X-wireops-Signature: sha256=&lt;hmac&gt;</code> header.</p>
            </div>
            <div>
              <div class="flex items-center justify-between mb-2">
                <label class="block text-sm font-medium">Custom Headers</label>
                <UButton icon="i-lucide-plus" size="xs" variant="outline" label="Add Header" @click="addHeader" />
              </div>
              <div v-if="webhookForm.webhook.headers.length === 0" class="text-xs text-gray-400 italic">No custom headers.</div>
              <div v-for="(header, i) in webhookForm.webhook.headers" :key="i" class="flex items-center gap-2 mb-2">
                <UInput v-model="header.key" placeholder="Header name" class="flex-1 font-mono text-sm" />
                <UInput v-model="header.value" placeholder="Value" class="flex-1 font-mono text-sm" />
                <UButton icon="i-lucide-x" size="xs" variant="ghost" color="error" @click="removeHeader(i)" />
              </div>
            </div>
          </div>

          <div v-if="webhookForm.provider === 'ntfy'" class="space-y-4 pt-4 border-t border-gray-150 dark:border-carbon-800/60">
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label for="ntfy-url" class="block text-sm font-medium mb-1">Server URL <span class="text-red-500">*</span></label>
                <UInput id="ntfy-url" v-model="webhookForm.ntfy.url" placeholder="https://ntfy.sh" class="w-full font-mono text-sm" />
              </div>
              <div>
                <label for="ntfy-topic" class="block text-sm font-medium mb-1">Topic <span class="text-red-500">*</span></label>
                <UInput id="ntfy-topic" v-model="webhookForm.ntfy.topic" placeholder="my-topic" class="w-full font-mono text-sm" />
              </div>
            </div>
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label for="ntfy-user" class="block text-sm font-medium mb-1">Username <span class="text-xs text-gray-400 font-normal">(optional)</span></label>
                <UInput id="ntfy-user" v-model="webhookForm.ntfy.user" placeholder="user" class="w-full font-mono text-sm" />
              </div>
              <div>
                <label for="ntfy-password" class="block text-sm font-medium mb-1">Password <span class="text-xs text-gray-400 font-normal">(optional)</span></label>
                <UInput
                  id="ntfy-password"
                  v-model="webhookForm.ntfy.password"
                  :type="ntfyHasSecret && webhookForm.ntfy.password === '••••••••' ? 'password' : 'text'"
                  placeholder="password"
                  class="w-full font-mono text-sm"
                  @focus="onNtfyPasswordFocus"
                />
              </div>
            </div>
            <div>
              <label for="ntfy-template" class="block text-sm font-medium mb-1">Custom Template <span class="text-xs text-gray-400 font-normal">(optional)</span></label>
              <UTextarea
                id="ntfy-template"
                v-model="webhookForm.ntfy.template"
                placeholder="Event: {{.Event}}&#10;Stack: {{.StackName}}&#10;Trigger: {{.Trigger}}&#10;Commit: {{.CommitSHA}}&#10;{{if .Error}}Error: {{.Error}}{{end}}"
                :rows="6"
                class="w-full font-mono text-sm"
              />
              <p v-pre class="text-xs text-gray-400 mt-1">
                Supports Go templates. Variables: <code>{{.StackName}}</code>, <code>{{.Event}}</code>, <code>{{.Status}}</code>, <code>{{.Trigger}}</code>, <code>{{.CommitSHA}}</code>, <code>{{.DurationMs}}</code>, <code>{{.Error}}</code>.
              </p>
            </div>
          </div>

          <!-- Actions -->
          <div class="flex items-center gap-2 pt-2 border-t border-gray-200 dark:border-carbon-800/60">
            <UButton label="Save" :loading="webhookLoading" @click="saveWebhook" />
            <UButton
              label="Send Test"
              icon="i-lucide-send"
              variant="outline"
              :loading="webhookTestLoading"
              :disabled="webhookForm.provider === 'ntfy' ? !webhookForm.ntfy.topic : !webhookForm.webhook.url"
              @click="sendTestWebhook"
            />
            <UButton
              v-if="webhookConfig"
              label="Remove"
              icon="i-lucide-trash-2"
              variant="ghost"
              color="error"
              :loading="webhookLoading"
              class="ml-auto"
              @click="deleteWebhook"
            />
          </div>
        </div>
      </UCard>
    </div>
  </div>
</template>
