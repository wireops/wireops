export function useApi() {
  const { $pb } = useNuxtApp()

  const baseUrl = () => $pb.baseURL

  async function handleResponse<T>(res: Response): Promise<T> {
    const data = await res.json()
    if (!res.ok || data?.error) {
      throw new Error(data?.error || `API Error: ${res.statusText}`)
    }
    return data
  }

  async function customPost<T = any>(path: string, body?: any): Promise<T> {
    const res = await fetch(`${baseUrl()}${path}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
      },
      body: body ? JSON.stringify(body) : undefined,
    })
    return handleResponse<T>(res)
  }

  async function customGet<T = any>(path: string): Promise<T> {
    const res = await fetch(`${baseUrl()}${path}`, {
      headers: {
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
      },
    })
    return handleResponse<T>(res)
  }

  async function customDelete<T = any>(path: string): Promise<T> {
    const res = await fetch(`${baseUrl()}${path}`, {
      method: 'DELETE',
      headers: {
        Authorization: $pb.authStore.token ? `Bearer ${$pb.authStore.token}` : '',
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

  // Sync event webhook (global singleton)
  type SyncEventsWebhookConfig = {
    id?: string
    provider: 'webhook' | 'ntfy'
    url: string
    secret: string
    events: string[]
    headers: string // JSON string "[{\"key\":\"...\",\"value\":\"...\"}]"
    enabled: boolean
    ntfy_user?: string
    ntfy_topic?: string
    ntfy_template?: string
  }
  type SyncEventsWebhookPayload = Omit<SyncEventsWebhookConfig, 'id' | 'enabled'>
  const getSyncEventsWebhook = () =>
    customGet<SyncEventsWebhookConfig | null>('/api/custom/sync-events-webhook')
  const setSyncEventsWebhook = (body: SyncEventsWebhookPayload) =>
    customPut('/api/custom/sync-events-webhook', body)
  const setNotificationsEnabled = (enabled: boolean) =>
    customPatch('/api/custom/sync-events-webhook/enabled', { enabled })
  const deleteSyncEventsWebhook = () =>
    customDelete('/api/custom/sync-events-webhook')
  const testSyncEventsWebhook = (body?: Partial<SyncEventsWebhookConfig>) =>
    customPost('/api/custom/sync-events-webhook/test', body)

  type DiscoveredProject = { project_name: string; compose_path: string; services: string[] }
  const discoverProjects = (agentId: string) =>
    customGet<DiscoveredProject[]>(`/api/custom/stacks/import/discover?agent=${agentId}`)

  type ImportStackBody = { name: string; agent_id: string; import_path: string; recreate_volumes: boolean }
  const importStack = (body: ImportStackBody) =>
    customPost<{ id: string; status: string }>('/api/custom/stacks/import', body)

  // Scheduled Jobs
  type JobDefinition = {
    title: string
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
    job_file: string
    enabled: boolean
    status: string
    last_run_at: string
    created: string
    updated: string
    repository: { id: string; name: string; git_url: string }
    definition: JobDefinition | null
    definition_error?: string
  }
  const listJobs = () => customGet<JobListItem[]>('/api/custom/jobs')
  const triggerJobRun = (jobId: string) => customPost(`/api/custom/jobs/${jobId}/run`)
  const cancelJobRun = (runId: string) => customPost(`/api/custom/job-runs/${runId}/cancel`)
  const getJobDefinition = (jobId: string) =>
    customGet<JobDefinition>(`/api/custom/jobs/${jobId}/definition`)

  const getAgents = () => customGet<{ id: string; hostname: string; fingerprint: string; status: string; last_seen: string; health_history: { status: string, timestamp: string }[]; tags: string[] }[]>('/api/custom/agents')
  const createAgentSeat = () => customPost<{ seat: string }>('/api/custom/agent/seat')
  const revokeAgent = (id: string) => customPost(`/api/custom/agents/${id}/revoke`)
  const transferStack = (stackId: string, targetAgentId: string) =>
    customPost(`/api/custom/stacks/${stackId}/transfer`, { target_agent_id: targetAgentId })
  type CertDetails = { issuer: string; subject: string; expiration_date: string; fingerprint: string }
  type PKIDetails = { ca: CertDetails; server: CertDetails }
  const getPKIDetails = () => customGet<PKIDetails>('/api/custom/settings/pki')

  return { triggerSync, triggerRollback, forceRedeploy, getServices, getStackResources, stopContainer, restartContainer, deleteStack, getComposeFile, getWebhookUrl, getContainerStats, getContainerLogs, getRepoCommits, getRepoFiles, testCredentials, keyscan, listOrphans, purgeOrphan, getSystemInfo, customPost, customGet, customPut, customPatch, getSyncEventsWebhook, setSyncEventsWebhook, setNotificationsEnabled, deleteSyncEventsWebhook, testSyncEventsWebhook, getAgents, createAgentSeat, revokeAgent, getPKIDetails, transferStack, discoverProjects, importStack, listJobs, triggerJobRun, cancelJobRun, getJobDefinition }
}
