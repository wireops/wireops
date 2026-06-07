<script setup lang="ts">
const { $pb } = useNuxtApp()
const toast = useToast()

const keyscanHost = ref('')
const keyscanPort = ref(22)
const { keyscan, getSyncEventsWebhook, setSyncEventsWebhook, setNotificationsEnabled, deleteSyncEventsWebhook, testSyncEventsWebhook, getGlobalWorkerPolicy, saveGlobalWorkerPolicy, getAppSettings, saveAppSettings } = useApi()
const backupLoading = ref(false)

// --- App Settings (Timezone) ---
const appSettings = ref({
  id: '',
  timezone: '',
})
const appSettingsLoading = ref(false)
const appSettingsSaving = ref(false)

const systemTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone
const availableTimezones = ref<{ label: string; value: string }[]>([
  { label: `System Default (${systemTimezone})`, value: 'system' }
])

onMounted(() => {
  try {
    const list = Intl.supportedValuesOf('timeZone')
    availableTimezones.value = [
      { label: `System Default (${systemTimezone})`, value: 'system' },
      ...list.map(tz => ({ label: tz, value: tz }))
    ]
  } catch (e) {
    // fallback if not supported
  }
  loadAppSettings()
})

async function loadAppSettings() {
  appSettingsLoading.value = true
  try {
    const data = await getAppSettings()
    if (data) {
      appSettings.value.id = data.id
      appSettings.value.timezone = data.timezone || 'system'
    }
  } catch (e) {
    // ignore
  } finally {
    appSettingsLoading.value = false
  }
}

async function handleSaveAppSettings() {
  appSettingsSaving.value = true
  try {
    const tzToSave = appSettings.value.timezone === 'system' ? '' : appSettings.value.timezone
    const data = await saveAppSettings(appSettings.value.id, { timezone: tzToSave })
    if (data) {
      appSettings.value.id = data.id
    }
    toast.add({
      title: 'Settings saved',
      description: 'You may need to restart the application (wireops container) for the new timezone to take effect on scheduled jobs.',
      color: 'success',
      timeout: 8000
    })
  } catch (e: any) {
    toast.add({ title: 'Failed to save settings', description: e?.message, color: 'error' })
  } finally {
    appSettingsSaving.value = false
  }
}

function timestampForBackupName() {
  const now = new Date()
  const opts: Intl.DateTimeFormatOptions = {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    hourCycle: 'h23',
    timeZone: (appSettings.value.timezone && appSettings.value.timezone !== 'system') ? appSettings.value.timezone : undefined
  }
  const parts = new Intl.DateTimeFormat('en-US', opts).formatToParts(now)
  const get = (type: string) => parts.find(p => p.type === type)?.value || '00'
  return `${get('year')}${get('month')}${get('day')}_${get('hour')}${get('minute')}${get('second')}`
}

async function exportDatabaseBackup() {
  if (backupLoading.value) return

  backupLoading.value = true
  const filename = `wireops_backup_${timestampForBackupName()}.zip`

  try {
    await $pb.backups.create(filename)
    const token = await $pb.files.getToken()
    const url = $pb.backups.getDownloadURL(token, filename)
    const res = await fetch(url)

    if (!res.ok) {
      throw new Error(`Download failed: ${res.statusText || res.status}`)
    }

    const blob = await res.blob()
    const objectUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = objectUrl
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(objectUrl)

    toast.add({ title: 'Database backup downloaded', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to export backup', description: e?.message, color: 'error' })
  } finally {
    backupLoading.value = false
  }
}

async function copyToClipboard(text: string) {
  if (!navigator?.clipboard?.writeText) {
    toast.add({ title: 'Clipboard API not available', color: 'error' })
    return
  }
  try {
    await navigator.clipboard.writeText(text)
    toast.add({ title: 'Copied!', color: 'success' })
  } catch (e) {
    toast.add({ title: 'Failed to copy', color: 'error' })
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

const route = useRoute()
const activeTab = ref((route.query.tab as string) || 'general')
const tabs = [
  { label: 'General',        value: 'general',        icon: 'i-lucide-settings-2' },
  { label: 'Notifications',  value: 'notifications',  icon: 'i-lucide-bell' },
  { label: 'Security',       value: 'security',       icon: 'i-lucide-shield' },
  { label: 'Worker Policies',value: 'worker-policies',icon: 'i-lucide-shield-check' },
  { label: 'Integrations',   value: 'integrations',   icon: 'i-lucide-puzzle' },
  { label: 'Users',          value: 'users',          icon: 'i-lucide-users' },
]

// --- Worker Policies (global) ---
const workerPolicy = ref({
  enabled: true,
  allowed_volumes: [] as string[],
  allowed_networks: [] as string[],
  allowed_images: [] as string[],
  prevent_latest_images: false,
  block_host_volumes: false,
})
const workerPolicyLoading = ref(false)
const workerPolicySaving = ref(false)

async function loadWorkerPolicy() {
  workerPolicyLoading.value = true
  try {
    const data = await getGlobalWorkerPolicy() as any
    workerPolicy.value = {
      enabled:               data?.enabled ?? true,
      allowed_volumes:       data?.allowed_volumes  ?? [],
      allowed_networks:      data?.allowed_networks ?? [],
      allowed_images:        data?.allowed_images   ?? [],
      prevent_latest_images: data?.prevent_latest_images ?? false,
      block_host_volumes:    data?.block_host_volumes    ?? false,
    }
  } catch {
    // no policy yet — defaults are fine
  } finally {
    workerPolicyLoading.value = false
  }
}

async function saveWorkerPolicyGlobal() {
  workerPolicySaving.value = true
  try {
    workerPolicy.value.allowed_volumes = workerPolicy.value.allowed_volumes.filter(v => v.trim() !== '')
    workerPolicy.value.allowed_networks = workerPolicy.value.allowed_networks.filter(n => n.trim() !== '')
    workerPolicy.value.allowed_images = workerPolicy.value.allowed_images.filter(i => i.trim() !== '')

    await saveGlobalWorkerPolicy(workerPolicy.value)
    toast.add({ title: 'Worker policy saved', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to save policy', description: e?.message, color: 'error' })
  } finally {
    workerPolicySaving.value = false
  }
}

const showConfirmToggleModal = ref(false)
const pendingToggleValue = ref(false)

function onTogglePolicyClick(val: boolean) {
  pendingToggleValue.value = val
  showConfirmToggleModal.value = true
}

async function confirmTogglePolicy() {
  workerPolicySaving.value = true
  try {
    workerPolicy.value.enabled = pendingToggleValue.value
    workerPolicy.value.allowed_volumes = workerPolicy.value.allowed_volumes.filter(v => v.trim() !== '')
    workerPolicy.value.allowed_networks = workerPolicy.value.allowed_networks.filter(n => n.trim() !== '')
    workerPolicy.value.allowed_images = workerPolicy.value.allowed_images.filter(i => i.trim() !== '')

    await saveGlobalWorkerPolicy(workerPolicy.value)
    toast.add({
      title: pendingToggleValue.value ? 'Worker policies enabled' : 'Worker policies disabled',
      color: pendingToggleValue.value ? 'success' : 'neutral'
    })
    showConfirmToggleModal.value = false
  } catch (e: any) {
    toast.add({ title: 'Failed to save policy', description: e?.message, color: 'error' })
  } finally {
    workerPolicySaving.value = false
  }
}

function cancelTogglePolicy() {
  showConfirmToggleModal.value = false
}


const { getIntegrations, saveIntegration } = useIntegrations()
const integrationsList = ref<any[]>([])
const integrationsLoading = ref(false)

const groupedIntegrations = computed(() => {
  const groups: Record<string, any[]> = {}
  for (const item of integrationsList.value) {
    const cat = item.category || 'Other'
    if (!groups[cat]) groups[cat] = []
    groups[cat].push(item)
  }
  return groups
})

async function loadIntegrations() {
  integrationsLoading.value = true
  try {
    integrationsList.value = await getIntegrations()
  } catch (e: any) {
    toast.add({ title: 'Failed to load integrations', color: 'error' })
  } finally {
    integrationsLoading.value = false
  }
}

async function handleSaveIntegration(integration: any) {
  try {
    const success = await saveIntegration(integration.slug, integration.enabled, integration.config)
    if (success) {
      toast.add({ title: 'Success', description: `${integration.slug} integration updated`, color: 'success' })
    } else {
      // Revert local state if save failed
      integration.enabled = !integration.enabled
      toast.add({ title: 'Error', description: `Failed to update ${integration.slug}`, color: 'error' })
    }
  } catch (err: any) {
    // Revert local state on exception
    integration.enabled = !integration.enabled
    toast.add({ title: 'Error', description: `An unexpected error occurred: ${err.message}`, color: 'error' })
  }
}

watch(activeTab, (val) => {
  if (val === 'users') loadUsers()
  if (val === 'integrations') loadIntegrations()
  if (val === 'worker-policies') loadWorkerPolicy()
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

    <UTabs v-model="activeTab" :items="tabs" />

    <!-- General -->
    <div v-if="activeTab === 'general'" class="space-y-6">
      <UCard>
        <template #header><h3 class="font-semibold">System Timezone</h3></template>
        <div class="flex flex-col sm:flex-row gap-4 items-start sm:items-center">
          <div class="flex-1">
            <p class="text-sm text-gray-500 mb-2">
              Set the global timezone for scheduled jobs and database backups. If not set, the system's default timezone will be used.
            </p>
            <USelectMenu
              v-model="appSettings.timezone"
              :items="availableTimezones"
              value-key="value"
              virtualize
              class="w-full sm:max-w-md"
            />
          </div>
          <UButton
            icon="i-lucide-save"
            label="Save"
            :loading="appSettingsSaving"
            @click="handleSaveAppSettings"
            class="shrink-0"
          />
        </div>
      </UCard>

      <UCard>
        <template #header><h3 class="font-semibold">Database Backup</h3></template>
        <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <p class="text-sm text-gray-500">
            Export a restorable PocketBase backup of the local database and data directory.
          </p>
          <UButton
            icon="i-lucide-download"
            label="Export Backup"
            :loading="backupLoading"
            :disabled="backupLoading"
            @click="exportDatabaseBackup"
          />
        </div>
      </UCard>

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

            <div v-if="webhookForm.provider === 'ntfy'" class="space-y-4 pt-4 border-t border-gray-100 dark:border-gray-800">
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

    <!-- Worker Policies -->
    <div v-if="activeTab === 'worker-policies'" class="space-y-6">
      <div v-if="workerPolicyLoading" class="text-sm text-gray-500">Loading policy...</div>
      <template v-else>
        <!-- Global Enable/Disable Toggle -->
        <UCard class="bg-gradient-to-r from-yellow-500/10 via-amber-500/5 to-transparent border border-yellow-500/20">
          <div class="flex items-center justify-between">
            <div class="space-y-1">
              <h3 class="font-semibold text-lg flex items-center gap-2 text-gray-900 dark:text-wire-200">
                <UIcon name="i-lucide-shield-alert" class="w-5 h-5 text-yellow-500" />
                Worker Policy Security System
              </h3>
              <p class="text-sm text-gray-500 dark:text-gray-400">
                Enable or disable global security policy enforcement (volumes, networks, and images) across all workers.
              </p>
            </div>
            <USwitch :model-value="workerPolicy.enabled" size="lg" @update:model-value="onTogglePolicyClick" />
          </div>
        </UCard>
        <WorkerPolicyForm v-model="workerPolicy" @save="saveWorkerPolicyGlobal" />
      </template>
    </div>

    <!-- Integrations -->
    <div v-if="activeTab === 'integrations'" class="space-y-6">
      <div v-if="integrationsLoading" class="text-sm text-gray-500">Loading integrations...</div>
      <template v-else>
        <div v-for="(items, category) in groupedIntegrations" :key="category" class="space-y-4 mt-6 first:mt-0">
          <IntegrationCategory :category="String(category)" />
          
          <UCard v-for="integration in items" :key="integration.slug">
            <template #header>
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <img :src="`https://cdn.jsdelivr.net/gh/selfhst/icons/svg/${integration.slug}.svg`" class="w-5 h-5 object-contain" alt="">
                  <h3 class="font-semibold">{{ integration.name }}</h3>
                </div>
                <USwitch v-model="integration.enabled" @update:model-value="handleSaveIntegration(integration)" />
              </div>
            </template>
            
            <div v-if="integration.enabled" class="space-y-4">
              <template v-if="integration.slug === 'dozzle'">
                <UFormField label="Dozzle URL" required>
                  <UInput v-model="integration.config.url" placeholder="http://dozzle.local:8080" />
                </UFormField>
              </template>
              <template v-else-if="integration.slug === 'traefik'">
                <UFormField label="Scheme">
                  <UInput v-model="integration.config.scheme" placeholder="https" />
                  <p class="text-xs text-gray-500 mt-1">Default is https</p>
                </UFormField>
                <UFormField label="Port">
                  <UInput v-model="integration.config.port" placeholder="443" />
                  <p class="text-xs text-gray-500 mt-1">Optional port to append to the URL</p>
                </UFormField>
              </template>

              <template v-else>
                <p class="text-sm text-gray-500 italic">No additional configuration required.</p>
              </template>
              
              <div class="flex justify-end pt-2">
                <UButton label="Save Config" size="sm" @click="handleSaveIntegration(integration)" />
              </div>
            </div>
            <div v-else>
              <p class="text-sm text-gray-500 italic">Enable this integration to configure its details.</p>
            </div>
          </UCard>
        </div>
      </template>
    </div>

    <!-- Confirm Toggle Policy Modal -->
    <UModal v-model:open="showConfirmToggleModal">
      <template #content>
        <ConfirmTogglePolicyModal
          :enabled="pendingToggleValue"
          :loading="workerPolicySaving"
          @confirm="confirmTogglePolicy"
          @cancel="cancelTogglePolicy"
        />
      </template>
    </UModal>
  </div>
</template>
