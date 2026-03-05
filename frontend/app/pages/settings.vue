<script setup lang="ts">
const { $pb } = useNuxtApp()
const toast = useToast()

const keyscanHost = ref('')
const keyscanPort = ref(22)
const { keyscan, getSyncEventsWebhook, setSyncEventsWebhook, setNotificationsEnabled, deleteSyncEventsWebhook, testSyncEventsWebhook, getPKIDetails } = useApi()

const { data: pkiDetails, pending: pkiPending } = useAsyncData('pki_details', getPKIDetails)

function copyToClipboard(text: string) {
  if (!navigator?.clipboard?.writeText) {
    toast.add({ title: 'Clipboard API not available', color: 'error' })
    return
  }
  try {
    navigator.clipboard.writeText(text)
    toast.add({ title: 'Copied!', color: 'success' })
  } catch (e) {
    toast.add({ title: 'Failed to copy', color: 'error' })
  }
}

function formatDatetime(dateStr: string) {
  if (!dateStr) return ''
  try {
    return new Date(dateStr).toLocaleString()
  } catch {
    return dateStr
  }
}

async function runKeyscan() {
  if (!keyscanHost.value) return
  keyscanLoading.value = true
  keyscanResult.value = ''
  try {
    const res = await keyscan(keyscanHost.value, keyscanPort.value) as any
    if (res.success === 'true') {
      keyscanResult.value = res.result
      toast.add({ title: 'Host key retrieved', color: 'success' })
    } else {
      keyscanResult.value = res.error || 'Failed'
      toast.add({ title: 'Keyscan failed', color: 'error' })
    }
  } catch (e: any) {
    keyscanResult.value = e?.message || 'Error'
  } finally {
    keyscanLoading.value = false
  }
}

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
        webhookForm.value.webhook.headers = cfg.headers ? JSON.parse(cfg.headers) : []
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

const keyscanLoading = ref(false)
const keyscanResult = ref('')

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

// --- Change Password ---
const changePasswordForm = ref({ oldPassword: '', password: '', passwordConfirm: '' })
const changePasswordLoading = ref(false)

async function handleChangePassword() {
  if (changePasswordForm.value.password !== changePasswordForm.value.passwordConfirm) {
    toast.add({ title: 'Passwords do not match', color: 'error' })
    return
  }
  changePasswordLoading.value = true
  try {
    const userId = $pb.authStore.record?.id
    if (!userId) {
      toast.add({ title: 'Session invalid', description: 'Please log in again.', color: 'error' })
      return
    }
    await $pb.collection('_superusers').update(userId, {
      oldPassword: changePasswordForm.value.oldPassword,
      password: changePasswordForm.value.password,
      passwordConfirm: changePasswordForm.value.passwordConfirm,
    })
    changePasswordForm.value = { oldPassword: '', password: '', passwordConfirm: '' }
    toast.add({ title: 'Password updated', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to update password', description: e?.message, color: 'error' })
  } finally {
    changePasswordLoading.value = false
  }
}

// --- User Management ---
const users = ref<any[]>([])
const usersLoading = ref(false)
const inviteEmail = ref('')
const inviteLoading = ref(false)

async function loadUsers() {
  usersLoading.value = true
  try {
    users.value = await $pb.collection('_superusers').getFullList({ sort: 'created' })
  } catch (e: any) {
    toast.add({ title: 'Failed to load users', description: e?.message, color: 'error' })
  } finally {
    usersLoading.value = false
  }
}

async function sendInvite() {
  if (!inviteEmail.value) return
  inviteLoading.value = true
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/invite`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${$pb.authStore.token}`,
      },
      body: JSON.stringify({ email: inviteEmail.value }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error)
    inviteEmail.value = ''
    toast.add({ title: 'Invitation sent', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to send invite', description: e?.message, color: 'error' })
  } finally {
    inviteLoading.value = false
  }
}

async function deleteUser(user: any) {
  if (!window.confirm(`Are you sure you want to remove user ${user.email}?`)) {
    return
  }
  try {
    await $pb.collection('_superusers').delete(user.id)
    await loadUsers()
    toast.add({ title: 'User removed', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to remove user', description: e?.message, color: 'error' })
  }
}

const activeTab = ref('general')
const tabs = [
  { label: 'General',       value: 'general',       icon: 'i-lucide-settings-2' },
  { label: 'Notifications', value: 'notifications', icon: 'i-lucide-bell' },
  { label: 'Security',      value: 'security',      icon: 'i-lucide-shield' },
  { label: 'Users',         value: 'users',         icon: 'i-lucide-users' },
]

watch(activeTab, (val) => {
  if (val === 'users') loadUsers()
})
</script>

<template>
  <div class="space-y-6">
    <h1 class="flex items-center gap-3 text-2xl font-bold">
      <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-yellow-400/10">
        <UIcon name="i-lucide-settings-2" class="w-5 h-5 text-yellow-400" />
      </div>
      Settings
    </h1>

    <UTabs :items="tabs" v-model="activeTab" />

    <!-- General -->
    <div v-if="activeTab === 'general'" class="space-y-6">
      <UCard>
        <template #header><h3 class="font-semibold">SSH Host Key Scanner</h3></template>
        <p class="text-sm text-gray-500 mb-3">
          Scan a remote host to retrieve its SSH public key for use in credentials.
        </p>
        <form class="flex flex-col sm:flex-row gap-2" @submit.prevent="runKeyscan">
          <UInput v-model="keyscanHost" placeholder="github.com" class="flex-1" />
          <div class="flex gap-2">
            <UInput v-model.number="keyscanPort" type="number" placeholder="22" class="w-20" />
            <UButton type="submit" label="Scan" :loading="keyscanLoading" />
          </div>
        </form>
        <div v-if="keyscanResult" class="mt-3">
          <pre class="p-3 bg-gray-100 dark:bg-gray-800 rounded text-xs overflow-x-auto font-mono">{{ keyscanResult }}</pre>
          <UButton
            v-if="keyscanResult && !keyscanResult.startsWith('Failed')"
            icon="i-lucide-copy"
            label="Copy"
            variant="outline"
            size="xs"
            class="mt-2"
            @click="copyToClipboard(keyscanResult)"
          />
        </div>
      </UCard>

    </div>

    <!-- Security -->
    <div v-if="activeTab === 'security'" class="space-y-6">
      <UCard>
        <template #header><h3 class="font-semibold">Change Password</h3></template>
        <form class="space-y-4" @submit.prevent="handleChangePassword">
          <UFormField label="Current Password">
            <UInput v-model="changePasswordForm.oldPassword" type="password" placeholder="••••••••" icon="i-lucide-lock" class="w-full" required />
          </UFormField>
          <UFormField label="New Password">
            <UInput v-model="changePasswordForm.password" type="password" placeholder="••••••••" icon="i-lucide-lock" class="w-full" required />
          </UFormField>
          <UFormField label="Confirm New Password">
            <UInput v-model="changePasswordForm.passwordConfirm" type="password" placeholder="••••••••" icon="i-lucide-lock" class="w-full" required />
          </UFormField>
          <UButton type="submit" label="Update Password" icon="i-lucide-check" :loading="changePasswordLoading" />
        </form>
      </UCard>

      <UCard>
        <template #header><h3 class="font-semibold">mTLS Certificates</h3></template>
        <p class="text-sm text-gray-500 mb-4">
          Details of the Root CA and the Server Certificate used for mTLS agent connections.
        </p>
        <div v-if="pkiPending" class="text-sm text-gray-500">Loading...</div>
        <div v-else-if="pkiDetails" class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div v-if="pkiDetails.ca" class="space-y-2">
            <h4 class="font-medium text-sm text-primary">Root CA</h4>
            <div class="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-lg p-3 text-xs font-mono space-y-1.5">
              <div><span class="text-gray-500 mr-2">Issuer:</span> {{ pkiDetails.ca?.issuer }}</div>
              <div><span class="text-gray-500 mr-2">Subject:</span> {{ pkiDetails.ca?.subject }}</div>
              <div><span class="text-gray-500 mr-2">Expires:</span> {{ formatDatetime(pkiDetails.ca?.expiration_date) }}</div>
              <div><span class="text-gray-500 mr-2">Fingerprint:</span> <span class="break-all">{{ pkiDetails.ca?.fingerprint }}</span></div>
            </div>
          </div>
          <div v-if="pkiDetails.server" class="space-y-2">
            <h4 class="font-medium text-sm text-primary">Server Certificate</h4>
            <div class="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-lg p-3 text-xs font-mono space-y-1.5">
              <div><span class="text-gray-500 mr-2">Issuer:</span> {{ pkiDetails.server?.issuer }}</div>
              <div><span class="text-gray-500 mr-2">Subject:</span> {{ pkiDetails.server?.subject }}</div>
              <div><span class="text-gray-500 mr-2">Expires:</span> {{ formatDatetime(pkiDetails.server?.expiration_date) }}</div>
              <div><span class="text-gray-500 mr-2">Fingerprint:</span> <span class="break-all">{{ pkiDetails.server?.fingerprint }}</span></div>
            </div>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Users -->
    <div v-if="activeTab === 'users'" class="space-y-6">
      <UCard>
        <template #header>
          <h3 class="font-semibold">Invite User</h3>
          <p class="text-xs text-gray-500 mt-0.5">Send a magic-link invitation to a new administrator.</p>
        </template>
        <form class="flex gap-2" @submit.prevent="sendInvite">
          <UInput v-model="inviteEmail" type="email" placeholder="user@example.com" icon="i-lucide-mail" class="flex-1" required />
          <UButton type="submit" label="Send Invite" icon="i-lucide-send" :loading="inviteLoading" />
        </form>
      </UCard>

      <UCard>
        <template #header><h3 class="font-semibold">Administrators</h3></template>
        <div v-if="usersLoading" class="text-sm text-gray-500">Loading...</div>
        <div v-else-if="users.length === 0" class="text-sm text-gray-500">No users found.</div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="u in users" :key="u.id" class="flex items-center justify-between py-3 first:pt-0 last:pb-0">
            <div class="flex items-center gap-3">
              <div class="flex items-center justify-center w-8 h-8 rounded-full bg-yellow-400/10">
                <UIcon name="i-lucide-user" class="w-4 h-4 text-yellow-400" />
              </div>
              <div>
                <p class="text-sm font-medium">{{ u.email }}</p>
                <p class="text-xs text-gray-500">Joined {{ new Date(u.created).toLocaleDateString() }}</p>
              </div>
            </div>
            <UBadge v-if="u.id === $pb.authStore.record?.id" label="You" color="neutral" variant="subtle" size="xs" />
            <UButton
              v-else
              icon="i-lucide-trash-2"
              size="xs"
              variant="ghost"
              color="error"
              @click="deleteUser(u)"
            />
          </li>
        </ul>
      </UCard>
    </div>

    <!-- Notifications -->
    <div v-if="activeTab === 'notifications'" class="flex flex-col md:flex-row gap-6 items-start">
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

            <div v-if="webhookForm.provider === 'webhook'" class="space-y-4 pt-4 border-t border-gray-100 dark:border-gray-800">
              <div>
                <label class="block text-sm font-medium mb-1">URL <span class="text-red-500">*</span></label>
                <UInput v-model="webhookForm.webhook.url" placeholder="https://hooks.example.com/wireops" class="w-full font-mono text-sm" />
              </div>
              <div>
                <label class="block text-sm font-medium mb-1">HMAC Secret <span class="text-xs text-gray-400 font-normal">(optional)</span></label>
                <UInput
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

            <div v-if="webhookForm.provider === 'ntfy'" class="space-y-4 pt-4 border-t border-gray-100 dark:border-gray-800">
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div>
                  <label class="block text-sm font-medium mb-1">Server URL <span class="text-red-500">*</span></label>
                  <UInput v-model="webhookForm.ntfy.url" placeholder="https://ntfy.sh" class="w-full font-mono text-sm" />
                </div>
                <div>
                  <label class="block text-sm font-medium mb-1">Topic <span class="text-red-500">*</span></label>
                  <UInput v-model="webhookForm.ntfy.topic" placeholder="my-topic" class="w-full font-mono text-sm" />
                </div>
              </div>
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div>
                  <label class="block text-sm font-medium mb-1">Username <span class="text-xs text-gray-400 font-normal">(optional)</span></label>
                  <UInput v-model="webhookForm.ntfy.user" placeholder="user" class="w-full font-mono text-sm" />
                </div>
                <div>
                  <label class="block text-sm font-medium mb-1">Password <span class="text-xs text-gray-400 font-normal">(optional)</span></label>
                  <UInput
                    v-model="webhookForm.ntfy.password"
                    :type="ntfyHasSecret && webhookForm.ntfy.password === '••••••••' ? 'password' : 'text'"
                    placeholder="password"
                    class="w-full font-mono text-sm"
                    @focus="onNtfyPasswordFocus"
                  />
                </div>
              </div>
              <div>
                <label class="block text-sm font-medium mb-1">Custom Template <span class="text-xs text-gray-400 font-normal">(optional)</span></label>
                <UTextarea
                  v-model="webhookForm.ntfy.template"
                  placeholder="Event: {{.Event}}
Stack: {{.StackName}}
Trigger: {{.Trigger}}
Commit: {{.CommitSHA}}
{{if .Error}}Error: {{.Error}}{{end}}"
                  :rows="6"
                  class="w-full font-mono text-sm"
                />
                <p v-pre class="text-xs text-gray-400 mt-1">
                  Supports Go templates. Variables: <code>{{.StackName}}</code>, <code>{{.Event}}</code>, <code>{{.Status}}</code>, <code>{{.Trigger}}</code>, <code>{{.CommitSHA}}</code>, <code>{{.DurationMs}}</code>, <code>{{.Error}}</code>.
                </p>
              </div>
            </div>

            <!-- Actions -->
            <div class="flex items-center gap-2 pt-2 border-t border-gray-200 dark:border-gray-700">
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
  </div>
</template>
