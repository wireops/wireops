<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { DateFormatter, getLocalTimeZone, today } from '@internationalized/date'
import { useRoute, useRouter } from 'vue-router'

const toast = useToast()
const { getAppSettings, saveAppSettings, listAuditLogs, listTerminalSessions } = useApi()

const route = useRoute()
const router = useRouter()

const tabs = [
  { label: 'Events', value: 'events', icon: 'i-lucide-history' },
  { label: 'Sessions', value: 'sessions', icon: 'i-lucide-terminal' },
]

function isValidTab(val: unknown): val is string {
  return tabs.some(t => t.value === val)
}

const activeTab = ref(isValidTab(route.query.tab) ? (route.query.tab as string) : 'events')

watch(activeTab, (newVal) => {
  if (route.query.tab !== newVal) {
    router.replace({ query: { ...route.query, tab: newVal } })
  }
  if (newVal === 'sessions' && terminalSessions.value.length === 0) {
    loadTerminalSessions()
  }
})

watch(() => route.query.tab, (newVal) => {
  if (isValidTab(newVal) && newVal !== activeTab.value) {
    activeTab.value = newVal
  }
})

// --- App Settings (Audit / Job Run Retention) ---
const appSettings = ref({
  id: '',
  timezone: '',
  audit_retention_days: 30,
  job_run_retention_days: 7,
  terminal_retention_days: 30,
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

function updateTerminalRetentionDays(value: unknown) {
  const next = parsePositiveInteger(value)
  if (next !== null) appSettings.value.terminal_retention_days = next
}

async function handleSaveAuditSettings() {
  if (
    !isPositiveInteger(appSettings.value.audit_retention_days)
    || !isPositiveInteger(appSettings.value.job_run_retention_days)
    || !isPositiveInteger(appSettings.value.terminal_retention_days)
  ) {
    toast.add({ title: 'Invalid retention settings', description: 'Retention values must be positive whole numbers.', color: 'error' })
    return
  }
  appSettingsSaving.value = true
  try {
    const data = await saveAppSettings({
      audit_retention_days: appSettings.value.audit_retention_days,
      job_run_retention_days: appSettings.value.job_run_retention_days,
      terminal_retention_days: appSettings.value.terminal_retention_days,
    })
    if (data) {
      appSettings.value.id = data.id
      appSettings.value.audit_retention_days = data.audit_retention_days || 30
      appSettings.value.job_run_retention_days = data.job_run_retention_days || 7
      appSettings.value.terminal_retention_days = data.terminal_retention_days || 30
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

// --- Terminal Sessions ---
const terminalSessions = ref<any[]>([])
const terminalTotal = ref(0)
const terminalPage = ref(1)
const terminalPerPage = 25
const terminalLoading = ref(false)

const terminalFilters = ref({
  stack_name: '',
  user_email: '',
  service_name: '',
  status: 'all',
})

const terminalStatusOptions = [
  { label: 'Any status', value: 'all' },
  { label: 'Open', value: 'open' },
  { label: 'Closed', value: 'closed' },
]

function formatTerminalDate(value: string) {
  if (!value || value.startsWith('0001-01-01')) return '-'
  return formatAuditDate(value)
}

async function loadTerminalSessions(page = terminalPage.value) {
  terminalLoading.value = true
  try {
    terminalPage.value = page
    const data = await listTerminalSessions({
      page: terminalPage.value,
      perPage: terminalPerPage,
      ...terminalFilters.value,
      status: terminalFilters.value.status === 'all' ? '' : terminalFilters.value.status,
    })
    terminalSessions.value = data.items || []
    terminalTotal.value = data.totalItems || 0
  } catch (e: any) {
    toast.add({ title: 'Failed to load terminal sessions', description: e?.message, color: 'error' })
  } finally {
    terminalLoading.value = false
  }
}

function applyTerminalFilters() {
  loadTerminalSessions(1)
}

function clearTerminalFilters() {
  terminalFilters.value = { stack_name: '', user_email: '', service_name: '', status: 'all' }
  loadTerminalSessions(1)
}

const replayModalOpen = ref(false)
const replaySession = ref<{ sessionId: string, label: string, exitCode: number } | null>(null)

function openReplay(session: any) {
  replaySession.value = {
    sessionId: session.session_id,
    label: `${session.stack_name || 'unknown stack'} / ${session.service_name || session.container_name || session.container_id}`,
    exitCode: session.exit_code,
  }
  replayModalOpen.value = true
}

onMounted(async () => {
  try {
    const data = await getAppSettings()
    if (data) {
      appSettings.value.id = data.id
      appSettings.value.timezone = data.timezone || 'system'
      appSettings.value.audit_retention_days = data.audit_retention_days || 30
      appSettings.value.job_run_retention_days = data.job_run_retention_days || 7
      appSettings.value.terminal_retention_days = data.terminal_retention_days || 30
    }
  } catch (e) {
    // ignore
  }

  if (activeTab.value === 'sessions') {
    loadTerminalSessions()
  } else {
    loadAuditLogs()
  }
})
</script>

<template>
  <div class="space-y-6">
    <UTabs v-model="activeTab" :items="tabs" />

    <UCard v-if="activeTab === 'events'">
      <template #header>
        <div class="flex items-center justify-between gap-3">
          <div>
            <h3 class="font-semibold">Audit Events</h3>
            <p class="text-xs text-gray-500 mt-0.5">Track of actions taken across stacks, workers, and settings.</p>
          </div>
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

    <UCard v-if="activeTab === 'sessions'">
      <template #header>
        <div class="flex items-center justify-between gap-3">
          <div>
            <h3 class="font-semibold">Terminal Sessions</h3>
            <p class="text-xs text-gray-500 mt-0.5">History of interactive container shell sessions, including closed containers or deleted stacks.</p>
          </div>
          <div class="flex items-center gap-2">
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
              aria-label="Refresh terminal sessions"
              :loading="terminalLoading"
              @click="loadTerminalSessions()"
            />
          </div>
        </div>
      </template>

      <form class="space-y-2 mb-4" @submit.prevent="applyTerminalFilters">
        <div class="grid grid-cols-1 sm:grid-cols-4 gap-2 items-center">
          <AppTextInput v-model="terminalFilters.stack_name" placeholder="Stack name" />
          <AppTextInput v-model="terminalFilters.service_name" placeholder="Service name" />
          <AppTextInput v-model="terminalFilters.user_email" placeholder="User email" />
          <AppSelectInput v-model="terminalFilters.status" :items="terminalStatusOptions" />
        </div>
        <div class="flex justify-end gap-1">
          <UButton icon="i-lucide-x" variant="ghost" size="sm" @click="clearTerminalFilters" />
          <UButton type="submit" icon="i-lucide-search" size="sm" />
        </div>
      </form>

      <div v-if="terminalLoading" class="text-sm text-gray-500 py-2">Loading terminal sessions...</div>
      <div v-else-if="terminalSessions.length === 0" class="text-sm text-gray-500 py-2">No terminal sessions found.</div>
      <div v-else class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="text-left text-xs uppercase text-gray-500 border-b border-gray-200 dark:border-gray-800">
            <tr>
              <th class="pb-2 pr-4 font-medium">Started</th>
              <th class="pb-2 pr-4 font-medium">Ended</th>
              <th class="pb-2 pr-4 font-medium">Stack</th>
              <th class="pb-2 pr-4 font-medium">Service / Container</th>
              <th class="pb-2 pr-4 font-medium">User</th>
              <th class="pb-2 pr-4 font-medium">Worker</th>
              <th class="pb-2 pr-4 font-medium">Status</th>
              <th class="pb-2 pr-4 font-medium">Exit Code</th>
              <th class="pb-2 pr-4 font-medium" />
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-gray-800">
            <tr v-for="sess in terminalSessions" :key="sess.id">
              <td class="py-1.5 pr-4 whitespace-nowrap text-xs">{{ formatTerminalDate(sess.started_at) }}</td>
              <td class="py-1.5 pr-4 whitespace-nowrap text-xs">{{ formatTerminalDate(sess.ended_at) }}</td>
              <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">{{ sess.stack_name || '-' }}</td>
              <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">
                {{ sess.service_name || sess.container_name || sess.container_id }}
              </td>
              <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">{{ sess.user_email || '-' }}</td>
              <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">{{ sess.worker_hostname || '-' }}</td>
              <td class="py-1.5 pr-4">
                <UBadge
                  :label="sess.status"
                  :color="sess.status === 'closed' ? 'neutral' : 'success'"
                  variant="subtle"
                  size="xs"
                  :ui="{ rounded: 'rounded-sm', padding: { xs: 'px-1.5 py-0' } }"
                />
              </td>
              <td class="py-1.5 pr-4 font-mono text-[11px] whitespace-nowrap">{{ sess.status === 'closed' ? sess.exit_code : '-' }}</td>
              <td class="py-1.5 pr-4 text-right">
                <UButton
                  label="Replay"
                  icon="i-lucide-play"
                  variant="ghost"
                  size="xs"
                  :disabled="sess.status !== 'closed'"
                  @click="openReplay(sess)"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="flex items-center justify-between pt-3 mt-2 border-t border-gray-100 dark:border-gray-800">
        <p class="text-xs text-gray-500">{{ terminalTotal }} sessions</p>
        <UPagination
          v-model:page="terminalPage"
          :items-per-page="terminalPerPage"
          :total="terminalTotal"
          size="sm"
          @update:page="loadTerminalSessions"
        />
      </div>
    </UCard>

    <TerminalReplayModal
      v-if="replaySession"
      v-model:open="replayModalOpen"
      :session-id="replaySession.sessionId"
      :label="replaySession.label"
      :exit-code="replaySession.exitCode"
    />

    <!-- Audit Settings Modal -->
    <UModal v-model:open="showAuditSettingsModal">
      <template #content>
        <UCard :ui="{ ring: '', divide: 'divide-y divide-gray-100 dark:divide-gray-800' }">
          <template #header>
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">Audit Settings</h3>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              Configure how long audit events, job run logs, and terminal session transcripts are retained.
            </p>
          </template>

          <div class="grid grid-cols-1 gap-4 sm:grid-cols-3 p-4">
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
            <UFormField label="Terminal session retention (days)">
              <AppTextInput
                :model-value="String(appSettings.terminal_retention_days)"
                type="number"
                @update:model-value="updateTerminalRetentionDays"
              />
            </UFormField>
          </div>

          <template #footer>
            <div class="flex justify-end gap-2">
              <CancelButton
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
