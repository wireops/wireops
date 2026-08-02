import { describe, it, expect } from 'vitest'
import { filterJobs, encodeGroupValue, decodeGroupValue, GROUP_ALL, GROUP_UNGROUPED } from './job-filter'

const jobs = [
  { id: '1', name: 'nightly-backup', repository: { id: 'r1', name: 'infra' }, job_file: 'backup.yml', status: 'active', definition: { group: 'data' } },
  { id: '2', definition: { name: 'cleanup-job', group: 'edge' }, repository: { id: 'r2', name: 'tools' }, job_file: 'cleanup.yml', status: 'paused' },
  { id: '3', name: 'deploy-check', repository: { id: 'r1', name: 'infra' }, job_file: 'check.yml', status: 'error' },
]

describe('filterJobs', () => {
  it('matches on name, repository name, and job_file text search', () => {
    expect(filterJobs(jobs, { searchQuery: 'backup', statusFilter: 'all', repositoryFilter: 'all' }).map(j => j.id)).toEqual(['1'])
    expect(filterJobs(jobs, { searchQuery: 'infra', statusFilter: 'all', repositoryFilter: 'all' }).map(j => j.id)).toEqual(['1', '3'])
    expect(filterJobs(jobs, { searchQuery: 'check.yml', statusFilter: 'all', repositoryFilter: 'all' }).map(j => j.id)).toEqual(['3'])
  })

  it('falls back to definition.name when name is absent', () => {
    expect(filterJobs(jobs, { searchQuery: 'cleanup-job', statusFilter: 'all', repositoryFilter: 'all' }).map(j => j.id)).toEqual(['2'])
  })

  it('combines text search with status and repository filters', () => {
    const result = filterJobs(jobs, { searchQuery: 'infra', statusFilter: 'error', repositoryFilter: 'r1' })
    expect(result.map(j => j.id)).toEqual(['3'])
  })

  it('filters by status alone', () => {
    expect(filterJobs(jobs, { searchQuery: '', statusFilter: 'paused', repositoryFilter: 'all' }).map(j => j.id)).toEqual(['2'])
  })

  it('filters by repository alone', () => {
    expect(filterJobs(jobs, { searchQuery: '', statusFilter: 'all', repositoryFilter: 'r2' }).map(j => j.id)).toEqual(['2'])
  })

  it('returns an empty array when nothing matches', () => {
    expect(filterJobs(jobs, { searchQuery: 'nonexistent', statusFilter: 'all', repositoryFilter: 'all' })).toEqual([])
    expect(filterJobs(jobs, { searchQuery: '', statusFilter: 'stalled', repositoryFilter: 'all' })).toEqual([])
  })

  it('trims leading/trailing whitespace and treats whitespace-only queries as empty', () => {
    expect(filterJobs(jobs, { searchQuery: '  backup  ', statusFilter: 'all', repositoryFilter: 'all' }).map(j => j.id)).toEqual(['1'])
    expect(filterJobs(jobs, { searchQuery: '   ', statusFilter: 'all', repositoryFilter: 'all' }).map(j => j.id)).toEqual(['1', '2', '3'])
  })

  it('filters by group alone', () => {
    expect(filterJobs(jobs, { searchQuery: '', statusFilter: 'all', repositoryFilter: 'all', groupFilter: 'data' }).map(j => j.id)).toEqual(['1'])
  })

  it('filters ungrouped jobs with the __ungrouped__ sentinel', () => {
    expect(filterJobs(jobs, { searchQuery: '', statusFilter: 'all', repositoryFilter: 'all', groupFilter: '__ungrouped__' }).map(j => j.id)).toEqual(['3'])
  })

  it('leaves results untouched when groupFilter is omitted or "all"', () => {
    expect(filterJobs(jobs, { searchQuery: '', statusFilter: 'all', repositoryFilter: 'all' }).map(j => j.id)).toEqual(['1', '2', '3'])
    expect(filterJobs(jobs, { searchQuery: '', statusFilter: 'all', repositoryFilter: 'all', groupFilter: 'all' }).map(j => j.id)).toEqual(['1', '2', '3'])
  })
})

describe('encodeGroupValue / decodeGroupValue', () => {
  it('round-trips a named group', () => {
    expect(decodeGroupValue(encodeGroupValue('data'))).toBe('data')
  })

  it('namespaces a group literally named "all" or "__ungrouped__" so it cannot collide with the sentinels', () => {
    expect(encodeGroupValue(GROUP_ALL)).not.toBe(GROUP_ALL)
    expect(encodeGroupValue(GROUP_UNGROUPED)).not.toBe(GROUP_UNGROUPED)
    expect(decodeGroupValue(encodeGroupValue(GROUP_ALL))).toBe(GROUP_ALL)
    expect(decodeGroupValue(encodeGroupValue(GROUP_UNGROUPED))).toBe(GROUP_UNGROUPED)
  })

  it('leaves sentinel values unchanged when decoded directly', () => {
    expect(decodeGroupValue(GROUP_ALL)).toBe(GROUP_ALL)
    expect(decodeGroupValue(GROUP_UNGROUPED)).toBe(GROUP_UNGROUPED)
  })

  it('filters correctly for a job whose real group collides with a sentinel name', () => {
    const collidingJobs = [
      { id: '1', definition: { group: 'all' } },
      { id: '2', definition: { group: '__ungrouped__' } },
      { id: '3', definition: {} },
    ]
    // filterJobs must receive the *encoded* value for a named group -- an
    // undecoded "all" would be swallowed by the GROUP_ALL early-exit and
    // show everything instead of just jobs in the group named "all".
    expect(filterJobs(collidingJobs, { searchQuery: '', statusFilter: 'all', repositoryFilter: 'all', groupFilter: encodeGroupValue('all') }).map(j => j.id)).toEqual(['1'])
    expect(filterJobs(collidingJobs, { searchQuery: '', statusFilter: 'all', repositoryFilter: 'all', groupFilter: encodeGroupValue('__ungrouped__') }).map(j => j.id)).toEqual(['2'])
  })
})
