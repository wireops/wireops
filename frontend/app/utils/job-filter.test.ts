import { describe, it, expect } from 'vitest'
import { filterJobs } from './job-filter'

const jobs = [
  { id: '1', name: 'nightly-backup', repository: { id: 'r1', name: 'infra' }, job_file: 'backup.yml', status: 'active' },
  { id: '2', definition: { name: 'cleanup-job' }, repository: { id: 'r2', name: 'tools' }, job_file: 'cleanup.yml', status: 'paused' },
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
})
