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

// buildStackStatusFilter mirrors stackEffectiveStatus's folding of the
// transient "syncing" state into active/pending, expressed as a PocketBase
// filter clause instead of a client-side predicate - so a server-paginated
// stacks query can filter by "effective" status the same way the UI displays
// it. Kept next to stackEffectiveStatus so the two stay in sync.
export function buildStackStatusFilter(effectiveStatus: string): string | null {
  switch (effectiveStatus) {
    case 'active':
      return "((status = 'active') || (status = 'syncing' && deployed_at != ''))"
    case 'paused':
      return "((status = 'paused') || (status = 'pending') || (status = 'syncing' && deployed_at = ''))"
    case 'error':
      return "(status = 'error')"
    case 'pending':
      return "((status = 'pending') || (status = 'syncing' && deployed_at = ''))"
    case 'degraded':
      // Best-effort: worker.docker_online is the last heartbeat's reading,
      // not a live connection check, so a worker that went fully offline
      // right as its Docker daemon died can still match here for a while.
      // Harmless overlap — the client-side badge/bar (stackFleetStatus,
      // which does use the live-computed worker status) is the accurate
      // signal; this filter only needs to be a reasonable approximation for
      // narrowing the paginated query.
      return "((status = 'active') || (status = 'syncing' && deployed_at != '')) && worker.docker_online = false"
    default:
      return null
  }
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

// stackFleetStatus folds "the deploy looks active but we can't currently
// verify it because the worker's Docker daemon is down" into its own
// 'degraded' bucket — a first-class status alongside active/paused/pending/
// error, not just a deploy-status-card nuance (see stackVisibleDeployStatus),
// so a stack that's quietly unverifiable is easy to spot across the whole
// fleet (list badges, the stacks-page availability bar) instead of only
// showing up once someone opens that one stack's detail page.
export function stackFleetStatus(stack: any, workersById?: WorkerLookup): string | undefined {
  if (stackVisibleDeployStatus(stack, workersById).key === 'degraded') return 'degraded'
  return stackEffectiveStatus(stack)
}

export function stackStatusBadge(stack: any, workersById?: WorkerLookup): StackStatusBadge {
  switch (stackFleetStatus(stack, workersById)) {
    case 'active':
      return { label: 'Active', color: 'success', dotClass: 'bg-emerald-400', borderClass: 'border-l-emerald-400 dark:border-l-emerald-400' }
    case 'degraded':
      return { label: 'Degraded', color: 'warning', dotClass: 'bg-orange-400', borderClass: 'border-l-orange-400 dark:border-l-orange-400' }
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
    // A degraded worker (connected, but its Docker daemon isn't responding)
    // is a more specific — and more actionable — reason we can't currently
    // verify the deploy than the generic "can't tell at all" Unknown, which
    // is reserved for truly unreachable workers (offline/revoked).
    if (worker.key === 'degraded') {
      return { ...worker }
    }
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
    case WORKER_STATUS.DEGRADED:
      return {
        key: 'degraded',
        label: 'Degraded',
        color: 'warning',
        icon: 'i-lucide-triangle-alert',
        iconClass: 'text-amber-500',
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
