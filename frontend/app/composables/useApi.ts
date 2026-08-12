import type { LintComposeBody, LintComposeResponse } from '~/types/lint'

export function useApi() {
  const { $pb } = useNuxtApp()

  const baseUrl = () => $pb.baseURL

  async function handleResponse<T>(res: Response, debugPath?: string): Promise<T> {
    const data = await res.json()
    if (!res.ok || data?.error) {
      const err = new Error(data?.error || `API Error: ${res.statusText}`) as any
      err.data = data
      err.status = res.status
      throw err
    }
    return data
  }

  async function customPost<T = any>(path: string, body?: any): Promise<T> {
    const res = await fetch(`${baseUrl()}${path}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
        'X-Wireops-Origin': 'ui',
      },
      body: body ? JSON.stringify(body) : undefined,
    })
    return handleResponse<T>(res, path)
  }

  async function customGet<T = any>(path: string): Promise<T> {
    const res = await fetch(`${baseUrl()}${path}`, {
      headers: {
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
        'X-Wireops-Origin': 'ui',
      },
    })
    return handleResponse<T>(res)
  }

  async function customDelete<T = any>(path: string): Promise<T> {
    const res = await fetch(`${baseUrl()}${path}`, {
      method: 'DELETE',
      headers: {
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
        'X-Wireops-Origin': 'ui',
      },
    })
    return handleResponse<T>(res)
  }

  async function customPut<T = any>(path: string, body?: any): Promise<T> {
    const res = await fetch(`${baseUrl()}${path}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
        'X-Wireops-Origin': 'ui',
      },
      body: body ? JSON.stringify(body) : undefined,
    })
    return handleResponse<T>(res)
  }

  async function customPatch<T = any>(path: string, body?: any): Promise<T> {
    const res = await fetch(`${baseUrl()}${path}`, {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json',
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
        'X-Wireops-Origin': 'ui',
      },
      body: body ? JSON.stringify(body) : undefined,
    })
    return handleResponse<T>(res)
  }

  const triggerSync = (stackId: string) => customPost(`/api/custom/stacks/${stackId}/sync`)
  const triggerRollback = (stackId: string, commitSha: string) =>
    customPost(`/api/custom/stacks/${stackId}/rollback`, { commit_sha: commitSha })
  const getServices = (stackId: string) => customGet(`/api/custom/stacks/${stackId}/services`)
  const getDependencyGraph = (stackId: string) =>
    customGet<import('../utils/dependency-graph-layout').DependencyGraph>(`/api/custom/stacks/${stackId}/dependency-graph`)
  type VolumeInfo = {
    name: string; docker_name: string; driver: string; mountpoint: string; scope: string
    created_at?: string; size_bytes?: number; options?: Record<string, string>
  }
  type NetworkIPAMConfig = {
    subnet?: string; gateway?: string; ip_range?: string; aux_addresses?: Record<string, string>
  }
  type NetworkInfo = {
    name: string; docker_name: string; id?: string; driver: string; scope: string; created_at?: string
    subnet?: string; gateway?: string; ipam_configs?: NetworkIPAMConfig[]
    enable_ipv4: boolean; enable_ipv6: boolean; internal: boolean; attachable: boolean; ingress: boolean; config_only: boolean
    options?: Record<string, string>
  }
  const getStackResources = (stackId: string) =>
    customGet<{ volumes: VolumeInfo[]; networks: NetworkInfo[] }>(`/api/custom/stacks/${stackId}/resources`)
  const stopContainer = (stackId: string, containerId: string) =>
    customPost(`/api/custom/stacks/${stackId}/container/stop`, { container_id: containerId })
  const restartContainer = (stackId: string, containerId: string) =>
    customPost(`/api/custom/stacks/${stackId}/container/restart`, { container_id: containerId })
  const deleteStack = (stackId: string, force?: boolean) => {
    const url = force ? `/api/custom/stacks/${stackId}?force=true` : `/api/custom/stacks/${stackId}`
    return customDelete(url)
  }
  const getComposeFile = (stackId: string) => customGet<{ content: string; filename: string }>(`/api/custom/stacks/${stackId}/compose`)
  const getWebhookUrl = (stackId: string) =>
    customGet<{ webhook_url: string }>(`/api/custom/stacks/${stackId}/webhook-url`).then((r) => r.webhook_url)
  const getContainerStats = (stackId: string, containerId: string) =>
    customGet<{ cpu_percent: number; mem_usage: number; mem_limit: number; started_at: string }>(`/api/custom/stacks/${stackId}/container/${containerId}/stats`)
  const getContainerLogs = (stackId: string, containerId: string, tail = 100) =>
    customGet<{ logs: string }>(`/api/custom/stacks/${encodeURIComponent(stackId)}/container/${encodeURIComponent(containerId)}/logs?tail=${encodeURIComponent(String(tail))}`)
  const forceRedeploy = (stackId: string, options: { recreate_containers: boolean; recreate_volumes: boolean; recreate_networks: boolean; pause_after_redeploy?: boolean }) =>
    customPost(`/api/custom/stacks/${stackId}/force-redeploy`, options)
  type ServiceOverride = { image?: string; ports?: string[]; networks?: string[]; scale?: number }
  const setRenderOverrides = (stackId: string, overrides: Record<string, ServiceOverride>) =>
    customPut(`/api/custom/stacks/${stackId}/render-overrides`, { overrides })
  const clearRenderOverrides = (stackId: string) =>
    customDelete(`/api/custom/stacks/${stackId}/render-overrides`)
  const getRenderOverridesDiff = (stackId: string) =>
    customGet<{ overrides: Record<string, ServiceOverride> | null; git?: Record<string, ServiceOverride>; git_error?: string }>(`/api/custom/stacks/${stackId}/render-overrides`)
  const getRepoCommits = (repoId: string) =>
    customGet<{ sha: string; message: string; author: string; date: string }[]>(`/api/custom/repositories/${repoId}/commits`)
  const getRepoFiles = (repoId: string) =>
    customGet<string[]>(`/api/custom/repositories/${repoId}/files`)
  const getStackFiles = (repoId: string) =>
    customGet<string[]>(`/api/custom/repositories/${repoId}/stack-files`)
  const getJobFiles = (repoId: string) =>
    customGet<string[]>(`/api/custom/repositories/${repoId}/job-files`)
  const getJobDefinitionFromFile = (repoId: string, file: string) =>
    customGet<JobDefinition>(`/api/custom/repositories/${repoId}/job-definition?file=${encodeURIComponent(file)}`)
  const getWireopsFiles = (repoId: string) =>
    customGet<string[]>(`/api/custom/repositories/${repoId}/wireops-files`)
  const getWireopsDefinitionFromFile = (repoId: string, file: string) =>
    customGet<WireopsDefinition>(`/api/custom/repositories/${repoId}/wireops-definition?file=${encodeURIComponent(file)}`)
  type CreateStackFromWireopsBody = { repository: string; worker: string; wireops_file: string }
  const createStackFromWireops = (body: CreateStackFromWireopsBody) =>
    customPost<{ id: string; name: string; status: string }>('/api/custom/stacks/from-wireops', body)
  const testCredentials = (body: any) => customPost('/api/custom/credentials/test', body)
  const keyscan = (host: string, port = 22) => customPost('/api/custom/credentials/keyscan', { host, port })
  const testRegistryCredential = (body: any) => customPost<{ success: boolean, error?: string, warning?: string }>('/api/custom/registry-credentials/test', body)
  const listGitProviders = () =>
    customGet<{ slug: string; name: string; configured: boolean; connected: boolean; account_login?: string; key_id?: string }[]>('/api/custom/git-providers')
  const getGitProviderAuthorizeUrl = (slug: string) =>
    customPost<{ url: string }>(`/api/custom/git-providers/${slug}/authorize-url`, {})
  const listGitProviderOrgs = (slug: string, keyId: string) =>
    customGet<{ login: string; avatar_url?: string }[]>(`/api/custom/git-providers/${slug}/orgs?key=${encodeURIComponent(keyId)}`)
  const listGitProviderRepos = (slug: string, keyId: string, org: string) =>
    customGet<{ full_name: string; name: string; owner: string; private: boolean; default_branch: string; clone_url: string }[]>(
      `/api/custom/git-providers/${slug}/repos?key=${encodeURIComponent(keyId)}&org=${encodeURIComponent(org)}`,
    )
  const listGitProviderBranches = (slug: string, keyId: string, repo: string) =>
    customGet<{ name: string }[]>(
      `/api/custom/git-providers/${slug}/branches?key=${encodeURIComponent(keyId)}&repo=${encodeURIComponent(repo)}`,
    )
  const listOrphans = () => customGet<{ dir_name: string; compose_file: string; has_compose: boolean }[]>('/api/custom/orphans')
  const purgeOrphan = (dirName: string) => customDelete(`/api/custom/orphans/${dirName}`)
  const getSystemInfo = () => customGet<{
    version: string
    docker_version: string
    compose_version: string
    repositories: number
    stacks: number
    disk_usage: number
    workspace_path: string
  }>('/api/custom/system/info')



  type WireopsDefinition = {
    version: string
    name: string
    group?: string
    deploy_timeout_seconds: number
    compose?: { remove_orphans?: boolean; force_pull?: boolean }
    jobs?: { wait_running?: boolean }
    worker?: { tags?: string[] }
    resolved_compose_path?: string
    resolved_compose_file?: string
    resolution_error?: string
  }

  // Static analysis only — runs server-side off `docker compose config`, so
  // it needs no worker to be online and never reaches a Docker daemon.
  // Types live in ~/types/lint so the components rendering the report share
  // one definition with this caller.
  const lintCompose = (body: LintComposeBody) =>
    customPost<LintComposeResponse>('/api/custom/lint/compose', body)

  type DiscoveredProject = { project_name: string; compose_path: string; services: string[] }
  const discoverProjects = (workerId: string) =>
    customGet<DiscoveredProject[]>(`/api/custom/stacks/import/discover?worker=${workerId}`)

  type ImportStackBody = { name: string; worker_id: string; import_path: string; recreate_volumes: boolean }
  const importStack = (body: ImportStackBody) =>
    customPost<{ id: string; status: string }>('/api/custom/stacks/import', body)

  // Scheduled Jobs
  type JobDefinition = {
    name: string
    description: string
    cron: string
    tags: string[]
    group?: string
    mode: 'once' | 'once_all'
    image: string
    command: string[]
    remove: boolean
    volumes?: string[]
    network?: string
  }
  type JobListItem = {
    id: string
    name: string
    description: string
    job_file: string
    enabled: boolean
    status: string
    last_run_at: string
    created: string
    updated: string
    repository: { id: string; name: string; git_url: string }
    definition: JobDefinition | null
    definition_error?: string
    errors?: string[]
    recent_runs?: { id: string; status: string; created: string }[]
  }
  type ListJobsParams = {
    page?: number
    perPage?: number
    status?: string
    repository?: string
    search?: string
  }
  // perPage defaults to the endpoint's max page size (200) rather than its
  // UI default (24) so callers that just want "every job" - the dashboard
  // widget and command palette index, neither of which paginates - keep
  // working as a single request without needing their own paging loop.
  const listJobs = (params: ListJobsParams = {}) => {
    const query = new URLSearchParams({
      page: String(params.page ?? 1),
      per_page: String(params.perPage ?? 200),
    })
    if (params.status) query.set('status', params.status)
    if (params.repository) query.set('repository', params.repository)
    if (params.search) query.set('search', params.search)
    return customGet<{ items: JobListItem[]; total_items: number }>(`/api/custom/jobs?${query.toString()}`)
  }
  const listJobGroups = () => customGet<{ groups: string[]; has_ungrouped: boolean }>('/api/custom/jobs/groups')
  const triggerJobRun = (jobId: string) => customPost(`/api/custom/jobs/${jobId}/run`)
  const cancelJobRun = (runId: string) => customPost(`/api/custom/job-runs/${runId}/cancel`)
  const deleteJobRun = (runId: string) => customDelete(`/api/custom/job-runs/${runId}`)
  const getJobDefinition = (jobId: string) =>
    customGet<JobDefinition>(`/api/custom/jobs/${jobId}/definition`)
  const getJobRaw = (jobId: string) =>
    customGet<{ content: string; filename: string }>(`/api/custom/jobs/${jobId}/raw`)

  type WorkerJobSummary = {
    id: string
    name: string
    common_tags: string[]
  }
  type WorkerInfo = {
    id: string
    hostname: string
    status: string
    last_seen: string
    health_history: { status: string, timestamp: string }[]
    tags: string[]
    token_status: string
    token_expires: string
    token_last_used: string
    job_count: number
    jobs: WorkerJobSummary[]
    version?: string
    docker_version?: string
    compose_version?: string
    os?: string
    arch?: string
    cpu_usage?: number
    memory_usage?: number
    disk_usage?: number
    docker_online?: boolean
  }
  const getWorkers = () => customGet<WorkerInfo[]>('/api/custom/workers')
  const createWorkerToken = () => customPost<{ token: string; token_id: string; status: string; expires_at: string }>('/api/custom/worker/tokens')
  const revokeWorker = (id: string) => customPost(`/api/custom/workers/${id}/revoke`)
  const transferStack = (stackId: string, targetWorkerId: string) =>
    customPost(`/api/custom/stacks/${stackId}/transfer`, { target_worker_id: targetWorkerId })

  // --- Worker Policies ---
  type PolicyData = {
    enabled?: boolean
    allowed_volumes: string[]
    allowed_networks: string[]
    allowed_images: string[]
    allowed_cap_add: string[]
    allowed_devices: string[]
    allowed_security_opt: string[]
    prevent_latest_images: boolean
    block_host_volumes: boolean
    block_privileged: boolean
    block_host_network: boolean
    block_host_pid: boolean
    block_host_ipc: boolean
    block_docker_socket: boolean
    allow_render_overrides: boolean
  }
  type PolicyOverrideFlagKeys =
    | 'prevent_latest_images'
    | 'block_host_volumes'
    | 'block_privileged'
    | 'block_host_network'
    | 'block_host_pid'
    | 'block_host_ipc'
    | 'block_docker_socket'
    | 'allow_render_overrides'
  type PolicyOverrideNullableAllowlistKeys =
    | 'allowed_images'
    | 'allowed_volumes'
    | 'allowed_networks'
    | 'allowed_cap_add'
    | 'allowed_devices'
    | 'allowed_security_opt'
  type WorkerPolicyOverride = Omit<PolicyData, PolicyOverrideFlagKeys | PolicyOverrideNullableAllowlistKeys> & {
    inherit: boolean
    // null means "inherit from global" — must not persist the resolved effective
    // value as a local override, or future global changes stop propagating.
    allowed_images: string[] | null
    allowed_volumes: string[] | null
    allowed_networks: string[] | null
    allowed_cap_add: string[] | null
    allowed_devices: string[] | null
    allowed_security_opt: string[] | null
    prevent_latest_images: boolean | null
    block_host_volumes: boolean | null
    block_privileged: boolean | null
    block_host_network: boolean | null
    block_host_pid: boolean | null
    block_host_ipc: boolean | null
    block_docker_socket: boolean | null
    allow_render_overrides: boolean | null
  }
  type WorkerPolicyResponse = WorkerPolicyOverride & { effective: PolicyData }

  const getWorkerPolicy = (workerId: string) =>
    customGet<WorkerPolicyResponse>(`/api/custom/workers/${workerId}/policy`)
  const saveWorkerPolicy = (workerId: string, body: WorkerPolicyOverride) =>
    customPut(`/api/custom/workers/${workerId}/policy`, body)
  const resetWorkerPolicy = (workerId: string) =>
    customDelete(`/api/custom/workers/${workerId}/policy`)
  const getGlobalWorkerPolicy = () =>
    customGet<PolicyData>('/api/custom/settings/worker-policy')
  const saveGlobalWorkerPolicy = (body: PolicyData) =>
    customPut('/api/custom/settings/worker-policy', body)

  // --- App Settings ---
  type AppSettings = {
    id: string
    timezone: string
    audit_retention_days: number
    job_run_retention_days: number
    terminal_retention_days: number
  }
  const getAppSettings = async () => {
    try {
      return await customGet<AppSettings>('/api/custom/settings/app-settings')
    } catch {
      return null
    }
  }
  const saveAppSettings = async (data: Partial<AppSettings>) => {
    return await customPut<AppSettings>('/api/custom/settings/app-settings', data)
  }

  // --- Audit Logs ---
  type AuditLog = {
    id: string
    actor_type: 'anonymous' | 'user' | 'system' | 'worker'
    actor_id: string
    action: string
    resource_type: string
    resource_id: string
    origin: 'api' | 'setup' | 'system' | 'ui' | 'webhook' | 'worker'
    status: 'success' | 'error'
    error_code: string
    metadata?: Record<string, any>
    expires_at: string
    created: string
  }
  type AuditLogResponse = {
    page: number
    perPage: number
    totalItems: number
    items: AuditLog[]
  }
  type AuditLogFilters = {
    page?: number
    perPage?: number
    from?: string
    to?: string
    actor_type?: string
    actor_id?: string
    action?: string
    resource_type?: string
    resource_id?: string
    origin?: string
    status?: string
  }
  const listAuditLogs = (filters: AuditLogFilters = {}) => {
    const params = new URLSearchParams()
    for (const [key, value] of Object.entries(filters)) {
      if (value !== undefined && value !== '') {
        params.set(key, String(value))
      }
    }
    const query = params.toString()
    return customGet<AuditLogResponse>(`/api/custom/audit-logs${query ? `?${query}` : ''}`)
  }

  // --- Terminal Sessions ---
  type TerminalSessionRecord = {
    id: string
    session_id: string
    stack_name: string
    service_name: string
    container_name: string
    container_id: string
    user_email: string
    worker_hostname: string
    started_at: string
    ended_at: string
    exit_code: number
    status: 'open' | 'closed'
  }
  type TerminalSessionResponse = {
    page: number
    perPage: number
    totalItems: number
    items: TerminalSessionRecord[]
  }
  type TerminalSessionFilters = {
    page?: number
    perPage?: number
    from?: string
    to?: string
    stack_name?: string
    user_email?: string
    service_name?: string
    status?: string
  }
  const listTerminalSessions = (filters: TerminalSessionFilters = {}) => {
    const params = new URLSearchParams()
    for (const [key, value] of Object.entries(filters)) {
      if (value !== undefined && value !== '') {
        params.set(key, String(value))
      }
    }
    const query = params.toString()
    return customGet<TerminalSessionResponse>(`/api/custom/terminal-sessions${query ? `?${query}` : ''}`)
  }

  // --- Backups ---
  type BackupInfo = {
    key: string
    size: number
    modified: string
    local?: boolean
    remote?: boolean
  }
  // Remote storage (S3) is the "s3" integration now (see
  // frontend/app/components/integrations/S3ConfigModal.vue) — not part of
  // these cron/retention-only backup settings.
  type BackupSettings = {
    cron: string
    cron_max_keep: number
  }
  const listBackups = () => customGet<BackupInfo[]>('/api/custom/backups')
  const createBackup = (filename?: string) => customPost<{ key: string }>('/api/custom/backups', filename ? { filename } : {})
  const deleteBackup = (key: string) => customDelete(`/api/custom/backups/${encodeURIComponent(key)}`)
  const restoreBackup = (key: string) => customPost(`/api/custom/backups/${encodeURIComponent(key)}/restore`, { confirm: true })
  const syncLocalBackup = (key: string) => customPost<{ status: string }>(`/api/custom/backups/${encodeURIComponent(key)}/sync-local`, {})
  const getBackupSettings = () => customGet<BackupSettings>('/api/custom/backups/settings')
  const saveBackupSettings = (data: BackupSettings) => customPut<BackupSettings>('/api/custom/backups/settings', data)

  type UpdateSelfBody = {
    name?: string
    old_password?: string
    password?: string
    password_confirm?: string
  }
  const updateSelf = (body: UpdateSelfBody) =>
    customPatch<{ id: string; name: string; email: string }>('/api/custom/users/me', body)

  return { triggerSync, triggerRollback, forceRedeploy, setRenderOverrides, clearRenderOverrides, getRenderOverridesDiff, getServices, getDependencyGraph, getStackResources, stopContainer, restartContainer, deleteStack, getComposeFile, getWebhookUrl, getContainerStats, getContainerLogs, getRepoCommits, getRepoFiles, getStackFiles, getJobFiles, getJobDefinitionFromFile, getWireopsFiles, getWireopsDefinitionFromFile, createStackFromWireops, lintCompose, testCredentials, keyscan, testRegistryCredential, listGitProviders, getGitProviderAuthorizeUrl, listGitProviderOrgs, listGitProviderRepos, listGitProviderBranches, listOrphans, purgeOrphan, getSystemInfo, customPost, customGet, customPut, customPatch, customDelete, getWorkers, createWorkerToken, revokeWorker, transferStack, discoverProjects, importStack, listJobs, listJobGroups, triggerJobRun, cancelJobRun, deleteJobRun, getJobDefinition, getJobRaw, getWorkerPolicy, saveWorkerPolicy, resetWorkerPolicy, getGlobalWorkerPolicy, saveGlobalWorkerPolicy, getAppSettings, saveAppSettings, listAuditLogs, listTerminalSessions, listBackups, createBackup, deleteBackup, restoreBackup, syncLocalBackup, getBackupSettings, saveBackupSettings, updateSelf }
}
