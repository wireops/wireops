export interface ParsedJobYaml {
  name?: string
  description?: string
  cron?: string
  mode?: string
  image?: string
  command?: string
  commandAsArray?: boolean
  tags?: string[]
  volumes?: { host: string; container: string }[]
  network?: string
  cpu?: string
  memory?: string
  timeout?: string
}

export function parseJobYaml(yamlContent: string): ParsedJobYaml {
  const result: ParsedJobYaml = {
    tags: [],
    volumes: []
  }

  const lines = yamlContent.split(/\r?\n/)
  let currentArraySection: 'tags' | 'volumes' | null = null
  let inResourcesSection = false

  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) {
      continue
    }

    // Detect indentation
    const indent = line.length - line.trimStart().length

    // If we were in resources, but the indent goes back to 0, we exited resources
    if (inResourcesSection && indent === 0) {
      inResourcesSection = false
    }

    // If we were in tags or volumes, but the line doesn't start with '-' and indent is 0, we exited that section
    if (currentArraySection && indent === 0 && !trimmed.startsWith('-')) {
      currentArraySection = null
    }

    // Section headers
    if (trimmed === 'tags:') {
      currentArraySection = 'tags'
      continue
    }
    if (trimmed === 'volumes:') {
      currentArraySection = 'volumes'
      continue
    }
    if (trimmed === 'resources:') {
      inResourcesSection = true
      currentArraySection = null
      continue
    }

    // Parse array items (e.g., - "value")
    if (trimmed.startsWith('-') && currentArraySection) {
      const value = trimmed.replace(/^-\s*/, '').replace(/^["']|["']$/g, '').trim()
      if (value) {
        if (currentArraySection === 'tags') {
          result.tags?.push(value)
        } else if (currentArraySection === 'volumes') {
          // Volumes can be "host:container"
          const splitIdx = value.indexOf(':')
          if (splitIdx !== -1) {
            const host = value.substring(0, splitIdx).trim()
            const container = value.substring(splitIdx + 1).trim()
            result.volumes?.push({ host, container })
          } else {
            result.volumes?.push({ host: value, container: '' })
          }
        }
      }
      continue
    }

    // Parse key-value lines
    const colonIdx = trimmed.indexOf(':')
    if (colonIdx === -1) {
      continue
    }

    const key = trimmed.substring(0, colonIdx).trim()
    const rawValue = trimmed.substring(colonIdx + 1).trim()
    const cleanValue = rawValue.replace(/^["']|["']$/g, '').replace(/\\"/g, '"').trim()

    if (inResourcesSection && indent > 0) {
      if (key === 'cpu') result.cpu = cleanValue
      if (key === 'memory') result.memory = cleanValue
      if (key === 'timeout') result.timeout = cleanValue
    } else if (indent === 0) {
      if (key === 'name') result.name = cleanValue
      if (key === 'description') result.description = cleanValue
      if (key === 'cron') result.cron = cleanValue
      if (key === 'mode') result.mode = cleanValue
      if (key === 'image') result.image = cleanValue
      if (key === 'network') result.network = cleanValue

      if (key === 'command') {
        // Command can be a string ("echo hello") or array ([echo, hello])
        if (cleanValue.startsWith('[') && cleanValue.endsWith(']')) {
          result.commandAsArray = true
          try {
            let jsonStr = cleanValue
            if (jsonStr.includes("'")) {
              jsonStr = jsonStr.replace(/'/g, '"')
            }
            const parts = JSON.parse(jsonStr)
            if (Array.isArray(parts)) {
              result.command = parts.map(p => {
                const str = String(p)
                if (str.includes(' ')) {
                  return `"${str}"`
                }
                return str
              }).join(' ')
            } else {
              result.command = cleanValue
            }
          } catch (e) {
            const inner = cleanValue.substring(1, cleanValue.length - 1)
            const parts = inner.split(',').map(p => p.trim().replace(/^["']|["']$/g, ''))
            result.command = parts.join(' ')
          }
        } else {
          result.commandAsArray = false
          result.command = cleanValue
        }
      }

      // Handle inline empty arrays like tags: [] or volumes: []
      if (key === 'tags' && cleanValue === '[]') {
        result.tags = []
      }
      if (key === 'volumes' && cleanValue === '[]') {
        result.volumes = []
      }
    }
  }

  return result
}
