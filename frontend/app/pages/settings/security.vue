<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'

const { $pb } = useNuxtApp()
const toast = useToast()
const { isAdmin } = usePermissions()
const { getAppSettings, saveAppSettings, getGlobalWorkerPolicy, saveGlobalWorkerPolicy } = useApi()

const route = useRoute()
const router = useRouter()

function defaultTab() {
  return isAdmin.value ? 'sso-mappings' : 'worker-policies'
}

const tabs = computed(() => {
  const list = []
  if (isAdmin.value) {
    list.push({ label: 'SSO Mappings', value: 'sso-mappings', icon: 'i-lucide-users' })
  }
  list.push({ label: 'Worker Policies', value: 'worker-policies', icon: 'i-lucide-shield-check' })
  return list
})

function isValidTab(val: unknown): val is string {
  return tabs.value.some(t => t.value === val)
}

const activeTab = ref(isValidTab(route.query.tab) ? (route.query.tab as string) : defaultTab())

watch(activeTab, (newVal) => {
  if (route.query.tab !== newVal) {
    router.replace({ query: { ...route.query, tab: newVal } })
  }
})

watch(() => route.query.tab, (newVal) => {
  if (isValidTab(newVal) && newVal !== activeTab.value) {
    activeTab.value = newVal
  } else if (!isValidTab(newVal) && newVal) {
    activeTab.value = defaultTab()
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

function parsePositiveInteger(value: unknown) {
  const text = String(value).trim()
  if (!/^\d+$/.test(text)) return null
  const numberValue = Number(text)
  return Number.isFinite(numberValue) && Number.isInteger(numberValue) && numberValue > 0 ? numberValue : null
}

function isPositiveInteger(value: unknown) {
  return parsePositiveInteger(value) !== null
}

function updateAuditRetentionDays(value: unknown) {
  const next = parsePositiveInteger(value)
  if (next !== null) appSettings.value.audit_retention_days = next
}

function updateJobRunRetentionDays(value: unknown) {
  const next = parsePositiveInteger(value)
  if (next !== null) appSettings.value.job_run_retention_days = next
}

async function handleSaveAppSettings(options: { title?: string; description?: string } = {}) {
  if (!isPositiveInteger(appSettings.value.audit_retention_days) || !isPositiveInteger(appSettings.value.job_run_retention_days)) {
    toast.add({ title: 'Invalid retention settings', description: 'Retention values must be positive whole numbers.', color: 'error' })
    return false
  }
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
  const group = ssoGroupRoleForm.value.group.trim()
  if (!group) {
    toast.add({ title: 'SSO group is required', color: 'error' })
    return
  }
  try {
    await apiFetch('/api/custom/sso-group-roles', {
      method: 'POST',
      body: JSON.stringify({ ...ssoGroupRoleForm.value, group }),
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
})
</script>

<template>
  <div class="space-y-6">
    <UTabs v-model="activeTab" :items="tabs" />

    <!-- SSO Mappings Tab -->
    <div v-if="activeTab === 'sso-mappings' && isAdmin" class="space-y-6">
      <AppPanelCard>
        <template #header>
          <h3 class="font-semibold">SSO Groups Claim</h3>
          <p class="text-xs text-gray-500 mt-0.5">Name of the claim in the SSO token that contains the user's group memberships.</p>
        </template>
        <UFormField label="Groups Claim">
          <div class="flex gap-2">
            <div class="flex-1 min-w-0 max-w-sm">
              <AppTextInput v-model="appSettings.sso_groups_claim" placeholder="groups" />
            </div>
            <UButton label="Save Claim" class="shrink-0" :loading="appSettingsSaving" @click="handleSaveAppSettings({ title: 'SSO claim saved', description: 'SSO group claim mapping was updated.' })" />
          </div>
        </UFormField>
      </AppPanelCard>

      <AppPanelCard>
        <template #header>
          <h3 class="font-semibold">SSO Group Role Mapping</h3>
          <p class="text-xs text-gray-500 mt-0.5">Map identity provider groups to fixed WireOps roles. No match means SSO login is denied.</p>
        </template>
        <div class="space-y-4">
          <form class="flex flex-col gap-2 sm:flex-row" @submit.prevent="createSSOGroupRole">
            <AppTextInput v-model="ssoGroupRoleForm.group" placeholder="wireops-admins" aria-label="SSO group name" class="flex-1" />
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
      </AppPanelCard>
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
      <UModal v-model:open="showConfirmToggleModal" :ui="{ content: 'bg-gray-50 dark:bg-(--ui-bg)' }">
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
      <UModal v-model:open="showStrictPresetModal" :ui="{ content: 'bg-gray-50 dark:bg-(--ui-bg)' }">
        <template #content>
          <ConfirmStrictPresetModal
            :loading="workerPolicySaving"
            @confirm="applyStrictProductionPreset"
            @cancel="cancelStrictProductionPreset"
          />
        </template>
      </UModal>
    </div>

  </div>
</template>
