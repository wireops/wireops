export interface FilterableJob {
  name?: string
  definition?: { name?: string }
  repository?: { id?: string, name?: string }
  job_file?: string
  status?: string
}

export function matchesJobSearch(job: FilterableJob, query: string): boolean {
  const q = query.toLowerCase()
  return (
    (job.name || job.definition?.name || '').toLowerCase().includes(q) ||
    (job.repository?.name || '').toLowerCase().includes(q) ||
    (job.job_file || '').toLowerCase().includes(q)
  )
}

export function filterJobs<T extends FilterableJob>(
  jobs: T[],
  { searchQuery, statusFilter, repositoryFilter }: { searchQuery: string, statusFilter: string, repositoryFilter: string }
): T[] {
  let filtered = jobs

  if (searchQuery) {
    filtered = filtered.filter(job => matchesJobSearch(job, searchQuery))
  }

  if (statusFilter !== 'all') {
    filtered = filtered.filter(job => job.status === statusFilter)
  }

  if (repositoryFilter !== 'all') {
    filtered = filtered.filter(job => job.repository?.id === repositoryFilter)
  }

  return filtered
}
