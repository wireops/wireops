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
      label: 'Up to date',
      color: 'info',
      icon: 'i-lucide-git-branch',
      iconClass: 'text-cyan-500',
      dotClass: 'bg-cyan-400',
      title: 'Git: Up to date',
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

// stackSyncStatus is stackSourceStatus's sibling for the stack detail page's
// "Sync" status card: same git-connectivity signal, but takes priority from
// last_error_category (set by the backend's markSyncError/markDeployError,
// see internal/sync/reconciler.go) so a sync-phase failure (git fetch,
// render, secrets/env resolve, policy check) shows up here even when the
// underlying repository record itself still reports "connected".
export function stackSyncStatus(stack: any): StackSourceDisplay {
  if (stack?.last_error_category === 'sync') {
    return {
      key: 'error',
      label: 'Error',
      color: 'error',
      icon: 'i-lucide-refresh-cw',
      iconClass: 'text-red-500',
      dotClass: 'bg-red-500',
      title: 'Sync: Error',
    }
  }

  if (stack?.source_type === 'local') {
    return {
      key: 'local',
      label: 'Local',
      color: 'warning',
      icon: 'i-lucide-hard-drive',
      iconClass: 'text-amber-500',
      dotClass: 'bg-amber-400',
      title: 'Sync: Local',
    }
  }

  const repoStatus = stack?.expand?.repository?.status
  if (repoStatus === 'connected') {
    return {
      key: 'connected',
      label: 'Up to date',
      color: 'info',
      icon: 'i-lucide-git-branch',
      iconClass: 'text-cyan-500',
      dotClass: 'bg-cyan-400',
      title: 'Sync: Up to date',
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
      title: 'Sync: Git Error',
    }
  }

  return {
    key: 'unknown',
    label: 'Unknown',
    color: 'neutral',
    icon: 'i-lucide-refresh-cw',
    iconClass: 'text-gray-400',
    dotClass: 'bg-gray-400',
    title: 'Sync: Unknown',
  }
}

// stackIsSyncing tells the caller whether a sync is currently in flight for
// the stack, so the UI can show a small in-progress indicator alongside the
// stable status instead of a distinct "Syncing" state (see stackEffectiveStatus).
export function stackIsSyncing(stack: any): boolean {
  return stack?.status === 'syncing'
}

// stackEffectiveStatus resolves which stable status to render. stack.status
// is transiently "syncing" for the duration of every reconcile, which used to
// render as its own blue badge/border - with several stacks syncing at once
// (routine, since cron intervals overlap) this made badges flip color across
// the whole screen. "syncing" is not a state distinct from the others: it's
// folded back into the stack's last known stable status (deployed if it has
// completed at least one deploy, otherwise pending) so the badge stays put;
// pair with stackIsSyncing() for the in-progress indicator.
export function stackEffectiveStatus(stack: any): string | undefined {
  if (stack?.status !== 'syncing') return stack?.status
  return stack?.deployed_at ? 'active' : 'pending'
}

// "active" reflects the post-deploy check's live docker-inspect result at
// the moment of the last deploy (internal/sync/postcheck.go) - not a
// continuously monitored live state. Nothing re-checks container health
// between reconciles, so a container that crashes hours later with no new
// git commit still reads "active" until the next deploy or the detail page
// triggers a fresh check. "Verified" instead of "Deployed" avoids implying
// it's confirmed running right now.
export function stackDeployStatus(status?: string): StackStatusDisplay {
  switch (status) {
    case 'active':
      return {
        key: 'deployed',
        label: 'Verified',
        color: 'success',
        icon: 'i-lucide-badge-check',
        iconClass: 'text-emerald-500',
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

export type StackStatusBadge = {
  label: string
  color: StackStatusColor
  dotClass: string
  borderClass: string
}

export function stackStatusBadge(stack: any): StackStatusBadge {
  switch (stackEffectiveStatus(stack)) {
    case 'active':
      return { label: 'Active', color: 'success', dotClass: 'bg-emerald-400', borderClass: 'border-l-emerald-400 dark:border-l-emerald-400' }
    case 'paused':
      return { label: 'Paused', color: 'warning', dotClass: 'bg-amber-400', borderClass: 'border-l-amber-400 dark:border-l-amber-400' }
    case 'pending':
      return { label: 'Pending', color: 'warning', dotClass: 'bg-amber-400', borderClass: 'border-l-amber-400 dark:border-l-amber-400' }
    case 'error':
      return { label: 'Error', color: 'error', dotClass: 'bg-rose-400', borderClass: 'border-l-rose-400 dark:border-l-rose-400' }
    default:
      return { label: 'Unknown', color: 'neutral', dotClass: 'bg-gray-400', borderClass: 'border-l-gray-300 dark:border-l-carbon-600' }
  }
}

export function stackVisibleDeployStatus(stack: any, workersById?: WorkerLookup): StackStatusDisplay {
  const deploy = stackDeployStatus(stackEffectiveStatus(stack))
  const worker = stackWorkerStatus(stack, workersById)

  if (deploy.key === 'deployed' && worker.key !== 'online') {
    return { ...UNKNOWN_STATUS }
  }

  return deploy
}

export type StackLastErrorInfo = {
  category: 'sync' | 'deploy'
  message: string
  at: string
  label: string
  color: StackStatusColor
  icon: string
  iconClass: string
}

// stackLastError surfaces the denormalized last_error_category/message/at
// fields set by the backend reconciler (internal/sync/reconciler.go
// markSyncError/markDeployError), so the stack page can show a dedicated
// sync-status card distinct from the deploy-status card without joining
// sync_logs/sync_log_phases.
export function stackLastError(stack: any): StackLastErrorInfo | null {
  const category = stack?.last_error_category
  if (category !== 'sync' && category !== 'deploy') return null
  const message = stack?.last_error_message || ''
  if (!message) return null

  return {
    category,
    message,
    at: stack?.last_error_at || '',
    label: 'Error Summary',
    color: 'error',
    icon: 'i-lucide-triangle-alert',
    iconClass: 'text-rose-500',
  }
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
        color: 'neutral',
        icon: 'i-lucide-wifi-off',
        iconClass: 'text-gray-400',
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
