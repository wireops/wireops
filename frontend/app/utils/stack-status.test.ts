import { describe, expect, it } from 'vitest'
import { stackDeployStatus, stackHasRenderOverrides, stackSourceStatus, stackStatusBadge, stackVisibleDeployStatus, stackWorkerStatus } from './stack-status'

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
      label: 'Connected',
      dotClass: 'bg-cyan-400',
      title: 'Git: Connected',
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

    expect(stackDeployStatus(stack.status).label).toBe('Deployed')
    expect(stackVisibleDeployStatus(stack, {
      'worker-1': { id: 'worker-1', status: 'OFFLINE' },
    })).toMatchObject({
      label: 'Unknown',
      icon: 'i-lucide-circle-help',
    })
    expect(stackVisibleDeployStatus({ ...stack, status: 'syncing' }, {
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
