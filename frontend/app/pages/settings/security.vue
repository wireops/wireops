<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { DateFormatter, getLocalTimeZone, today } from '@internationalized/date'

const { $pb } = useNuxtApp()
const toast = useToast()
const { isAdmin } = usePermissions()
const { getAppSettings, saveAppSettings, getGlobalWorkerPolicy, saveGlobalWorkerPolicy, listAuditLogs } = useApi()

const route = useRoute()
const router = useRouter()
const activeTab = ref((route.query.tab as string) || 'credentials')

const tabs = computed(() => {
  const list = [
    { label: 'Credentials', value: 'credentials', icon: 'i-lucide-key' }
  ]
  if (isAdmin.value) {
    list.push({ label: 'SSO Mappings', value: 'sso-mappings', icon: 'i-lucide-users' })
  }
  list.push({ label: 'Worker Policies', value: 'worker-policies', icon: 'i-lucide-shield-check' })
  list.push({ label: 'Audit', value: 'audit', icon: 'i-lucide-clipboard-list' })
  return list
})

watch(activeTab, (newVal) => {
  if (route.query.tab !== newVal) {
    router.replace({ query: { ...route.query, tab: newVal } })
  }
})

watch(() => route.query.tab, (newVal) => {
  if (newVal && newVal !== activeTab.value) {
    activeTab.value = newVal as string
  }
})

const roleOptions = [
  { label: 'Viewer', value: 'viewer' },
  { label: 'Operator', value: 'operator' },
  { label: 'Admin', value: 'admin' },
]

// --- App Settings (Timezone, SSO Group Claim, Audit Retention) ---
const appSettings = ref({
  id: '',
  timezone: '',
  audit_retention_days: 30,
  job_run_retention_days: 7,
  sso_groups_claim: 'groups',
})
const appSettingsSaving = ref(false)
const appSettingsLoaded = ref(false)

async function handleSaveAppSettings(options: { title?: string; description?: string } = {}) {
  appSettingsSaving.value = true
  try {
    const tzToSave = appSettings.value.timezone === 'system' ? '' : appSettings.value.timezone
    const payload: any = { timezone: tzToSave }
    payload.audit_retention_days = appSettings.value.audit_retention_days
    payload.job_run_retention_days = appSettings.value.job_run_retention_days
    payload.sso_groups_claim = appSettings.value.sso_groups_claim || 'groups'

    const data = await saveAppSettings(payload)
    if (data) {
      appSettings.value.id = data.id
      appSettings.value.timezone = data.timezone || 'system'
      appSettings.value.audit_retention_days = data.audit_retention_days || 30
      appSettings.value.job_run_retention_days = data.job_run_retention_days || 7
      appSettings.value.sso_groups_claim = data.sso_groups_claim || 'groups'
      appSettingsLoaded.value = true
    }
    toast.add({
      title: options.title || 'Settings saved',
      description: options.description || 'Settings updated successfully.',
      color: 'success'
    })
    return true
  } catch (e: any) {
    toast.add({ title: 'Failed to save settings', description: e?.message, color: 'error' })
    return false
  } finally {
    appSettingsSaving.value = false
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

// --- SSO Group Roles ---

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

// --- Worker Policies ---
const workerPolicy = ref({
  enabled: true,
  allowed_volumes: [] as string[],
  allowed_networks: [] as string[],
  allowed_images: [] as string[],
  allowed_cap_add: [] as string[],
  allowed_devices: [] as string[],
  allowed_security_opt: [] as string[],
  prevent_latest_images: false,
  block_host_volumes: false,
  block_privileged: false,
  block_host_network: false,
  block_host_pid: false,
  block_host_ipc: false,
  block_docker_socket: false,
  allow_render_overrides: false,
})
const workerPolicyLoading = ref(false)
const workerPolicySaving = ref(false)
const showConfirmToggleModal = ref(false)
const pendingToggleValue = ref(false)
const showStrictPresetModal = ref(false)

async function loadWorkerPolicy() {
  workerPolicyLoading.value = true
  try {
    const data = await getGlobalWorkerPolicy() as any
    workerPolicy.value = {
      enabled:               data?.enabled ?? true,
      allowed_volumes:       data?.allowed_volumes  ?? [],
      allowed_networks:      data?.allowed_networks ?? [],
      allowed_images:        data?.allowed_images   ?? [],
      allowed_cap_add:       data?.allowed_cap_add  ?? [],
      allowed_devices:       data?.allowed_devices  ?? [],
      allowed_security_opt:  data?.allowed_security_opt ?? [],
      prevent_latest_images: data?.prevent_latest_images ?? false,
      block_host_volumes:    data?.block_host_volumes    ?? false,
      block_privileged:      data?.block_privileged      ?? false,
      block_host_network:    data?.block_host_network    ?? false,
      block_host_pid:        data?.block_host_pid        ?? false,
      block_host_ipc:        data?.block_host_ipc        ?? false,
      block_docker_socket:   data?.block_docker_socket   ?? false,
      allow_render_overrides: data?.allow_render_overrides ?? false,
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
    workerPolicy.value.allowed_cap_add = workerPolicy.value.allowed_cap_add.filter(c => c.trim() !== '')
    workerPolicy.value.allowed_devices = workerPolicy.value.allowed_devices.filter(d => d.trim() !== '')
    workerPolicy.value.allowed_security_opt = workerPolicy.value.allowed_security_opt.filter(s => s.trim() !== '')

    await saveGlobalWorkerPolicy(workerPolicy.value)
    toast.add({ title: 'Worker policy saved', color: 'success' })
  } catch (e: any) {
    toast.add({ title: 'Failed to save policy', description: e?.message, color: 'error' })
  } finally {
    workerPolicySaving.value = false
  }
}

function requestStrictProductionPreset() {
  showStrictPresetModal.value = true
}

function cancelStrictProductionPreset() {
  showStrictPresetModal.value = false
}

async function applyStrictProductionPreset() {
  workerPolicy.value.enabled = true
  workerPolicy.value.block_privileged = true
  workerPolicy.value.block_host_network = true
  workerPolicy.value.block_host_pid = true
  workerPolicy.value.block_host_ipc = true
  workerPolicy.value.block_docker_socket = true
  workerPolicy.value.allow_render_overrides = false
  await saveWorkerPolicyGlobal()
  showStrictPresetModal.value = false
}

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

// --- Audit Logs ---
const auditLogs = ref<any[]>([])
const auditTotal = ref(0)
const auditPage = ref(1)
const auditPerPage = 25
const auditLoading = ref(false)
const auditDateRange = ref({
  start: today(getLocalTimeZone()).subtract({ days: 30 }),
  end: today(getLocalTimeZone()),
})
const showAuditSettingsModal = ref(false)

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

async function handleSaveAuditSettings() {
  const saved = await handleSaveAppSettings({
    title: 'Audit settings saved',
    description: 'Audit and job run retention settings were updated.',
  })
  if (saved) {
    showAuditSettingsModal.value = false
  }
}

onMounted(async () => {
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
  }

  loadSSOGroupRoles()
  loadWorkerPolicy()
  loadAuditLogs()
})
</script>

<template>
  <div class="space-y-6">
    <UTabs v-model="activeTab" :items="tabs" />

    <!-- Credentials Tab -->
    <div v-if="activeTab === 'credentials'" class="space-y-6">
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

    <!-- SSO Mappings Tab -->
    <div v-if="activeTab === 'sso-mappings' && isAdmin" class="space-y-6">
      <UCard>
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
    </div>

    <!-- Worker Policies Tab -->
    <div v-if="activeTab === 'worker-policies'" class="space-y-6">
      <div v-if="workerPolicyLoading" class="text-sm text-gray-500">Loading policy...</div>
      <template v-else>
        <!-- Global Enable/Disable Toggle -->
        <UCard class="bg-gradient-to-r from-yellow-500/10 via-amber-500/5 to-transparent border border-yellow-500/20">
          <div class="flex items-center justify-between gap-4">
            <div class="space-y-1">
              <h3 class="font-semibold text-lg flex items-center gap-2 text-gray-900 dark:text-wire-200">
                <UIcon name="i-lucide-shield-alert" class="w-5 h-5 text-yellow-500" />
                Worker Policy Security System
              </h3>
              <p class="text-sm text-gray-500 dark:text-gray-400">
                Enable or disable global security policy enforcement (volumes, networks, images, and container isolation) across all workers.
              </p>
            </div>
            <USwitch :model-value="workerPolicy.enabled" size="lg" @update:model-value="onTogglePolicyClick" />
          </div>
          <div class="flex justify-end mt-4">
            <UButton
              icon="i-lucide-shield-check"
              variant="outline"
              color="error"
              label="Apply Strict Production Preset"
              :loading="workerPolicySaving"
              @click="requestStrictProductionPreset"
            />
          </div>
        </UCard>
        <WorkerPolicyForm v-model="workerPolicy" @save="saveWorkerPolicyGlobal" />
      </template>

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

      <!-- Confirm Strict Production Preset Modal -->
      <UModal v-model:open="showStrictPresetModal">
        <template #content>
          <ConfirmStrictPresetModal
            :loading="workerPolicySaving"
            @confirm="applyStrictProductionPreset"
            @cancel="cancelStrictProductionPreset"
          />
        </template>
      </UModal>
    </div>

    <!-- Audit Tab -->
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
    </div>
  </div>
</template>
