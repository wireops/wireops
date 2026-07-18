import { beforeEach, describe, expect, it, vi } from 'vitest'

function jsonResponse(body: any = {}) {
  return { ok: true, statusText: 'OK', json: async () => body } as Response
}

describe('useApi backup wrappers', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.resetModules()
    fetchMock = vi.fn().mockResolvedValue(jsonResponse())
    ;(globalThis as any).fetch = fetchMock
    ;(globalThis as any).useNuxtApp = () => ({
      $pb: { baseURL: 'http://test', authStore: { token: 'test-token' } },
    })
  })

  it('listBackups issues a GET to /api/custom/backups', async () => {
    const { useApi } = await import('./useApi')
    const { listBackups } = useApi()

    await listBackups()

    expect(fetchMock).toHaveBeenCalledWith(
      'http://test/api/custom/backups',
      expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer test-token' }) }),
    )
    expect(fetchMock.mock.calls[0][1].method).toBeUndefined()
  })

  it('createBackup POSTs an optional filename body', async () => {
    const { useApi } = await import('./useApi')
    const { createBackup } = useApi()

    await createBackup('my-backup.zip')

    expect(fetchMock).toHaveBeenCalledWith(
      'http://test/api/custom/backups',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ filename: 'my-backup.zip' }) }),
    )
  })

  it('deleteBackup URL-encodes special-character keys', async () => {
    const { useApi } = await import('./useApi')
    const { deleteBackup } = useApi()

    await deleteBackup('backup with spaces & stuff.zip')

    expect(fetchMock).toHaveBeenCalledWith(
      'http://test/api/custom/backups/backup%20with%20spaces%20%26%20stuff.zip',
      expect.objectContaining({ method: 'DELETE' }),
    )
  })

  it('restoreBackup URL-encodes the key and posts confirm:true', async () => {
    const { useApi } = await import('./useApi')
    const { restoreBackup } = useApi()

    await restoreBackup('backup/with/slashes.zip')

    expect(fetchMock).toHaveBeenCalledWith(
      'http://test/api/custom/backups/backup%2Fwith%2Fslashes.zip/restore',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ confirm: true }) }),
    )
  })

  it('syncLocalBackup URL-encodes the key and posts to sync-local', async () => {
    const { useApi } = await import('./useApi')
    const { syncLocalBackup } = useApi()

    await syncLocalBackup('back up#1.zip')

    expect(fetchMock).toHaveBeenCalledWith(
      'http://test/api/custom/backups/back%20up%231.zip/sync-local',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({}) }),
    )
  })

  it('getBackupSettings issues a GET to the settings endpoint', async () => {
    const { useApi } = await import('./useApi')
    const { getBackupSettings } = useApi()

    await getBackupSettings()

    expect(fetchMock).toHaveBeenCalledWith(
      'http://test/api/custom/backups/settings',
      expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer test-token' }) }),
    )
  })

  it('saveBackupSettings PUTs the cron/retention body', async () => {
    const { useApi } = await import('./useApi')
    const { saveBackupSettings } = useApi()

    await saveBackupSettings({ cron: '0 3 * * *', cron_max_keep: 5 })

    expect(fetchMock).toHaveBeenCalledWith(
      'http://test/api/custom/backups/settings',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ cron: '0 3 * * *', cron_max_keep: 5 }) }),
    )
  })
})
