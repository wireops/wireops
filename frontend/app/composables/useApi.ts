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
  type VolumeInfo = { name: string; driver: string; mountpoint: string; scope: string }
  type NetworkInfo = { name: string; driver: string; scope: string; subnet?: string; gateway?: string }
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
    customGet<{ logs: string }>(`/api/custom/stacks/${stackId}/container/${containerId}/logs?tail=${tail}`)
  const forceRedeploy = (stackId: string, options: { recreate_containers: boolean; recreate_volumes: boolean; recreate_networks: boolean }) =>
    customPost(`/api/custom/stacks/${stackId}/force-redeploy`, options)
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
  const testCredentials = (body: any) => customPost('/api/custom/credentials/test', body)
  const keyscan = (host: string, port = 22) => customPost('/api/custom/credentials/keyscan', { host, port })
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
  const listJobs = () => customGet<JobListItem[]>('/api/custom/jobs')
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
    prevent_latest_images: boolean
    block_host_volumes: boolean
  }
  type WorkerPolicyOverride = Omit<PolicyData, 'prevent_latest_images' | 'block_host_volumes'> & {
    inherit: boolean
    prevent_latest_images: boolean | null
    block_host_volumes: boolean | null
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

  return { triggerSync, triggerRollback, forceRedeploy, getServices, getStackResources, stopContainer, restartContainer, deleteStack, getComposeFile, getWebhookUrl, getContainerStats, getContainerLogs, getRepoCommits, getRepoFiles, getStackFiles, getJobFiles, getJobDefinitionFromFile, testCredentials, keyscan, listOrphans, purgeOrphan, getSystemInfo, customPost, customGet, customPut, customPatch, customDelete, getWorkers, createWorkerToken, revokeWorker, transferStack, discoverProjects, importStack, listJobs, triggerJobRun, cancelJobRun, deleteJobRun, getJobDefinition, getJobRaw, getWorkerPolicy, saveWorkerPolicy, resetWorkerPolicy, getGlobalWorkerPolicy, saveGlobalWorkerPolicy, getAppSettings, saveAppSettings, listAuditLogs }
}
