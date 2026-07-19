import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import BackupsPage from '../backups.vue'

const baseBackup = { key: 'wireops_backup_1.zip', size: 1024, modified: '2026-07-01T00:00:00.000Z' }

function setupGlobals(overrides: { listBackups?: any; createBackup?: any; deleteBackup?: any; restoreBackup?: any; syncLocalBackup?: any; getIntegrations?: any; listAuditLogs?: any } = {}) {
  const toastAdd = vi.fn()
  const logout = vi.fn()

  ;(globalThis as any).useNuxtApp = () => ({
    $pb: { baseURL: 'http://localhost:8090', authStore: { token: 'user-token' } },
    $pbSuperuser: { baseURL: 'http://localhost:8090', authStore: { token: '', isSuperuser: false } },
  })
  ;(globalThis as any).useToast = () => ({ add: toastAdd })
  ;(globalThis as any).usePermissions = () => ({ isAdmin: ref(true) })
  ;(globalThis as any).useAuth = () => ({ logout })

  const listBackups = overrides.listBackups || vi.fn().mockResolvedValue([baseBackup])
  const createBackup = overrides.createBackup || vi.fn().mockResolvedValue({ key: 'wireops_backup_2.zip' })
  const deleteBackup = overrides.deleteBackup || vi.fn().mockResolvedValue({})
  const restoreBackup = overrides.restoreBackup || vi.fn().mockResolvedValue({})
  const syncLocalBackup = overrides.syncLocalBackup || vi.fn().mockResolvedValue({ status: 'synced' })
  const getBackupSettings = vi.fn().mockResolvedValue({ cron: '0 3 * * *', cron_max_keep: 3 })
  const saveBackupSettings = vi.fn().mockResolvedValue({ cron: '0 4 * * *', cron_max_keep: 5 })
  const getIntegrations = overrides.getIntegrations || vi.fn().mockResolvedValue([])
  const listAuditLogs = overrides.listAuditLogs || vi.fn().mockResolvedValue({ items: [], page: 1, perPage: 10, totalItems: 0 })

  ;(globalThis as any).useApi = () => ({
    listBackups,
    createBackup,
    deleteBackup,
    restoreBackup,
    syncLocalBackup,
    getBackupSettings,
    saveBackupSettings,
    listAuditLogs,
  })
  ;(globalThis as any).useIntegrations = () => ({ getIntegrations })

  return { toastAdd, logout, listBackups, createBackup, deleteBackup, restoreBackup, syncLocalBackup, getBackupSettings, saveBackupSettings, getIntegrations, listAuditLogs }
}

function mountPage() {
  return mount(BackupsPage, {
    global: { stubs: { transition: false } },
    shallow: true,
  })
}

describe('backups.vue', () => {
  it('loads and lists backups, then creates a new one', async () => {
    const { createBackup, listBackups } = setupGlobals()
    const wrapper = mount(BackupsPage, { global: { stubs: { transition: false } }, shallow: true })
    await flushPromises()

    expect(listBackups).toHaveBeenCalledTimes(1)
    expect((wrapper.vm as any).backups).toHaveLength(1)
    expect((wrapper.vm as any).backups[0].key).toBe(baseBackup.key)

    await (wrapper.vm as any).handleCreateBackup()
    await flushPromises()

    expect(createBackup).toHaveBeenCalledTimes(1)
    expect(listBackups).toHaveBeenCalledTimes(2)
  })

  it('clears the stale list and surfaces an error toast when loading backups fails', async () => {
    const listBackups = vi.fn().mockRejectedValue(new Error('storage unreachable'))
    const { toastAdd } = setupGlobals({ listBackups })
    const wrapper = mountPage()
    await flushPromises()

    expect((wrapper.vm as any).backups).toHaveLength(0)
    expect((wrapper.vm as any).backupsError).toBe('storage unreachable')
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ title: 'Failed to load backups' }))
  })

  it('tracks whether the s3 integration is enabled', async () => {
    const getIntegrations = vi.fn().mockResolvedValue([{ slug: 's3', enabled: true, name: 'S3 Storage', category: 'Storage Backend', config: {} }])
    setupGlobals({ getIntegrations })
    const wrapper = mountPage()
    await flushPromises()

    expect(getIntegrations).toHaveBeenCalledTimes(1)
    expect((wrapper.vm as any).isRemoteEnabled).toBe(true)
  })

  it('labels each backup Local or Local + S3 based on its own remote flag, not a global toggle', async () => {
    const listBackups = vi.fn().mockResolvedValue([
      { key: 'local_only.zip', size: 512, modified: '2026-07-01T00:00:00.000Z', local: true, remote: false },
      { key: 'mirrored.zip', size: 1024, modified: '2026-07-02T00:00:00.000Z', local: true, remote: true },
      { key: 'remote_only.zip', size: 2048, modified: '2026-07-03T00:00:00.000Z', local: false, remote: true },
    ])
    setupGlobals({ listBackups })
    const wrapper = mountPage()
    await flushPromises()

    // loadBackups sorts most-recently-modified first.
    const backupSource = (wrapper.vm as any).backupSource
    const byKey = Object.fromEntries((wrapper.vm as any).backups.map((b: any) => [b.key, b]))
    expect(backupSource(byKey['local_only.zip'])).toBe('Local')
    expect(backupSource(byKey['mirrored.zip'])).toBe('Local + S3')
    expect(backupSource(byKey['remote_only.zip'])).toBe('S3 only')
  })

  it('loads and renders the backup mirror history', async () => {
    const listAuditLogs = vi.fn().mockResolvedValue({
      items: [
        { id: 'a1', resource_id: 'wireops_backup_1.zip', status: 'error', error_code: 'connection refused', created: '2026-07-01T00:00:00.000Z' },
      ],
      page: 1,
      perPage: 10,
      totalItems: 1,
    })
    setupGlobals({ listAuditLogs })
    const wrapper = mountPage()
    await flushPromises()

    expect(listAuditLogs).toHaveBeenCalledWith(expect.objectContaining({ action: 'backup.mirror', resource_type: 'backup' }))
    expect((wrapper.vm as any).mirrorHistory).toHaveLength(1)
    expect((wrapper.vm as any).mirrorHistory[0].status).toBe('error')
  })

  it('syncs a remote-only backup down to local disk', async () => {
    const remoteOnly = { key: 'remote_only.zip', size: 2048, modified: '2026-07-03T00:00:00.000Z', local: false, remote: true }
    const listBackups = vi.fn().mockResolvedValue([remoteOnly])
    const { syncLocalBackup } = setupGlobals({ listBackups })
    const wrapper = mountPage()
    await flushPromises()

    await (wrapper.vm as any).handleSyncLocal(remoteOnly)
    await flushPromises()

    expect(syncLocalBackup).toHaveBeenCalledWith('remote_only.zip')
    expect(listBackups).toHaveBeenCalledTimes(2)
  })

  it('starts the restore countdown and logs out through the centralized auth flow once it elapses', async () => {
    vi.useFakeTimers()
    try {
      const { restoreBackup, logout } = setupGlobals()
      const wrapper = mountPage()
      await flushPromises()

      ;(wrapper.vm as any).requestRestore(baseBackup)
      await flushPromises()
      await (wrapper.vm as any).confirmRestore()
      await flushPromises()

      expect(restoreBackup).toHaveBeenCalledWith(baseBackup.key)
      expect((wrapper.vm as any).restoreStarted).toBe(true)

      vi.advanceTimersByTime(12_000)
      await flushPromises()

      expect(logout).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })
})
