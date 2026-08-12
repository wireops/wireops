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

  it('builds requests for the remaining stack, repository, job, worker, policy, and audit API helpers', async () => {
    const { useApi } = await import('./useApi')
    const api = useApi()

    await api.triggerSync('stack id')
    await api.triggerRollback('stack id', 'abc')
    await api.forceRedeploy('stack id', { recreate_containers: true, recreate_volumes: false, recreate_networks: false })
    await api.setRenderOverrides('stack id', { web: { image: 'nginx:1.0' } })
    await api.clearRenderOverrides('stack id')
    await api.getRenderOverridesDiff('stack id')
    await api.getServices('stack id')
    await api.getDependencyGraph('stack id')
    await api.getStackResources('stack id')
    await api.stopContainer('stack id', 'container id')
    await api.restartContainer('stack id', 'container id')
    await api.deleteStack('stack id', true)
    await api.getComposeFile('stack id')
    await api.getWebhookUrl('stack id')
    await api.getContainerStats('stack id', 'container id')
    await api.getContainerLogs('stack id', 'container id', 25)
    await api.getRepoCommits('repo id')
    await api.getRepoFiles('repo id')
    await api.getStackFiles('repo id')
    await api.getJobFiles('repo id')
    await api.getJobDefinitionFromFile('repo id', 'jobs/backup.yaml')
    await api.getWireopsFiles('repo id')
    await api.getWireopsDefinitionFromFile('repo id', 'wireops.yaml')
    await api.createStackFromWireops({ repository: 'repo id', worker: 'worker id', wireops_file: 'wireops.yaml' })
    await api.lintCompose({ repository: 'repo id', compose_path: '.', compose_file: 'compose.yaml' })
    await api.testCredentials({ auth_type: 'none' })
    await api.keyscan('host.example', 2200)
    await api.testRegistryCredential({ host: 'registry.example' })
    await api.listGitProviders()
    await api.getGitProviderAuthorizeUrl('github')
    await api.listGitProviderOrgs('github', 'key id')
    await api.listGitProviderRepos('github', 'key id', 'org name')
    await api.listGitProviderBranches('github', 'key id', 'owner/repo')
    await api.listOrphans()
    await api.purgeOrphan('orphan dir')
    await api.getSystemInfo()
    await api.discoverProjects('worker id')
    await api.importStack({ name: 'imported', worker_id: 'worker id', import_path: '/stack', recreate_volumes: false })
    await api.listJobs({ page: 2, perPage: 10, status: 'enabled', repository: 'repo id', search: 'backup task' })
    await api.listJobGroups()
    await api.triggerJobRun('job id')
    await api.cancelJobRun('run id')
    await api.deleteJobRun('run id')
    await api.getJobDefinition('job id')
    await api.getJobRaw('job id')
    await api.getWorkers()
    await api.createWorkerToken()
    await api.revokeWorker('worker id')
    await api.transferStack('stack id', 'worker id')
    await api.getWorkerPolicy('worker id')
    await api.saveWorkerPolicy('worker id', { inherit: true } as any)
    await api.resetWorkerPolicy('worker id')
    await api.getGlobalWorkerPolicy()
    await api.saveGlobalWorkerPolicy({ allowed_volumes: [] } as any)
    await api.getAppSettings()
    await api.saveAppSettings({ timezone: 'America/Sao_Paulo' })
    await api.listAuditLogs({ page: 2, status: 'error' })
    await api.listTerminalSessions({ page: 2, service_name: 'api' })
    await api.updateSelf({ name: 'New name' })

    const paths = fetchMock.mock.calls.map(([url]) => String(url))
    expect(paths).toContain('http://test/api/custom/stacks/stack id/container/container id/logs?tail=25')
    expect(paths).toContain('http://test/api/custom/git-providers/github/repos?key=key%20id&org=org%20name')
    expect(paths).toContain('http://test/api/custom/jobs?page=2&per_page=10&status=enabled&repository=repo+id&search=backup+task')
    expect(paths).toContain('http://test/api/custom/audit-logs?page=2&status=error')
    expect(paths).toContain('http://test/api/custom/terminal-sessions?page=2&service_name=api')
  })

  it('omits optional query parameters, sends empty auth when logged out, and exposes API errors', async () => {
    const { useApi } = await import('./useApi')
    const api = useApi()

    await api.listJobs()
    await api.listAuditLogs({ page: undefined, action: '' })
    await api.listTerminalSessions({ status: '' })
    expect(fetchMock.mock.calls.map(([url]) => String(url))).toEqual([
      'http://test/api/custom/jobs?page=1&per_page=200',
      'http://test/api/custom/audit-logs',
      'http://test/api/custom/terminal-sessions',
    ])

    fetchMock.mockResolvedValueOnce({ ok: false, status: 403, statusText: 'Forbidden', json: async () => ({ error: 'denied' }) })
    await expect(api.customGet('/denied')).rejects.toMatchObject({ message: 'denied', status: 403, data: { error: 'denied' } })
  })

  it('returns null when app settings are not available yet', async () => {
    const { useApi } = await import('./useApi')
    const api = useApi()
    fetchMock.mockResolvedValueOnce({ ok: false, status: 404, statusText: 'Not Found', json: async () => ({ error: 'not configured' }) })

    await expect(api.getAppSettings()).resolves.toBeNull()
  })
})
