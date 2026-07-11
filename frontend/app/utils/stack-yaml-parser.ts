export interface ParsedStackYaml {
  name?: string
  timeout?: string
  removeOrphans?: boolean
  forcePull?: boolean
  waitRunning?: boolean
  workerTags?: string[]
  syncInterval?: string
}

export function parseStackYaml(yamlContent: string): ParsedStackYaml {
  const result: ParsedStackYaml = {
    workerTags: []
  }

  const lines = yamlContent.split(/\r?\n/)
  let section: 'compose' | 'jobs' | 'worker' | 'sync' | null = null
  let inWorkerTags = false

  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) {
      continue
    }

    const indent = line.length - line.trimStart().length

    if (indent === 0) {
      section = null
      inWorkerTags = false
    } else if (indent > 0 && section === 'worker' && trimmed !== 'tags:' && !trimmed.startsWith('-')) {
      inWorkerTags = false
    }

    if (indent === 0) {
      if (trimmed === 'compose:') {
        section = 'compose'
        continue
      }
      if (trimmed === 'jobs:') {
        section = 'jobs'
        continue
      }
      if (trimmed === 'worker:') {
        section = 'worker'
        continue
      }
      if (trimmed === 'sync:') {
        section = 'sync'
        continue
      }
    }

    if (section === 'worker' && trimmed === 'tags:') {
      inWorkerTags = true
      continue
    }

    if (inWorkerTags && trimmed.startsWith('-')) {
      const value = trimmed.replace(/^-\s*/, '').replace(/^["']|["']$/g, '').trim()
      if (value) {
        result.workerTags?.push(value)
      }
      continue
    }

    const colonIdx = trimmed.indexOf(':')
    if (colonIdx === -1) {
      continue
    }

    const key = trimmed.substring(0, colonIdx).trim()
    const rawValue = trimmed.substring(colonIdx + 1).trim()
    const cleanValue = rawValue.replace(/^["']|["']$/g, '').replace(/\\"/g, '"').trim()

    if (indent === 0) {
      if (key === 'name') result.name = cleanValue
      if (key === 'timeout') result.timeout = cleanValue
    } else if (section === 'compose') {
      if (key === 'remove_orphans') result.removeOrphans = cleanValue === 'true'
      if (key === 'force_pull') result.forcePull = cleanValue === 'true'
    } else if (section === 'jobs') {
      if (key === 'wait_running') result.waitRunning = cleanValue === 'true'
    } else if (section === 'sync') {
      if (key === 'interval') result.syncInterval = cleanValue
    }
  }

  return result
}
