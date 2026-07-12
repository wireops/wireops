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
  return OS_ICONS.linux
}

export function osLabel(os?: string): string {
  return os && os.length > 0 ? os : 'linux'
}

export function archIcon(_arch?: string): string {
  return 'i-lucide-cpu'
}
