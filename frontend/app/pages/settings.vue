<script setup lang="ts">
import { DateFormatter, getLocalTimeZone, today } from '@internationalized/date'

const { $pb } = useNuxtApp()
const toast = useToast()
const { isAdmin, isViewer } = usePermissions()

if (isViewer.value) {
  navigateTo('/')
}

const roleOptions = [
  { label: 'Viewer', value: 'viewer' },
  { label: 'Operator', value: 'operator' },
  { label: 'Admin', value: 'admin' },
]

const keyscanHost = ref('')
const keyscanPort = ref(22)
const { keyscan, getSyncEventsWebhook, setSyncEventsWebhook, setNotificationsEnabled, deleteSyncEventsWebhook, testSyncEventsWebhook, getGlobalWorkerPolicy, saveGlobalWorkerPolicy, getAppSettings, saveAppSettings, listAuditLogs } = useApi()
const backupLoading = ref(false)

// --- App Settings (Timezone) ---
const appSettings = ref({
  id: '',
  timezone: '',
  audit_retention_days: 30,
  job_run_retention_days: 7,
  sso_groups_claim: 'groups',
})
const appSettingsLoading = ref(false)
const appSettingsSaving = ref(false)
const appSettingsLoaded = ref(false)
const showAuditSettingsModal = ref(false)

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
      appSettings.value.audit_retention_days = data.audit_retention_days || 30
      appSettings.value.job_run_retention_days = data.job_run_retention_days || 7
      appSettings.value.sso_groups_claim = data.sso_groups_claim || 'groups'
      appSettingsLoaded.value = true
    }
  } catch (e) {
    // ignore
  } finally {
    appSettingsLoading.value = false
  }
}

async function handleSaveAppSettings(options: { title?: string; description?: string } = {}) {
  appSettingsSaving.value = true
  try {
    const tzToSave = appSettings.value.timezone === 'system' ? '' : appSettings.value.timezone
    const payload: any = { timezone: tzToSave }
    if (appSettingsLoaded.value) {
      payload.audit_retention_days = appSettings.value.audit_retention_days
      payload.job_run_retention_days = appSettings.value.job_run_retention_days
      payload.sso_groups_claim = appSettings.value.sso_groups_claim || 'groups'
    }
    const data = await saveAppSettings(payload)
    if (data) {
      appSettings.value.id = data.id
      appSettings.value.audit_retention_days = data.audit_retention_days || 30
      appSettings.value.job_run_retention_days = data.job_run_retention_days || 7
      appSettings.value.sso_groups_claim = data.sso_groups_claim || appSettings.value.sso_groups_claim || 'groups'
      appSettingsLoaded.value = true
    }
    toast.add({
      title: options.title || 'Settings saved',
      description: options.description || 'You may need to restart the application (wireops container) for the new timezone to take effect on scheduled jobs.',
      color: 'success',
      timeout: 8000
    })
    return true
  } catch (e: any) {
    toast.add({ title: 'Failed to save settings', description: e?.message, color: 'error' })
    return false
  } finally {
    appSettingsSaving.value = false
  }
}

async function handleSaveAuditSettings() {
  const saved = await handleSaveAppSettings({
    title: 'Audit settings saved',
    description: 'Audit and job run retention settings were updated.',
  })
  if (saved) {
    showAuditSettingsModal.value = false
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
    const res = await fetch(`${$pb.baseURL}/api/custom/backups`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
        'X-Wireops-Origin': 'ui',
      },
      body: JSON.stringify({ filename }),
    })

    if (!res.ok) {
      let message = `Download failed: ${res.statusText || res.status}`
      try {
        const data = await res.json()
        if (data?.error) message = data.error
      } catch {
        // Keep the default message when the response is not JSON.
      }
      throw new Error(message)
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
    await $pb.collection('users').update(userId, {
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
const inviteRole = ref('viewer')
const inviteLoading = ref(false)

async function loadUsers() {
  usersLoading.value = true
  try {
    users.value = await $pb.collection('users').getFullList({ sort: 'created' })
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
        'X-Wireops-Origin': 'ui',
      },
      body: JSON.stringify({ email: inviteEmail.value, role: inviteRole.value }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error)
    inviteEmail.value = ''
    inviteRole.value = 'viewer'
    toast.add({ title: 'Invitation sent', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to send invite', description: e?.message, color: 'error' })
  } finally {
    inviteLoading.value = false
  }
}

async function toggleUserDisabled(user: any) {
  const action = user.disabled ? 'enable' : 'disable'
  if (!window.confirm(`Are you sure you want to ${action} user ${user.email}?`)) {
    return
  }
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/${user.id}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${$pb.authStore.token}`,
        'X-Wireops-Origin': 'ui',
      },
      body: JSON.stringify({ disabled: !user.disabled }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error)
    user.disabled = !user.disabled
    toast.add({ title: user.disabled ? 'User disabled' : 'User enabled', color: 'success' })
  } catch (e: any) {
    toast.add({ title: `Failed to ${action} user`, description: e?.message, color: 'error' })
    await loadUsers()
  }
}

async function updateUserRole(user: any, role: string) {
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/users/${user.id}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${$pb.authStore.token}`,
        'X-Wireops-Origin': 'ui',
      },
      body: JSON.stringify({ role }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error)
    user.role = role
    toast.add({ title: 'Role updated', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to update role', description: e?.message, color: 'error' })
    await loadUsers()
  }
}

// --- Service Accounts & SSO Group Roles ---
const serviceAccounts = ref<any[]>([])
const serviceAccountsLoading = ref(false)
const serviceAccountForm = ref({ name: '', description: '', role: 'viewer' })
const createdApiKey = ref('')
const apiKeyNames = ref<Record<string, string>>({})

const ssoGroupRoles = ref<any[]>([])
const ssoGroupRolesLoading = ref(false)
const ssoGroupRoleForm = ref({ group: '', role: 'viewer' })

async function apiFetch(path: string, options: RequestInit = {}) {
  const res = await fetch(`${$pb.baseURL}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${$pb.authStore.token}`,
      'X-Wireops-Origin': 'ui',
      ...(options.headers || {}),
    },
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error || 'request failed')
  return data
}

async function loadServiceAccounts() {
  if (!isAdmin.value) return
  serviceAccountsLoading.value = true
  try {
    const accounts = await apiFetch('/api/custom/service-accounts')
    accounts.forEach((acc: any) => {
      if (!apiKeyNames.value[acc.id]) apiKeyNames.value[acc.id] = 'default'
    })
    serviceAccounts.value = accounts
  } catch (e: any) {
    toast.add({ title: 'Failed to load service accounts', description: e?.message, color: 'error' })
  } finally {
    serviceAccountsLoading.value = false
  }
}

async function createServiceAccount() {
  try {
    await apiFetch('/api/custom/service-accounts', {
      method: 'POST',
      body: JSON.stringify({ ...serviceAccountForm.value, enabled: true }),
    })
    serviceAccountForm.value = { name: '', description: '', role: 'viewer' }
    await loadServiceAccounts()
    toast.add({ title: 'Service account created', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to create service account', description: e?.message, color: 'error' })
  }
}

async function issueApiKey(account: any) {
  try {
    const data = await apiFetch(`/api/custom/service-accounts/${account.id}/keys`, {
      method: 'POST',
      body: JSON.stringify({ name: apiKeyNames.value[account.id] || 'default' }),
    })
    createdApiKey.value = data.api_key
    apiKeyNames.value[account.id] = 'default'
    await loadServiceAccounts()
    toast.add({ title: 'API key issued', description: 'Copy it now. It will not be shown again.', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to issue API key', description: e?.message, color: 'error' })
  }
}

async function revokeApiKey(account: any, key: any) {
  try {
    await apiFetch(`/api/custom/service-accounts/${account.id}/keys/${key.id}`, { method: 'DELETE' })
    await loadServiceAccounts()
    toast.add({ title: 'API key revoked', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to revoke API key', description: e?.message, color: 'error' })
  }
}

async function loadSSOGroupRoles() {
  if (!isAdmin.value) return
  ssoGroupRolesLoading.value = true
  try {
    ssoGroupRoles.value = await apiFetch('/api/custom/sso-group-roles')
  } catch (e: any) {
    toast.add({ title: 'Failed to load SSO group mappings', description: e?.message, color: 'error' })
  } finally {
    ssoGroupRolesLoading.value = false
  }
}

async function createSSOGroupRole() {
  try {
    await apiFetch('/api/custom/sso-group-roles', {
      method: 'POST',
      body: JSON.stringify(ssoGroupRoleForm.value),
    })
    ssoGroupRoleForm.value = { group: '', role: 'viewer' }
    await loadSSOGroupRoles()
    toast.add({ title: 'SSO group mapping saved', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to save SSO mapping', description: e?.message, color: 'error' })
  }
}

async function deleteSSOGroupRole(mapping: any) {
  try {
    await apiFetch(`/api/custom/sso-group-roles/${mapping.id}`, { method: 'DELETE' })
    await loadSSOGroupRoles()
    toast.add({ title: 'SSO group mapping deleted', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to delete SSO mapping', description: e?.message, color: 'error' })
  }
}

const route = useRoute()
const activeTab = ref((route.query.tab as string) || 'general')
const tabs = [
  { label: 'General',        value: 'general',        icon: 'i-lucide-settings-2' },
  { label: 'Notifications',  value: 'notifications',  icon: 'i-lucide-bell' },
  { label: 'Security',       value: 'security',       icon: 'i-lucide-shield' },
  { label: 'Worker Policies',value: 'worker-policies',icon: 'i-lucide-shield-check' },
  { label: 'Audit',          value: 'audit',          icon: 'i-lucide-clipboard-list' },
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

const auditLogs = ref<any[]>([])
const auditTotal = ref(0)
const auditPage = ref(1)
const auditPerPage = 25
const auditLoading = ref(false)
const auditDateRange = ref({
  start: today(getLocalTimeZone()).subtract({ days: 30 }),
  end: today(getLocalTimeZone()),
})

function auditBoundaryISO(value: { toDate: (timeZone: string) => Date }, endOfDay = false) {
  const date = value.toDate(getLocalTimeZone())
  if (endOfDay) {
    date.setHours(23, 59, 59, 999)
  } else {
    date.setHours(0, 0, 0, 0)
  }
  return date.toISOString()
}

const auditFilters = ref({
  from: auditBoundaryISO(auditDateRange.value.start),
  to: auditBoundaryISO(auditDateRange.value.end, true),
  action: '',
  resource_type: '',
  resource_id: '',
  actor_type: 'all',
  actor_id: '',
  origin: 'all',
  status: 'all',
})

const auditStatusOptions = [
  { label: 'Any status', value: 'all' },
  { label: 'Success', value: 'success' },
  { label: 'Error', value: 'error' },
]

const auditActorTypeOptions = [
  { label: 'Any actor', value: 'all' },
  { label: 'Anonymous', value: 'anonymous' },
  { label: 'User', value: 'user' },
  { label: 'Agent', value: 'agent' },
  { label: 'System', value: 'system' },
  { label: 'Worker', value: 'worker' },
]

const auditOriginOptions = [
  { label: 'Any origin', value: 'all' },
  { label: 'UI', value: 'ui' },
  { label: 'API', value: 'api' },
  { label: 'API Key', value: 'api_key' },
  { label: 'Webhook', value: 'webhook' },
  { label: 'Setup', value: 'setup' },
  { label: 'System', value: 'system' },
  { label: 'Worker', value: 'worker' },
]

const auditDateFormatter = new DateFormatter('en-US', { dateStyle: 'medium' })
const auditDateRangeLabel = computed(() => {
  const { start, end } = auditDateRange.value
  if (!start || !end) return 'Select date range'
  return `${auditDateFormatter.format(start.toDate(getLocalTimeZone()))} - ${auditDateFormatter.format(end.toDate(getLocalTimeZone()))}`
})

function formatAuditDate(value: string) {
  if (!value) return ''
  const tz = appSettings.value.timezone && appSettings.value.timezone !== 'system' ? appSettings.value.timezone : Intl.DateTimeFormat().resolvedOptions().timeZone
  return new Intl.DateTimeFormat('en-US', {
    dateStyle: 'short',
    timeStyle: 'medium',
    timeZone: tz
  }).format(new Date(value))
}

function formatAuditMetadata(log: any) {
  const metadata = log?.metadata || {}
  const parts: string[] = []

  if (Array.isArray(metadata.changed_fields) && metadata.changed_fields.length) {
    parts.push(`body: ${metadata.changed_fields.join(', ')}`)
  }
  if (Array.isArray(metadata.record_changed_fields) && metadata.record_changed_fields.length) {
    parts.push(`record: ${metadata.record_changed_fields.join(', ')}`)
  }
  if (Array.isArray(metadata.query_keys) && metadata.query_keys.length) {
    parts.push(`query: ${metadata.query_keys.join(', ')}`)
  }
  if (metadata.request_id) {
    parts.push(`request: ${metadata.request_id}`)
  }

  return parts.join(' • ')
}

function applyAuditDateRange() {
  const { start, end } = auditDateRange.value
  if (!start || !end) return
  auditFilters.value.from = auditBoundaryISO(start)
  auditFilters.value.to = auditBoundaryISO(end, true)
  applyAuditFilters()
}

async function loadAuditLogs(page = auditPage.value) {
  auditLoading.value = true
  try {
    auditPage.value = page
    const data = await listAuditLogs({
      page: auditPage.value,
      perPage: auditPerPage,
      ...auditFilters.value,
      actor_type: auditFilters.value.actor_type === 'all' ? '' : auditFilters.value.actor_type,
      origin: auditFilters.value.origin === 'all' ? '' : auditFilters.value.origin,
      status: auditFilters.value.status === 'all' ? '' : auditFilters.value.status,
    })
    auditLogs.value = data.items || []
    auditTotal.value = data.totalItems || 0
  } catch (e: any) {
    toast.add({ title: 'Failed to load audit logs', description: e?.message, color: 'error' })
  } finally {
    auditLoading.value = false
  }
}

function applyAuditFilters() {
  loadAuditLogs(1)
}

function clearAuditFilters() {
  auditDateRange.value = {
    start: today(getLocalTimeZone()).subtract({ days: 30 }),
    end: today(getLocalTimeZone()),
  }
  auditFilters.value = {
    from: auditBoundaryISO(auditDateRange.value.start),
    to: auditBoundaryISO(auditDateRange.value.end, true),
    action: '',
    resource_type: '',
    resource_id: '',
    actor_type: 'all',
    actor_id: '',
    origin: 'all',
    status: 'all',
  }
  loadAuditLogs(1)
}

watch(activeTab, (val) => {
  if (val === 'users') loadUsers()
  if (val === 'integrations') loadIntegrations()
  if (val === 'worker-policies') loadWorkerPolicy()
  if (val === 'audit') loadAuditLogs()
  if (val === 'security') {
    loadServiceAccounts()
    loadSSOGroupRoles()
  }
}, { immediate: true })
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
        <div class="space-y-4">
          <div>
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

      <UCard v-if="isAdmin">
        <template #header>
          <h3 class="font-semibold">SSO Group Role Mapping</h3>
          <p class="text-xs text-gray-500 mt-0.5">Map identity provider groups to fixed WireOps roles. No match means SSO login is denied.</p>
        </template>
        <div class="space-y-4">
          <UFormField label="Groups Claim">
            <div class="flex gap-2">
              <UInput v-model="appSettings.sso_groups_claim" placeholder="groups" class="max-w-sm" />
              <UButton label="Save Claim" :loading="appSettingsSaving" @click="handleSaveAppSettings({ title: 'SSO claim saved', description: 'SSO group claim mapping was updated.' })" />
            </div>
          </UFormField>
          <form class="flex flex-col gap-2 sm:flex-row" @submit.prevent="createSSOGroupRole">
            <UInput v-model="ssoGroupRoleForm.group" placeholder="wireops-admins" class="flex-1" required />
            <USelectMenu v-model="ssoGroupRoleForm.role" :items="roleOptions" value-key="value" class="w-full sm:w-40" />
            <UButton type="submit" label="Add Mapping" icon="i-lucide-plus" />
          </form>
          <div v-if="ssoGroupRolesLoading" class="text-sm text-gray-500">Loading mappings...</div>
          <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
            <li v-for="mapping in ssoGroupRoles" :key="mapping.id" class="flex items-center justify-between py-3">
              <div>
                <p class="text-sm font-medium">{{ mapping.group }}</p>
                <p class="text-xs text-gray-500">Role: {{ mapping.role }}</p>
              </div>
              <UButton icon="i-lucide-trash-2" size="xs" variant="ghost" color="error" @click="deleteSSOGroupRole(mapping)" />
            </li>
          </ul>
        </div>
      </UCard>

      <UCard v-if="isAdmin">
        <template #header>
          <h3 class="font-semibold">Service Accounts & API Keys</h3>
          <p class="text-xs text-gray-500 mt-0.5">Programmatic access for agents and external clients. API keys inherit the service account role.</p>
        </template>
        <div class="space-y-4">
          <UAlert
            v-if="createdApiKey"
            color="warning"
            title="Copy this API key now"
            :description="createdApiKey"
            icon="i-lucide-key-round"
          />
          <form class="grid gap-2 md:grid-cols-[1fr_1fr_160px_auto]" @submit.prevent="createServiceAccount">
            <UInput v-model="serviceAccountForm.name" placeholder="automation-bot" required />
            <UInput v-model="serviceAccountForm.description" placeholder="Description" />
            <USelectMenu v-model="serviceAccountForm.role" :items="roleOptions" value-key="value" />
            <UButton type="submit" label="Create" icon="i-lucide-plus" />
          </form>
          <div v-if="serviceAccountsLoading" class="text-sm text-gray-500">Loading service accounts...</div>
          <div v-else-if="serviceAccounts.length === 0" class="text-sm text-gray-500">No service accounts yet.</div>
          <div v-else class="space-y-3">
            <div v-for="account in serviceAccounts" :key="account.id" class="rounded-lg border border-gray-200 p-3 dark:border-gray-800">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-medium">{{ account.name }}</p>
                  <p class="text-xs text-gray-500">{{ account.description || 'No description' }}</p>
                  <p v-if="account.created_by_email" class="text-xs text-gray-400 mt-0.5">Created by {{ account.created_by_email }}</p>
                  <UBadge :label="account.role" color="primary" variant="subtle" size="xs" class="mt-2" />
                </div>
                <div class="flex gap-2">
                  <UInput v-model="apiKeyNames[account.id]" placeholder="key name" size="xs" class="w-32" />
                  <UButton size="xs" label="Issue Key" icon="i-lucide-key-round" @click="issueApiKey(account)" />
                </div>
              </div>
              <ul v-if="account.keys?.length" class="mt-3 divide-y divide-gray-100 text-xs dark:divide-gray-800">
                <li v-for="key in account.keys" :key="key.id" class="flex items-center justify-between py-2">
                  <span>{{ key.name }} · {{ key.key_prefix }} · {{ key.revoked ? 'revoked' : 'active' }}</span>
                  <UButton v-if="!key.revoked" size="xs" variant="ghost" color="error" label="Revoke" @click="revokeApiKey(account, key)" />
                </li>
              </ul>
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
        <form class="flex flex-col gap-2 sm:flex-row" @submit.prevent="sendInvite">
          <UInput v-model="inviteEmail" type="email" placeholder="user@example.com" icon="i-lucide-mail" class="flex-1" required />
          <USelectMenu v-model="inviteRole" :items="roleOptions" value-key="value" class="w-full sm:w-40" />
          <UButton type="submit" label="Send Invite" icon="i-lucide-send" :loading="inviteLoading" />
        </form>
      </UCard>

      <UCard>
        <template #header><h3 class="font-semibold">Users</h3></template>
        <div v-if="usersLoading" class="text-sm text-gray-500">Loading...</div>
        <div v-else-if="users.length === 0" class="text-sm text-gray-500">No users found.</div>
        <ul v-else class="divide-y divide-gray-100 dark:divide-gray-800">
          <li v-for="u in users" :key="u.id" class="flex items-center justify-between py-3 first:pt-0 last:pb-0">
            <div class="flex items-center gap-3">
              <div class="flex items-center justify-center w-8 h-8 rounded-full" :class="u.disabled ? 'bg-gray-400/10' : 'bg-yellow-400/10'">
                <UIcon name="i-lucide-user" class="w-4 h-4" :class="u.disabled ? 'text-gray-400' : 'text-yellow-400'" />
              </div>
              <div>
                <ULink
                  :to="`/settings/users/${u.id}`"
                  active-class="text-primary"
                  inactive-class="text-sm font-medium text-gray-900 hover:text-yellow-500 dark:text-white dark:hover:text-yellow-400"
                  :class="{ 'opacity-50': u.disabled }"
                >
                  {{ u.email }}
                </ULink>
                <UBadge v-if="u.is_sso" label="SSO" color="primary" variant="subtle" size="xs" class="ml-2" />
                <p class="text-xs text-gray-500 mt-0.5">Joined {{ new Date(u.created).toLocaleDateString() }}</p>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <template v-if="u.protected">
                <span class="text-sm font-medium text-gray-500 w-36 text-right px-3">Admin</span>
                <UBadge label="Protected" color="warning" variant="subtle" size="xs" />
              </template>
              <template v-else>
                <USelectMenu
                  :model-value="u.role || 'viewer'"
                  :items="roleOptions"
                  value-key="value"
                  class="w-36"
                  :disabled="u.is_sso"
                  @update:model-value="updateUserRole(u, String($event))"
                />
                <UBadge v-if="u.disabled" label="Disabled" color="neutral" variant="subtle" size="xs" />
              </template>
              <UBadge v-if="u.id === $pb.authStore.record?.id" label="You" color="neutral" variant="subtle" size="xs" />
              <UButton
                v-if="!u.protected && u.id !== $pb.authStore.record?.id"
                :icon="u.disabled ? 'i-lucide-user-check' : 'i-lucide-user-x'"
                size="xs"
                variant="ghost"
                :color="u.disabled ? 'success' : 'warning'"
                :title="u.disabled ? 'Enable user' : 'Disable user'"
                @click="toggleUserDisabled(u)"
              />
            </div>
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

    <!-- Audit -->
    <div v-if="activeTab === 'audit'" class="space-y-6">
      <UCard>
        <template #header>
          <div class="flex items-center justify-between gap-3">
            <h3 class="font-semibold">Audit Events</h3>
            <div class="flex items-center gap-2">
              <UPopover>
                <UButton
                  icon="i-lucide-calendar-range"
                  variant="outline"
                  size="md"
                  color="neutral"
                  :label="auditDateRangeLabel"
                />

                <template #content>
                  <UCalendar
                    v-model="auditDateRange"
                    range
                    :number-of-months="2"
                    @update:model-value="applyAuditDateRange"
                  />
                </template>
              </UPopover>
              <UButton
                icon="i-lucide-settings"
                variant="outline"
                size="md"
                aria-label="Audit settings"
                @click="showAuditSettingsModal = true"
              />
              <UButton
                icon="i-lucide-refresh-cw"
                variant="outline"
                size="md"
                aria-label="Refresh audit events"
                :loading="auditLoading"
                @click="loadAuditLogs()"
              />
            </div>
          </div>
        </template>

        <form class="flex flex-wrap items-center gap-2 mb-4" @submit.prevent="applyAuditFilters">
          <UInput v-model="auditFilters.action" placeholder="Action" size="sm" class="w-32" />
          <UInput v-model="auditFilters.resource_type" placeholder="Resource Type" size="sm" class="w-32" />
          <UInput v-model="auditFilters.resource_id" placeholder="Resource ID" size="sm" class="w-32" />
          <USelect v-model="auditFilters.actor_type" :items="auditActorTypeOptions" size="sm" class="w-32" />
          <UInput v-model="auditFilters.actor_id" placeholder="Actor ID" size="sm" class="w-32" />
          <USelect v-model="auditFilters.origin" :items="auditOriginOptions" size="sm" class="w-32" />
          <USelect v-model="auditFilters.status" :items="auditStatusOptions" size="sm" class="w-32" />
          <div class="flex gap-1 ml-auto">
            <UButton icon="i-lucide-x" variant="ghost" size="sm" @click="clearAuditFilters" />
            <UButton type="submit" icon="i-lucide-search" size="sm" />
          </div>
        </form>

        <div v-if="auditLoading" class="text-sm text-gray-500 py-2">Loading audit events...</div>
        <div v-else-if="auditLogs.length === 0" class="text-sm text-gray-500 py-2">No audit events found.</div>
        <div v-else class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead class="text-left text-xs uppercase text-gray-500 border-b border-gray-200 dark:border-gray-800">
              <tr>
                <th class="pb-2 pr-4 font-medium">Time</th>
                <th class="pb-2 pr-4 font-medium">Action</th>
                <th class="pb-2 pr-4 font-medium">Resource</th>
                <th class="pb-2 pr-4 font-medium">Actor</th>
                <th class="pb-2 pr-4 font-medium">Origin</th>
                <th class="pb-2 pr-4 font-medium">Status</th>
                <th class="pb-2 pr-4 font-medium">Metadata</th>
                <th class="pb-2 pr-4 font-medium">Error</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-gray-800">
              <tr v-for="log in auditLogs" :key="log.id">
                <td class="py-1.5 pr-4 whitespace-nowrap text-xs">{{ formatAuditDate(log.created) }}</td>
                <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">{{ log.action }}</td>
                <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">
                  {{ log.resource_type }}<span v-if="log.resource_id">/{{ log.resource_id }}</span>
                </td>
                <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">
                  {{ log.actor_type }}<span v-if="log.actor_id">/{{ log.actor_id }}</span>
                </td>
                <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">{{ log.origin }}</td>
                <td class="py-1.5 pr-4">
                  <UBadge
                    :label="log.status"
                    :color="log.status === 'success' ? 'success' : 'error'"
                    variant="subtle"
                    size="xs"
                    :ui="{ rounded: 'rounded-sm', padding: { xs: 'px-1.5 py-0' } }"
                  />
                </td>
                <td class="py-1.5 pr-4 text-[11px] text-gray-500 min-w-64">
                  {{ formatAuditMetadata(log) || '-' }}
                </td>
                <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">{{ log.error_code || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="flex items-center justify-between pt-3 mt-2 border-t border-gray-100 dark:border-gray-800">
          <p class="text-xs text-gray-500">{{ auditTotal }} events</p>
          <UPagination
            v-model:page="auditPage"
            :items-per-page="auditPerPage"
            :total="auditTotal"
            size="sm"
            @update:page="loadAuditLogs"
          />
        </div>
      </UCard>
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

    <!-- Audit Settings Modal -->
    <UModal v-model:open="showAuditSettingsModal">
      <template #content>
        <UCard :ui="{ ring: '', divide: 'divide-y divide-gray-100 dark:divide-gray-800' }">
          <template #header>
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">Audit Settings</h3>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              Configure how long audit events and job run logs are retained.
            </p>
          </template>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 p-4">
            <UFormField label="Audit retention (days)">
              <UInput
                v-model.number="appSettings.audit_retention_days"
                type="number"
                min="1"
              />
            </UFormField>
            <UFormField label="Job run retention (days)">
              <UInput
                v-model.number="appSettings.job_run_retention_days"
                type="number"
                min="1"
              />
            </UFormField>
          </div>

          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton
                label="Cancel"
                color="neutral"
                variant="ghost"
                :disabled="appSettingsSaving"
                @click="showAuditSettingsModal = false"
              />
              <UButton
                icon="i-lucide-save"
                label="Save"
                :loading="appSettingsSaving"
                @click="handleSaveAuditSettings"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>

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
