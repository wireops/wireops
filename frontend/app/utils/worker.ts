export const WORKER_STATUS = {
  ACTIVE: 'ACTIVE',
  OFFLINE: 'OFFLINE',
  REVOKED: 'REVOKED',
  PENDING: 'PENDING',
} as const

export const TOKEN_STATUS = {
  ACTIVE: 'ACTIVE',
  STAGING: 'STAGING',
  REVOKED: 'REVOKED',
  EXPIRED: 'EXPIRED',
} as const

export function tokenBadgeColor(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  switch (status) {
    case TOKEN_STATUS.ACTIVE: return 'success'
    case TOKEN_STATUS.STAGING: return 'warning'
    case TOKEN_STATUS.REVOKED:
    case TOKEN_STATUS.EXPIRED: return 'error'
    default: return 'neutral'
  }
}

export function workerHasReportedInfo(worker: { os?: string, docker_version?: string } | null | undefined): boolean {
  return !!(worker?.os || worker?.docker_version)
}

export function workerStatus(worker: { status?: string } | null | undefined): string {
  return String(worker?.status || '').toUpperCase()
}

export function isWorkerClickable(worker: { status?: string } | null | undefined): boolean {
  return workerStatus(worker) !== WORKER_STATUS.REVOKED
}

export function matchesWorkerSearch(worker: { hostname?: string, id?: string, tags?: string[] } | null | undefined, query: string): boolean {
  const q = query.trim().toLowerCase()
  return (
    (worker?.hostname || '').toLowerCase().includes(q) ||
    (worker?.id || '').toLowerCase().includes(q) ||
    (worker?.tags || []).some(tag => tag.toLowerCase().includes(q))
  )
}

export function filterVisibleWorkers<T extends { status?: string, hostname?: string, id?: string, tags?: string[] }>(
  workers: T[],
  { showRevoked, searchQuery }: { showRevoked: boolean, searchQuery: string }
): T[] {
  let filtered = showRevoked ? workers : workers.filter(w => workerStatus(w) !== WORKER_STATUS.REVOKED)
  if (searchQuery.trim()) {
    filtered = filtered.filter(w => matchesWorkerSearch(w, searchQuery))
  }
  return filtered
}

export function usageColor(pct: number): 'success' | 'warning' | 'error' {
  if (pct > 90) return 'error'
  if (pct >= 70) return 'warning'
  return 'success'
}

export function roundUsage(pct: number): number {
  return Math.round(pct * 100) / 100
}

const OS_ICONS: Record<string, string> = {
  linux: 'i-simple-icons-linux',
  darwin: 'i-simple-icons-apple',
  macos: 'i-simple-icons-apple',
  windows: 'i-simple-icons-windows11',
  freebsd: 'i-simple-icons-freebsd',
  openbsd: 'i-simple-icons-openbsd',
}

export function osIcon(os?: string): string {
  const key = (os || '').toLowerCase()
  for (const [name, icon] of Object.entries(OS_ICONS)) {
    if (key.includes(name)) return icon
  }
  return 'i-lucide-help-circle'
}

export function osLabel(os?: string): string {
  return os && os.length > 0 ? os : 'unknown'
}

export function archIcon(_arch?: string): string {
  return 'i-lucide-cpu'
}
