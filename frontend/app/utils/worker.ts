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
