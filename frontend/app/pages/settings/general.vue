<script setup lang="ts">
import { ref, onMounted } from 'vue'

const toast = useToast()
const { getAppSettings, saveAppSettings, keyscan } = useApi()

const keyscanHost = ref('')
const keyscanPort = ref(22)
const keyscanLoading = ref(false)
const keyscanResult = ref('')

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

async function handleSaveAppSettings() {
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
</script>

<template>
  <div class="space-y-6">
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
      <template #header><h3 class="font-semibold">SSH Host Key Scanner</h3></template>
      <p class="text-sm text-gray-500 mb-3">
        Scan a remote host to retrieve its SSH public key for use in credentials.
      </p>
      <form class="flex flex-col sm:flex-row gap-2" @submit.prevent="runKeyscan">
        <AppTextInput v-model="keyscanHost" placeholder="github.com" class="flex-1" />
        <div class="flex gap-2">
          <AppTextInput
            :model-value="String(keyscanPort)"
            type="number"
            placeholder="22"
            class="w-20"
            @update:model-value="(v) => keyscanPort = Number(v)"
          />
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
</template>
