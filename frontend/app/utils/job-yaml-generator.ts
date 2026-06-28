export interface JobYamlVolume {
  host: string
  container: string
}

export interface BuildJobYamlOptions {
  name: string
  description: string
  cron: string
  tags: string[]
  mode: string
  image: string
  command: string
  commandAsArray: boolean
  includeEmptyCommand?: boolean
  volumes: JobYamlVolume[]
  network: string
  cpu: string
  memory: string
  timeout: string
}

function yamlString(value: string) {
  return JSON.stringify(value)
}

function parseCommand(cmd: string): string[] {
  const args: string[] = []
  const regex = /[^\s"']+|"([^"]*)"|'([^']*)'/g
  let match
  while ((match = regex.exec(cmd)) !== null) {
    if (match[1] !== undefined) {
      args.push(match[1])
    } else if (match[2] !== undefined) {
      args.push(match[2])
    } else {
      let clean = match[0]
      while (clean.startsWith(',')) {
        clean = clean.substring(1)
      }
      while (clean.endsWith(',')) {
        clean = clean.substring(0, clean.length - 1)
      }
      if (clean) {
        args.push(clean)
      }
    }
  }
  return args
}

export function buildJobYaml(options: BuildJobYamlOptions) {
  const lines: string[] = []

  lines.push(`name: ${yamlString(options.name)}`)
  lines.push(`description: ${yamlString(options.description)}`)
  lines.push(`cron: ${yamlString(options.cron)}`)

  if (options.tags.length > 0) {
    lines.push('tags:')
    options.tags.forEach(tag => {
      lines.push(`  - ${yamlString(tag)}`)
    })
  } else {
    lines.push('tags: []')
  }

  lines.push(`mode: ${yamlString(options.mode)}`)
  lines.push(`image: ${yamlString(options.image)}`)

  const cmdTrimmed = options.command.trim()
  if (cmdTrimmed || options.includeEmptyCommand) {
    if (options.commandAsArray) {
      const parts = parseCommand(cmdTrimmed)
      lines.push(`command: [${parts.map(part => yamlString(part)).join(', ')}]`)
    } else {
      lines.push(`command: ${yamlString(cmdTrimmed)}`)
    }
  }

  const volumes = options.volumes
    .map(volume => ({ host: volume.host.trim(), container: volume.container.trim() }))
    .filter(volume => volume.host && volume.container)

  if (volumes.length > 0) {
    lines.push('volumes:')
    volumes.forEach(volume => {
      lines.push(`  - ${yamlString(`${volume.host}:${volume.container}`)}`)
    })
  } else {
    lines.push('volumes: []')
  }

  const network = options.network.trim()
  if (network) {
    lines.push(`network: ${yamlString(network)}`)
  }

  lines.push('resources:')
  lines.push(`  cpu: ${yamlString(options.cpu)}`)
  lines.push(`  memory: ${yamlString(options.memory)}`)
  lines.push(`  timeout: ${yamlString(options.timeout)}`)

  return lines.join('\n')
}
