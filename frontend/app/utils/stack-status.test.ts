import { describe, expect, it } from 'vitest'
import { stackDeployStatus, stackHasRenderOverrides, stackIsSyncing, stackLastError, stackSourceStatus, stackStatusBadge, stackSyncStatus, stackVisibleDeployStatus, stackWorkerStatus } from './stack-status'

describe('stackHasRenderOverrides', () => {
  it('is false when render_overrides is missing or empty', () => {
    expect(stackHasRenderOverrides({})).toBe(false)
    expect(stackHasRenderOverrides({ render_overrides: null })).toBe(false)
    expect(stackHasRenderOverrides({ render_overrides: {} })).toBe(false)
  })

  it('is true when render_overrides has at least one service entry', () => {
    expect(stackHasRenderOverrides({ render_overrides: { web: { image: 'nginx:test' } } })).toBe(true)
  })
})

describe('stack status helpers', () => {
  it('normalizes repository source states and dot colors', () => {
    expect(stackSourceStatus({
      source_type: 'git',
      expand: { repository: { status: 'connected' } },
    })).toMatchObject({
      label: 'Up to date',
      dotClass: 'bg-cyan-400',
      title: 'Git: Up to date',
    })

    expect(stackSourceStatus({
      source_type: 'git',
      expand: { repository: { status: 'error' } },
    })).toMatchObject({
      label: 'Git Error',
      dotClass: 'bg-red-500',
      title: 'Git: Error',
    })

    expect(stackSourceStatus({
      source_type: 'local',
      import_path: '/srv/apps/docker-compose.yml',
    })).toMatchObject({
      label: 'Local',
      dotClass: 'bg-amber-400',
      title: 'Source: Local',
    })

    expect(stackSourceStatus({ source_type: 'git' })).toMatchObject({
      label: 'Unknown',
      dotClass: 'bg-gray-400',
      title: 'Git: Unknown',
    })
  })

  it('keeps deploy and worker status independent', () => {
    const stack = {
      id: 'stack-1',
      worker: 'worker-1',
      status: 'active',
      expand: { worker: { id: 'worker-1', status: 'ACTIVE' } },
    }

    expect(stackDeployStatus(stack.status).label).toBe('Verified')
    expect(stackVisibleDeployStatus(stack, {
      'worker-1': { id: 'worker-1', status: 'OFFLINE' },
    })).toMatchObject({
      label: 'Unknown',
      icon: 'i-lucide-circle-help',
    })
    // a stack re-syncing after a previous successful deploy resolves to the
    // "active" effective status (see stackEffectiveStatus) - same Unknown
    // override applies if the worker looks offline.
    expect(stackVisibleDeployStatus({ ...stack, status: 'syncing', deployed_at: '2026-01-01T00:00:00Z' }, {
      'worker-1': { id: 'worker-1', status: 'OFFLINE' },
    })).toMatchObject({
      label: 'Unknown',
      icon: 'i-lucide-circle-help',
    })
    expect(stackWorkerStatus(stack, {
      'worker-1': { id: 'worker-1', status: 'OFFLINE' },
    })).toMatchObject({
      label: 'Offline',
      icon: 'i-lucide-wifi-off',
    })
  })
})

describe('stackSyncStatus', () => {
  it('takes priority from last_error_category=sync over repository/source state', () => {
    expect(stackSyncStatus({
      last_error_category: 'sync',
      source_type: 'local',
      expand: { repository: { status: 'connected' } },
    })).toMatchObject({
      key: 'error',
      label: 'Error',
      title: 'Sync: Error',
    })
  })

  it('does not take priority from last_error_category=deploy', () => {
    expect(stackSyncStatus({
      last_error_category: 'deploy',
      source_type: 'git',
      expand: { repository: { status: 'connected' } },
    })).toMatchObject({
      key: 'connected',
      title: 'Sync: Up to date',
    })
  })

  it.each([
    { name: 'local source', stack: { source_type: 'local' }, expected: { key: 'local', label: 'Local', title: 'Sync: Local' } },
    { name: 'connected repository', stack: { source_type: 'git', expand: { repository: { status: 'connected' } } }, expected: { key: 'connected', label: 'Up to date', title: 'Sync: Up to date' } },
    { name: 'errored repository', stack: { source_type: 'git', expand: { repository: { status: 'error' } } }, expected: { key: 'error', label: 'Git Error', title: 'Sync: Git Error' } },
    { name: 'unknown repository status', stack: { source_type: 'git' }, expected: { key: 'unknown', label: 'Unknown', title: 'Sync: Unknown' } },
  ])('resolves $name', ({ stack, expected }) => {
    expect(stackSyncStatus(stack)).toMatchObject(expected)
  })
})

describe('stackIsSyncing', () => {
  it('is true only while stack.status is syncing', () => {
    expect(stackIsSyncing({ status: 'syncing' })).toBe(true)
    expect(stackIsSyncing({ status: 'active' })).toBe(false)
    expect(stackIsSyncing({})).toBe(false)
  })
})

describe('stackLastError', () => {
  it.each([
    { name: 'sync category', category: 'sync' },
    { name: 'deploy category', category: 'deploy' },
  ])('surfaces a $name error with message and timestamp', ({ category }) => {
    expect(stackLastError({
      last_error_category: category,
      last_error_message: 'git fetch failed',
      last_error_at: '2026-01-01T00:00:00Z',
    })).toMatchObject({
      category,
      message: 'git fetch failed',
      at: '2026-01-01T00:00:00Z',
      label: 'Error Summary',
      color: 'error',
    })
  })

  it('returns null for an unrecognized category', () => {
    expect(stackLastError({ last_error_category: 'bogus', last_error_message: 'x' })).toBeNull()
  })

  it('returns null when last_error_category is missing', () => {
    expect(stackLastError({})).toBeNull()
  })

  it('returns null when the message is empty despite a valid category', () => {
    expect(stackLastError({ last_error_category: 'sync', last_error_message: '' })).toBeNull()
    expect(stackLastError({ last_error_category: 'sync' })).toBeNull()
  })
})

describe('stackStatusBadge', () => {
  it('maps a known status to its label, color, dot and border classes', () => {
    expect(stackStatusBadge({ status: 'active' })).toEqual({
      label: 'Active',
      color: 'success',
      dotClass: 'bg-emerald-400',
      borderClass: 'border-l-emerald-400 dark:border-l-emerald-400',
    })
  })

  it('falls back to Unknown for missing or unrecognized status', () => {
    const unknown = {
      label: 'Unknown',
      color: 'neutral',
      dotClass: 'bg-gray-400',
      borderClass: 'border-l-gray-300 dark:border-l-carbon-600',
    }
    expect(stackStatusBadge({ status: 'bogus' })).toEqual(unknown)
    expect(stackStatusBadge({})).toEqual(unknown)
  })
})
