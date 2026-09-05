import { describe, it, expect } from 'vitest'
import { filterVisibleWorkers, isWorkerClickable, isWorkerVersionOutdated, matchesWorkerSearch, workerStatus } from './worker'

const workers = [
  { id: 'abc123', hostname: 'edge-eu-1', tags: ['gpu', 'prod'], status: 'ACTIVE' },
  { id: 'def456', hostname: 'edge-us-1', tags: ['staging'], status: 'OFFLINE' },
  { id: 'ghi789', hostname: 'edge-us-2', tags: ['prod'], status: 'REVOKED' },
]

describe('workerStatus / isWorkerClickable', () => {
  it('uppercases the raw status', () => {
    expect(workerStatus({ status: 'active' })).toBe('ACTIVE')
    expect(workerStatus({})).toBe('')
  })

  it('marks revoked workers as not clickable', () => {
    expect(isWorkerClickable({ status: 'REVOKED' })).toBe(false)
    expect(isWorkerClickable({ status: 'ACTIVE' })).toBe(true)
  })
})

describe('matchesWorkerSearch', () => {
  it('matches on hostname, id, or tag', () => {
    expect(matchesWorkerSearch(workers[0], 'edge-eu')).toBe(true)
    expect(matchesWorkerSearch(workers[0], 'abc123')).toBe(true)
    expect(matchesWorkerSearch(workers[0], 'gpu')).toBe(true)
    expect(matchesWorkerSearch(workers[0], 'nope')).toBe(false)
  })

  it('trims leading/trailing whitespace from the query', () => {
    expect(matchesWorkerSearch(workers[0], '  edge-eu  ')).toBe(true)
  })
})

describe('filterVisibleWorkers', () => {
  it('hides revoked workers by default and matches search across hostname/id/tags', () => {
    const result = filterVisibleWorkers(workers, { showRevoked: false, searchQuery: 'us' })
    expect(result.map(w => w.id)).toEqual(['def456'])
  })

  it('shows revoked workers when showRevoked is true', () => {
    const result = filterVisibleWorkers(workers, { showRevoked: true, searchQuery: 'prod' })
    expect(result.map(w => w.id)).toEqual(['abc123', 'ghi789'])
  })

  it('returns an empty array when no worker is visible', () => {
    expect(filterVisibleWorkers(workers, { showRevoked: false, searchQuery: 'nonexistent' })).toEqual([])
    expect(filterVisibleWorkers([workers[2]!], { showRevoked: false, searchQuery: '' })).toEqual([])
  })

  it('trims whitespace and treats whitespace-only queries as empty', () => {
    expect(filterVisibleWorkers(workers, { showRevoked: false, searchQuery: '  us  ' }).map(w => w.id)).toEqual(['def456'])
    expect(filterVisibleWorkers(workers, { showRevoked: false, searchQuery: '   ' }).map(w => w.id)).toEqual(['abc123', 'def456'])
  })
})

describe('isWorkerVersionOutdated', () => {
  it('flags a worker version older than the server version', () => {
    expect(isWorkerVersionOutdated('1.4.0', '1.5.0')).toBe(true)
    expect(isWorkerVersionOutdated('v1.4.0', 'v1.4.1')).toBe(true)
    expect(isWorkerVersionOutdated('1.4', '1.4.1')).toBe(true)
  })

  it('does not flag a worker on the same or newer version', () => {
    expect(isWorkerVersionOutdated('1.5.0', '1.5.0')).toBe(false)
    expect(isWorkerVersionOutdated('1.6.0', '1.5.0')).toBe(false)
  })

  it('ignores non-numeric versions like "dev" builds', () => {
    expect(isWorkerVersionOutdated('dev', '1.5.0')).toBe(false)
    expect(isWorkerVersionOutdated('1.4.0', 'dev')).toBe(false)
  })

  it('does not parse a pre-release or build-metadata suffix as a plain numeric segment', () => {
    // parseInt("0-dev") / parseInt("0+build") return 0, not NaN - guard
    // against silently treating these as ordinary releases.
    expect(isWorkerVersionOutdated('1.4.0-dev', '1.5.0')).toBe(false)
    expect(isWorkerVersionOutdated('1.4.0+build.1', '1.5.0')).toBe(false)
    expect(isWorkerVersionOutdated('1.4.0', '1.5.0-dev')).toBe(false)
    expect(isWorkerVersionOutdated('1.4.0', '1.5.0+build.1')).toBe(false)
  })

  it('returns false when either version is missing', () => {
    expect(isWorkerVersionOutdated(undefined, '1.5.0')).toBe(false)
    expect(isWorkerVersionOutdated('1.4.0', null)).toBe(false)
  })
})
