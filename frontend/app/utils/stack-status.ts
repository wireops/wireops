import { WORKER_STATUS } from './worker'

export type StackStatusColor = 'success' | 'primary' | 'info' | 'error' | 'warning' | 'neutral'

export type StackStatusDisplay = {
  key: string
  label: string
  color: StackStatusColor
  icon: string
  iconClass: string
}

const UNKNOWN_STATUS: StackStatusDisplay = {
  key: 'unknown',
  label: 'Unknown',
  color: 'neutral',
  icon: 'i-lucide-circle-help',
  iconClass: 'text-gray-400',
}

export type StackSourceDisplay = StackStatusDisplay & {
  dotClass: string
  title: string
}

type WorkerLookup = Record<string, any> | Map<string, any>

function getWorkerFromLookup(workerID: string, workersById?: WorkerLookup) {
  if (!workersById) return null
  if (workersById instanceof Map) return workersById.get(workerID) || null
  return workersById[workerID] || null
}

export function stackHasRenderOverrides(stack: any): boolean {
  const overrides = stack?.render_overrides
  return !!overrides && typeof overrides === 'object' && Object.keys(overrides).length > 0
}

export function stackRepositorySubtitle(stack: any): string {
  if (stack?.source_type === 'local') {
    return stack.import_path || 'Local stack'
  }
  return stack?.expand?.repository?.name || 'Unknown repo'
}

export function stackSourceStatus(stack: any): StackSourceDisplay {
  if (stack?.source_type === 'local') {
    return {
      key: 'local',
      label: 'Local',
      color: 'warning',
      icon: 'i-lucide-hard-drive',
      iconClass: 'text-amber-500',
      dotClass: 'bg-amber-400',
      title: 'Source: Local',
    }
  }

  const repoStatus = stack?.expand?.repository?.status
  if (repoStatus === 'connected') {
    return {
      key: 'connected',
      label: 'Connected',
      color: 'info',
      icon: 'i-lucide-git-branch',
      iconClass: 'text-cyan-500',
      dotClass: 'bg-cyan-400',
      title: 'Git: Connected',
    }
  }

  if (repoStatus === 'error') {
    return {
      key: 'error',
      label: 'Git Error',
      color: 'error',
      icon: 'i-lucide-git-branch',
      iconClass: 'text-red-500',
      dotClass: 'bg-red-500',
      title: 'Git: Error',
    }
  }

  return {
    key: 'unknown',
    label: 'Unknown',
    color: 'neutral',
    icon: 'i-lucide-git-branch',
    iconClass: 'text-gray-400',
    dotClass: 'bg-gray-400',
    title: 'Git: Unknown',
  }
}

export function stackDeployStatus(status?: string): StackStatusDisplay {
  switch (status) {
    case 'active':
      return {
        key: 'deployed',
        label: 'Deployed',
        color: 'success',
        icon: 'i-lucide-badge-check',
        iconClass: 'text-emerald-500',
      }
    case 'syncing':
      return {
        key: 'syncing',
        label: 'Syncing',
        color: 'primary',
        icon: 'i-lucide-refresh-cw',
        iconClass: 'text-sky-500',
      }
    case 'pending':
      return {
        key: 'queued',
        label: 'Queued',
        color: 'warning',
        icon: 'i-lucide-clock',
        iconClass: 'text-amber-500',
      }
    case 'paused':
      return {
        key: 'paused',
        label: 'Paused',
        color: 'warning',
        icon: 'i-lucide-pause-circle',
        iconClass: 'text-amber-500',
      }
    case 'error':
      return {
        key: 'failed',
        label: 'Failed',
        color: 'error',
        icon: 'i-lucide-circle-x',
        iconClass: 'text-rose-500',
      }
    default:
      return { ...UNKNOWN_STATUS }
  }
}

export function stackVisibleDeployStatus(stack: any, workersById?: WorkerLookup): StackStatusDisplay {
  const deploy = stackDeployStatus(stack?.status)
  const worker = stackWorkerStatus(stack, workersById)

  if ((deploy.key === 'deployed' || deploy.key === 'syncing') && worker.key !== 'online') {
    return { ...UNKNOWN_STATUS }
  }

  return deploy
}

export function stackWorkerName(stack: any): string {
  return stack?.expand?.worker?.hostname || 'Unknown worker'
}

export function stackWorkerStatus(stack: any, workersById?: WorkerLookup): StackStatusDisplay {
  const workerID = stack?.worker || stack?.expand?.worker?.id
  const liveWorker = workerID ? getWorkerFromLookup(workerID, workersById) : null
  const worker = liveWorker || stack?.expand?.worker
  const status = worker?.status

  switch (status) {
    case WORKER_STATUS.ACTIVE:
      return {
        key: 'online',
        label: 'Online',
        color: 'success',
        icon: 'i-lucide-wifi',
        iconClass: 'text-emerald-500',
      }
    case WORKER_STATUS.OFFLINE:
      return {
        key: 'offline',
        label: 'Offline',
        color: 'warning',
        icon: 'i-lucide-wifi-off',
        iconClass: 'text-amber-500',
      }
    case WORKER_STATUS.REVOKED:
      return {
        key: 'revoked',
        label: 'Revoked',
        color: 'error',
        icon: 'i-lucide-ban',
        iconClass: 'text-rose-500',
      }
    default:
      return { ...UNKNOWN_STATUS }
  }
}
