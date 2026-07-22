import { describe, it, expect } from 'vitest'
import { filterVisibleWorkers, isWorkerClickable, matchesWorkerSearch, workerStatus } from './worker'

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
