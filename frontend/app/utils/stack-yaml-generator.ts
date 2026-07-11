export interface BuildStackYamlOptions {
  name: string
  timeout: string
  removeOrphans: boolean
  forcePull: boolean
  waitRunning: boolean
  workerTags: string[]
  syncInterval: string
}

function yamlString(value: string) {
  return JSON.stringify(value)
}

export function buildStackYaml(options: BuildStackYamlOptions) {
  const lines: string[] = []

  lines.push('version: "wireops.v1"')
  lines.push(`name: ${yamlString(options.name)}`)

  const timeout = options.timeout.trim()
  if (timeout) {
    lines.push(`timeout: ${yamlString(timeout)}`)
  }

  lines.push('compose:')
  lines.push(`  remove_orphans: ${options.removeOrphans}`)
  lines.push(`  force_pull: ${options.forcePull}`)

  lines.push('jobs:')
  lines.push(`  wait_running: ${options.waitRunning}`)

  const workerTags = options.workerTags.map(tag => tag.trim()).filter(Boolean)
  if (workerTags.length > 0) {
    lines.push('worker:')
    lines.push('  tags:')
    workerTags.forEach(tag => {
      lines.push(`    - ${yamlString(tag)}`)
    })
  }

  const syncInterval = options.syncInterval.trim()
  if (syncInterval) {
    lines.push('sync:')
    lines.push(`  interval: ${yamlString(syncInterval)}`)
  }

  return lines.join('\n')
}
