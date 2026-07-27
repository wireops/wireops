<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { DateFormatter, getLocalTimeZone, today } from '@internationalized/date'

const toast = useToast()
const { getAppSettings, saveAppSettings, listAuditLogs } = useApi()

// --- App Settings (Audit / Job Run Retention) ---
const appSettings = ref({
  id: '',
  timezone: '',
  audit_retention_days: 30,
  job_run_retention_days: 7,
})
const appSettingsSaving = ref(false)

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

async function handleSaveAuditSettings() {
  if (!isPositiveInteger(appSettings.value.audit_retention_days) || !isPositiveInteger(appSettings.value.job_run_retention_days)) {
    toast.add({ title: 'Invalid retention settings', description: 'Retention values must be positive whole numbers.', color: 'error' })
    return
  }
  appSettingsSaving.value = true
  try {
    const data = await saveAppSettings({
      audit_retention_days: appSettings.value.audit_retention_days,
      job_run_retention_days: appSettings.value.job_run_retention_days,
    })
    if (data) {
      appSettings.value.id = data.id
      appSettings.value.audit_retention_days = data.audit_retention_days || 30
      appSettings.value.job_run_retention_days = data.job_run_retention_days || 7
    }
    toast.add({ title: 'Audit settings saved', description: 'Audit and job run retention settings were updated.', color: 'success' })
    showAuditSettingsModal.value = false
  } catch (e: any) {
    toast.add({ title: 'Failed to save settings', description: e?.message, color: 'error' })
  } finally {
    appSettingsSaving.value = false
  }
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

onMounted(async () => {
  try {
    const data = await getAppSettings()
    if (data) {
      appSettings.value.id = data.id
      appSettings.value.timezone = data.timezone || 'system'
      appSettings.value.audit_retention_days = data.audit_retention_days || 30
      appSettings.value.job_run_retention_days = data.job_run_retention_days || 7
    }
  } catch (e) {
    // ignore
  }

  loadAuditLogs()
})
</script>

<template>
  <div class="space-y-6">
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

      <form class="space-y-2 mb-4" @submit.prevent="applyAuditFilters">
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
          <AppTextInput v-model="auditFilters.action" placeholder="Action" />
          <AppTextInput v-model="auditFilters.resource_type" placeholder="Resource Type" />
          <AppTextInput v-model="auditFilters.resource_id" placeholder="Resource ID" />
        </div>
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-2 items-center">
          <AppSelectInput v-model="auditFilters.actor_type" :items="auditActorTypeOptions" />
          <AppTextInput v-model="auditFilters.actor_id" placeholder="Actor ID" />
          <AppSelectInput v-model="auditFilters.origin" :items="auditOriginOptions" />
          <AppSelectInput v-model="auditFilters.status" :items="auditStatusOptions" />
        </div>
        <div class="flex justify-end gap-1">
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
              <AppTextInput
                :model-value="String(appSettings.audit_retention_days)"
                type="number"
                @update:model-value="updateAuditRetentionDays"
              />
            </UFormField>
            <UFormField label="Job run retention (days)">
              <AppTextInput
                :model-value="String(appSettings.job_run_retention_days)"
                type="number"
                @update:model-value="updateJobRunRetentionDays"
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
</template>
