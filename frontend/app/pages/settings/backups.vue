<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'

const { $pb, $pbSuperuser } = useNuxtApp()
const toast = useToast()
const { isAdmin } = usePermissions()
const { listBackups, createBackup, deleteBackup, restoreBackup, getBackupSettings, saveBackupSettings } = useApi()

type BackupInfo = { key: string; size: number; modified: string }

const backups = ref<BackupInfo[]>([])
const backupsLoading = ref(false)
const backupsError = ref('')
const createLoading = ref(false)
const deleteLoading = ref('')
const uploadLoading = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)
const isSuperuser = computed(() => !!$pbSuperuser.authStore.isSuperuser)

const settings = ref({
  cron: '',
  cron_max_keep: 3,
  s3: {
    enabled: false,
    bucket: '',
    region: '',
    endpoint: '',
    accessKey: '',
    secret: '',
    forcePathStyle: true,
  },
})

// PocketBase serves backups from a single filesystem at a time (S3 if
// enabled, otherwise local disk — see core.App.NewBackupsFilesystem), so
// every listed backup currently shares the same storage origin. Tracked
// separately from settings.s3.enabled (the editable, unsaved toggle state)
// so the badge reflects what's actually persisted/active on the backend,
// not whatever the settings form happens to be mid-edit.
const activeS3Enabled = ref(false)
const backupSource = computed(() => (activeS3Enabled.value ? 'S3' : 'Local'))
const settingsLoading = ref(false)
const settingsSaving = ref(false)

const showDeleteModal = ref(false)
const deleteTarget = ref<BackupInfo | null>(null)

const showUploadModal = ref(false)
const uploadTarget = ref<File | null>(null)

const showRestoreModal = ref(false)
const restoreTarget = ref<BackupInfo | null>(null)
const restoreConfirmText = ref('')
const restoreLoading = ref(false)
const restoreStarted = ref(false)
const restoreCountdown = ref(0)
const RESTORE_REDIRECT_SECONDS = 12

function formatSize(bytes: number) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i++
  }
  return `${size.toFixed(1)} ${units[i]}`
}

function formatDate(value: string) {
  if (!value) return ''
  return new Intl.DateTimeFormat('en-US', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}

async function loadBackups() {
  backupsLoading.value = true
  backupsError.value = ''
  try {
    const data = await listBackups()
    backups.value = (data || []).sort((a, b) => new Date(b.modified).getTime() - new Date(a.modified).getTime())
  } catch (e: any) {
    // Clear the stale list instead of leaving it on screen — it may belong
    // to a filesystem (e.g. local disk) that's no longer the active one
    // after switching storage backends, so its download/restore actions
    // would silently fail against the now-active filesystem.
    backups.value = []
    backupsError.value = e?.message || 'Failed to load backups.'
    toast.add({ title: 'Failed to load backups', description: e?.message, color: 'error' })
  } finally {
    backupsLoading.value = false
  }
}

// Shared merge for both load and save responses: an unconfigured S3 target
// keeps the "force path-style" default (true) instead of being overwritten
// by the backend's Go zero value (false), which is indistinguishable from an
// explicit prior save. Using the same function in both places prevents the
// two call sites from silently diverging.
function mergeSettingsResponse(data: { cron?: string; cron_max_keep?: number; s3?: Partial<typeof settings.value.s3> } | null) {
  if (!data) return
  settings.value.cron = data.cron || ''
  settings.value.cron_max_keep = data.cron_max_keep || 3
  const s3IsUnconfigured = !data.s3?.bucket && !data.s3?.endpoint && !data.s3?.accessKey
  settings.value.s3 = {
    ...settings.value.s3,
    ...(data.s3 || {}),
    forcePathStyle: s3IsUnconfigured ? true : !!data.s3?.forcePathStyle,
  }
  activeS3Enabled.value = !!data.s3?.enabled
}

async function loadSettings() {
  settingsLoading.value = true
  try {
    const data = await getBackupSettings()
    mergeSettingsResponse(data)
  } catch (e: any) {
    toast.add({ title: 'Failed to load backup settings', description: e?.message, color: 'error' })
  } finally {
    settingsLoading.value = false
  }
}

async function handleCreateBackup() {
  createLoading.value = true
  try {
    await createBackup()
    toast.add({ title: 'Backup created', color: 'success' })
    await loadBackups()
  } catch (e: any) {
    toast.add({ title: 'Failed to create backup', description: e?.message, color: 'error' })
  } finally {
    createLoading.value = false
  }
}

function triggerUpload() {
  uploadInput.value?.click()
}

function handleUploadFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return

  uploadTarget.value = file
  showUploadModal.value = true
}

async function confirmUploadBackup() {
  const file = uploadTarget.value
  if (!file) return

  uploadLoading.value = true
  try {
    const form = new FormData()
    form.append('file', file)
    const res = await fetch(`${$pbSuperuser.baseURL}/api/custom/backups/upload`, {
      method: 'POST',
      headers: {
        Authorization: $pbSuperuser.authStore.token ? `Bearer ${$pbSuperuser.authStore.token}` : '',
        'X-Wireops-Origin': 'ui',
      },
      body: form,
    })
    const data = await res.json().catch(() => null)
    if (!res.ok) throw new Error(data?.error || `Upload failed: ${res.statusText || res.status}`)
    toast.add({ title: 'Backup uploaded', color: 'success' })
    showUploadModal.value = false
    uploadTarget.value = null
    await loadBackups()
  } catch (e: any) {
    toast.add({ title: 'Failed to upload backup', description: e?.message, color: 'error' })
  } finally {
    uploadLoading.value = false
  }
}

async function downloadBackup(backup: BackupInfo) {
  try {
    const res = await fetch(`${$pb.baseURL}/api/custom/backups/${encodeURIComponent(backup.key)}/download`, {
      headers: {
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
        'X-Wireops-Origin': 'ui',
      },
    })
    if (!res.ok) throw new Error(`Download failed: ${res.statusText || res.status}`)
    const blob = await res.blob()
    const objectUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = objectUrl
    a.download = backup.key
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(objectUrl)
  } catch (e: any) {
    toast.add({ title: 'Failed to download backup', description: e?.message, color: 'error' })
  }
}

function requestDeleteBackup(backup: BackupInfo) {
  deleteTarget.value = backup
  showDeleteModal.value = true
}

async function confirmDeleteBackup() {
  if (!deleteTarget.value) return
  const backup = deleteTarget.value
  deleteLoading.value = backup.key
  try {
    await deleteBackup(backup.key)
    toast.add({ title: 'Backup deleted', color: 'success' })
    showDeleteModal.value = false
    await loadBackups()
  } catch (e: any) {
    toast.add({ title: 'Failed to delete backup', description: e?.message, color: 'error' })
  } finally {
    deleteLoading.value = ''
  }
}

function requestRestore(backup: BackupInfo) {
  restoreTarget.value = backup
  restoreConfirmText.value = ''
  restoreStarted.value = false
  showRestoreModal.value = true
}

let restoreCountdownInterval: ReturnType<typeof setInterval> | null = null

function startRestoreCountdown() {
  restoreStarted.value = true
  restoreCountdown.value = RESTORE_REDIRECT_SECONDS
  restoreCountdownInterval = setInterval(() => {
    restoreCountdown.value -= 1
    if (restoreCountdown.value <= 0) {
      clearInterval(restoreCountdownInterval!)
      restoreCountdownInterval = null
      $pb.authStore.clear()
      navigateTo('/login')
    }
  }, 1000)
}

onUnmounted(() => {
  if (restoreCountdownInterval) clearInterval(restoreCountdownInterval)
})

async function confirmRestore() {
  if (!restoreTarget.value) return
  restoreLoading.value = true
  try {
    await restoreBackup(restoreTarget.value.key)
    startRestoreCountdown()
  } catch (e: any) {
    toast.add({ title: 'Failed to restore backup', description: e?.message, color: 'error' })
  } finally {
    restoreLoading.value = false
  }
}

// PocketBase's S3 filesystem treats the whole "bucket" value as one literal
// URL path segment (see tools/filesystem/internal/s3blob), so a value like
// "my-bucket/wireops" breaks listing with a "NoSuchKey" error — not a
// bucket+prefix combo. Block the save instead of letting it silently
// half-work (uploads succeed, listing fails).
const bucketPrefixError = computed(() => {
  if (!settings.value.s3.enabled || !settings.value.s3.bucket.includes('/')) return undefined
  return "Bucket must not include a path/prefix — PocketBase doesn't support it and listing will fail. Use a dedicated bucket instead."
})

async function handleSaveSettings() {
  if (bucketPrefixError.value) {
    toast.add({ title: 'Failed to save backup settings', description: bucketPrefixError.value, color: 'error' })
    return
  }
  settingsSaving.value = true
  try {
    const data = await saveBackupSettings(settings.value)
    mergeSettingsResponse(data)
    toast.add({ title: 'Backup settings saved', color: 'success' })
    await loadBackups()
  } catch (e: any) {
    toast.add({ title: 'Failed to save backup settings', description: e?.message, color: 'error' })
  } finally {
    settingsSaving.value = false
  }
}

onMounted(() => {
  loadBackups()
  loadSettings()
})
</script>

<template>
  <div v-if="isAdmin" class="space-y-6">
    <UCard>
      <template #header>
        <div class="flex items-center justify-between gap-3">
          <div>
            <h3 class="font-semibold">Backups</h3>
            <p class="text-xs text-gray-500 mt-0.5">
              Full database + data directory backups, stored on this host (or on the S3 target below).
              A backup made without also preserving <code>SECRET_KEY</code> separately cannot decrypt stack secrets on restore.
            </p>
          </div>
          <div class="flex gap-2">
            <UButton
              v-if="isSuperuser"
              icon="i-lucide-upload"
              label="Upload Backup"
              variant="outline"
              :loading="uploadLoading"
              @click="triggerUpload"
            />
            <UButton icon="i-lucide-play" label="Backup Now" :loading="createLoading" @click="handleCreateBackup" />
          </div>
        </div>
      </template>

      <input ref="uploadInput" type="file" accept=".zip" class="hidden" @change="handleUploadFile">

      <p v-if="!isSuperuser" class="text-xs text-gray-500 -mt-2 mb-2">
        Uploading a backup file requires a real PocketBase superuser session, which this login can't provide.
        See the Disaster Recovery docs for how to restore from a file on your machine.
      </p>

      <UAlert
        v-if="backupsError"
        color="error"
        icon="i-lucide-alert-circle"
        title="Failed to load backups"
        :description="backupsError"
        class="mb-2"
      >
        <template #actions>
          <UButton label="Retry" size="xs" color="error" variant="outline" @click="loadBackups" />
        </template>
      </UAlert>
      <div v-if="backupsLoading" class="text-sm text-gray-500 py-2">Loading backups...</div>
      <div v-else-if="!backupsError && backups.length === 0" class="text-sm text-gray-500 py-2">No backups yet.</div>
      <div v-else-if="!backupsError" class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="text-left text-xs uppercase text-gray-500 border-b border-gray-200 dark:border-gray-800">
            <tr>
              <th class="pb-2 pr-4 font-medium">Name</th>
              <th class="pb-2 pr-4 font-medium">Size</th>
              <th class="pb-2 pr-4 font-medium">Created</th>
              <th class="pb-2 pr-4 font-medium">Source</th>
              <th class="pb-2 pr-4 font-medium text-right">Actions</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-gray-800">
            <tr v-for="backup in backups" :key="backup.key">
              <td class="py-2 pr-4 font-mono text-xs">{{ backup.key }}</td>
              <td class="py-2 pr-4 text-xs">{{ formatSize(backup.size) }}</td>
              <td class="py-2 pr-4 text-xs">{{ formatDate(backup.modified) }}</td>
              <td class="py-2 pr-4">
                <UBadge
                  :icon="backupSource === 'S3' ? 'i-lucide-cloud' : 'i-lucide-hard-drive'"
                  :label="backupSource"
                  :color="backupSource === 'S3' ? 'info' : 'neutral'"
                  variant="subtle"
                  size="sm"
                />
              </td>
              <td class="py-2 pr-4">
                <div class="flex justify-end gap-1">
                  <UButton icon="i-lucide-download" size="xs" variant="ghost" aria-label="Download" @click="downloadBackup(backup)" />
                  <UButton icon="i-lucide-history" size="xs" variant="ghost" color="warning" aria-label="Restore" @click="requestRestore(backup)" />
                  <UButton
                    icon="i-lucide-trash-2"
                    size="xs"
                    variant="ghost"
                    color="error"
                    aria-label="Delete"
                    :loading="deleteLoading === backup.key"
                    @click="requestDeleteBackup(backup)"
                  />
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </UCard>

    <UCard>
      <template #header><h3 class="font-semibold">Scheduled Backups</h3></template>
      <div v-if="settingsLoading" class="text-sm text-gray-500 py-2">Loading settings...</div>
      <div v-else class="space-y-4">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <UFormField label="Cron schedule">
            <AppTextInput v-model="settings.cron" placeholder="0 3 * * *" class="font-mono" />
          </UFormField>
          <UFormField label="Keep last N scheduled backups">
            <AppTextInput
              :model-value="String(settings.cron_max_keep)"
              type="number"
              @update:model-value="(v) => settings.cron_max_keep = Number(v)"
            />
          </UFormField>
        </div>
        <p class="text-xs text-gray-500 -mt-2">e.g. '0 3 * * *' for daily at 3am. Empty disables scheduled backups.</p>

        <UButton icon="i-lucide-save" label="Save" :loading="settingsSaving" @click="handleSaveSettings" />
      </div>
    </UCard>

    <UCard>
      <template #header><h3 class="font-semibold">Remote Storage (S3)</h3></template>
      <div v-if="settingsLoading" class="text-sm text-gray-500 py-2">Loading settings...</div>
      <div v-else class="space-y-4">
        <div class="flex items-center gap-2">
          <USwitch v-model="settings.s3.enabled" />
          <span class="text-sm font-medium">Store backups on S3-compatible storage</span>
        </div>

        <div v-if="settings.s3.enabled" class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <UFormField label="Bucket" :error="bucketPrefixError">
            <AppTextInput v-model="settings.s3.bucket" placeholder="my-wireops-backups" />
          </UFormField>
          <UFormField label="Region">
            <AppTextInput v-model="settings.s3.region" />
          </UFormField>
        </div>

        <div v-if="settings.s3.enabled" class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <UFormField label="Access Key">
            <AppTextInput v-model="settings.s3.accessKey" />
          </UFormField>
          <UFormField label="Secret Key">
            <AppTextInput v-model="settings.s3.secret" type="password" placeholder="••••••••" />
          </UFormField>
          <UFormField label="Endpoint">
            <AppTextInput v-model="settings.s3.endpoint" placeholder="https://s3.example.com" />
          </UFormField>
          <div class="pt-6">
            <div class="flex items-center gap-2">
              <USwitch v-model="settings.s3.forcePathStyle" />
              <span class="text-sm">Force path-style addressing</span>
            </div>
            <p class="text-xs text-gray-500 mt-1">
              Use <code class="font-mono">https://endpoint/bucket</code> instead of <code class="font-mono">https://bucket.endpoint</code>.
              Required for most self-hosted S3-compatible services (e.g. MinIO); leave off for AWS S3.
            </p>
          </div>
        </div>

        <UButton icon="i-lucide-save" label="Save" :loading="settingsSaving" @click="handleSaveSettings" />
      </div>
    </UCard>

    <UModal v-model:open="showUploadModal" :dismissible="!uploadLoading" :close="!uploadLoading">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-triangle-alert" class="w-5 h-5 text-yellow-400" />
              <h2 class="font-semibold text-yellow-400">Upload Backup</h2>
            </div>
          </template>
          <div class="space-y-3 text-sm text-gray-500 dark:text-wire-200/60">
            <p>
              You're about to upload
              <code class="font-mono">{{ uploadTarget?.name }}</code>
              as a backup file.
            </p>
            <p>
              This file becomes a valid <span class="font-semibold text-gray-900 dark:text-wire-200">restore target</span> —
              restoring it later replaces the entire database and data directory. Only upload files you produced yourself
              (e.g. via <span class="font-mono text-xs">Backup Now</span> or the scheduled backup job) or fully trust the source of.
            </p>
            <p class="text-xs text-red-500 font-medium">A malicious or corrupted backup file can destroy this instance's data if restored.</p>
          </div>
          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton
                label="Cancel"
                variant="outline"
                color="neutral"
                :disabled="uploadLoading"
                @click="showUploadModal = false; uploadTarget = null"
              />
              <UButton
                label="Upload Backup"
                color="warning"
                icon="i-lucide-upload"
                :loading="uploadLoading"
                @click="confirmUploadBackup"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>

    <UModal v-model:open="showDeleteModal">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center gap-2">
              <UIcon name="i-lucide-trash-2" class="w-5 h-5 text-red-500" />
              <h2 class="font-semibold text-red-500">Delete Backup</h2>
            </div>
          </template>
          <div class="space-y-3 text-sm text-gray-500 dark:text-wire-200/60">
            <p>
              Are you sure you want to delete
              <code class="font-mono">{{ deleteTarget?.key }}</code>?
            </p>
            <p class="text-xs text-red-500 font-medium">This action cannot be undone.</p>
          </div>
          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton label="Cancel" variant="outline" color="neutral" @click="showDeleteModal = false" />
              <UButton
                label="Delete Backup"
                color="error"
                icon="i-lucide-trash-2"
                :loading="deleteLoading === deleteTarget?.key"
                @click="confirmDeleteBackup"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>

    <UModal v-model:open="showRestoreModal" :dismissible="!restoreStarted" :close="!restoreStarted">
      <template #content>
        <UCard :ui="{ ring: '', divide: 'divide-y divide-gray-100 dark:divide-gray-800' }">
          <template v-if="!restoreStarted" #header>
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <UIcon name="i-lucide-alert-triangle" class="text-red-500" />
              Restore Backup
            </h3>
          </template>

          <div v-if="!restoreStarted" class="p-4 space-y-3 text-sm">
            <p>
              This replaces the entire current database and data directory with the contents of
              <code class="font-mono">{{ restoreTarget?.key }}</code> and restarts the server. This cannot be undone
              except by restoring another backup.
            </p>
            <p class="text-gray-500">
              Make sure the currently configured <code>SECRET_KEY</code> matches the one used when this backup was
              created — a mismatch is detected on the next server boot and blocks startup.
            </p>
            <UFormField label="Type the backup name to confirm">
              <AppTextInput v-model="restoreConfirmText" :placeholder="restoreTarget?.key" />
            </UFormField>
          </div>

          <div v-else class="p-6 flex flex-col items-center gap-3 text-center">
            <UIcon name="i-lucide-refresh-cw" class="w-8 h-8 text-yellow-500 animate-spin" />
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">Restoring backup</h3>
            <p class="text-sm text-gray-500">
              The server is restarting to apply <code class="font-mono">{{ restoreTarget?.key }}</code>. You'll be
              sent to the login page in
            </p>
            <p class="text-3xl font-bold tabular-nums">{{ restoreCountdown }}s</p>
          </div>

          <template v-if="!restoreStarted" #footer>
            <div class="flex justify-end gap-2">
              <UButton label="Cancel" color="neutral" variant="ghost" :disabled="restoreLoading" @click="showRestoreModal = false" />
              <UButton
                label="Restore"
                color="error"
                icon="i-lucide-history"
                :loading="restoreLoading"
                :disabled="restoreConfirmText !== restoreTarget?.key"
                @click="confirmRestore"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>
  </div>
</template>
