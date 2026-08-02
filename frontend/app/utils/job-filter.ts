export const GROUP_ALL = 'all'
export const GROUP_UNGROUPED = '__ungrouped__'
const GROUP_PREFIX = 'group:'

// Named groups are namespaced with a prefix before being used as a <select>
// value, so a group literally named "all" or "__ungrouped__" can't collide
// with the sentinel filter values below it.
export function encodeGroupValue(name: string): string {
  return `${GROUP_PREFIX}${name}`
}

export function decodeGroupValue(value: string): string {
  return value.startsWith(GROUP_PREFIX) ? value.slice(GROUP_PREFIX.length) : value
}

export interface FilterableJob {
  name?: string
  definition?: { name?: string, group?: string }
  repository?: { id?: string, name?: string }
  job_file?: string
  status?: string
}

export function matchesJobSearch(job: FilterableJob, query: string): boolean {
  const q = query.trim().toLowerCase()
  return (
    (job.name || job.definition?.name || '').toLowerCase().includes(q) ||
    (job.repository?.name || '').toLowerCase().includes(q) ||
    (job.job_file || '').toLowerCase().includes(q)
  )
}

export function filterJobs<T extends FilterableJob>(
  jobs: T[],
  { searchQuery, statusFilter, repositoryFilter, groupFilter }: { searchQuery: string, statusFilter: string, repositoryFilter: string, groupFilter?: string }
): T[] {
  let filtered = jobs

  if (searchQuery.trim()) {
    filtered = filtered.filter(job => matchesJobSearch(job, searchQuery))
  }

  if (statusFilter !== 'all') {
    filtered = filtered.filter(job => job.status === statusFilter)
  }

  if (repositoryFilter !== 'all') {
    filtered = filtered.filter(job => job.repository?.id === repositoryFilter)
  }

  if (groupFilter && groupFilter !== GROUP_ALL) {
    filtered = groupFilter === GROUP_UNGROUPED
      ? filtered.filter(job => !job.definition?.group)
      : filtered.filter(job => job.definition?.group === decodeGroupValue(groupFilter))
  }

  return filtered
}
